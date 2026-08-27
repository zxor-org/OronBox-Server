package server

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminBlobRequeue(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	detail, err := a.store.AdminRequeueBlobReplica(r.Context(), r.PathValue("sha256"))
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "blob.replica.requeue", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), 409)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "blob.replica.requeue", "success", a.clientIP(r), r.UserAgent(), "sha256="+detail.Blob.SHA256)
	http.Redirect(w, r, "/admin/storage/blobs/"+detail.Blob.SHA256+"?action=requeued", 302)
}

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
	actor := currentAdmin(r)
	_ = a.store.RecordAuditData(r.Context(), actor, "blob.read", "success", a.clientIP(r), r.UserAgent(), adminBlobReadAuditData(record.SHA256, r.URL.Query().Get("download") == "1"))
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}

func adminBlobReadAuditData(sha256 string, download bool) store.AuditData {
	return store.AuditData{
		Message:  fmt.Sprintf("sha256=%s download=%t", sha256, download),
		Target:   store.AuditTarget{Type: "blob", ID: sha256, Label: "sensitive blob read"},
		Metadata: map[string]any{"download": download, "sensitive": true},
	}
}
