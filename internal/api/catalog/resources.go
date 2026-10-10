package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleGetResources implements GET /api/v1/resources (Spec §3.1)
func HandleGetResources(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("query"))
		if q == "" {
			q = strings.TrimSpace(r.URL.Query().Get("q"))
		}
		kind := strings.TrimSpace(r.URL.Query().Get("type"))
		if kind == "" {
			kind = strings.TrimSpace(r.URL.Query().Get("restype"))
		}
		tagsParam := strings.TrimSpace(r.URL.Query().Get("tags"))
		if tagsParam == "" {
			tagsParam = strings.TrimSpace(r.URL.Query().Get("tag"))
		}
		devicesParam := strings.TrimSpace(r.URL.Query().Get("devices"))
		if devicesParam == "" {
			devicesParam = strings.TrimSpace(r.URL.Query().Get("device"))
		}
		sort := strings.TrimSpace(r.URL.Query().Get("sort"))
		seed := httputil.ParseInt(r.URL.Query().Get("seed"), 0)
		hidePaid := r.URL.Query().Get("hide_paid") == "true" || r.URL.Query().Get("hide_paid") == "1"
		hideForcePaid := r.URL.Query().Get("hide_force_paid") == "true" || r.URL.Query().Get("hide_force_paid") == "1"
		featured := r.URL.Query().Get("featured") == "true" || r.URL.Query().Get("featured") == "1"

		limit := httputil.ParseInt(r.URL.Query().Get("limit"), 20)
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		offset := httputil.ParseInt(r.URL.Query().Get("offset"), 0)
		if offset < 0 {
			offset = 0
		}

		query := `SELECT r.resource_id, r.title, r.restype, r.summary, r.authors, r.owner_uid, COALESCE(u.username, ''),
		                 r.devices, r.tags, r.paid_type, r.icon_sha256, r.cover_sha256,
		                 COALESCE(rr.version, '1.0.0'), r.download_count, r.coin_count, r.rating, r.rating_count,
		                 r.curation_grade, to_char(r.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
		          FROM resources r
		          LEFT JOIN users u ON r.owner_uid = u.bandbbs_uid
		          LEFT JOIN resource_revisions rr ON r.current_revision_id = rr.id
		          WHERE r.moderation_state = 'visible'`
		countQuery := `SELECT count(*) FROM resources r WHERE r.moderation_state = 'visible'`
		var args []any

		if q != "" {
			idx := len(args) + 1
			cond := fmt.Sprintf(` AND (r.title ILIKE $%d OR r.summary ILIKE $%d OR r.resource_id ILIKE $%d)`, idx, idx, idx)
			query += cond
			countQuery += cond
			args = append(args, "%"+q+"%")
		}
		if kind != "" {
			idx := len(args) + 1
			cond := fmt.Sprintf(` AND r.restype = $%d`, idx)
			query += cond
			countQuery += cond
			args = append(args, kind)
		}
		if tagsParam != "" {
			tagList := strings.Split(tagsParam, ",")
			for _, t := range tagList {
				t = strings.TrimSpace(t)
				if t != "" {
					idx := len(args) + 1
					cond := fmt.Sprintf(` AND r.tags @> jsonb_build_array($%d)`, idx)
					query += cond
					countQuery += cond
					args = append(args, t)
				}
			}
		}
		if devicesParam != "" {
			devList := strings.Split(devicesParam, ",")
			for _, d := range devList {
				d = strings.TrimSpace(d)
				if d != "" {
					idx := len(args) + 1
					cond := fmt.Sprintf(` AND r.devices @> jsonb_build_array($%d)`, idx)
					query += cond
					countQuery += cond
					args = append(args, d)
				}
			}
		}
		if hidePaid {
			query += ` AND r.paid_type != 'paid' AND r.paid_type != 'force_paid'`
			countQuery += ` AND r.paid_type != 'paid' AND r.paid_type != 'force_paid'`
		}
		if hideForcePaid {
			query += ` AND r.paid_type != 'force_paid'`
			countQuery += ` AND r.paid_type != 'force_paid'`
		}
		if featured {
			query += ` AND r.curation_grade = 'featured'`
			countQuery += ` AND r.curation_grade = 'featured'`
		}

		switch sort {
		case "downloads":
			query += ` ORDER BY r.download_count DESC, r.last_released_at DESC NULLS LAST`
		case "coins":
			query += ` ORDER BY r.coin_count DESC, r.last_released_at DESC NULLS LAST`
		case "name":
			query += ` ORDER BY r.title ASC`
		case "time":
			query += ` ORDER BY r.last_released_at DESC NULLS LAST`
		case "random":
			if seed > 0 {
				query += fmt.Sprintf(` ORDER BY ('x' || substr(md5(r.resource_id || '%d'), 1, 8))::bit(32)::bigint ASC`, seed)
			} else {
				query += ` ORDER BY random()`
			}
		case "recommendation":
			// Spec §3.1.1: 动态综合推荐算法 (冷启动扶持 + 时间衰减 + 投币与下载 + 评分 + 标签权重系数 + 精选保底 + 20%受控随机扰动)
			seedVal := seed
			if seedVal <= 0 {
				seedVal = int(time.Now().Unix() / 86400)
			}

			// Spec §3.1.1 默认推荐因子配置
			coldStartDays := 14.0
			coldStartMaxBoost := 45.0
			timeDecayHalfDays := 45.0
			timeDecayExponent := 1.2
			weightCoins := 6.0
			weightDownloads := 3.0
			weightRating := 2.0
			featuredBonus := 20.0
			jitterRatio := 0.20

			var configJSON []byte
			_ = s.Pool.QueryRow(r.Context(), `SELECT value FROM server_settings WHERE key = 'recommendation_algorithm_config'`).Scan(&configJSON)
			if len(configJSON) > 0 {
				var cfg struct {
					ColdStartDays     float64 `json:"cold_start_days"`
					ColdStartMaxBoost float64 `json:"cold_start_max_boost"`
					TimeDecayHalfDays float64 `json:"time_decay_half_days"`
					TimeDecayExponent float64 `json:"time_decay_exponent"`
					WeightCoins       float64 `json:"weight_coins"`
					WeightDownloads   float64 `json:"weight_downloads"`
					WeightRating      float64 `json:"weight_rating"`
					FeaturedBonus     float64 `json:"featured_bonus"`
					JitterRatio       float64 `json:"jitter_ratio"`
				}
				if json.Unmarshal(configJSON, &cfg) == nil {
					if cfg.ColdStartDays > 0 {
						coldStartDays = cfg.ColdStartDays
					}
					if cfg.ColdStartMaxBoost > 0 {
						coldStartMaxBoost = cfg.ColdStartMaxBoost
					}
					if cfg.TimeDecayHalfDays > 0 {
						timeDecayHalfDays = cfg.TimeDecayHalfDays
					}
					if cfg.TimeDecayExponent > 0 {
						timeDecayExponent = cfg.TimeDecayExponent
					}
					if cfg.WeightCoins > 0 {
						weightCoins = cfg.WeightCoins
					}
					if cfg.WeightDownloads > 0 {
						weightDownloads = cfg.WeightDownloads
					}
					if cfg.WeightRating > 0 {
						weightRating = cfg.WeightRating
					}
					if cfg.FeaturedBonus > 0 {
						featuredBonus = cfg.FeaturedBonus
					}
					if cfg.JitterRatio > 0 {
						jitterRatio = cfg.JitterRatio
					}
				}
			}

			jitterMod := int(jitterRatio*2000.0) + 1
			jitterHalf := int(jitterRatio * 1000.0)

			query += fmt.Sprintf(` ORDER BY (
				(
					(
						CASE 
							WHEN GREATEST(0.0, EXTRACT(EPOCH FROM (now() - COALESCE(r.last_released_at, r.created_at))) / 86400.0) <= 3.0 THEN %f
							WHEN GREATEST(0.0, EXTRACT(EPOCH FROM (now() - COALESCE(r.last_released_at, r.created_at))) / 86400.0) <= %f THEN %f * (1.0 - (EXTRACT(EPOCH FROM (now() - COALESCE(r.last_released_at, r.created_at))) / 86400.0 - 3.0) / (%f - 3.0))
							ELSE 0.0
						END
						+ (%f * (LN(r.coin_count + 1) / 0.693147) + %f * (LN(r.download_count + 1) / 2.302585) + %f * r.rating)
						* POWER(0.5, POWER(GREATEST(0.0, EXTRACT(EPOCH FROM (now() - COALESCE(r.last_released_at, r.created_at))) / 86400.0) / %f, %f))
					)
					* COALESCE((
						SELECT MAX(t.coefficient) 
						FROM tags t 
						WHERE jsonb_typeof(r.tags) = 'array' 
						  AND t.name IN (SELECT jsonb_array_elements_text(r.tags)) 
						  AND t.enabled = true
					), 1.0)
					+ CASE WHEN r.curation_grade = 'featured' THEN %f ELSE 0.0 END
				)
				* (1.0 + (abs(('x' || substr(md5(r.resource_id || '%d'), 1, 8))::bit(32)::bigint) %%%% %d - %d) / 1000.0)
			) DESC, r.last_released_at DESC NULLS LAST`,
				coldStartMaxBoost,
				coldStartDays,
				coldStartMaxBoost,
				coldStartDays,
				weightCoins,
				weightDownloads,
				weightRating,
				timeDecayHalfDays,
				timeDecayExponent,
				featuredBonus,
				seedVal,
				jitterMod,
				jitterHalf,
			)
		default:
			query += ` ORDER BY r.last_released_at DESC NULLS LAST`
		}

		var total int
		err := s.Pool.QueryRow(r.Context(), countQuery, args...).Scan(&total)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		idx1 := len(args) + 1
		idx2 := len(args) + 2
		query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, idx1, idx2)
		args = append(args, limit, offset)

		rows, err := s.Pool.Query(r.Context(), query, args...)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []CatalogItem
		for rows.Next() {
			var it CatalogItem
			var authorsRaw json.RawMessage
			var devicesRaw json.RawMessage
			var tagsRaw json.RawMessage
			var iconSHA string
			var coverSHA string
			var ownerName string

			if err := rows.Scan(&it.ID, &it.Name, &it.Kind, &it.Summary, &authorsRaw, &it.OwnerUID, &ownerName,
				&devicesRaw, &tagsRaw, &it.PaidType, &iconSHA, &coverSHA,
				&it.Version, &it.DownloadCount, &it.CoinCount, &it.Rating, &it.RatingCount,
				&it.CurationGrade, &it.UpdatedAt); err != nil {
				httputil.Error(w, http.StatusInternalServerError, "scan_error", err.Error())
				return
			}

			it.Authors = authorsRaw
			it.IconSHA256 = iconSHA
			it.CoverSHA256 = coverSHA
			_ = json.Unmarshal(devicesRaw, &it.Devices)
			_ = json.Unmarshal(tagsRaw, &it.Tags)
			it.Attributes = it.Tags
			if it.Devices == nil {
				it.Devices = []string{}
			}
			if it.Tags == nil {
				it.Tags = []string{}
			}
			if it.Attributes == nil {
				it.Attributes = []string{}
			}

			var authList []struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(authorsRaw, &authList)
			if len(authList) > 0 && authList[0].Name != "" {
				it.Author = authList[0].Name
			}
			if ownerName != "" {
				it.Owner = ownerName
			} else {
				it.Owner = it.Author
			}

			if iconSHA != "" {
				it.IconURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, iconSHA)
			}
			if coverSHA != "" {
				it.CoverURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, coverSHA)
			}

			items = append(items, it)
		}

		if items == nil {
			items = []CatalogItem{}
		}

		httputil.JSON(w, http.StatusOK, CatalogListResponse{
			Items:   items,
			Total:   total,
			HasMore: (offset + len(items)) < total,
		})
	}
}

// HandleGetResourceDetail implements GET /api/v1/resources/:id (Spec §3.2)
func HandleGetResourceDetail(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					id = parts[idx+1]
					break
				}
			}
		}
		if id == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "缺少资源ID")
			return
		}

		var it ResourceDetailResponse
		var authorsRaw json.RawMessage
		var devicesRaw json.RawMessage
		var tagsRaw json.RawMessage
		var previewsRaw json.RawMessage
		var iconSHA string
		var coverSHA string
		var ownerName string
		var revID *int64

		err := s.Pool.QueryRow(r.Context(), `
			SELECT r.resource_id, r.title, r.restype, r.summary, r.description, r.authors, r.owner_uid, COALESCE(u.username, ''),
			       r.devices, r.tags, r.paid_type, r.icon_sha256, r.cover_sha256, r.preview_shas,
			       r.current_revision_id, r.download_count, r.coin_count, r.rating, r.rating_count,
			       r.curation_grade, to_char(r.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM resources r
			LEFT JOIN users u ON r.owner_uid = u.bandbbs_uid
			WHERE r.resource_id = $1 AND r.moderation_state = 'visible'
		`, id).Scan(&it.ID, &it.Name, &it.Kind, &it.Summary, &it.Description, &authorsRaw, &it.OwnerUID, &ownerName,
			&devicesRaw, &tagsRaw, &it.PaidType, &iconSHA, &coverSHA, &previewsRaw,
			&revID, &it.DownloadCount, &it.CoinCount, &it.Rating, &it.RatingCount,
			&it.CurationGrade, &it.UpdatedAt)

		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "resource_not_found", "资源不存在或已下架")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		it.Authors = authorsRaw
		it.IconSHA256 = iconSHA
		it.CoverSHA256 = coverSHA
		_ = json.Unmarshal(devicesRaw, &it.Devices)
		_ = json.Unmarshal(tagsRaw, &it.Tags)
		it.Attributes = it.Tags
		if it.Devices == nil {
			it.Devices = []string{}
		}
		if it.Tags == nil {
			it.Tags = []string{}
		}
		if it.Attributes == nil {
			it.Attributes = []string{}
		}

		var authList []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(authorsRaw, &authList)
		if len(authList) > 0 && authList[0].Name != "" {
			it.Author = authList[0].Name
		}
		if ownerName != "" {
			it.Owner = ownerName
		} else {
			it.Owner = it.Author
		}

		if iconSHA != "" {
			it.IconURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, iconSHA)
		}
		if coverSHA != "" {
			it.CoverURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, coverSHA)
		}

		var previewSHAs []string
		_ = json.Unmarshal(previewsRaw, &previewSHAs)
		it.PreviewURLs = make([]string, 0, len(previewSHAs))
		for _, sha := range previewSHAs {
			if sha != "" {
				it.PreviewURLs = append(it.PreviewURLs, fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, sha))
			}
		}

		it.Downloads = make(map[string]DownloadTarget)
		if revID != nil {
			var rawDownloads json.RawMessage
			_ = s.Pool.QueryRow(r.Context(), `
				SELECT version, changelog, downloads 
				FROM resource_revisions 
				WHERE id = $1
			`, *revID).Scan(&it.Version, &it.Changelog, &rawDownloads)

			var dlMap map[string]struct {
				BlobSHA256   string `json:"blob_sha256"`
				OriginalName string `json:"original_name"`
			}
			_ = json.Unmarshal(rawDownloads, &dlMap)

			for dev, item := range dlMap {
				var sizeBytes int64
				_ = s.Pool.QueryRow(r.Context(), `SELECT size_bytes FROM blobs WHERE sha256 = $1`, item.BlobSHA256).Scan(&sizeBytes)

				it.Downloads[dev] = DownloadTarget{
					DownloadURL: fmt.Sprintf("%s/api/v1/resources/%s/download/%s", baseURL, it.ID, dev),
					SHA256:      item.BlobSHA256,
					SizeBytes:   sizeBytes,
					FileName:    item.OriginalName,
					VersionCode: 1,
				}
			}
		}

		httputil.JSON(w, http.StatusOK, it)
	}
}

// HandleDownloadDeviceRedirect implements GET /api/v1/resources/:id/download/:device (Spec §3.3)
func HandleDownloadDeviceRedirect(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resID := r.PathValue("id")
		device := r.PathValue("device")
		if resID == "" || device == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) >= 6 {
				resID = parts[3]
				device = parts[5]
			}
		}
		if resID == "" || device == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}

		if s == nil || s.Pool == nil {
			httputil.Error(w, http.StatusInternalServerError, "store_unavailable", "数据库未连接")
			return
		}

		var revID *int64
		err := s.Pool.QueryRow(r.Context(), `
			SELECT current_revision_id FROM resources WHERE resource_id = $1 AND moderation_state = 'visible'
		`, resID).Scan(&revID)
		if err == pgx.ErrNoRows || revID == nil {
			httputil.Error(w, http.StatusNotFound, "resource_not_found", "资源不存在")
			return
		}

		var rawDownloads json.RawMessage
		_ = s.Pool.QueryRow(r.Context(), `SELECT downloads FROM resource_revisions WHERE id = $1`, *revID).Scan(&rawDownloads)

		var dlMap map[string]struct {
			BlobSHA256 string `json:"blob_sha256"`
		}
		_ = json.Unmarshal(rawDownloads, &dlMap)
		target, ok := dlMap[device]
		if !ok || target.BlobSHA256 == "" {
			httputil.Error(w, http.StatusNotFound, "device_not_supported", "此资源未提供该机型安装包")
			return
		}

		// Track download event
		ip := strings.Split(r.RemoteAddr, ":")[0]
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = strings.TrimSpace(strings.Split(fwd, ",")[0])
		}

		var userUID *int
		if token := httputil.ExtractBearerToken(r); token != "" {
			if uid, err := httputil.AuthenticateUser(r.Context(), s, token); err == nil && uid > 0 {
				userUID = &uid
			}
		}

		res, err := s.Pool.Exec(r.Context(), `
			INSERT INTO download_events (resource_id, device, user_uid, ip, user_agent, download_date)
			VALUES ($1, $2, $3, $4, $5, CURRENT_DATE)
			ON CONFLICT (resource_id, ip, device, download_date) DO NOTHING
		`, resID, device, userUID, ip, r.UserAgent())

		if err == nil && res.RowsAffected() > 0 {
			_, _ = s.Pool.Exec(r.Context(), `
				UPDATE resources SET download_count = download_count + 1 WHERE resource_id = $1
			`, resID)
		}

		// 302 Redirect to Blob download endpoint
		targetURL := fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, target.BlobSHA256)
		http.Redirect(w, r, targetURL, http.StatusFound)
	}
}
