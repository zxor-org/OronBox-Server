package server

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) handleAdminBlob(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimSpace(r.PathValue("sha256"))
	if !sha256Pattern.MatchString(digest) {
		http.NotFound(w, r)
		return
	}
	record, err := a.store.Blob(r.Context(), digest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	reader, err := a.blobs.Open(r.Context(), record.LocalKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", record.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+record.SHA256+`"`)
	if r.URL.Query().Get("download") == "1" {
		name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
		if name == "" || name == "." {
			name = record.SHA256
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}
