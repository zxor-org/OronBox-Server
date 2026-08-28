package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	githuboauth "github.com/zxor-org/OronBox-Server/internal/oauth/github"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

const (
	ProviderBandBBS = "bandbbs"
	ProviderGitHub  = "github"
)

type Dependencies struct {
	Config     config.Config
	Store      *store.Store
	BandBBS    *bandbbs.Client
	GitHub     *githuboauth.Client
	StartedAt  time.Time
	Blobs      blob.Store
	Creator    *creator.Service
	R2         *blob.R2
	Moderation *moderation.Service
}

type App struct {
	cfg             config.Config
	store           *store.Store
	bandbbs         *bandbbs.Client
	github          *githuboauth.Client
	startedAt       time.Time
	secrets         *authcore.Secrets
	blobs           blob.Store
	creator         *creator.Service
	r2              *blob.R2
	moderation      *moderation.Service
	templates       *web.Templates
	trustedProxies  []*net.IPNet
	downloadLimiter *ipRateLimiter
	authAttempts    *ipRateLimiter
	authFailures    *ipRateLimiter
}

func New(deps Dependencies) *App {
	secrets, err := authcore.NewSecrets(deps.Config.EncryptionKey)
	if err != nil {
		panic(err)
	}
	app := &App{
		cfg:             deps.Config,
		store:           deps.Store,
		bandbbs:         deps.BandBBS,
		github:          deps.GitHub,
		startedAt:       deps.StartedAt,
		secrets:         secrets,
		blobs:           deps.Blobs,
		creator:         deps.Creator,
		r2:              deps.R2,
		moderation:      deps.Moderation,
		templates:       web.NewTemplates(),
		downloadLimiter: newIPRateLimiter(deps.Config.Limits.DownloadRatePerMin),
		authAttempts:    newWindowLimiter(deps.Config.Limits.AuthRatePerMin, time.Minute, 120),
		authFailures:    newWindowLimiter(deps.Config.Limits.AuthFailureBurst, deps.Config.Limits.AuthFailureWindow, 20),
	}
	for _, value := range deps.Config.TrustedProxyCIDRs {
		if _, network, parseErr := net.ParseCIDR(value); parseErr == nil {
			app.trustedProxies = append(app.trustedProxies, network)
		}
	}
	return app
}

// render writes a page. The request is required because every rendered POST
// form carries a session-bound CSRF token, which can only be derived from the
// admin session attached to the request context.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	if values, ok := data.(map[string]any); ok {
		if _, exists := values["PageSizes"]; !exists {
			values["PageSizes"] = []int{25, 50, 100}
		}
		if _, exists := values["CSRFToken"]; !exists {
			values["CSRFToken"] = a.adminCSRFTokenFor(r)
		}
		// The drawer is derived from the session rather than passed in by each
		// handler, so no page can accidentally offer a reviewer a link that
		// only answers with a 403.
		role, _ := r.Context().Value(adminRoleContextKey{}).(string)
		values["Role"] = role
		values["Nav"] = web.NavigationFor(role)
		values["HomePath"] = web.HomePathFor(role)
	}
	if err := a.templates.Render(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public OAuth endpoints
	mux.HandleFunc("GET /", a.handleRoot)
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /open", a.handleOpen)
	mux.HandleFunc("GET /oauth2/bandbbs/start", a.handleBandBBSStart)
	mux.Handle("POST /api/oauth/bandbbs/publish/start", a.requireUser(http.HandlerFunc(a.handleBandBBSPublishStart)))
	mux.HandleFunc("GET /oauth2/bandbbs/callback", a.handleBandBBSCallback)
	mux.HandleFunc("GET /oauth2/github/callback", a.handleGitHubWebCallback)
	mux.HandleFunc("GET /auth/success", a.handleAuthSuccess)
	mux.HandleFunc("GET /auth/failed", a.handleAuthFailed)
	mux.HandleFunc("POST /api/oauth/bandbbs/exchange", a.throttleCredentials(a.handleTicketExchange))
	mux.HandleFunc("POST /api/oauth/bandbbs/refresh", a.throttleCredentials(a.handleRefresh))
	mux.Handle("POST /api/oauth/bandbbs/token/refresh", a.requireUser(http.HandlerFunc(a.handleBandBBSTokenRefresh)))
	mux.Handle("GET /api/me", a.requireUser(http.HandlerFunc(a.handleMe)))
	mux.Handle("GET /api/me/grants", a.requireUser(http.HandlerFunc(a.handleGrants)))
	mux.Handle("GET /api/coins", a.requireUser(http.HandlerFunc(a.handleCoinAccount)))
	mux.Handle("POST /api/coins/checkin", a.requireUser(http.HandlerFunc(a.handleCoinCheckin)))
	mux.Handle("POST /api/session/revoke", a.requireUser(http.HandlerFunc(a.handleSessionRevoke)))
	mux.HandleFunc("GET /api/resources", a.handleListResources)
	mux.HandleFunc("GET /api/home", a.handleHome)
	mux.HandleFunc("GET /api/blog", a.handleBlogList)
	mux.HandleFunc("GET /api/blog/{slug}", a.handleBlogPost)
	mux.HandleFunc("GET /api/plugins", a.handleListPlugins)
	mux.Handle("POST /api/plugins", a.requireUser(http.HandlerFunc(a.handleUploadPlugin)))
	mux.HandleFunc("GET /api/plugins/{id}/package", a.handlePluginPackage)
	mux.HandleFunc("GET /api/plugins/{id}/icon", a.handlePluginIcon)
	mux.Handle("DELETE /api/plugins/{id}", a.requireUser(http.HandlerFunc(a.handleDeletePlugin)))
	mux.HandleFunc("GET /api/resource-attributes", a.handleResourceAttributes)
	mux.HandleFunc("GET /api/resources/{id}", a.handleResource)
	mux.HandleFunc("GET /api/collections", a.handleCollections)
	mux.HandleFunc("GET /api/collections/{id}", a.handleCollection)
	mux.Handle("POST /api/resources/{id}/coins", a.requireUser(http.HandlerFunc(a.handleResourceCoin)))
	mux.Handle("GET /api/resources/{id}/coins", a.requireUser(http.HandlerFunc(a.handleResourceCoinStatus)))
	mux.HandleFunc("GET /api/resources/{id}/comments", a.handleListComments)
	mux.Handle("POST /api/resources/{id}/comments", a.requireUser(http.HandlerFunc(a.handleCreateComment)))
	mux.Handle("DELETE /api/comments/{id}", a.requireUser(http.HandlerFunc(a.handleDeleteComment)))
	mux.Handle("GET /api/messages", a.requireUser(http.HandlerFunc(a.handleMessages)))
	mux.Handle("DELETE /api/messages", a.requireUser(http.HandlerFunc(a.handleClearMessages)))
	mux.Handle("POST /api/messages/{id}/read", a.requireUser(http.HandlerFunc(a.handleReadMessage)))
	mux.Handle("GET /api/announcements/unread", a.requireUser(http.HandlerFunc(a.handleAnnouncements)))
	mux.Handle("POST /api/announcements/read", a.requireUser(http.HandlerFunc(a.handleReadAnnouncements)))
	mux.HandleFunc("GET /api/devices", a.handleDevices)
	mux.HandleFunc("GET /api/meta/legal/{document}", a.handleLegalDocument)
	mux.HandleFunc("GET /api/app/releases", a.handleAppRelease)
	mux.HandleFunc("GET /api/blobs/{sha256}", a.handleBlob)
	mux.Handle("POST /api/oauth/github/device/start", a.requireUser(http.HandlerFunc(a.handleGitHubDeviceStart)))
	mux.Handle("POST /api/oauth/github/device/poll", a.requireUser(http.HandlerFunc(a.handleGitHubDevicePoll)))
	mux.Handle("POST /api/oauth/github/web/start", a.requireUser(http.HandlerFunc(a.handleGitHubWebStart)))
	mux.Handle("POST /api/oauth/github/web/status", a.requireUser(http.HandlerFunc(a.handleGitHubWebStatus)))
	mux.Handle("DELETE /api/oauth/github/grant", a.requireUser(http.HandlerFunc(a.handleGitHubGrantDelete)))
	mux.Handle("POST /api/feedback", a.requireUser(http.HandlerFunc(a.handleCreateFeedback)))
	mux.Handle("GET /api/feedback", a.requireUser(http.HandlerFunc(a.handleFeedbackList)))
	mux.Handle("GET /api/feedback/{ticket}", a.requireUser(http.HandlerFunc(a.handleFeedback)))
	mux.Handle("POST /api/feedback/{ticket}/replies", a.requireUser(http.HandlerFunc(a.handleFeedbackReply)))
	mux.Handle("GET /api/creator/resources", a.requireCreator(a.handleCreatorList))
	mux.Handle("GET /api/creator/coins/stats", a.requireCreator(a.handleCreatorCoinStats))
	mux.Handle("POST /api/creator/resources", a.requireCreator(a.handleCreatorCreate))
	mux.Handle("GET /api/creator/resources/{resource}", a.requireCreator(a.handleCreatorWorkspace))
	mux.Handle("POST /api/creator/resources/{resource}/publish", a.requireCreator(a.handleCreatorPublish))
	mux.Handle("PUT /api/creator/resources/{resource}/draft", a.requireCreator(a.handleCreatorDraft))
	mux.Handle("GET /api/creator/resources/{resource}/blobs/{sha256}", a.requireCreator(a.handleCreatorBlob))
	mux.Handle("POST /api/creator/resources/{resource}/takedown", a.requireCreator(a.handleCreatorTakedown))
	mux.Handle("POST /api/creator/resources/{resource}/restore", a.requireCreator(a.handleCreatorRestore))
	mux.Handle("DELETE /api/creator/resources/{resource}", a.requireCreator(a.handleCreatorDelete))
	mux.Handle("GET /api/creator/collections", a.requireCreator(a.handleCreatorCollectionList))
	mux.Handle("POST /api/creator/collections", a.requireCreator(a.handleCreatorCollectionCreate))
	mux.Handle("GET /api/creator/collections/{collection}", a.requireCreator(a.handleCreatorCollection))
	mux.Handle("PATCH /api/creator/collections/{collection}", a.requireCreator(a.handleCreatorCollectionUpdate))
	mux.Handle("PUT /api/creator/collections/{collection}/resources", a.requireCreator(a.handleCreatorCollectionResources))
	mux.Handle("DELETE /api/creator/collections/{collection}", a.requireCreator(a.handleCreatorCollectionDelete))
	mux.Handle("GET /api/creator/resources/{resource}/relationships", a.requireCreator(a.handleCreatorRelationships))
	mux.Handle("POST /api/creator/resources/{resource}/collaborators", a.requireCreator(a.handleCreatorCollaboratorInvite))
	mux.Handle("DELETE /api/creator/resources/{resource}/collaborators/{user}", a.requireUser(http.HandlerFunc(a.handleCollaboratorRemove)))
	mux.Handle("PUT /api/creator/resources/{resource}/source", a.requireCreator(a.handleCreatorSource))
	mux.Handle("POST /api/resources/{resource}/collaboration/accept", a.requireUser(http.HandlerFunc(a.handleCollaboratorAccept)))
	mux.Handle("DELETE /api/resources/{resource}/collaboration", a.requireUser(http.HandlerFunc(a.handleCollaboratorDecline)))
	mux.Handle("GET /api/collaborations", a.requireUser(http.HandlerFunc(a.handleCollaborationInvitations)))

	// Self-hosted console assets. Serving the scripts here instead of inlining
	// them is what lets the CSP forbid inline script entirely.
	staticAsset := func(contentType, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("GET /assets/app.css", staticAsset("text/css; charset=utf-8", web.CSS))
	mux.HandleFunc("GET /assets/theme.js", staticAsset("text/javascript; charset=utf-8", web.ThemeJS))
	mux.HandleFunc("GET /assets/transition.js", staticAsset("text/javascript; charset=utf-8", web.TransitionJS))

	// Admin console
	mux.HandleFunc("GET /admin/login", a.handleAdminLoginPage)
	mux.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusMovedPermanently)
	})
	mux.HandleFunc("POST /admin/login", a.handleAdminLogin)
	mux.HandleFunc("POST /admin/logout", a.requireAdmin(a.handleAdminLogout))
	mux.HandleFunc("GET /admin/api/session", a.requireAdmin(a.handleAdminAPISession))
	mux.HandleFunc("GET /admin/api/reviews", a.requireAdmin(a.handleAdminAPIReviews))
	mux.HandleFunc("GET /admin/api/reviews/{review}", a.requireAdmin(a.handleAdminAPIReview))
	mux.HandleFunc("POST /admin/api/reviews/{review}/decision", a.requireAdmin(a.handleAdminAPIReviewDecision))
	mux.HandleFunc("GET /admin/api/resources", a.requireAdmin(a.handleAdminAPIResources))
	mux.HandleFunc("GET /admin/api/comments", a.requireAdmin(a.handleAdminAPIComments))
	mux.HandleFunc("GET /admin/assets/{path...}", a.requireAdmin(a.serveAdminSPA))
	mux.HandleFunc("GET /admin", a.requireAdmin(a.handleAdminDashboard))
	mux.HandleFunc("GET /admin/api/users", a.requireAdminRole("admin", a.handleAdminAPIUsers))
	mux.HandleFunc("GET /admin/api/collections", a.requireAdmin(a.handleAdminAPICollections))
	mux.HandleFunc("GET /admin/api/publications", a.requireAdmin(a.handleAdminAPIPublications))
	mux.HandleFunc("GET /admin/api/devices", a.requireAdmin(a.handleAdminAPIDevices))
	mux.HandleFunc("GET /admin/api/plugins", a.requireAdmin(a.handleAdminAPIPlugins))
	mux.HandleFunc("POST /admin/api/plugins/{plugin}", a.requireAdmin(a.handleAdminAPIPluginReview))
	mux.HandleFunc("GET /admin/api/tickets", a.requireAdmin(a.handleAdminAPITickets))
	mux.HandleFunc("POST /admin/api/tickets/{ticket}", a.requireAdmin(a.handleAdminAPITicketAction))
	mux.HandleFunc("GET /admin/api/messages", a.requireAdmin(a.handleAdminAPIMessages))
	mux.HandleFunc("GET /admin/api/announcements", a.requireAdminRole("admin", a.handleAdminAPIAnnouncements))
	mux.HandleFunc("GET /admin/api/releases", a.requireAdmin(a.handleAdminAPIReleases))
	mux.HandleFunc("GET /admin/api/releases/{release}", a.requireAdmin(a.handleAdminAPIRelease))
	mux.HandleFunc("GET /admin/api/blog", a.requireAdminRole("admin", a.handleAdminAPIBlog))
	mux.HandleFunc("GET /admin/api/audit", a.requireAdmin(a.handleAdminAPIAudit))
	mux.HandleFunc("GET /admin/api/health", a.requireAdmin(a.handleAdminAPIHealth))
	mux.HandleFunc("GET /admin/api/settings", a.requireAdminRole("admin", a.handleAdminAPISettings))
	mux.HandleFunc("GET /admin/api/resources/{resource}", a.requireAdmin(a.handleAdminAPIResource))
	mux.HandleFunc("POST /admin/api/resources/{resource}/draft", a.requireAdmin(a.handleAdminAPIResourceDraft))
	mux.HandleFunc("POST /admin/api/resources/{resource}/draft/{revision}/submit", a.requireAdmin(a.handleAdminAPIResourceSubmit))
	mux.HandleFunc("POST /admin/api/comments/{comment}", a.requireAdmin(a.handleAdminAPICommentDecision))
	mux.HandleFunc("POST /admin/api/users/{user}/state", a.requireAdminRole("admin", a.handleAdminAPIUserState))
	mux.HandleFunc("POST /admin/api/publications/{publication}", a.requireAdminRole("admin", a.handleAdminAPIPublicationAction))
	mux.HandleFunc("GET /admin/api/coins/ledger", a.requireAdminRole("admin", a.handleAdminAPICoinLedger))
	// The legacy console is gone; the SPA still needs these three transport
	// routes: media/artifact uploads are multipart POSTs, and cleanup preview
	// runs the HMAC preview the JSON execute endpoint validates against.
	mux.HandleFunc("POST /admin/resources/{resource}/draft/{revision}/media", a.requireAdmin(a.handleAdminRevisionMediaUpload))
	mux.HandleFunc("POST /admin/resources/{resource}/draft/{revision}/artifacts", a.requireAdmin(a.handleAdminRevisionArtifactUpload))
	mux.HandleFunc("GET /admin/blobs/{sha256}", a.requireAdmin(a.handleAdminBlob))
	mux.HandleFunc("GET /admin/audit.csv", a.requireAdmin(a.handleAdminAuditCSV))
	mux.HandleFunc("POST /admin/cleanup/preview", a.requireAdminRole("admin", a.handleAdminCleanupPreview))
	mux.HandleFunc("GET /admin/api/coins", a.requireAdminRole("admin", a.handleAdminCoinOverview))
	mux.HandleFunc("POST /admin/api/coins/users/{user}", a.requireAdminRole("admin", a.handleAdminCoinUser))
	mux.HandleFunc("POST /admin/api/coins/invalidate", a.requireAdminRole("admin", a.handleAdminCoinInvalidate))
	mux.HandleFunc("GET /admin/api/collections/review", a.requireAdmin(a.handleAdminCollectionReviewQueue))
	mux.HandleFunc("POST /admin/api/collections/review/{revision}", a.requireAdmin(a.handleAdminCollectionReviewDecision))
	mux.HandleFunc("GET /admin/api/overview", a.requireAdmin(a.handleAdminAPIOverview))
	mux.HandleFunc("GET /admin/api/analytics", a.requireAdmin(a.handleAdminAPIAnalytics))
	mux.HandleFunc("GET /admin/api/catalog", a.requireAdmin(a.handleAdminAPICatalog))
	mux.HandleFunc("POST /admin/api/reviews/bulk", a.requireAdmin(a.handleAdminAPIReviewBulk))
	mux.HandleFunc("POST /admin/api/reviews/{review}/checklist", a.requireAdmin(a.handleAdminAPIReviewChecklist))
	mux.HandleFunc("POST /admin/api/resources/{resource}/state", a.requireAdminRole("admin", a.handleAdminAPIResourceState))
	mux.HandleFunc("POST /admin/api/resources/{resource}/draft/{revision}/discard", a.requireAdmin(a.handleAdminAPIResourceDiscard))
	mux.HandleFunc("POST /admin/api/resources/{resource}/revisions/{revision}/rollback", a.requireAdmin(a.handleAdminAPIResourceRollback))
	mux.HandleFunc("POST /admin/api/resources/{resource}/draft/{revision}/governance", a.requireAdmin(a.handleAdminAPIResourceGovernance))
	mux.HandleFunc("GET /admin/api/home", a.requireAdmin(a.handleAdminAPIHome))
	mux.HandleFunc("POST /admin/api/home/banners", a.requireAdminRole("admin", a.handleAdminAPIHomeBanner))
	mux.HandleFunc("POST /admin/api/home/banners/{banner}", a.requireAdminRole("admin", a.handleAdminAPIHomeBanner))
	mux.HandleFunc("POST /admin/api/home/banners/{banner}/delete", a.requireAdminRole("admin", a.handleAdminAPIHomeBannerDelete))
	mux.HandleFunc("POST /admin/api/home/sections", a.requireAdminRole("admin", a.handleAdminAPIHomeSection))
	mux.HandleFunc("POST /admin/api/home/sections/{section}/delete", a.requireAdminRole("admin", a.handleAdminAPIHomeSectionDelete))
	mux.HandleFunc("POST /admin/api/home/cards", a.requireAdminRole("admin", a.handleAdminAPIHomeCard))
	mux.HandleFunc("POST /admin/api/home/cards/{card}/delete", a.requireAdminRole("admin", a.handleAdminAPIHomeCardDelete))
	mux.HandleFunc("POST /admin/api/home/{kind}/{id}/move", a.requireAdminRole("admin", a.handleAdminAPIHomeMove))
	mux.HandleFunc("POST /admin/api/comments/bulk", a.requireAdmin(a.handleAdminAPICommentBulk))
	mux.HandleFunc("POST /admin/api/moderation/prompt", a.requireAdminRole("admin", a.handleAdminAPIModerationPrompt))
	mux.HandleFunc("GET /admin/api/users/{user}", a.requireAdminRole("admin", a.handleAdminAPIUserDetail))
	mux.HandleFunc("POST /admin/api/users/{user}/sessions", a.requireAdminRole("admin", a.handleAdminAPIUserSessions))
	mux.HandleFunc("POST /admin/api/messages", a.requireAdminRole("admin", a.handleAdminAPIMessageSend))
	mux.HandleFunc("POST /admin/api/announcements", a.requireAdminRole("admin", a.handleAdminAPIAnnouncementCreate))
	mux.HandleFunc("POST /admin/api/announcements/{announcement}/delete", a.requireAdminRole("admin", a.handleAdminAPIAnnouncementDelete))
	mux.HandleFunc("GET /admin/api/blog/{slug}", a.requireAdminRole("admin", a.handleAdminAPIBlogGet))
	mux.HandleFunc("POST /admin/api/blog", a.requireAdminRole("admin", a.handleAdminAPIBlogSave))
	mux.HandleFunc("POST /admin/api/blog/{slug}", a.requireAdminRole("admin", a.handleAdminAPIBlogSave))
	mux.HandleFunc("POST /admin/api/blog/{slug}/delete", a.requireAdminRole("admin", a.handleAdminAPIBlogDelete))
	mux.HandleFunc("POST /admin/api/releases", a.requireAdminRole("admin", a.handleAdminAPIReleasePublish))
	mux.HandleFunc("POST /admin/api/releases/{release}/notes", a.requireAdminRole("admin", a.handleAdminAPIReleaseNotes))
	mux.HandleFunc("POST /admin/api/releases/{release}/state", a.requireAdminRole("admin", a.handleAdminAPIReleaseStateJSON))
	mux.HandleFunc("POST /admin/api/devices", a.requireAdminRole("admin", a.handleAdminAPIDeviceSaveJSON))
	mux.HandleFunc("POST /admin/api/devices/{device}", a.requireAdminRole("admin", a.handleAdminAPIDeviceSaveJSON))
	mux.HandleFunc("GET /admin/api/blobs", a.requireAdmin(a.handleAdminAPIBlobs))
	mux.HandleFunc("GET /admin/api/blobs/{sha256}", a.requireAdmin(a.handleAdminAPIBlob))
	mux.HandleFunc("POST /admin/api/blobs/{sha256}/requeue", a.requireAdminRole("admin", a.handleAdminAPIBlobRequeue))
	mux.HandleFunc("GET /admin/api/oauth/events", a.requireAdmin(a.handleAdminAPIOAuthEvents))
	mux.HandleFunc("GET /admin/api/oauth/events/{id}", a.requireAdmin(a.handleAdminAPIOAuthEvent))
	mux.HandleFunc("GET /admin/api/oauth/states", a.requireAdmin(a.handleAdminAPIOAuthStates))
	mux.HandleFunc("GET /admin/api/oauth/states/{id}", a.requireAdmin(a.handleAdminAPIOAuthState))
	mux.HandleFunc("GET /admin/api/oauth/tickets", a.requireAdmin(a.handleAdminAPIOAuthTickets))
	mux.HandleFunc("GET /admin/api/oauth/tickets/{id}", a.requireAdmin(a.handleAdminAPIOAuthTicket))
	mux.HandleFunc("GET /admin/api/clients", a.requireAdmin(a.handleAdminAPIClients))
	mux.HandleFunc("POST /admin/api/cleanup", a.requireAdminRole("admin", a.handleAdminAPICleanupJSON))
	mux.HandleFunc("POST /admin/api/attributes", a.requireAdminRole("admin", a.handleAdminAPIAttribute))
	mux.HandleFunc("POST /admin/api/attributes/{attribute}/delete", a.requireAdminRole("admin", a.handleAdminAPIAttributeDelete))
	mux.HandleFunc("GET /admin/api/plugins/{plugin}", a.requireAdmin(a.handleAdminAPIPluginGet))
	mux.HandleFunc("POST /admin/api/plugins/{plugin}/metadata", a.requireAdmin(a.handleAdminAPIPluginMetadataJSON))
	mux.HandleFunc("POST /admin/api/plugins/{plugin}/state", a.requireAdminRole("admin", a.handleAdminAPIPluginStateJSON))
	mux.HandleFunc("POST /admin/api/publications/retry-failed", a.requireAdminRole("admin", a.handleAdminAPIPublicationsRetry))
	mux.HandleFunc("GET /admin/api/collections/{collection}", a.requireAdmin(a.handleAdminAPICollectionGet))
	mux.HandleFunc("POST /admin/api/collections/{collection}", a.requireAdmin(a.handleAdminAPICollectionSave))
	mux.HandleFunc("GET /admin/api/tickets/{ticket}", a.requireAdmin(a.handleAdminAPITicketDetail))
	mux.HandleFunc("GET /admin/api/audit/{id}", a.requireAdmin(a.handleAdminAPIAuditDetail))
	mux.HandleFunc("GET /admin/{path...}", a.requireAdmin(a.serveAdminSPA))

	return mux
}

func CORS(origins []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[strings.TrimRight(origin, "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
		if origin != "" && (allowed[origin] || allowed["*"]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-OronBox-App-Id, X-OronBox-Version, X-OronBox-Build, X-OronBox-Platform")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			if origin == "" || (!allowed[origin] && !allowed["*"]) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "server_home", map[string]any{"Title": "Server"})
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	statusCode := http.StatusOK
	if err := a.store.Ping(r.Context()); err != nil {
		status = "database_error"
		statusCode = http.StatusServiceUnavailable
	}
	writeJSON(w, statusCode, map[string]any{
		"status":     status,
		"version":    a.cfg.Version,
		"commit":     a.cfg.Commit,
		"started_at": a.startedAt.Format(time.RFC3339),
	})
}

func (a *App) handleBandBBSStart(w http.ResponseWriter, r *http.Request) {
	authorizationURL, err := a.startBandBBSAuthorization(r, "login", "", r.URL.Query().Get("return_uri"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_start_failed", err.Error()))
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusFound)
}

func (a *App) handleBandBBSPublishStart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ReturnURI string `json:"return_uri"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	authorizationURL, err := a.startBandBBSAuthorization(r, "publish", currentUser(r).ID, request.ReturnURI)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_start_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorization_url": authorizationURL})
}

func (a *App) startBandBBSAuthorization(r *http.Request, purpose, userID, requestedReturnURI string) (string, error) {
	start := time.Now()
	meta := a.clientMeta(r)
	state, err := randomState()
	if err != nil {
		return "", err
	}
	returnURI := a.cfg.ClientRedirectURI
	scopes := a.cfg.BandBBS.Scopes
	if purpose == "publish" {
		if len(a.cfg.BandBBS.PublishScopes) == 0 {
			return "", fmt.Errorf("BandBBS publishing scopes are not configured")
		}
		scopes = mergeScopes(a.cfg.BandBBS.Scopes, a.cfg.BandBBS.PublishScopes)
	}
	if candidate := strings.TrimSpace(requestedReturnURI); candidate != "" && a.allowedReturnURI(candidate) {
		returnURI = candidate
	}
	err = a.store.CreateState(r.Context(), store.CreateStateParams{
		ID:        state,
		Provider:  ProviderBandBBS,
		ExpiresAt: time.Now().UTC().Add(a.cfg.StateTTL),
		Meta:      meta,
		ReturnURI: returnURI,
		Purpose:   purpose,
		UserID:    userID,
	})
	if err != nil {
		a.recordEvent(r, "start", "failure", meta, state, "", "", "", "", "db_error", err.Error(), start)
		return "", err
	}
	a.recordEvent(r, "start", "success", meta, state, "", "", config.ScopeString(scopes), "", "", "", start)
	return a.bandbbs.AuthorizeURL(state, scopes), nil
}

func (a *App) allowedReturnURI(candidate string) bool {
	if candidate == a.cfg.ClientRedirectURI {
		return true
	}
	if strings.TrimRight(candidate, "/") == strings.TrimRight(a.cfg.PublicURL, "/")+"/admin" {
		return true
	}
	parsed, err := url.Parse(candidate)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	origin := parsed.Scheme + "://" + parsed.Host
	return slices.Contains(a.cfg.WebClientOrigins, origin)
}

func (a *App) handleBandBBSCallback(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	stateID := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || stateID == "" {
		a.recordEvent(r, "callback", "failure", a.clientMeta(r), stateID, "", "", "", "", "invalid_callback", "missing code or state", start)
		http.Redirect(w, r, "/auth/failed?error=invalid_callback", http.StatusFound)
		return
	}
	state, err := a.store.ConsumeState(r.Context(), ProviderBandBBS, stateID)
	if err != nil {
		a.recordEvent(r, "callback", "failure", a.clientMeta(r), stateID, "", "", "", "", "invalid_state", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	meta := model.ClientMeta{AppID: state.AppID, Version: state.AppVersion, Build: state.AppBuild, Platform: state.Platform, IP: state.IP, UA: state.UserAgent}
	token, err := a.bandbbs.ExchangeCode(r.Context(), code)
	if err != nil {
		a.recordEvent(r, "token_exchange", "failure", meta, stateID, "", "", "", "", "token_exchange_failed", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=token_exchange_failed", http.StatusFound)
		return
	}
	requiredScopes := a.cfg.BandBBS.Scopes
	grantProvider := ProviderBandBBS
	if state.Purpose == "publish" {
		requiredScopes = mergeScopes(a.cfg.BandBBS.Scopes, a.cfg.BandBBS.PublishScopes)
		grantProvider = "bandbbs_publish"
	}
	providerUserID, actualScopes, err := a.bandbbs.ValidateScopes(r.Context(), token, requiredScopes)
	if err != nil {
		a.revokePair(r, token)
		a.recordEvent(r, "scope_check", "failure", meta, stateID, "", providerUserID, "", actualScopes, "scope_mismatch", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=scope_mismatch", http.StatusFound)
		return
	}
	token.Scope = actualScopes
	profile, err := a.bandbbs.Me(r.Context(), token.AccessToken)
	if err != nil {
		a.revokePair(r, token)
		a.recordEvent(r, "identity", "failure", meta, stateID, "", providerUserID, "", actualScopes, "identity_failed", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=identity_failed", http.StatusFound)
		return
	}
	if state.UserID != "" {
		expected, expectedErr := a.store.UserByID(r.Context(), state.UserID)
		if expectedErr != nil || expected.BandBBSUserID != profile.UserID {
			a.revokePair(r, token)
			a.recordEvent(r, "identity", "failure", meta, stateID, "", providerUserID, "", actualScopes, "account_mismatch", "publication authorization account does not match the signed-in user", start)
			http.Redirect(w, r, appendQuery(state.ReturnURI, "error", "account_mismatch"), http.StatusFound)
			return
		}
	}
	user, err := a.store.UpsertUser(r.Context(), store.UpsertUserParams{
		BandBBSUserID: profile.UserID,
		Username:      profile.Username,
		AvatarURL:     preferredAvatar(profile.AvatarURLs),
	})
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=identity_save_failed", http.StatusFound)
		return
	}
	// The env whitelist keeps working as the super-admin bootstrap: members
	// are promoted to the admin role on every login.
	if containsInt64(a.cfg.Admin.BandBBSUserIDs, user.BandBBSUserID) && user.Role != "admin" {
		if user, err = a.store.SetUserRole(r.Context(), user.ID, "admin"); err != nil {
			http.Redirect(w, r, "/auth/failed?error=identity_save_failed", http.StatusFound)
			return
		}
	}
	if user.BannedAt != nil {
		a.recordEvent(r, "identity", "failure", meta, stateID, "", providerUserID, "", actualScopes, "account_banned", "account is banned", start)
		http.Redirect(w, r, "/auth/failed?error=account_banned", http.StatusFound)
		return
	}
	accessCipher, err := a.secrets.Encrypt(token.AccessToken)
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
		return
	}
	refreshCipher, err := a.secrets.Encrypt(token.RefreshToken)
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
		return
	}
	var grantExpiry *time.Time
	if token.ExpiresIn > 0 {
		expires := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
		grantExpiry = &expires
	}
	// Refresh tokens always stay server-side. The publish grant additionally
	// carries write scopes and is only ever used by the publish worker.
	if err := a.store.UpsertOAuthGrant(r.Context(), store.GrantParams{
		UserID: user.ID, Provider: grantProvider, Subject: providerUserID,
		Scopes: config.ParseScopes(actualScopes), AccessTokenCipher: accessCipher,
		RefreshTokenCipher: refreshCipher, TokenType: token.TokenType, ExpiresAt: grantExpiry,
	}); err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_save_failed", http.StatusFound)
		return
	}
	var tokenCipher []byte
	if state.Purpose != "publish" {
		// The login grant is read-only: hand the access token (never the
		// refresh token) to the client through the login ticket so the app
		// can query BandBBS directly.
		payload, err := json.Marshal(model.TokenPayload{
			AccessToken: token.AccessToken,
			TokenType:   token.TokenType,
			ExpiresIn:   token.ExpiresIn,
			Scope:       token.Scope,
		})
		if err != nil {
			http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
			return
		}
		tokenCipher, err = a.secrets.Encrypt(string(payload))
		if err != nil {
			http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
			return
		}
	}
	ticket, ticketID, err := a.createTicket(r, meta, user.ID, state.ReturnURI, tokenCipher)
	if err != nil {
		a.recordEvent(r, "ticket_create", "failure", meta, stateID, "", providerUserID, "", actualScopes, "ticket_create_failed", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=ticket_create_failed", http.StatusFound)
		return
	}
	a.recordEvent(r, "callback", "success", meta, stateID, ticketID, providerUserID, "", actualScopes, "", "", start)
	target := appendQuery(state.ReturnURI, "ticket", ticket)
	if strings.TrimRight(state.ReturnURI, "/") == strings.TrimRight(a.cfg.PublicURL, "/")+"/admin" {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	transitionTarget, err := a.loginTransitionTarget(state.ReturnURI, ticket)
	if err != nil {
		a.recordEvent(r, "callback", "failure", meta, stateID, ticketID, providerUserID, "", actualScopes, "invalid_return_uri", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "授权完成",
		Heading:     "授权完成",
		Description: "可以返回 OronBox 继续使用",
		Target:      transitionTarget,
		Auto:        true,
		Tone:        "success",
	})
}

func (a *App) handleTicketExchange(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	meta := a.clientMeta(r)
	var req struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ticket) == "" {
		a.recordEvent(r, "ticket_exchange", "failure", meta, "", "", "", "", "", "invalid_request", "missing ticket", start)
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "missing ticket"))
		return
	}
	accessToken, err := authcore.RandomToken(32)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("token_create_failed", "failed to create session"))
		return
	}
	refreshToken, err := authcore.RandomToken(48)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("token_create_failed", "failed to create session"))
		return
	}
	user, tokenCipher, err := a.store.ConsumeLoginTicket(r.Context(), authcore.HashToken(strings.TrimSpace(req.Ticket), a.cfg.SessionSecret), store.SessionParams{
		AccessHash: authcore.HashToken(accessToken, a.cfg.SessionSecret), RefreshHash: authcore.HashToken(refreshToken, a.cfg.SessionSecret),
		AccessExpiresAt: time.Now().UTC().Add(a.cfg.AccessTokenTTL), RefreshExpiresAt: time.Now().UTC().Add(a.cfg.RefreshTokenTTL), Meta: meta,
	})
	if err != nil {
		a.recordEvent(r, "ticket_exchange", "failure", meta, "", "", "", "", "", "invalid_ticket", err.Error(), start)
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_ticket", "ticket is invalid or expired"))
		return
	}
	response := model.SessionTokens{AccessToken: accessToken, RefreshToken: refreshToken, TokenType: "Bearer", ExpiresIn: int64(a.cfg.AccessTokenTTL.Seconds()), RefreshExpiresIn: int64(a.cfg.RefreshTokenTTL.Seconds()), User: user}
	if len(tokenCipher) > 0 {
		raw, err := a.secrets.Decrypt(tokenCipher)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("token_decrypt_failed", err.Error()))
			return
		}
		var payload model.TokenPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("token_decrypt_failed", err.Error()))
			return
		}
		response.BandBBS = &payload
	}
	a.recordEvent(r, "ticket_exchange", "success", meta, "", "", fmt.Sprint(user.BandBBSUserID), "", "", "", "", start)
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleBandBBSTokenRefresh(w http.ResponseWriter, r *http.Request) {
	// The client only ever holds the read-only BandBBS access token; the
	// refresh token is kept encrypted in the server-side login grant.
	grant, err := a.store.OAuthGrant(r.Context(), currentUser(r).ID, ProviderBandBBS)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errorBody("bandbbs_grant_invalid", err.Error()))
		return
	}
	refresh, err := a.secrets.Decrypt(grant.RefreshTokenCipher)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("bandbbs_refresh_failed", err.Error()))
		return
	}
	token, err := a.bandbbs.Refresh(r.Context(), refresh)
	if err != nil {
		oauthLog(r.Context()).Warn("BandBBS token refresh failed", "user_id", currentUser(r).ID, "error", err)
		writeJSON(w, http.StatusUnauthorized, errorBody("bandbbs_refresh_failed", err.Error()))
		return
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refresh
	}
	subject, actualScopes, err := a.bandbbs.ValidateScopes(r.Context(), token, a.cfg.BandBBS.Scopes)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errorBody("bandbbs_refresh_failed", err.Error()))
		return
	}
	// Write-capable tokens must never reach the client through this endpoint.
	if config.HasScopes(actualScopes, a.cfg.BandBBS.PublishScopes) {
		writeJSON(w, http.StatusForbidden, errorBody("bandbbs_scope_forbidden", "refreshed token must not carry publish scopes"))
		return
	}
	accessCipher, err := a.secrets.Encrypt(token.AccessToken)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("bandbbs_refresh_failed", err.Error()))
		return
	}
	refreshCipher, err := a.secrets.Encrypt(token.RefreshToken)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("bandbbs_refresh_failed", err.Error()))
		return
	}
	var expiresAt *time.Time
	if token.ExpiresIn > 0 {
		expires := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
		expiresAt = &expires
	}
	if err := a.store.UpsertOAuthGrant(r.Context(), store.GrantParams{
		UserID: currentUser(r).ID, Provider: ProviderBandBBS, Subject: subject,
		Scopes: config.ParseScopes(actualScopes), AccessTokenCipher: accessCipher,
		RefreshTokenCipher: refreshCipher, TokenType: token.TokenType, ExpiresAt: expiresAt,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("bandbbs_refresh_failed", err.Error()))
		return
	}
	oauthLog(r.Context()).Info("BandBBS token refreshed", "user_id", currentUser(r).ID)
	writeJSON(w, http.StatusOK, model.TokenPayload{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
		ExpiresIn:   token.ExpiresIn,
		Scope:       actualScopes,
	})
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	meta := a.clientMeta(r)
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "missing refresh_token"))
		return
	}
	accessToken, err := authcore.RandomToken(32)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("token_create_failed", "failed to create session"))
		return
	}
	refreshToken, err := authcore.RandomToken(48)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("token_create_failed", "failed to create session"))
		return
	}
	user, err := a.store.RotateSession(r.Context(), authcore.HashToken(strings.TrimSpace(req.RefreshToken), a.cfg.SessionSecret), store.SessionParams{
		AccessHash: authcore.HashToken(accessToken, a.cfg.SessionSecret), RefreshHash: authcore.HashToken(refreshToken, a.cfg.SessionSecret),
		AccessExpiresAt: time.Now().UTC().Add(a.cfg.AccessTokenTTL), RefreshExpiresAt: time.Now().UTC().Add(a.cfg.RefreshTokenTTL), Meta: meta,
	})
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errorBody("invalid_refresh_token", "refresh token is invalid or expired"))
		return
	}
	writeJSON(w, http.StatusOK, model.SessionTokens{AccessToken: accessToken, RefreshToken: refreshToken, TokenType: "Bearer", ExpiresIn: int64(a.cfg.AccessTokenTTL.Seconds()), RefreshExpiresIn: int64(a.cfg.RefreshTokenTTL.Seconds()), User: user})
}

func (a *App) createTicket(r *http.Request, meta model.ClientMeta, userID, returnURI string, tokenCipher []byte) (string, string, error) {
	ticket, err := authcore.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	ticketID := shortID(ticket)
	_, err = a.store.CreateLoginTicket(r.Context(), store.LoginTicketParams{
		TicketHash: authcore.HashToken(ticket, a.cfg.SessionSecret), UserID: userID,
		ExpiresAt: time.Now().UTC().Add(a.cfg.LoginTicketTTL), Meta: meta, ReturnURI: returnURI,
		TokenCipher: tokenCipher,
	})
	return ticket, ticketID, err
}

func containsInt64(values []int64, expected int64) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func preferredAvatar(values map[string]string) string {
	for _, key := range []string{"l", "m", "h", "o", "s"} {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}

func mergeScopes(groups ...[]string) []string {
	seen := map[string]bool{}
	var result []string
	for _, group := range groups {
		for _, scope := range group {
			if !seen[scope] {
				seen[scope] = true
				result = append(result, scope)
			}
		}
	}
	return result
}

func (a *App) revokePair(r *http.Request, token model.TokenPayload) {
	if err := a.bandbbs.Revoke(r.Context(), token.AccessToken, "access_token"); err != nil {
		oauthLog(r.Context()).Warn("access token revocation failed", "provider", ProviderBandBBS, "error", err)
	}
	if token.RefreshToken != "" {
		if err := a.bandbbs.Revoke(r.Context(), token.RefreshToken, "refresh_token"); err != nil {
			oauthLog(r.Context()).Warn("refresh token revocation failed", "provider", ProviderBandBBS, "error", err)
		}
	}
}

// oauthLog keeps request-scoped fields but files the event under the oauth
// component.
func oauthLog(ctx context.Context) *slog.Logger {
	return observability.From(ctx).With("component", "oauth")
}

func (a *App) recordEvent(r *http.Request, eventType, result string, meta model.ClientMeta, stateID, ticketID, userID, expectedScopes, actualScopes, errorCode, errorMessage string, started time.Time) {
	if expectedScopes == "" {
		expectedScopes = config.ScopeString(a.cfg.BandBBS.Scopes)
	}
	err := a.store.RecordOAuthEvent(r.Context(), model.OAuthEvent{
		Provider:       ProviderBandBBS,
		EventType:      eventType,
		Result:         result,
		AppID:          meta.AppID,
		AppVersion:     meta.Version,
		AppBuild:       meta.Build,
		Platform:       meta.Platform,
		IP:             meta.IP,
		UserAgent:      meta.UA,
		StateID:        stateID,
		TicketID:       ticketID,
		ProviderUserID: userID,
		ExpectedScopes: expectedScopes,
		ActualScopes:   actualScopes,
		ErrorCode:      errorCode,
		ErrorMessage:   errorMessage,
		LatencyMS:      time.Since(started).Milliseconds(),
	})
	if err != nil {
		oauthLog(r.Context()).Error("OAuth audit event could not be recorded", "event", eventType, "result", result, "error", err)
	}
	attrs := []any{
		"provider", ProviderBandBBS,
		"event", eventType,
		"result", result,
		"app_id", meta.AppID,
		"platform", meta.Platform,
		"provider_user_id", userID,
		"latency_ms", time.Since(started).Milliseconds(),
	}
	if errorCode != "" {
		attrs = append(attrs, "error_code", errorCode, "error", errorMessage)
	}
	if result == "success" {
		oauthLog(r.Context()).Info("OAuth flow event", attrs...)
	} else {
		oauthLog(r.Context()).Warn("OAuth flow event", attrs...)
	}
}

func (a *App) clientMeta(r *http.Request) model.ClientMeta {
	return model.ClientMeta{
		AppID:    firstNonEmpty(r.Header.Get("X-OronBox-App-Id"), r.URL.Query().Get("app_id"), "unknown"),
		Version:  firstNonEmpty(r.Header.Get("X-OronBox-Version"), r.URL.Query().Get("app_version"), "unknown"),
		Platform: firstNonEmpty(r.Header.Get("X-OronBox-Platform"), r.URL.Query().Get("platform"), "unknown"),
		Build:    firstNonEmpty(r.Header.Get("X-OronBox-Build"), r.URL.Query().Get("app_build"), "unknown"),
		IP:       a.clientIP(r),
		UA:       r.UserAgent(),
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	if status >= http.StatusInternalServerError {
		if body, ok := value.(map[string]string); ok {
			code := body["error"]
			message := body["message"]
			observability.For("http").Error(
				"request handler failed",
				"request_id", w.Header().Get("X-Request-ID"),
				"code", code,
				"error", message,
			)
			value = errorBody(code, "The server could not complete the request")
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func errorBody(code, message string) map[string]string {
	return map[string]string{"error": code, "message": message}
}

func randomState() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, 32)
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return string(buf), nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func shortID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func appendQuery(rawURI string, key string, value string) string {
	u, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	peer := net.ParseIP(host)
	trusted := false
	for _, network := range a.trustedProxies {
		if peer != nil && network.Contains(peer) {
			trusted = true
			break
		}
	}
	if trusted {
		if value := validForwardedIP(r.Header.Get("CF-Connecting-IP")); value != "" {
			return value
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			for _, value := range strings.Split(forwarded, ",") {
				if value = validForwardedIP(value); value != "" {
					return value
				}
			}
		}
		if value := validForwardedIP(r.Header.Get("X-Real-IP")); value != "" {
			return value
		}
	}
	return host
}

func validForwardedIP(value string) string {
	value = strings.TrimSpace(value)
	if net.ParseIP(value) == nil {
		return ""
	}
	return value
}
