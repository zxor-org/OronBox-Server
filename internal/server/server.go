package server

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
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
	if d.StartWorkers && d.Store != nil {
		go app.runCoordinator(context.Background())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.handleHome)
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
	mux.HandleFunc("GET /assets/favicon.svg", staticAsset("image/svg+xml", web.FaviconSVG))
	mux.HandleFunc("GET /favicon.ico", staticAsset("image/svg+xml", web.FaviconSVG))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"status": "pass", "timestamp": time.Now().UTC(), "database": map[bool]string{true: "connected", false: "unavailable"}[d.Store != nil]})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.Store == nil {
			jsonResponse(w, 503, map[string]string{"status": "not_ready"})
			return
		}
		if err := d.Store.Pool.Ping(r.Context()); err != nil {
			jsonResponse(w, 503, map[string]string{"status": "not_ready"})
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /open", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("source") == "deviceQr" && strings.TrimSpace(query.Get("name")) != "" && compactMAC.MatchString(query.Get("mac")) {
			parameters := url.Values{
				"source": {"deviceQr"},
				"name":   {query.Get("name")},
				"mac":    {strings.ToUpper(query.Get("mac"))},
			}
			if authkey := strings.TrimSpace(query.Get("authkey")); authkey != "" {
				parameters.Set("authkey", authkey)
			}
			deepLink := (&url.URL{Scheme: "oronbox", Host: "open", RawQuery: parameters.Encode()}).String()
			app.renderTransition(w, r, web.TransitionPageData{
				Title:       "在 OronBox 中打开",
				Heading:     "在 OronBox 中打开",
				Description: "正在尝试唤起 OronBox，若没有自动打开，请点击下方按钮",
				ButtonLabel: "打开 OronBox",
				Target:      template.URL(deepLink),
				Auto:        true,
				Tone:        "info",
			})
			return
		}
		if pkg := strings.TrimSpace(query.Get("res")); pkg != "" {
			deepLink := "oronbox://open?res=" + escape(pkg)
			app.renderTransition(w, r, web.TransitionPageData{
				Title:       "在 OronBox 中打开",
				Heading:     "在 OronBox 中打开",
				Description: "正在尝试唤起 OronBox，若没有自动打开，请点击下方按钮",
				ButtonLabel: "打开 OronBox",
				Target:      template.URL(deepLink),
				Auto:        true,
				Tone:        "info",
			})
			return
		}
		http.Redirect(w, r, "/", 302)
	})
	mux.Handle("GET /api/v1/app/releases", app.withAttestation(http.HandlerFunc(app.releases)))
	mux.Handle("GET /api/v1/notices", app.withAttestation(http.HandlerFunc(app.notices)))
	mux.Handle("POST /api/v1/auth/bandbbs/start", app.withAttestation(http.HandlerFunc(app.authStart)))
	mux.Handle("GET /api/v1/auth/bandbbs/start", app.withAttestation(http.HandlerFunc(app.authStart)))
	mux.HandleFunc("GET /oauth2/bandbbs/callback", app.authCallback)
	mux.HandleFunc("GET /auth/success", app.authSuccess)
	mux.HandleFunc("GET /auth/failed", app.authFailed)
	register := func(pattern string, h http.Handler) { mux.Handle(pattern, app.withAttestation(authRequired(app, h))) }
	mux.Handle("POST /api/v1/auth/bandbbs/exchange", app.withAttestation(http.HandlerFunc(app.exchange)))
	mux.Handle("POST /api/v1/auth/bandbbs/refresh", app.withAttestation(http.HandlerFunc(app.refresh)))
	register("POST /api/v1/auth/session/revoke", http.HandlerFunc(app.revoke))
	register("POST /api/v1/auth/bandbbs/publish/start", http.HandlerFunc(app.publishStart))
	register("GET /api/v1/users/me", http.HandlerFunc(app.me))
	register("GET /api/v1/users/me/grants", http.HandlerFunc(app.grants))
	register("POST /oauth2/github/web/start", http.HandlerFunc(app.githubWebStart))
	register("POST /oauth2/github/web/status", http.HandlerFunc(app.githubWebStatus))
	mux.HandleFunc("GET /oauth2/github/callback", app.githubWebCallback)
	register("POST /oauth2/github/device/start", http.HandlerFunc(app.githubStart))
	register("POST /oauth2/github/device/poll", http.HandlerFunc(app.githubPoll))
	register("DELETE /oauth2/github/grant", http.HandlerFunc(app.githubRevoke))
	register("GET /api/v1/coins", http.HandlerFunc(app.coins))
	register("POST /api/v1/coins/checkin", http.HandlerFunc(app.checkin))
	register("GET /api/v1/messages", http.HandlerFunc(app.messages))
	register("DELETE /api/v1/messages", http.HandlerFunc(app.messagesClear))
	register("POST /api/v1/feedback", http.HandlerFunc(app.feedback))
	register("GET /api/v1/feedback", http.HandlerFunc(app.feedback))
	register("GET /api/v1/plugins", http.HandlerFunc(app.pluginListPublic))
	register("POST /api/v1/plugins", http.HandlerFunc(app.pluginUpload))
	register("GET /api/v1/plugins/my", http.HandlerFunc(app.pluginMy))
	register("DELETE /api/v1/plugins/{id}", http.HandlerFunc(app.pluginDelete))
	mux.Handle("/api/v1/resources/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/interactions") && r.Method == "GET" {
			app.withAttestation(http.HandlerFunc(app.interactions)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "GET" {
			app.withAttestation(http.HandlerFunc(app.comments)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "POST" {
			app.withAttestation(authRequired(app, http.HandlerFunc(app.commentCreate))).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/coins") && r.Method == "GET" {
			app.withAttestation(authRequired(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				app.coinStatus(w, r)
			}))).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/coins") && r.Method == "POST" {
			app.withAttestation(authRequired(app, http.HandlerFunc(app.tip))).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/ratings") && r.Method == "POST" {
			app.withAttestation(authRequired(app, http.HandlerFunc(app.rating))).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/download-events") && r.Method == "POST" {
			app.withAttestation(http.HandlerFunc(app.download)).ServeHTTP(w, r)
			return
		}
		jsonError(w, 404, "not_found", "resource endpoint not found", nil)
	}))
	mux.Handle("/api/v1/comments/", app.withAttestation(authRequired(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			app.commentDelete(w, r)
			return
		}
		jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
	}))))
	mux.Handle("/api/v1/messages/", app.withAttestation(authRequired(app, http.HandlerFunc(app.messageItem))))
	mux.Handle("/api/v1/feedback/", app.withAttestation(authRequired(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.feedbackItem(w, r)
	}))))
	mux.Handle("/api/v1/creator/", app.withAttestation(authRequired(app, http.HandlerFunc(app.creatorContract))))
	mux.HandleFunc("GET /console", app.consoleRedirect)
	mux.HandleFunc("GET /console/", app.consoleHandler)
	mux.HandleFunc("GET /admin/api/auth/bandbbs/authorize", app.adminAuthorize)
	mux.HandleFunc("POST /admin/api/auth/logout", app.adminLogout)
	mux.HandleFunc("GET /admin/api/auth/session", app.adminSession)
	mux.Handle("POST /admin/api/auth/refresh", app.adminProtected(http.HandlerFunc(app.adminRefresh)))
	mux.Handle("POST /admin/api/console/sync", app.adminProtected(http.HandlerFunc(app.consoleSyncPost)))
	mux.Handle("GET /admin/api/console/status", app.adminProtected(http.HandlerFunc(app.consoleStatusGet)))
	mux.Handle("PUT /admin/api/auth/minimum-version", app.adminProtected(http.HandlerFunc(app.minimumVersionPut)))
	mux.Handle("POST /admin/api/releases/sync", app.adminProtected(http.HandlerFunc(app.releaseSync)))
	mux.Handle("GET /admin/api/revocations", app.adminProtected(http.HandlerFunc(app.revocationsGet)))
	mux.Handle("POST /admin/api/revocations", app.adminProtected(http.HandlerFunc(app.revocationsPost)))
	mux.Handle("DELETE /admin/api/revocations/{fingerprint}", app.adminProtected(http.HandlerFunc(app.revocationsDelete)))
	mux.Handle("/admin/api/", app.adminProtected(http.HandlerFunc(app.adminContract)))
	return recoverMiddleware(mux)
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func jsonError(w http.ResponseWriter, status int, code, message string, detail any) {
	jsonResponse(w, status, map[string]any{"code": code, "message": message, "detail": detail})
}
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				jsonResponse(w, 500, map[string]string{"error": "internal_error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
