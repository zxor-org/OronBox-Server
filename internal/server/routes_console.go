package server

import (
	"github.com/zxor-org/OronBox-Server/internal/web"
	"net/http"
)

func (a *App) registerConsoleRoutes(mux *http.ServeMux) {
	for path, asset := range map[string]struct{ contentType, body string }{
		"/assets/favicon.svg":   {"image/svg+xml", web.FaviconSVG},
		"/assets/app.css":       {"text/css; charset=utf-8", web.CSS},
		"/assets/theme.js":      {"text/javascript; charset=utf-8", web.ThemeJS},
		"/assets/transition.js": {"text/javascript; charset=utf-8", web.TransitionJS},
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", asset.contentType)
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write([]byte(asset.body))
		})
	}

	mux.HandleFunc("GET /console", a.requireAdmin(a.handleAdminDashboard))
	mux.HandleFunc("GET /console/{$}", a.requireAdmin(a.handleAdminDashboard))
	mux.HandleFunc("GET /console/login", a.handleAdminLoginPage)
	mux.HandleFunc("GET /console/assets/{path...}", a.serveConsole)
	mux.HandleFunc("GET /console/favicon.svg", a.serveConsole)
	mux.HandleFunc("POST /console/resources/{resource}/draft/{revision}/media", a.requireAdmin(a.handleAdminRevisionMediaUpload))
	mux.HandleFunc("POST /console/resources/{resource}/draft/{revision}/artifacts", a.requireAdmin(a.handleAdminRevisionArtifactUpload))
	mux.HandleFunc("GET /console/blobs/{sha256}", a.requireAdmin(a.handleAdminBlob))
	mux.HandleFunc("GET /console/audit.csv", a.requireAdmin(a.handleAdminAuditCSV))
	mux.HandleFunc("POST /console/cleanup/preview", a.requireAdminRole("admin", a.handleAdminCleanupPreview))
	mux.HandleFunc("GET /console/settings", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /console/resources", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /console/reviews", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /console/comments", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /console/feedback", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /console/blogs", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /console/users", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /console/plugins", a.requireAdmin(a.serveConsole))
	mux.HandleFunc("GET /console/system", a.requireAdminRole("admin", a.serveConsole))
	mux.HandleFunc("GET /console/governance", a.requireAdminRole("admin", a.serveConsole))
}
