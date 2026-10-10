package catalog

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleGetTags implements GET /api/v1/tags?scene=filter|creator
func HandleGetTags(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scene := r.URL.Query().Get("scene")
		query := `SELECT id, name, coefficient, usage_count FROM tags WHERE enabled = true`
		if scene == "filter" {
			// Spec §3.5: 客户端筛选严格排除 0 引用标签
			query += ` AND usage_count > 0`
		}
		query += ` ORDER BY sort_order ASC, usage_count DESC`

		rows, err := s.Pool.Query(r.Context(), query)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var list []TagItem
		for rows.Next() {
			var t TagItem
			if err := rows.Scan(&t.ID, &t.Name, &t.Coefficient, &t.UsageCount); err == nil {
				list = append(list, t)
			}
		}
		if list == nil {
			list = []TagItem{}
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"items": list})
	}
}
