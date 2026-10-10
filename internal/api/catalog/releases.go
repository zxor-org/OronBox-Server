package catalog

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleGetLatestRelease implements GET /api/v1/releases/latest and GET /api/v1/app/releases
func HandleGetLatestRelease(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var it ReleaseItem
		err := s.Pool.QueryRow(r.Context(), `
			SELECT version, minimum_version, notes_zh, notes_en, 
			       to_char(published_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM app_releases
			ORDER BY published_at DESC
			LIMIT 1
		`).Scan(&it.Version, &it.MinimumVersion, &it.NotesZH, &it.NotesEN, &it.PublishedAt)

		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "release_not_found", "暂无可用发布版本")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, it)
	}
}
