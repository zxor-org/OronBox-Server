package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type AuditItem struct {
	ID         int64           `json:"id"`
	ActorUID   *int            `json:"actor_uid"`
	Action     string          `json:"action"`
	Result     string          `json:"result"`
	IP         *string         `json:"ip"`
	TargetData json.RawMessage `json:"target_data"`
	CreatedAt  string          `json:"created_at"`
}

func HandleAdminGetAudit(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := httputil.AuthenticateAdmin(r.Context(), s, r); err != nil {
			httputil.Error(w, 401, "unauthorized", "未授权")
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size < 1 {
			size = 100
		}
		if size > 1000 {
			size = 1000
		}
		if page > 1000000 {
			page = 1000000
		}
		var total int64
		if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM audit_logs`).Scan(&total); err != nil {
			httputil.Error(w, 500, "db_error", "读取审计记录失败")
			return
		}
		rows, err := s.Pool.Query(r.Context(), `SELECT id,actor_uid,action,result,ip,target_data,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"') FROM audit_logs ORDER BY id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
		if err != nil {
			httputil.Error(w, 500, "db_error", "读取审计记录失败")
			return
		}
		defer rows.Close()
		items := []AuditItem{}
		for rows.Next() {
			var item AuditItem
			if err := rows.Scan(&item.ID, &item.ActorUID, &item.Action, &item.Result, &item.IP, &item.TargetData, &item.CreatedAt); err != nil {
				httputil.Error(w, 500, "db_error", "读取审计记录失败")
				return
			}
			items = append(items, item)
		}
		if rows.Err() != nil {
			httputil.Error(w, 500, "db_error", "读取审计记录失败")
			return
		}
		httputil.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "size": size})
	}
}
