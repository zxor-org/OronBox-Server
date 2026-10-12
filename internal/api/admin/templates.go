package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func validTemplate(item TemplateItem) bool {
	return strings.TrimSpace(item.Scope) != "" && len(item.Scope) <= 32 && len(item.Decision) <= 16 &&
		strings.TrimSpace(item.Title) != "" && len([]rune(item.Title)) <= 128 &&
		strings.TrimSpace(item.Body) != "" && len([]rune(item.Body)) <= 20000
}

// HandleAdminSaveTemplate creates or updates a moderation reason template.
func HandleAdminSaveTemplate(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未授权")
			return
		}
		var item TemplateItem
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		if json.NewDecoder(r.Body).Decode(&item) != nil || !validTemplate(item) {
			httputil.Error(w, 400, "invalid_body", "模板字段无效")
			return
		}
		if r.Method == http.MethodPut {
			id, parseErr := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/templates/"), 10, 64)
			if parseErr != nil || id <= 0 {
				httputil.Error(w, 400, "invalid_id", "模板ID无效")
				return
			}
			result, e := s.Pool.Exec(r.Context(), `UPDATE moderation_reason_templates SET scope=$2, decision=$3, title=$4, body=$5, position=$6, enabled=$7 WHERE id=$1`, id, item.Scope, item.Decision, item.Title, item.Body, item.Position, item.Enabled)
			if e != nil {
				httputil.Error(w, 500, "db_error", "更新模板失败")
				return
			}
			if result.RowsAffected() == 0 {
				httputil.Error(w, 404, "not_found", "模板不存在")
				return
			}
			item.ID = id
		} else {
			err = s.Pool.QueryRow(r.Context(), `INSERT INTO moderation_reason_templates (scope,decision,title,body,position,enabled) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, item.Scope, item.Decision, item.Title, item.Body, item.Position, item.Enabled).Scan(&item.ID)
			if err != nil {
				httputil.Error(w, 500, "db_error", "创建模板失败")
				return
			}
		}
		target, _ := json.Marshal(map[string]any{"id": item.ID})
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid,action,result,ip,target_data) VALUES ($1,'save_reason_template','success',$2,$3)`, uid, r.RemoteAddr, target)
		httputil.JSON(w, 200, item)
	}
}

func HandleAdminDeleteTemplate(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未授权")
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/templates/"), 10, 64)
		if err != nil || id <= 0 {
			httputil.Error(w, 400, "invalid_id", "模板ID无效")
			return
		}
		result, err := s.Pool.Exec(r.Context(), `DELETE FROM moderation_reason_templates WHERE id=$1`, id)
		if err != nil {
			httputil.Error(w, 500, "db_error", "删除模板失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 404, "not_found", "模板不存在")
			return
		}
		target, _ := json.Marshal(map[string]any{"id": id})
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid,action,result,ip,target_data) VALUES ($1,'delete_reason_template','success',$2,$3)`, uid, r.RemoteAddr, target)
		httputil.JSON(w, 200, map[string]string{"status": "deleted"})
	}
}
