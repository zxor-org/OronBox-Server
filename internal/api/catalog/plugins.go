package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type PluginItem struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Author        string          `json:"author"`
	Description   string          `json:"description"`
	UploaderUID   int             `json:"uploader_uid"`
	Runtime       string          `json:"runtime"`
	Permissions   json.RawMessage `json:"permissions"`
	State         string          `json:"state"`
	Version       string          `json:"version,omitempty"`
	PackageSHA256 string          `json:"package_sha256,omitempty"`
}

// HandleGetPlugins implements GET /api/v1/plugins
func HandleGetPlugins(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := s.Pool.Query(r.Context(), `
			SELECT p.id, p.name, p.author, p.description, p.uploader_uid, p.runtime, p.permissions, p.state,
			       COALESCE(pv.version, ''), COALESCE(pv.package_sha256, '')
			FROM plugins p
			LEFT JOIN plugin_versions pv ON p.current_version_id = pv.id
			WHERE p.state = 'visible'
			ORDER BY p.created_at DESC
		`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var list []PluginItem
		for rows.Next() {
			var p PluginItem
			if err := rows.Scan(&p.ID, &p.Name, &p.Author, &p.Description, &p.UploaderUID, &p.Runtime, &p.Permissions, &p.State, &p.Version, &p.PackageSHA256); err == nil {
				list = append(list, p)
			}
		}
		if list == nil {
			list = []PluginItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// HandleGetPluginDetail implements GET /api/v1/plugins/{id}
func HandleGetPluginDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/")
		id = strings.Trim(id, "/")

		var p PluginItem
		err := s.Pool.QueryRow(r.Context(), `
			SELECT p.id, p.name, p.author, p.description, p.uploader_uid, p.runtime, p.permissions, p.state,
			       COALESCE(pv.version, ''), COALESCE(pv.package_sha256, '')
			FROM plugins p
			LEFT JOIN plugin_versions pv ON p.current_version_id = pv.id
			WHERE p.id = $1 AND p.state = 'visible'
		`, id).Scan(&p.ID, &p.Name, &p.Author, &p.Description, &p.UploaderUID, &p.Runtime, &p.Permissions, &p.State, &p.Version, &p.PackageSHA256)

		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "plugin_not_found", "插件不存在")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, p)
	}
}

// HandleDownloadPluginPackage implements GET /api/v1/plugins/:id/package (Spec §4.2)
func HandleDownloadPluginPackage(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "plugins" && idx+1 < len(parts) {
					id = parts[idx+1]
					break
				}
			}
		}

		var pkgSHA string
		err := s.Pool.QueryRow(r.Context(), `
			SELECT COALESCE(pv.package_sha256, '')
			FROM plugins p
			JOIN plugin_versions pv ON p.current_version_id = pv.id
			WHERE p.id = $1 AND p.state = 'visible'
		`, id).Scan(&pkgSHA)

		if err == pgx.ErrNoRows || pkgSHA == "" {
			httputil.Error(w, http.StatusNotFound, "plugin_not_found", "插件安装包不存在")
			return
		}

		http.Redirect(w, r, fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, pkgSHA), http.StatusFound)
	}
}
