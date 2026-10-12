package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/api/admin"
	"github.com/zxor-org/OronBox-Server/internal/api/auth"
	"github.com/zxor-org/OronBox-Server/internal/api/catalog"
	"github.com/zxor-org/OronBox-Server/internal/api/community"
	"github.com/zxor-org/OronBox-Server/internal/api/creator"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

var compactMAC = regexp.MustCompile(`(?i)^[0-9a-f]{12}$`)

type Dependencies struct {
	Config       config.Config
	Store        *store.Store
	StartedAt    time.Time
	StartWorkers bool
}

func New(d Dependencies) http.Handler {
	app := newApplication(d)
	app.refreshRevocation()
	go app.cleanupOAuthStates(context.Background())
	if d.StartWorkers && d.Store != nil {
		go app.runCoordinator(context.Background())
		go app.runPublications(context.Background())
		go app.runReleaseSyncWorker(context.Background())
	}

	baseURL := d.Config.BaseURL
	if baseURL == "" {
		baseURL = d.Config.PublicURL
	}
	if baseURL == "" {
		baseURL = "https://ob-api.zxor.org"
	}
	blobDir := d.Config.BlobStorageDir
	if blobDir == "" {
		blobDir = "/var/lib/oronbox/blobs"
	}
	r2PublicURL := d.Config.R2.PublicURL
	r2Client := catalog.NewR2Client(d.Config.R2)
	catalogOwner, catalogRepo, _ := syndication.SplitRepository(d.Config.Gitea.CatalogRepo)
	publicationSource := syndication.PublicationConfigSource{
		Client:   syndication.GiteaClient{BaseURL: d.Config.Gitea.APIURL, Token: d.Config.Gitea.ClientSecret},
		Owner:    catalogOwner,
		Repo:     catalogRepo,
		Ref:      d.Config.GiteaCatalogRef,
		LocalDir: d.Config.PublicationConfigDir,
	}
	communityGitea := syndication.GiteaClient{BaseURL: d.Config.Gitea.APIURL, Token: d.Config.Gitea.ClientSecret}
	blogs := admin.NewBlogHandlers(d.Store, communityGitea, catalogOwner, catalogRepo, d.Config.GiteaCatalogRef)

	authSvc := auth.NewService(d.Config, d.Store, app.cipher, &app.states)

	mux := http.NewServeMux()

	// 1. Static Web & Health Check
	staticAsset := func(contentType, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("GET /{$}", app.handleHome)
	mux.HandleFunc("GET /assets/app.css", staticAsset("text/css; charset=utf-8", web.CSS))
	mux.HandleFunc("GET /assets/theme.js", staticAsset("text/javascript; charset=utf-8", web.ThemeJS))
	mux.HandleFunc("GET /assets/transition.js", staticAsset("text/javascript; charset=utf-8", web.TransitionJS))
	mux.HandleFunc("GET /assets/favicon.svg", staticAsset("image/svg+xml", web.FaviconSVG))
	mux.HandleFunc("GET /favicon.ico", staticAsset("image/svg+xml", web.FaviconSVG))

	// Spec §2.1: GET /healthz 含 timestamp
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		database := "unavailable"
		if d.Store != nil && d.Store.Pool != nil && d.Store.Pool.Ping(r.Context()) == nil {
			database = "connected"
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":    "pass",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"database":  database,
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.Store == nil || d.Store.Pool == nil || d.Store.Pool.Ping(r.Context()) != nil {
			httputil.Error(w, http.StatusServiceUnavailable, "not_ready", "服务未就绪")
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// 2. Open Deep Link Handler
	mux.HandleFunc("GET /open", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if pkg := strings.TrimSpace(query.Get("res")); pkg != "" {
			deepLink := "oronbox://open?res=" + url.QueryEscape(pkg)
			http.Redirect(w, r, deepLink, http.StatusTemporaryRedirect)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	})

	// 3. Client Public Catalog & Blobs API
	// Spec §3.3: 公开分机型安装包直下 (免构建鉴权)
	mux.Handle("GET /api/v1/resources/{id}/download/{device}", catalog.HandleDownloadDeviceRedirect(d.Store, baseURL))
	mux.Handle("GET /api/v1/blobs/", catalog.HandleServeBlob(d.Store, blobDir, r2PublicURL))
	mux.Handle("POST /api/v1/blobs", authRequired(app, app.withAttestation(catalog.HandleUploadBlob(d.Store, blobDir, r2Client))))

	// Spec §3.1 / §3.2 / §3.5 / §3.6: 资源探索与详情 (构建鉴权)
	mux.Handle("GET /api/v1/resources", app.withAttestation(catalog.HandleGetResources(d.Store, baseURL)))
	mux.Handle("GET /api/v1/resources/", app.withAttestation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.Contains(p, "/download/") {
			catalog.HandleDownloadDeviceRedirect(d.Store, baseURL)(w, r)
			return
		}
		if strings.HasSuffix(p, "/comments") {
			community.HandleGetComments(d.Store)(w, r)
			return
		}
		if strings.HasSuffix(p, "/coins") {
			authRequired(app, community.HandleGetCoinVote(d.Store)).ServeHTTP(w, r)
			return
		}
		catalog.HandleGetResourceDetail(d.Store, baseURL)(w, r)
	})))
	mux.Handle("POST /api/v1/resources/", app.withAttestation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/comments") {
			authRequired(app, community.HandlePostComment(d.Store)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(p, "/coins") {
			authRequired(app, community.HandleCoinVote(d.Store)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(p, "/ratings") {
			authRequired(app, community.HandlePostRating(d.Store)).ServeHTTP(w, r)
			return
		}
		httputil.Error(w, http.StatusNotFound, "not_found", "接口不存在")
	})))
	mux.Handle("DELETE /api/v1/comments/", app.withAttestation(authRequired(app, community.HandleDeleteComment(d.Store))))

	mux.Handle("GET /api/v1/tags", app.withAttestation(catalog.HandleGetTags(d.Store)))
	mux.Handle("GET /api/v1/collections/", app.withAttestation(catalog.HandleGetCollectionDetail(d.Store, baseURL)))

	// 4. Client Plugins & Releases
	mux.Handle("GET /api/v1/plugins", app.withAttestation(catalog.HandleGetPlugins(d.Store)))
	mux.Handle("GET /api/v1/plugins/{id}", app.withAttestation(catalog.HandleGetPluginDetail(d.Store)))
	mux.Handle("POST /api/v1/plugins", app.withAttestation(authRequired(app, catalog.HandleUploadPlugin(d.Store, blobDir, r2Client))))
	mux.Handle("DELETE /api/v1/plugins/{id}", app.withAttestation(authRequired(app, catalog.HandleDeletePlugin(d.Store))))
	mux.Handle("GET /api/v1/plugins/{id}/package", app.withAttestation(catalog.HandleDownloadPluginPackage(d.Store, baseURL)))
	mux.Handle("GET /api/v1/app/releases", app.withAttestation(catalog.HandleGetLatestRelease(d.Store)))
	// Deprecated alias: preserved for backward compatibility
	mux.Handle("GET /api/v1/releases/latest", app.withAttestation(catalog.HandleGetLatestRelease(d.Store)))

	// 5. User Profile & Messages & Coins (with Attestation)
	mux.Handle("GET /api/v1/users/me", app.withAttestation(authRequired(app, community.HandleGetMe(d.Store))))
	mux.Handle("GET /api/v1/messages", app.withAttestation(authRequired(app, community.HandleGetMessages(d.Store))))
	mux.Handle("DELETE /api/v1/messages", app.withAttestation(authRequired(app, community.HandleClearMessages(d.Store))))
	mux.Handle("PUT /api/v1/messages/", app.withAttestation(authRequired(app, community.HandleMarkMessageRead(d.Store))))
	mux.Handle("GET /api/v1/announcements/unread", app.withAttestation(authRequired(app, community.HandleGetAnnouncements(d.Store))))
	mux.Handle("POST /api/v1/announcements/read", app.withAttestation(authRequired(app, community.HandleReadAnnouncements(d.Store))))
	mux.Handle("GET /api/v1/coins/account", app.withAttestation(authRequired(app, community.HandleGetCoinAccount(d.Store))))
	mux.Handle("POST /api/v1/coins/checkin", app.withAttestation(authRequired(app, community.HandlePostCheckin(d.Store))))
	mux.Handle("GET /api/v1/coins/ledger", app.withAttestation(authRequired(app, community.HandleGetCoinLedger(d.Store))))

	// 6. Feedback & Tickets (with Attestation)
	mux.Handle("GET /api/v1/feedback", app.withAttestation(authRequired(app, community.HandleGetMyFeedback(d.Store))))
	mux.Handle("POST /api/v1/feedback", app.withAttestation(authRequired(app, community.HandleCreateFeedback(d.Store))))
	mux.Handle("GET /api/v1/feedback/", app.withAttestation(authRequired(app, community.HandleGetFeedbackDetail(d.Store))))
	mux.Handle("POST /api/v1/feedback/", app.withAttestation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/replies") {
			authRequired(app, community.HandlePostFeedbackReply(d.Store)).ServeHTTP(w, r)
			return
		}
		authRequired(app, community.HandleCreateFeedback(d.Store)).ServeHTTP(w, r)
	})))

	// 7. Creator Resources & Collections & Collaborators (with Attestation)
	mux.Handle("GET /api/v1/creator/resources", app.withAttestation(authRequired(app, creator.HandleCreatorGetResources(d.Store))))
	mux.Handle("POST /api/v1/creator/resources", app.withAttestation(authRequired(app, creator.HandleCreatorCreateResource(d.Store))))
	mux.Handle("PUT /api/v1/creator/resources/", app.withAttestation(authRequired(app, creator.HandleCreatorSaveDraft(d.Store, blobDir, r2Client))))
	mux.Handle("POST /api/v1/creator/resources/", app.withAttestation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/publish") {
			authRequired(app, creator.HandleCreatorPublishResourceWithPublicationSource(d.Store, blobDir, publicationSource)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(p, "/collaborators") {
			authRequired(app, creator.HandleCreatorInviteCollaborator(d.Store)).ServeHTTP(w, r)
			return
		}
		httputil.Error(w, http.StatusNotFound, "not_found", "接口不存在")
	})))
	mux.Handle("GET /api/v1/creator/resources/", app.withAttestation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/relationships") {
			authRequired(app, creator.HandleCreatorGetRelationships(d.Store)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/source") {
			authRequired(app, creator.HandleCreatorGetRelationships(d.Store)).ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			authRequired(app, creator.HandleCreatorDownloadBlob(d.Store, blobDir, r2PublicURL)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/collaborators") {
			authRequired(app, creator.HandleCreatorGetCollaborators(d.Store)).ServeHTTP(w, r)
			return
		}
		httputil.Error(w, http.StatusNotFound, "not_found", "接口不存在")
	})))
	mux.Handle("GET /api/v1/creator/resources/{id}", app.withAttestation(authRequired(app, creator.HandleCreatorGetResourceDetail(d.Store))))
	mux.Handle("DELETE /api/v1/creator/resources/{id}/collaborators/{userId}", app.withAttestation(authRequired(app, creator.HandleCreatorRemoveCollaborator(d.Store))))
	mux.Handle("GET /api/v1/collaborations", app.withAttestation(authRequired(app, creator.HandleGetCollaborationInvitations(d.Store))))
	mux.Handle("POST /api/v1/resources/{id}/collaboration/accept", app.withAttestation(authRequired(app, creator.HandleAcceptCollaboration(d.Store))))
	mux.Handle("DELETE /api/v1/resources/{id}/collaboration", app.withAttestation(authRequired(app, creator.HandleDeclineCollaboration(d.Store))))
	mux.Handle("GET /api/v1/creator/coins/stats", app.withAttestation(authRequired(app, creator.HandleCreatorCoinStats(d.Store))))
	mux.Handle("PUT /api/v1/creator/resources/{id}/source", app.withAttestation(authRequired(app, creator.HandleCreatorResourceSource(d.Store))))
	mux.Handle("POST /api/v1/creator/resources/{id}/takedown", app.withAttestation(authRequired(app, creator.HandleCreatorResourceState(d.Store, false))))
	mux.Handle("POST /api/v1/creator/resources/{id}/restore", app.withAttestation(authRequired(app, creator.HandleCreatorResourceState(d.Store, true))))
	mux.Handle("DELETE /api/v1/creator/resources/{id}", app.withAttestation(authRequired(app, creator.HandleCreatorDeleteResource(d.Store))))

	mux.Handle("GET /api/v1/creator/collections", app.withAttestation(authRequired(app, creator.HandleCreatorGetCollections(d.Store))))
	mux.Handle("GET /api/v1/creator/collections/{id}", app.withAttestation(authRequired(app, creator.HandleCreatorGetCollectionDetail(d.Store))))
	mux.Handle("POST /api/v1/creator/collections", app.withAttestation(authRequired(app, creator.HandleCreatorSaveCollection(d.Store))))
	mux.Handle("POST /api/v1/creator/collections/", app.withAttestation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/publish") {
			authRequired(app, creator.HandleCreatorPublishCollection(d.Store)).ServeHTTP(w, r)
			return
		}
		httputil.Error(w, http.StatusNotFound, "not_found", "接口不存在")
	})))
	mux.Handle("PUT /api/v1/creator/collections/", app.withAttestation(authRequired(app, creator.HandleCreatorUpdateCollectionResources(d.Store))))
	mux.Handle("PATCH /api/v1/creator/collections/", app.withAttestation(authRequired(app, creator.HandleCreatorUpdateCollection(d.Store))))
	mux.Handle("DELETE /api/v1/creator/collections/", app.withAttestation(authRequired(app, creator.HandleCreatorDeleteCollection(d.Store))))

	// Deprecated aliases: preserved for backward compatibility
	mux.Handle("POST /api/v1/creator/submissions", app.withAttestation(authRequired(app, creator.HandleCreatorSubmit(d.Store, blobDir))))
	mux.Handle("GET /api/v1/creator/submissions", app.withAttestation(authRequired(app, creator.HandleCreatorGetSubmissions(d.Store))))
	mux.Handle("GET /api/v1/creator/submissions/", app.withAttestation(authRequired(app, creator.HandleCreatorGetSubmissionDetail(d.Store))))
	mux.Handle("GET /api/v1/creator/submissions/{id}/astrobox", app.withAttestation(http.HandlerFunc(app.getAstroboxReview)))
	mux.Handle("GET /api/v1/creator/submissions/{id}/astrobox/comments", app.withAttestation(http.HandlerFunc(app.getAstroboxComments)))
	mux.Handle("POST /api/v1/creator/submissions/{id}/astrobox/comments", app.withAttestation(http.HandlerFunc(app.createAstroboxComment)))
	mux.Handle("POST /api/v1/creator/submissions/{id}/astrobox/fix", app.withAttestation(http.HandlerFunc(app.appendAstroboxFix)))
	mux.Handle("PATCH /api/v1/creator/submissions/{id}/astrobox/comments/{comment}", app.withAttestation(http.HandlerFunc(app.updateAstroboxComment)))
	mux.Handle("DELETE /api/v1/creator/submissions/{id}/astrobox/comments/{comment}", app.withAttestation(http.HandlerFunc(app.deleteAstroboxComment)))

	// 8. BandBBS & GitHub OAuth2
	mux.Handle("POST /api/v1/auth/bandbbs/start", app.withAttestation(http.HandlerFunc(authSvc.HandleAuthStart)))
	mux.Handle("GET /api/v1/auth/bandbbs/start", app.withAttestation(http.HandlerFunc(authSvc.HandleAuthStart)))
	mux.HandleFunc("GET /oauth2/bandbbs/callback", authSvc.HandleAuthCallback(app.renderTransition, app.finishAdminLogin))
	mux.Handle("POST /api/v1/auth/ticket/exchange", app.withAttestation(http.HandlerFunc(authSvc.HandleTicketExchange)))
	mux.Handle("POST /api/v1/auth/session/refresh", app.withAttestation(http.HandlerFunc(authSvc.HandleSessionRefresh)))

	mux.Handle("POST /api/v1/auth/bandbbs/token/refresh", app.withAttestation(authRequired(app, http.HandlerFunc(authSvc.HandleBandBBSTokenRefresh))))
	mux.Handle("POST /api/v1/auth/session/revoke", app.withAttestation(authRequired(app, http.HandlerFunc(authSvc.HandleRevokeSession))))

	mux.Handle("GET /api/v1/auth/github/web/start", app.withAttestation(authRequired(app, http.HandlerFunc(authSvc.HandleGithubWebStart))))
	mux.Handle("GET /api/v1/auth/github/grant", app.withAttestation(authRequired(app, http.HandlerFunc(authSvc.HandleGithubWebStatus))))
	mux.HandleFunc("GET /oauth2/github/callback", authSvc.HandleGithubWebCallback)
	mux.Handle("DELETE /api/v1/auth/github/grant", app.withAttestation(authRequired(app, http.HandlerFunc(authSvc.HandleGithubRevoke))))

	// 9. Admin Console & Management APIs
	mux.HandleFunc("GET /console", app.consoleRedirect)
	mux.HandleFunc("GET /console/", app.consoleHandler)
	mux.HandleFunc("GET /admin/api/auth/bandbbs/authorize", app.adminAuthorize)
	mux.HandleFunc("POST /admin/api/auth/logout", app.adminLogout)
	mux.HandleFunc("GET /admin/api/auth/session", app.adminSession)
	mux.Handle("POST /admin/api/auth/refresh", app.adminProtected(http.HandlerFunc(app.adminRefresh)))
	mux.Handle("PUT /admin/api/auth/minimum-version", app.adminProtected(admin.HandleMinimumVersionPut(d.Store, app.refreshRevocation)))
	mux.Handle("GET /admin/api/revocations", app.adminProtected(admin.HandleRevocationsGet(d.Store, func() string {
		app.revMu.RLock()
		defer app.revMu.RUnlock()
		return app.minimum
	})))
	mux.Handle("POST /admin/api/revocations", app.adminProtected(admin.HandleRevocationsPostWithMaster(d.Store, d.Config.AttestationMasterKey, app.refreshRevocation)))
	mux.Handle("DELETE /admin/api/revocations/{fingerprint}", app.adminProtected(admin.HandleRevocationsDelete(d.Store, app.refreshRevocation)))
	mux.Handle("POST /admin/api/console/sync", app.adminProtected(http.HandlerFunc(app.consoleSyncPost)))
	mux.Handle("GET /admin/api/console/status", app.adminProtected(http.HandlerFunc(app.consoleStatusGet)))
	mux.Handle("POST /admin/api/releases/sync", app.adminProtected(http.HandlerFunc(app.releaseSync)))
	mux.Handle("POST /admin/api/publications/{id}/retry", app.adminProtected(http.HandlerFunc(app.retryPublication)))
	mux.Handle("GET /admin/api/roles/members", app.adminProtected(admin.HandleGetRoleMembers(d.Store)))
	mux.Handle("POST /admin/api/roles/admin", app.adminProtected(admin.HandleSetAdminRole(d.Store, d.Config.AdminBandBBSUIDs)))
	mux.Handle("POST /admin/api/roles/reviewer", app.adminProtected(admin.HandleSetReviewerRole(d.Store, d.Config.AdminBandBBSUIDs)))

	// Admin UGC Reviews & Management
	mux.Handle("GET /admin/api/reviews", app.adminProtected(admin.HandleAdminGetReviews(d.Store)))
	mux.Handle("GET /admin/api/reviews/revisions/", app.adminProtected(admin.HandleAdminGetReviewDetail(d.Store, baseURL)))
	mux.Handle("POST /admin/api/reviews/revisions/", app.adminProtected(admin.HandleAdminDecideReviewWithPublicationSource(d.Store, blobDir, publicationSource)))
	mux.Handle("POST /admin/api/reviews/collections/", app.adminProtected(admin.HandleAdminDecideCollectionReview(d.Store)))

	// Admin Resources & Plugins
	mux.Handle("GET /admin/api/resources", app.adminProtected(admin.HandleAdminGetResources(d.Store)))
	mux.Handle("PUT /admin/api/resources/", app.adminProtected(admin.HandleAdminUpdateResource(d.Store)))
	mux.Handle("POST /admin/api/resources/", app.adminProtected(admin.HandleAdminUpdateResourceState(d.Store)))

	mux.Handle("GET /admin/api/plugins", app.adminProtected(admin.HandleAdminGetPlugins(d.Store)))
	mux.Handle("GET /admin/api/plugins/{id}/download", app.adminProtected(admin.HandleAdminDownloadPlugin(d.Store, blobDir, r2PublicURL)))
	mux.Handle("POST /admin/api/plugins/", app.adminProtected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/review") {
			admin.HandleAdminReviewPlugin(d.Store)(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/state") {
			admin.HandleAdminStatePlugin(d.Store)(w, r)
			return
		}
		httputil.Error(w, http.StatusNotFound, "not_found", "接口不存在")
	})))

	// Admin Users & Coins
	mux.Handle("GET /admin/api/users", app.adminProtected(admin.HandleAdminGetUsers(d.Store)))
	mux.Handle("POST /admin/api/users/", app.adminProtected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/state") {
			admin.HandleAdminUpdateUserState(d.Store)(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			admin.HandleAdminKickUserSessions(d.Store)(w, r)
			return
		}
		httputil.Error(w, http.StatusNotFound, "not_found", "接口不存在")
	})))
	mux.Handle("POST /admin/api/coins/users/", app.adminProtected(admin.HandleAdminAdjustCoins(d.Store)))

	// Admin Feedback & Comments
	mux.Handle("GET /admin/api/tickets", app.adminProtected(admin.HandleAdminGetTickets(d.Store)))
	mux.Handle("GET /admin/api/tickets/", app.adminProtected(admin.HandleAdminGetTicketDetail(d.Store)))
	mux.Handle("POST /admin/api/tickets/", app.adminProtected(admin.HandleAdminReplyTicket(d.Store)))
	mux.Handle("GET /admin/api/comments", app.adminProtected(admin.HandleAdminGetComments(d.Store)))
	mux.Handle("POST /admin/api/comments/", app.adminProtected(admin.HandleAdminUpdateCommentState(d.Store)))

	// Admin Settings & Tags
	mux.Handle("GET /admin/api/settings/recommendation", app.adminProtected(admin.HandleAdminGetRecommendationSettings(d.Store)))
	mux.Handle("PUT /admin/api/settings/recommendation", app.adminProtected(admin.HandleAdminUpdateRecommendationSettings(d.Store)))
	mux.Handle("GET /admin/api/settings/ai-moderation", app.adminProtected(admin.HandleAdminGetAIModeration(d.Store)))
	mux.Handle("PUT /admin/api/settings/ai-moderation", app.adminProtected(admin.HandleAdminUpdateAIModeration(d.Store)))

	mux.Handle("GET /admin/api/tags", app.adminProtected(admin.HandleAdminGetTags(d.Store)))
	mux.Handle("POST /admin/api/tags", app.adminProtected(admin.HandleAdminCreateTag(d.Store)))
	mux.Handle("PUT /admin/api/tags/", app.adminProtected(admin.HandleAdminUpdateTag(d.Store)))
	mux.Handle("DELETE /admin/api/tags/", app.adminProtected(admin.HandleAdminDeleteTag(d.Store)))

	mux.Handle("GET /admin/api/templates", app.adminProtected(admin.HandleAdminGetTemplates(d.Store)))
	mux.Handle("POST /admin/api/templates", app.adminProtected(admin.HandleAdminSaveTemplate(d.Store)))
	mux.Handle("PUT /admin/api/templates/", app.adminProtected(admin.HandleAdminSaveTemplate(d.Store)))
	mux.Handle("DELETE /admin/api/templates/", app.adminProtected(admin.HandleAdminDeleteTemplate(d.Store)))
	mux.Handle("GET /admin/api/audit", app.adminProtected(admin.HandleAdminGetAudit(d.Store)))
	mux.Handle("GET /admin/api/blogs", app.adminProtected(http.HandlerFunc(blogs.Get)))
	mux.Handle("POST /admin/api/blogs", app.adminProtected(http.HandlerFunc(blogs.Create)))
	mux.Handle("PUT /admin/api/blogs/", app.adminProtected(http.HandlerFunc(blogs.Update)))
	mux.Handle("DELETE /admin/api/blogs/", app.adminProtected(http.HandlerFunc(blogs.Delete)))
	mux.Handle("GET /api/blog", http.HandlerFunc(blogs.PublicList))
	mux.Handle("GET /api/blog/", http.HandlerFunc(blogs.PublicGet))
	mux.HandleFunc("GET /api/home", func(w http.ResponseWriter, r *http.Request) {
		httputil.JSON(w, http.StatusOK, map[string]any{"banners": []any{}, "sections": []any{}, "resource_feed_available": false})
	})

	// 10. Legacy client fallbacks (v1.1.3 and older)
	legacyNotice := map[string]any{
		"id":         "migration-maintenance-notice",
		"name":       "服务器正在更新迁移 / Maintenance Notice",
		"kind":       "quick_app",
		"owner":      "OronBox Team",
		"summary":    "服务器正在进行架构升级与数据迁移，米坛登录和 OronBox 资源及创作者功能暂不可用，请耐心等待迁移完成\n\nThe server is undergoing architecture upgrade and migration. BandBBS login, resources, and creator features are temporarily unavailable.",
		"version":    "1.0.0",
		"devices":    []string{},
		"attributes": []string{"公告", "维护"},
		"updated_at": "2026-10-13T00:00:00Z",
	}
	mux.HandleFunc("GET /api/resources", func(w http.ResponseWriter, r *http.Request) {
		httputil.JSON(w, http.StatusOK, map[string]any{"total": 1, "resources": []any{legacyNotice}})
	})
	mux.HandleFunc("GET /api/resources/", func(w http.ResponseWriter, r *http.Request) {
		httputil.JSON(w, http.StatusOK, legacyNotice)
	})
	mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		httputil.JSON(w, http.StatusOK, map[string]any{"devices": []any{}})
	})

	return requestLogger(recoverMiddleware(rateLimitMiddleware(app, mux)))
}
