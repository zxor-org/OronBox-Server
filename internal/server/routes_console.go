package server

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) registerConsoleRoutes(mux *http.ServeMux) {
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
	mux.HandleFunc("GET /admin/login", a.handleAdminLoginPage)
	mux.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusMovedPermanently)
	})
	mux.HandleFunc("POST /admin/login", a.handleAdminLogin)
	mux.HandleFunc("POST /admin/logout", a.requireAdmin(a.handleAdminLogout))
	mux.HandleFunc("GET /admin/assets/{path...}", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin", a.requireAdmin(a.handleAdminDashboard))
	mux.HandleFunc("POST /admin/resources/{resource}/draft/{revision}/media", a.requireAdmin(a.handleAdminRevisionMediaUpload))
	mux.HandleFunc("POST /admin/resources/{resource}/draft/{revision}/artifacts", a.requireAdmin(a.handleAdminRevisionArtifactUpload))
	mux.HandleFunc("GET /admin/blobs/{sha256}", a.requireAdmin(a.handleAdminBlob))
	mux.HandleFunc("GET /admin/audit.csv", a.requireAdmin(a.handleAdminAuditCSV))
	mux.HandleFunc("POST /admin/cleanup/preview", a.requireAdminRole("admin", a.handleAdminCleanupPreview))
	mux.HandleFunc("GET /admin/review", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/review/{id}", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/resources", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/resources/{id}", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/comments", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/collections", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/collections/review", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/collections/{id}", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/publications", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/users", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/users/{id}", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/reports", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/feedback", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/devices", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/plugins", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/plugins/{id}", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/coins", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/messages", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/home", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/blog", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/blog/{slug}", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/announcements", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/releases", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/clients", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/storage/blobs", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/health", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/audit", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/operations", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/governance", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/runtime", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/favicon.svg", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/settings", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /admin/oauth/events", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/oauth/states", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /admin/oauth/tickets", a.requireAdmin(a.serveConsole))
}
