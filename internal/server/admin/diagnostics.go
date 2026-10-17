package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIAudit(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminAuditLogs(r.Context(), store.AdminAuditLogQuery{
		Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("audit_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "created_at": item.CreatedAt, "actor_user_id": item.ActorUserID,
			"username": item.Username, "action": item.Action, "result": item.Result,
			"ip": item.IP, "user_agent": item.UserAgent, "message": item.Message,
			"target": item.Target, "before": item.Before, "after": item.After, "metadata": item.Metadata,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "audit_failed")
}

func (a *Handler) HandleAdminAPIHealth(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	dbStatus := "ok"
	if err := a.store.Ping(r.Context()); err != nil {
		dbStatus = err.Error()
	}
	diagnostics, err := a.store.AdminHealthDiagnostics(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("health_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{{"db": dbStatus, "latency": time.Since(started).Round(time.Millisecond).String(), "version": a.cfg.Version}},
		"total": 1, "page": 1, "per_page": 1, "diagnostics": diagnostics,
	})
}

func (a *Handler) HandleAdminAPIBlobs(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminBlobs(r.Context(), store.AdminBlobQuery{
		Search: r.URL.Query().Get("q"), ReplicaState: r.URL.Query().Get("replica_state"),
		Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("blobs_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "blobs_failed")
}

func (a *Handler) HandleAdminAPIBlob(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminBlob(r.Context(), r.PathValue("sha256"))
	if err != nil {
		if errors.Is(err, store.ErrAdminBlobNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "blob was not found"))
		} else {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("blob_failed", err.Error()))
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPIBlobRequeue(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminRequeueBlobReplica(r.Context(), r.PathValue("sha256"))
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("blob_failed", err.Error()))
		return
	}
	a.audit(r, "blob.requeue", "success", r.PathValue("sha256"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": detail})
}

func (a *Handler) HandleAdminAPIOAuthEvents(w http.ResponseWriter, r *http.Request) {
	from, to := TimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthEvents(r.Context(), store.AdminOAuthEventQuery{
		Search: r.URL.Query().Get("q"), Result: r.URL.Query().Get("result"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "oauth_failed")
}

func (a *Handler) HandleAdminAPIOAuthEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid OAuth event id"))
		return
	}
	detail, err := a.store.AdminOAuthEvent(r.Context(), id)
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "OAuth event was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPIOAuthStates(w http.ResponseWriter, r *http.Request) {
	from, to := TimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthStates(r.Context(), store.AdminOAuthStateQuery{
		Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), From: from, To: to,
		Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "oauth_failed")
}

func (a *Handler) HandleAdminAPIOAuthState(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminOAuthState(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "OAuth state was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPIOAuthTickets(w http.ResponseWriter, r *http.Request) {
	from, to := TimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthTickets(r.Context(), store.AdminOAuthTicketQuery{
		Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), From: from, To: to,
		Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "oauth_failed")
}

func (a *Handler) HandleAdminAPIOAuthTicket(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminOAuthTicket(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "OAuth ticket was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPIClients(w http.ResponseWriter, r *http.Request) {
	from, to := TimeRange(r.URL.Query())
	page, err := a.store.AdminClientStats(r.Context(), store.AdminClientStatsQuery{
		Search: r.URL.Query().Get("q"), Result: r.URL.Query().Get("result"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("clients_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "clients_failed")
}

func (a *Handler) HandleAdminAPIAuditDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid id"))
		return
	}
	item, err := a.store.AdminAuditLog(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrAdminAuditNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "audit log was not found"))
		} else {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("audit_failed", err.Error()))
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
