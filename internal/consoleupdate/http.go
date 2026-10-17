package consoleupdate

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/gitea"
)

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, root := m.installedConsole()
	if root == "" {
		if strings.HasPrefix(r.URL.Path, "/admin/assets/") || r.URL.Path == "/admin/favicon.svg" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "30")
		http.Error(w, "Console build is not available", http.StatusServiceUnavailable)
		return
	}
	p := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/admin"), "/")
	if p == "" {
		p = "index.html"
	}
	if !gitea.ValidPath(p) {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(p, "assets/") && !strings.Contains(filepath.Base(p), ".") {
		p = "index.html"
	}
	file := filepath.Join(root, filepath.FromSlash(p))
	if p != "index.html" && strings.HasPrefix(p, "assets/") {
		if _, err := os.Stat(file); err != nil {
			raw, readErr := os.ReadFile(filepath.Join(m.root, "current.json"))
			var state struct {
				Previous string `json:"previous_directory"`
			}
			if readErr == nil && json.Unmarshal(raw, &state) == nil {
				if _, err := uuid.Parse(state.Previous); err == nil {
					file = filepath.Join(m.root, "versions", state.Previous, filepath.FromSlash(p))
				}
			}
		}
	}
	if p != "index.html" {
		if _, err := os.Stat(file); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public,max-age=31536000,immutable")
		http.ServeFile(w, r, file)
		return
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		http.Error(w, "Console unavailable", 503)
		return
	}
	nonce, err := auth.RandomToken(18)
	if err != nil {
		http.Error(w, "Console unavailable", 503)
		return
	}
	policy := strings.Replace(m.csp, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1)
	w.Header().Set("Content-Security-Policy", policy)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, strings.ReplaceAll(string(raw), "__CSP_NONCE__", nonce))
}
