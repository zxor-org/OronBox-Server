package creator

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/api/catalog"
	"github.com/zxor-org/OronBox-Server/internal/attestation"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

type CreatorSubmitRequest struct {
	ResourceID  string          `json:"resource_id"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Restype     string          `json:"restype"`
	PaidType    string          `json:"paid_type"`
	Version     string          `json:"version"`
	Changelog   string          `json:"changelog"`
	IconSHA256  string          `json:"icon_sha256"`
	CoverSHA256 string          `json:"cover_sha256"`
	PreviewSHAs []string        `json:"preview_shas"`
	Downloads   json.RawMessage `json:"downloads"`
	Tags        []string        `json:"tags"`
}

type CreatorResourceItem struct {
	ResourceID      string  `json:"resource_id"`
	Title           string  `json:"title"`
	Summary         string  `json:"summary"`
	Restype         string  `json:"restype"`
	ModerationState string  `json:"moderation_state"`
	CurrentVersion  string  `json:"current_version,omitempty"`
	PendingVersion  string  `json:"pending_version,omitempty"`
	DownloadCount   int     `json:"download_count"`
	CoinCount       int     `json:"coin_count"`
	Rating          float64 `json:"rating"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type creatorDraftRequest struct {
	Version          string           `json:"version"`
	Name             string           `json:"name"`
	Summary          string           `json:"summary"`
	Changelog        string           `json:"changelog"`
	Downloads        json.RawMessage  `json:"downloads"`
	IconSHA256       string           `json:"icon_sha256"`
	CoverSHA256      string           `json:"cover_sha256"`
	PreviewSHAs      []string         `json:"preview_shas"`
	Tags             []string         `json:"tags"`
	Attributes       []string         `json:"attributes"`
	Links            []map[string]any `json:"links"`
	Publications     []map[string]any `json:"publications"`
	PaidType         string           `json:"paid_type"`
	PurchaseLink     string           `json:"purchase_link"`
	PurchasePrice    any              `json:"purchase_price"`
	PurchaseCurrency string           `json:"purchase_currency"`
}

func decodeCreatorBundle(ctx context.Context, data []byte, storageDir string, r2 *catalog.R2Client, req *creatorDraftRequest, s *store.Store) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid_bundle: %w", err)
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		clean := filepath.Clean(file.Name)
		if file.FileInfo().IsDir() || clean != file.Name || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		files[file.Name] = file
	}
	manifestFile := files["manifest.json"]
	if manifestFile == nil {
		return fmt.Errorf("invalid_bundle: manifest.json missing")
	}
	reader, err := manifestFile.Open()
	if err != nil {
		return err
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(reader, 2<<20))
	_ = reader.Close()
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("invalid_bundle_manifest: %w", err)
	}
	req.Name = strings.TrimSpace(fmt.Sprint(manifest["name"]))
	req.Summary = strings.TrimSpace(fmt.Sprint(manifest["summary"]))
	req.Changelog = strings.TrimSpace(fmt.Sprint(manifest["changelog"]))
	if tags, ok := manifest["tags"].([]any); ok {
		for _, tag := range tags {
			req.Tags = append(req.Tags, strings.TrimSpace(fmt.Sprint(tag)))
		}
	}
	if attributes, ok := manifest["attributes"].([]any); ok {
		for _, attribute := range attributes {
			if value := strings.TrimSpace(fmt.Sprint(attribute)); value != "" {
				req.Attributes = append(req.Attributes, value)
				req.Tags = append(req.Tags, value)
			}
		}
	}
	if links, ok := manifest["links"].([]any); ok {
		for _, raw := range links {
			if link, ok := raw.(map[string]any); ok {
				req.Links = append(req.Links, link)
			}
		}
	}
	if publications, ok := manifest["publications"].([]any); ok {
		for _, raw := range publications {
			if publication, ok := raw.(map[string]any); ok {
				req.Publications = append(req.Publications, publication)
			}
		}
	}
	if value, ok := manifest["paid_type"].(string); ok {
		req.PaidType = strings.TrimSpace(value)
	}
	if value, ok := manifest["purchase_link"].(string); ok {
		req.PurchaseLink = strings.TrimSpace(value)
	}
	req.PurchasePrice = manifest["purchase_price"]
	if value, ok := manifest["purchase_currency"].(string); ok {
		req.PurchaseCurrency = strings.TrimSpace(value)
	}
	readAsset := func(path, mediaType string) (string, int64, error) {
		file := files[path]
		if file == nil {
			return "", 0, fmt.Errorf("bundle file missing: %s", path)
		}
		asset, openErr := file.Open()
		if openErr != nil {
			return "", 0, openErr
		}
		defer asset.Close()
		sha, size, _, persistErr := catalog.PersistUploadedBlob(ctx, s, storageDir, io.LimitReader(asset, 100<<20), mediaType, r2)
		return sha, size, persistErr
	}
	media, _ := manifest["media"].(map[string]any)
	if icon, ok := media["icon"].(map[string]any); ok {
		sha, _, err := readAsset(strings.TrimSpace(fmt.Sprint(icon["file"])), "image/webp")
		if err != nil {
			return err
		}
		req.IconSHA256 = sha
	}
	if cover, ok := media["cover"].(map[string]any); ok {
		sha, _, err := readAsset(strings.TrimSpace(fmt.Sprint(cover["file"])), "image/webp")
		if err != nil {
			return err
		}
		req.CoverSHA256 = sha
	}
	if previews, ok := media["previews"].([]any); ok {
		for _, raw := range previews {
			preview, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sha, _, err := readAsset(strings.TrimSpace(fmt.Sprint(preview["file"])), "image/webp")
			if err != nil {
				return err
			}
			req.PreviewSHAs = append(req.PreviewSHAs, sha)
		}
	}
	artifacts := map[string]map[string]any{}
	if entries, ok := manifest["artifacts"].([]any); ok {
		for _, raw := range entries {
			artifact, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sha, size, err := readAsset(strings.TrimSpace(fmt.Sprint(artifact["file"])), "application/octet-stream")
			if err != nil {
				return err
			}
			name := filepath.Base(strings.TrimSpace(fmt.Sprint(artifact["original_name"])))
			version := strings.TrimSpace(fmt.Sprint(artifact["package_version"]))
			if req.Version == "" && validSemver(version) {
				req.Version = version
			}
			devices, _ := artifact["device_ids"].([]any)
			packageID := strings.TrimSpace(fmt.Sprint(artifact["package_id"]))
			artifactType := strings.TrimSpace(fmt.Sprint(artifact["type"]))
			for _, device := range devices {
				deviceID := strings.TrimSpace(fmt.Sprint(device))
				if deviceID != "" {
					artifacts[deviceID] = map[string]any{
						"blob_sha256": sha, "original_name": name, "size_bytes": size,
						"package_id": packageID, "package_version": version, "type": artifactType,
					}
				}
			}
		}
	}
	if len(artifacts) > 0 {
		req.Downloads, _ = json.Marshal(artifacts)
	}
	return nil
}

func validSemver(v string) bool {
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func validateSummary(summary string) error {
	n := len([]rune(strings.TrimSpace(summary)))
	if n < 2 || n > 80 {
		return fmt.Errorf("summary_must_be_2_to_80_chars")
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func validateDownloads(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var artifacts map[string]struct {
		BlobSHA256   string `json:"blob_sha256"`
		OriginalName string `json:"original_name"`
	}
	if err := json.Unmarshal(raw, &artifacts); err != nil || len(artifacts) == 0 {
		return fmt.Errorf("downloads_required")
	}
	for device, artifact := range artifacts {
		if strings.TrimSpace(device) == "" || len(artifact.BlobSHA256) != 64 {
			return fmt.Errorf("invalid_download_artifact")
		}
		if _, err := hex.DecodeString(artifact.BlobSHA256); err != nil {
			return fmt.Errorf("invalid_download_artifact")
		}
		name := strings.ToLower(filepath.Base(strings.TrimSpace(artifact.OriginalName)))
		if name == "" || (!strings.HasSuffix(name, ".rpk") && !strings.HasSuffix(name, ".bin")) {
			return fmt.Errorf("invalid_download_filename")
		}
		var size int64
		if err := tx.QueryRow(ctx, `SELECT size_bytes FROM blobs WHERE sha256 = $1`, artifact.BlobSHA256).Scan(&size); err != nil || size <= 0 || size > 100<<20 {
			return fmt.Errorf("invalid_download_artifact")
		}
	}
	return nil
}

func validateStoredPackages(baseDir, resourceID string, raw json.RawMessage) error {
	var artifacts map[string]struct {
		BlobSHA256   string `json:"blob_sha256"`
		OriginalName string `json:"original_name"`
	}
	if err := json.Unmarshal(raw, &artifacts); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if !strings.HasSuffix(strings.ToLower(artifact.OriginalName), ".rpk") {
			continue
		}
		path := filepath.Join(baseDir, artifact.BlobSHA256[:2], artifact.BlobSHA256[2:4], artifact.BlobSHA256)
		data, err := os.ReadFile(path)
		if err != nil {
			data, err = os.ReadFile(filepath.Join(baseDir, artifact.BlobSHA256))
		}
		if err != nil {
			return fmt.Errorf("package_blob_unavailable")
		}
		if err := syndication.ValidateQuickAppPackage(data, resourceID); err != nil {
			return fmt.Errorf("invalid_package: %w", err)
		}
	}
	return nil
}

func validateStoredIcon(baseDir string, cfg map[string]any) error {
	metadata, _ := cfg["draft_metadata"].(map[string]any)
	sha, _ := metadata["icon_sha256"].(string)
	if sha == "" {
		return fmt.Errorf("icon_required")
	}
	path := filepath.Join(baseDir, sha[:2], sha[2:4], sha)
	data, err := os.ReadFile(path)
	if err != nil {
		data, err = os.ReadFile(filepath.Join(baseDir, sha))
	}
	if err != nil || syndication.ValidateIconWebP(data) != nil {
		return fmt.Errorf("icon_must_be_512_webp")
	}
	return nil
}

func validateMetadata(ctx context.Context, tx pgx.Tx, cfg map[string]any) error {
	metadata, _ := cfg["draft_metadata"].(map[string]any)
	if metadata == nil {
		return fmt.Errorf("metadata_required")
	}
	icon, _ := metadata["icon_sha256"].(string)
	if strings.TrimSpace(icon) == "" {
		return fmt.Errorf("icon_required")
	}
	for _, key := range []string{"icon_sha256", "cover_sha256"} {
		if sha, _ := metadata[key].(string); sha != "" {
			if len(sha) != 64 {
				return fmt.Errorf("invalid_metadata_blob")
			}
			if _, err := hex.DecodeString(sha); err != nil {
				return fmt.Errorf("invalid_metadata_blob")
			}
			var mediaType string
			if err := tx.QueryRow(ctx, `SELECT media_type FROM blobs WHERE sha256 = $1`, sha).Scan(&mediaType); err != nil {
				return fmt.Errorf("metadata_blob_not_found")
			}
			if key == "icon_sha256" && !strings.EqualFold(strings.TrimSpace(strings.Split(mediaType, ";")[0]), "image/webp") {
				return fmt.Errorf("icon_must_be_webp")
			}
		}
	}
	if previews, ok := metadata["preview_shas"].([]any); ok {
		for _, item := range previews {
			sha, _ := item.(string)
			if len(sha) != 64 {
				return fmt.Errorf("invalid_metadata_blob")
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blobs WHERE sha256 = $1)`, sha).Scan(&exists); err != nil || !exists {
				return fmt.Errorf("metadata_blob_not_found")
			}
		}
	}
	return nil
}

// HandleCreatorGetResources implements GET /api/v1/creator/resources (Spec §12.1)
func HandleCreatorGetResources(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT r.resource_id, r.title, r.summary, r.restype, r.moderation_state,
			       COALESCE(cur_rev.version, ''),
			       COALESCE(pend_rev.version, ''),
			       r.download_count, r.coin_count, r.rating,
			       to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(r.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM resources r
			LEFT JOIN resource_revisions cur_rev ON r.current_revision_id = cur_rev.id
			LEFT JOIN LATERAL (
				SELECT version FROM resource_revisions 
				WHERE resource_id = r.resource_id AND state IN ('waiting_review', 'draft', 'changes_requested')
				ORDER BY id DESC LIMIT 1
			) pend_rev ON true
			WHERE r.owner_uid = $1
			   OR EXISTS (SELECT 1 FROM resource_collaborators rc WHERE rc.resource_id = r.resource_id AND rc.user_uid = $1 AND rc.status = 'accepted')
			ORDER BY r.updated_at DESC
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []CreatorResourceItem
		for rows.Next() {
			var it CreatorResourceItem
			if err := rows.Scan(&it.ResourceID, &it.Title, &it.Summary, &it.Restype, &it.ModerationState,
				&it.CurrentVersion, &it.PendingVersion, &it.DownloadCount, &it.CoinCount, &it.Rating,
				&it.CreatedAt, &it.UpdatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []CreatorResourceItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleCreatorCreateResource implements POST /api/v1/creator/resources (Spec §12.2)
func HandleCreatorCreateResource(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var req struct {
			ResourceID string `json:"resource_id"`
			Title      string `json:"title"`
			Summary    string `json:"summary"`
			Restype    string `json:"restype"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		req.ResourceID = strings.TrimSpace(req.ResourceID)
		req.Title = strings.TrimSpace(req.Title)
		if req.ResourceID == "" || req.Title == "" {
			httputil.Error(w, http.StatusBadRequest, "missing_fields", "缺少 resource_id 或 title")
			return
		}
		if req.Restype == "" {
			req.Restype = "quick_app"
		}

		authorsJSON, _ := json.Marshal([]map[string]any{{"uid": userUID}})
		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO resources (resource_id, title, summary, restype, owner_uid, authors, moderation_state)
			VALUES ($1, $2, $3, $4, $5, $6, 'suspended')
		`, req.ResourceID, req.Title, req.Summary, req.Restype, userUID, authorsJSON)

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "create_failed", "资源ID已存在或创建失败")
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"resource_id": req.ResourceID,
			"status":      "created",
		})
	}
}

// HandleCreatorSaveDraft implements PUT /api/v1/creator/resources/:id/draft (Spec §12.3)
func HandleCreatorSaveDraft(s *store.Store, options ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		resID := r.PathValue("id")
		if resID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					resID = parts[idx+1]
					break
				}
			}
		}

		var req creatorDraftRequest
		storageDir := "/var/lib/oronbox/blobs"
		var r2 *catalog.R2Client
		for _, option := range options {
			switch value := option.(type) {
			case string:
				if strings.TrimSpace(value) != "" {
					storageDir = value
				}
			case *catalog.R2Client:
				r2 = value
			}
		}
		// Authorize before unpacking a bundle. Blob persistence happens while
		// decoding the zip, so checking ownership first prevents an attacker from
		// using an arbitrary resource ID to create orphaned blobs.
		var ownerUID int
		if err := s.Pool.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID); err != nil {
			httputil.Error(w, http.StatusNotFound, "resource_not_found", "资源不存在")
			return
		}
		if ownerUID != userUID {
			var collaborator bool
			_ = s.Pool.QueryRow(r.Context(), `SELECT true FROM resource_collaborators WHERE resource_id = $1 AND user_uid = $2 AND status = 'accepted'`, resID, userUID).Scan(&collaborator)
			if !collaborator {
				httputil.Error(w, http.StatusForbidden, "forbidden", "无权编辑该资源")
				return
			}
		}
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			r.Body = http.MaxBytesReader(w, r.Body, 110<<20)
			if err := r.ParseMultipartForm(110 << 20); err != nil {
				httputil.Error(w, http.StatusBadRequest, "invalid_body", "multipart 参数格式错误")
				return
			}
			form := r.MultipartForm.Value
			get := func(key string) string {
				if values := form[key]; len(values) > 0 {
					return values[0]
				}
				return ""
			}
			req.Version, req.Name, req.Summary, req.Changelog = get("version"), get("name"), get("summary"), get("changelog")
			req.IconSHA256, req.CoverSHA256 = get("icon_sha256"), get("cover_sha256")
			if raw := get("downloads"); raw != "" {
				req.Downloads = json.RawMessage(raw)
			}
			if raw := get("preview_shas"); raw != "" {
				_ = json.Unmarshal([]byte(raw), &req.PreviewSHAs)
			}
			if raw := get("tags"); raw != "" {
				_ = json.Unmarshal([]byte(raw), &req.Tags)
			}
			artifacts := map[string]map[string]any{}
			if len(req.Downloads) > 0 {
				_ = json.Unmarshal(req.Downloads, &artifacts)
			}
			for field, headers := range r.MultipartForm.File {
				for idx, header := range headers {
					file, openErr := header.Open()
					if openErr != nil {
						httputil.Error(w, http.StatusBadRequest, "invalid_file", "无法读取上传文件")
						return
					}
					var source io.Reader = file
					if field == "icon" {
						data, readErr := io.ReadAll(io.LimitReader(file, (100<<20)+1))
						if readErr != nil || len(data) > 100<<20 || syndication.ValidateIconWebP(data) != nil {
							_ = file.Close()
							httputil.Error(w, http.StatusBadRequest, "invalid_icon", "图标必须是 512x512 WebP")
							return
						}
						source = bytes.NewReader(data)
					}
					sha, size, _, saveErr := catalog.PersistUploadedBlob(r.Context(), s, storageDir, source, header.Header.Get("Content-Type"), r2)
					_ = file.Close()
					if saveErr != nil {
						httputil.Error(w, http.StatusBadRequest, "upload_failed", "附件保存失败")
						return
					}
					switch {
					case field == "icon":
						req.IconSHA256 = sha
					case field == "cover":
						req.CoverSHA256 = sha
					case field == "preview":
						req.PreviewSHAs = append(req.PreviewSHAs, sha)
					case strings.HasPrefix(field, "download_"):
						device := strings.TrimPrefix(field, "download_")
						if device == "" {
							httputil.Error(w, http.StatusBadRequest, "invalid_file", "缺少下载机型")
							return
						}
						artifacts[device] = map[string]any{"blob_sha256": sha, "original_name": filepath.Base(header.Filename), "size_bytes": size}
					case field == "download":
						devices := form["device"]
						if idx >= len(devices) || strings.TrimSpace(devices[idx]) == "" {
							httputil.Error(w, http.StatusBadRequest, "invalid_file", "download 缺少 device")
							return
						}
						artifacts[devices[idx]] = map[string]any{"blob_sha256": sha, "original_name": filepath.Base(header.Filename), "size_bytes": size}
					}
				}
			}
			if len(artifacts) > 0 {
				req.Downloads, _ = json.Marshal(artifacts)
			}
		} else if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/zip") {
			body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 110<<20))
			if readErr != nil {
				httputil.Error(w, http.StatusBadRequest, "invalid_body", "无法读取资源包")
				return
			}
			if err := decodeCreatorBundle(r.Context(), body, storageDir, r2, &req, s); err != nil {
				httputil.Error(w, http.StatusBadRequest, "invalid_bundle", err.Error())
				return
			}
		} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		if req.Version != "" && !validSemver(req.Version) {
			httputil.Error(w, http.StatusBadRequest, "invalid_version", "版本号必须为 x.y.z")
			return
		}
		if req.Summary != "" {
			if err := validateSummary(req.Summary); err != nil {
				httputil.Error(w, http.StatusBadRequest, "invalid_summary", "摘要长度必须为 2-80 个字符")
				return
			}
		}
		if len(req.Downloads) > 0 {
			var artifacts map[string]any
			if json.Unmarshal(req.Downloads, &artifacts) != nil {
				httputil.Error(w, http.StatusBadRequest, "invalid_downloads", "downloads 格式错误")
				return
			}
		}

		var revID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Check ownership
			var ownerUID int
			if err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID); err != nil {
				return err
			}
			if ownerUID != userUID {
				var allowed bool
				_ = tx.QueryRow(r.Context(), `
					SELECT true FROM resource_collaborators WHERE resource_id = $1 AND user_uid = $2 AND status = 'accepted'
				`, resID, userUID).Scan(&allowed)
				if !allowed {
					return fmt.Errorf("forbidden")
				}
			}

			if req.Version == "" {
				req.Version = "1.0.0"
			}
			var existingState string
			_ = tx.QueryRow(r.Context(), `SELECT state FROM resource_revisions WHERE resource_id = $1 AND version = $2`, resID, req.Version).Scan(&existingState)
			if existingState == "approved" || existingState == "superseded" || existingState == "waiting_review" {
				return fmt.Errorf("version_not_editable")
			}
			if req.Downloads == nil {
				req.Downloads = json.RawMessage("{}")
			}
			if req.PreviewSHAs == nil {
				req.PreviewSHAs = []string{}
			}
			if req.Tags == nil {
				req.Tags = []string{}
			}
			metadata := map[string]any{
				"icon_sha256": req.IconSHA256, "cover_sha256": req.CoverSHA256,
				"preview_shas": req.PreviewSHAs, "tags": req.Tags,
				"attributes": req.Attributes, "links": req.Links,
				"publications": req.Publications, "paid_type": req.PaidType,
				"purchase_link": req.PurchaseLink, "purchase_price": req.PurchasePrice,
				"purchase_currency": req.PurchaseCurrency,
			}
			draftConfig, _ := json.Marshal(map[string]any{
				"draft_metadata":   metadata,
				"publication_plan": req.Publications,
			})

			// Insert draft revision
			return tx.QueryRow(r.Context(), `
				INSERT INTO resource_revisions (resource_id, version, name, summary, changelog, downloads, config, state, creator_uid)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8)
				ON CONFLICT (resource_id, version) DO UPDATE
				SET name = EXCLUDED.name, summary = EXCLUDED.summary, changelog = EXCLUDED.changelog,
				    downloads = EXCLUDED.downloads, config = EXCLUDED.config,
				    state = CASE WHEN resource_revisions.state IN ('approved', 'superseded') THEN resource_revisions.state ELSE 'draft' END,
				    updated_at = now()
				WHERE resource_revisions.state IN ('draft', 'changes_requested', 'rejected')
				RETURNING id
			`, resID, req.Version, req.Name, req.Summary, req.Changelog, req.Downloads, draftConfig, userUID).Scan(&revID)
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "save_draft_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "draft_saved",
			"revision_id": revID,
		})
	}
}

// HandleCreatorPublishResource implements POST /api/v1/creator/resources/:id/publish (Spec §12.4)
func HandleCreatorPublishResource(s *store.Store, storageDirs ...string) http.HandlerFunc {
	storageDir := "/var/lib/oronbox/blobs"
	configDir := ""
	if len(storageDirs) > 1 {
		configDir = storageDirs[1]
	}
	if len(storageDirs) > 0 && strings.TrimSpace(storageDirs[0]) != "" {
		storageDir = storageDirs[0]
	}
	return handleCreatorPublishResource(s, storageDir, syndication.PublicationConfigSource{LocalDir: configDir})
}

// HandleCreatorPublishResourceWithPublicationSource uses the central Gitea
// publication mappings configured by the server.
func HandleCreatorPublishResourceWithPublicationSource(s *store.Store, storageDir string, source syndication.PublicationConfigSource) http.HandlerFunc {
	if strings.TrimSpace(storageDir) == "" {
		storageDir = "/var/lib/oronbox/blobs"
	}
	return handleCreatorPublishResource(s, storageDir, source)
}

func handleCreatorPublishResource(s *store.Store, storageDir string, source syndication.PublicationConfigSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		resID := r.PathValue("id")
		if resID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					resID = parts[idx+1]
					break
				}
			}
		}

		var req struct {
			Targets     []string        `json:"targets"`
			Summary     string          `json:"summary"`
			Description string          `json:"description"`
			Config      json.RawMessage `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		if len(req.Targets) == 0 {
			httputil.Error(w, http.StatusBadRequest, "missing_targets", "至少需要一个发布渠道")
			return
		}
		for _, target := range req.Targets {
			if target != "bandbbs" && target != "astrobox" && target != "oronbox" {
				httputil.Error(w, http.StatusBadRequest, "invalid_target", "发布渠道无效")
				return
			}
		}
		seenTargets := map[string]bool{}
		for _, target := range req.Targets {
			if seenTargets[target] {
				httputil.Error(w, http.StatusBadRequest, "invalid_target", "发布渠道重复")
				return
			}
			seenTargets[target] = true
		}
		if allow, action, reason, _, reviewErr := moderation.Review(r.Context(), s, strings.TrimSpace(req.Summary+"\n"+req.Description)); reviewErr != nil {
			httputil.Error(w, http.StatusServiceUnavailable, "moderation_unavailable", "内容审核服务暂时不可用")
			return
		} else if !allow && action == "reject" {
			httputil.Error(w, http.StatusBadRequest, "content_rejected", reason)
			return
		}

		var revID int64
		var existingConfig json.RawMessage
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var ownerUID int
			if err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID); err != nil {
				return err
			}
			if ownerUID != userUID {
				var allowed bool
				_ = tx.QueryRow(r.Context(), `
					SELECT true FROM resource_collaborators WHERE resource_id = $1 AND user_uid = $2 AND status = 'accepted'
				`, resID, userUID).Scan(&allowed)
				if !allowed {
					return fmt.Errorf("forbidden")
				}
			}

			// Find latest draft or changes_requested revision
			err := tx.QueryRow(r.Context(), `
				SELECT id, config FROM resource_revisions
				WHERE resource_id = $1 AND state IN ('draft', 'changes_requested')
				ORDER BY id DESC LIMIT 1 FOR UPDATE
			`, resID).Scan(&revID, &existingConfig)
			if err != nil {
				return fmt.Errorf("no_revisable_version")
			}

			var publishConfig map[string]any
			_ = json.Unmarshal(existingConfig, &publishConfig)
			if publishConfig == nil {
				publishConfig = map[string]any{}
			}
			var requestConfig map[string]any
			if len(req.Config) > 0 {
				_ = json.Unmarshal(req.Config, &requestConfig)
			}
			for key, value := range requestConfig {
				publishConfig[key] = value
			}
			publishConfig["targets"] = req.Targets
			metadata, _ := publishConfig["draft_metadata"].(map[string]any)
			if metadata == nil {
				metadata = map[string]any{}
			}
			if req.Description != "" {
				metadata["description"] = req.Description
			}
			publishConfig["draft_metadata"] = metadata
			if catalogCfg, ok := publishConfig["catalog"].(map[string]any); ok {
				paidType, _ := catalogCfg["paid_type"].(string)
				if paidType != "" && paidType != "free" && containsString(req.Targets, "bandbbs") {
					bandCfg, _ := publishConfig["bandbbs"].(map[string]any)
					purchase, _ := bandCfg["purchase"].(map[string]any)
					link, _ := purchase["link"].(string)
					if strings.TrimSpace(link) == "" {
						return fmt.Errorf("purchase_link_required")
					}
				}
			}
			mergedConfig, _ := json.Marshal(publishConfig)
			_, err = tx.Exec(r.Context(), `
				UPDATE resource_revisions 
				SET state = 'waiting_review', summary = COALESCE(NULLIF($2, ''), summary),
				    config = COALESCE($1, config), updated_at = now()
				WHERE id = $3
			`, mergedConfig, req.Summary, revID)
			if err != nil {
				return err
			}
			var version, summary string
			var downloads json.RawMessage
			if err := tx.QueryRow(r.Context(), `SELECT version, summary, downloads FROM resource_revisions WHERE id = $1`, revID).Scan(&version, &summary, &downloads); err != nil {
				return err
			}
			if !validSemver(version) {
				return fmt.Errorf("invalid_version")
			}
			if err := validateSummary(summary); err != nil {
				return err
			}
			rows, err := tx.Query(r.Context(), `SELECT version FROM resource_revisions WHERE resource_id = $1 AND state IN ('approved', 'superseded')`, resID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var approvedVersion string
				if err := rows.Scan(&approvedVersion); err != nil {
					rows.Close()
					return err
				}
				if attestation.CompareVersions(version, approvedVersion) <= 0 {
					rows.Close()
					return fmt.Errorf("version_must_increase")
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			if err := validateDownloads(r.Context(), tx, downloads); err != nil {
				return err
			}
			if err := validateStoredPackages(storageDir, resID, downloads); err != nil {
				return err
			}
			if err := validateMetadata(r.Context(), tx, publishConfig); err != nil {
				return err
			}
			if err := validateStoredIcon(storageDir, publishConfig); err != nil {
				return err
			}
			// Resolve server-owned device/category mappings before accepting the
			// submission. The client supplies purchase details, never routing IDs.
			if containsString(req.Targets, "bandbbs") {
				devices, mapping, err := source.Load(r.Context())
				if err != nil {
					return err
				}
				var artifacts map[string]syndication.DownloadEntry
				if err := json.Unmarshal(downloads, &artifacts); err != nil {
					return err
				}
				if _, err := syndication.PlanPublications(req.Targets, artifacts, devices, mapping); err != nil {
					return err
				}
			}

			// Create review case
			_, err = tx.Exec(r.Context(), `
				INSERT INTO review_cases (revision_id, state)
				VALUES ($1, 'pending')
				ON CONFLICT (revision_id) DO UPDATE SET state = 'pending', updated_at = now()
			`, revID)
			if err != nil {
				return err
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data) VALUES ($1, 'system', 'review.submitted', '提审已提交审核', '资源版本已进入审核队列', $2, jsonb_build_object('revision_id',$3,'review_id',$3))`, userUID, resID, revID)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "publish_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "waiting_review",
			"revision_id": revID,
			"message":     "提审已成功提交审核",
		})
	}
}

// HandleCreatorSubmit implements POST /api/v1/creator/submissions (Compatibility alias)
func HandleCreatorSubmit(s *store.Store, storageDirs ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var req CreatorSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "请求体JSON格式错误")
			return
		}

		req.ResourceID = strings.TrimSpace(req.ResourceID)
		req.Title = strings.TrimSpace(req.Title)
		req.Version = strings.TrimSpace(req.Version)
		if req.ResourceID == "" || req.Title == "" || req.Version == "" {
			httputil.Error(w, http.StatusBadRequest, "missing_fields", "缺少必要参数 resource_id/title/version")
			return
		}
		if !validSemver(req.Version) || validateSummary(req.Summary) != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_metadata", "版本号或摘要不符合规范")
			return
		}

		var revID int64
		storageDir := "/var/lib/oronbox/blobs"
		if len(storageDirs) > 0 && strings.TrimSpace(storageDirs[0]) != "" {
			storageDir = storageDirs[0]
		}
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var ownerUID int
			err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, req.ResourceID).Scan(&ownerUID)

			authorsJSON, _ := json.Marshal([]map[string]any{{"uid": userUID}})
			tagsJSON, _ := json.Marshal(req.Tags)
			previewsJSON, _ := json.Marshal(req.PreviewSHAs)

			if err == pgx.ErrNoRows {
				ownerUID = userUID
				_, err = tx.Exec(r.Context(), `
					INSERT INTO resources (resource_id, title, summary, description, restype, authors, owner_uid,
					                      tags, paid_type, icon_sha256, cover_sha256, preview_shas, moderation_state)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'suspended')
				`, req.ResourceID, req.Title, req.Summary, req.Description, req.Restype, authorsJSON, ownerUID,
					tagsJSON, req.PaidType, req.IconSHA256, req.CoverSHA256, previewsJSON)
				if err != nil {
					return err
				}
			} else if err == nil {
				if ownerUID != userUID {
					return pgx.ErrTxClosed
				}
			} else {
				return err
			}

			if err := validateDownloads(r.Context(), tx, req.Downloads); err != nil {
				return err
			}
			if err := validateStoredPackages(storageDir, req.ResourceID, req.Downloads); err != nil {
				return err
			}
			legacyConfig, _ := json.Marshal(map[string]any{"draft_metadata": map[string]any{"icon_sha256": req.IconSHA256, "cover_sha256": req.CoverSHA256, "preview_shas": req.PreviewSHAs, "tags": req.Tags}})
			err = tx.QueryRow(r.Context(), `
				INSERT INTO resource_revisions (resource_id, version, name, summary, changelog, downloads, config, state, creator_uid)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'waiting_review', $8)
				RETURNING id
			`, req.ResourceID, req.Version, req.Title, req.Summary, req.Changelog, req.Downloads, legacyConfig, userUID).Scan(&revID)
			if err != nil {
				return err
			}

			_, err = tx.Exec(r.Context(), `
				INSERT INTO review_cases (revision_id, state)
				VALUES ($1, 'pending')
				ON CONFLICT (revision_id) DO UPDATE SET state = 'pending', updated_at = now()
			`, revID)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "submit_failed", "提审失败，请检查包名归属或版本号是否重复")
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "waiting_review",
			"revision_id": revID,
		})
	}
}

// HandleCreatorGetSubmissions implements GET /api/v1/creator/submissions (Compatibility alias)
func HandleCreatorGetSubmissions(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT r.id, r.resource_id, r.version, r.name, r.state, r.review_note,
			       to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       COALESCE(to_char(r.reviewed_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), '')
			FROM resource_revisions r
			WHERE r.creator_uid = $1
			ORDER BY r.created_at DESC
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		type SubItem struct {
			ID         int64  `json:"id"`
			ResourceID string `json:"resource_id"`
			Version    string `json:"version"`
			Name       string `json:"name"`
			State      string `json:"state"`
			ReviewNote string `json:"review_note"`
			CreatedAt  string `json:"created_at"`
			ReviewedAt string `json:"reviewed_at,omitempty"`
		}

		var list []SubItem
		for rows.Next() {
			var it SubItem
			if err := rows.Scan(&it.ID, &it.ResourceID, &it.Version, &it.Name, &it.State, &it.ReviewNote, &it.CreatedAt, &it.ReviewedAt); err == nil {
				list = append(list, it)
			}
		}
		if list == nil {
			list = []SubItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// HandleCreatorGetSubmissionDetail implements GET
// /api/v1/creator/submissions/:revision_id. It is the detail seam for the
// creator review status page; external publication state is read-only here.
func HandleCreatorGetSubmissionDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		id := r.PathValue("id")
		if id == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for i, part := range parts {
				if part == "submissions" && i+1 < len(parts) {
					id = parts[i+1]
					break
				}
			}
		}
		revisionID := httputil.ParseInt(id, 0)
		if revisionID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "版本ID无效")
			return
		}
		var (
			resourceID, title, restype, moderationState string
			downloadCount                               int
			updatedAt                                   string
			version, name, summary, state, changelog    string
			downloads, config                           json.RawMessage
		)
		err = s.Pool.QueryRow(r.Context(), `
			SELECT r.resource_id, r.title, r.restype, r.moderation_state, r.download_count,
			       to_char(r.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       rr.version, rr.name, rr.summary, rr.state, rr.changelog, rr.downloads, rr.config
			FROM resource_revisions rr JOIN resources r ON r.resource_id = rr.resource_id
			WHERE rr.id = $1 AND rr.creator_uid = $2
		`, revisionID, userUID).Scan(&resourceID, &title, &restype, &moderationState, &downloadCount,
			&updatedAt, &version, &name, &summary, &state, &changelog, &downloads, &config)
		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "submission_not_found", "提审版本不存在")
			return
		}
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		var publicationPlan []map[string]any
		var cfg map[string]any
		_ = json.Unmarshal(config, &cfg)
		if raw, ok := cfg["publication_plan"].([]any); ok {
			for _, item := range raw {
				if value, ok := item.(map[string]any); ok {
					publicationPlan = append(publicationPlan, value)
				}
			}
		}
		publications := make([]map[string]any, 0)
		rows, queryErr := s.Pool.Query(r.Context(), `
			SELECT id, provider, category_id, state, COALESCE(error_message, ''), COALESCE(external_url, '')
			FROM publications WHERE revision_id = $1 ORDER BY provider, category_id
		`, revisionID)
		if queryErr == nil {
			defer rows.Close()
			for rows.Next() {
				var pubID, categoryID int64
				var provider, pubState, errorMessage, externalURL string
				if rows.Scan(&pubID, &provider, &categoryID, &pubState, &errorMessage, &externalURL) == nil {
					publications = append(publications, map[string]any{"id": pubID, "provider": provider, "category_id": categoryID, "state": pubState, "error_message": errorMessage, "external_url": externalURL})
				}
			}
		}
		httputil.JSON(w, http.StatusOK, map[string]any{
			"resource":         map[string]any{"id": resourceID, "resource_id": resourceID, "slug": resourceID, "title": title, "kind": restype, "moderation_state": moderationState, "download_count": downloadCount, "updated_at": updatedAt},
			"current_revision": map[string]any{"id": revisionID, "number": revisionID, "name": name, "summary": summary, "state": state, "publication_plan": publicationPlan},
			"revisions":        []any{map[string]any{"id": revisionID, "number": revisionID, "name": name, "summary": summary, "state": state, "changelog": changelog, "downloads": downloads}},
			"publications":     publications,
		})
	}
}
