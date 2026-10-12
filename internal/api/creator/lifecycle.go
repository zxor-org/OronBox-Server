package creator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/api/catalog"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func creatorResourceID(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, p := range parts {
		if p == "resources" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// HandleCreatorGetResourceDetail exposes draft data only to the owner or an accepted collaborator.
func HandleCreatorGetResourceDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := creatorResourceID(r)
		var detail json.RawMessage
		err = s.Pool.QueryRow(r.Context(), `
            SELECT jsonb_build_object(
              'resource_id', r.resource_id, 'resource', to_jsonb(r) || jsonb_build_object('id',r.resource_id,'slug',r.resource_id),
              'current_revision', (SELECT to_jsonb(v) || jsonb_build_object('number',v.revision_no) FROM resource_revisions v WHERE v.id=r.current_revision_id),
              'pending_revision', (SELECT to_jsonb(v) || jsonb_build_object('number',v.revision_no) FROM resource_revisions v WHERE v.resource_id=r.resource_id AND v.state IN ('draft','waiting_review','changes_requested') ORDER BY v.revision_no DESC,v.id DESC LIMIT 1),
              'revisions', COALESCE((SELECT jsonb_agg(to_jsonb(v) || jsonb_build_object('number',v.revision_no) ORDER BY v.revision_no DESC,v.id DESC) FROM resource_revisions v WHERE v.resource_id=r.resource_id),'[]'::jsonb),
              'publications', COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.created_at DESC) FROM publications p JOIN resource_revisions v ON v.id=p.revision_id WHERE v.resource_id=r.resource_id),'[]'::jsonb),
              'review', (SELECT to_jsonb(rc) FROM review_cases rc JOIN resource_revisions v ON v.id=rc.revision_id WHERE v.resource_id=r.resource_id ORDER BY rc.updated_at DESC LIMIT 1),
              'bindings', COALESCE((SELECT jsonb_agg(to_jsonb(b) || jsonb_build_object('metadata',b.meta) ORDER BY b.created_at DESC) FROM external_bindings b WHERE b.resource_id=r.resource_id),'[]'::jsonb))
            FROM resources r WHERE r.resource_id=$1 AND (r.owner_uid=$2 OR EXISTS (
              SELECT 1 FROM resource_collaborators c WHERE c.resource_id=r.resource_id AND c.user_uid=$2 AND c.status='accepted'))`, id, uid).Scan(&detail)
		if err == pgx.ErrNoRows {
			httputil.Error(w, 404, "resource_not_found", "资源不存在或无权访问")
			return
		}
		if err != nil {
			httputil.Error(w, 500, "db_error", "读取资源失败")
			return
		}
		httputil.JSON(w, 200, detail)
	}
}

func HandleCreatorResourceSource(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := creatorResourceID(r)
		var req struct {
			AuthorName        string `json:"author_name"`
			SourceURL         string `json:"source_url"`
			LicenseName       string `json:"license_name"`
			AuthorizationNote string `json:"authorization_note"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil || len(req.AuthorName) > 256 || len(req.SourceURL) > 1024 || len(req.LicenseName) > 128 || len(req.AuthorizationNote) > 2000 {
			httputil.Error(w, 400, "invalid_body", "来源信息无效")
			return
		}
		result, err := s.Pool.Exec(r.Context(), `INSERT INTO resource_sources(resource_id,author_name,source_url,license_name,authorization_note,updated_at) SELECT $1,$2,$3,$4,$5,now() WHERE EXISTS (SELECT 1 FROM resources WHERE resource_id=$1 AND owner_uid=$6) ON CONFLICT(resource_id) DO UPDATE SET author_name=$2,source_url=$3,license_name=$4,authorization_note=$5,updated_at=now()`, id, req.AuthorName, req.SourceURL, req.LicenseName, req.AuthorizationNote, uid)
		if err != nil {
			httputil.Error(w, 500, "db_error", "保存来源信息失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 403, "forbidden", "仅资源所有者可修改来源信息")
			return
		}
		httputil.JSON(w, 200, map[string]string{"status": "updated"})
	}
}

func HandleCreatorGetRelationships(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := creatorResourceID(r)
		var owner int
		if s.Pool.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id=$1`, id).Scan(&owner) != nil {
			httputil.Error(w, 404, "resource_not_found", "资源不存在")
			return
		}
		if owner != uid {
			httputil.Error(w, 403, "forbidden", "无权查看资源关系")
			return
		}
		var source struct{ AuthorName, SourceURL, LicenseName, AuthorizationNote string }
		_ = s.Pool.QueryRow(r.Context(), `SELECT author_name,source_url,license_name,authorization_note FROM resource_sources WHERE resource_id=$1`, id).Scan(&source.AuthorName, &source.SourceURL, &source.LicenseName, &source.AuthorizationNote)
		httputil.JSON(w, 200, map[string]any{"source": source})
	}
}

func HandleCreatorResourceState(s *store.Store, restore bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := creatorResourceID(r)
		state := "suspended"
		if restore {
			state = "visible"
		}
		result, err := s.Pool.Exec(r.Context(), `UPDATE resources SET moderation_state=$1,updated_at=now() WHERE resource_id=$2 AND owner_uid=$3 AND ($4 = false OR current_revision_id IS NOT NULL)`, state, id, uid, restore)
		if err != nil {
			httputil.Error(w, 500, "db_error", "更新资源状态失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 404, "resource_not_found", "资源不存在或无权操作")
			return
		}
		httputil.JSON(w, 200, map[string]string{"status": state})
	}
}

func HandleCreatorDeleteResource(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := creatorResourceID(r)
		if external := strings.TrimSpace(r.URL.Query().Get("delete_external")); external != "" {
			httputil.Error(w, http.StatusConflict, "external_delete_requires_review", "外部平台删除需要单独的联发撤回流程，资源尚未删除")
			return
		}
		result, err := s.Pool.Exec(r.Context(), `DELETE FROM resources WHERE resource_id=$1 AND owner_uid=$2`, id, uid)
		if err != nil {
			httputil.Error(w, 500, "db_error", "删除资源失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 404, "resource_not_found", "资源不存在或无权操作")
			return
		}
		httputil.JSON(w, 200, map[string]string{"status": "deleted", "resource_id": id})
	}
}

func HandleCreatorDownloadBlob(s *store.Store, dir, r2 string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := creatorResourceID(r)
		var ok bool
		if s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM resources WHERE resource_id=$1 AND (owner_uid=$2 OR EXISTS(SELECT 1 FROM resource_collaborators c WHERE c.resource_id=$1 AND c.user_uid=$2 AND c.status='accepted')))`, id, uid).Scan(&ok) != nil || !ok {
			httputil.Error(w, 403, "forbidden", "无权读取资源文件")
			return
		}
		sha := r.PathValue("sha256")
		if sha == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			sha = parts[len(parts)-1]
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = "/api/v1/blobs/" + sha
		catalog.HandleServeBlob(s, dir, r2)(w, clone)
	}
}
