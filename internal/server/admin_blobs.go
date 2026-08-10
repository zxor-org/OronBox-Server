package server

import (
	"errors"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminBlobs(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminBlobs(r.Context(), store.AdminBlobQuery{Search: r.URL.Query().Get("q"), MediaType: r.URL.Query().Get("media_type"), ReplicaState: r.URL.Query().Get("replica_state"), Referenced: r.URL.Query().Get("referenced"), Sort: r.URL.Query().Get("sort"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, "admin_blobs", map[string]any{"Title": "Blob 与副本", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/storage/blobs", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminBlobDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminBlob(r.Context(), r.PathValue("sha256"))
	if errors.Is(err, store.ErrAdminBlobNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, "admin_blob_detail", map[string]any{"Title": detail.Blob.SHA256, "Detail": detail, "Action": r.URL.Query().Get("action")})
}

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
