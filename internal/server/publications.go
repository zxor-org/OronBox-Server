package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

func (a *application) retryPublication(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || a.db == nil || a.db.Pool == nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_id", "无效的发布任务ID")
		return
	}
	result, err := a.db.Pool.Exec(r.Context(), `UPDATE publications SET state = 'pending', attempts = 0, error_message = '', next_attempt_at = now(), updated_at = now() WHERE id = $1 AND state IN ('failed','external_rejected')`, id)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "retry_failed", "重试发布任务失败")
		return
	}
	if result.RowsAffected() == 0 {
		httputil.Error(w, http.StatusConflict, "not_retryable", "当前发布任务不可重试")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"status": "queued", "publication_id": id})
}

func (a *application) runPublications(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		a.runPublicationWorker(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runPublicationWorker claims a small batch of due publication jobs. The claim
// and attempt rows are written before the external call, so a process crash
// leaves an explicit retryable state instead of a permanently pending job.
func (a *application) runPublicationWorker(ctx context.Context) {
	if a.db == nil || a.db.Pool == nil || a.cipher == nil {
		return
	}
	// Recover jobs whose worker lease expired while an external request was in
	// flight. Without this, a process crash would leave them permanently stuck
	// in dispatching.
	if result, err := a.db.Pool.Exec(ctx, `
		UPDATE publications
		SET state = 'failed', error_message = 'publication worker lease expired', next_attempt_at = now(), updated_at = now()
		WHERE state = 'dispatching' AND updated_at < now() - interval '10 minutes' AND attempts < 5
	`); err == nil && result.RowsAffected() > 0 {
		a.logger.Warn("publication leases recovered", "count", result.RowsAffected())
	}
	for i := 0; i < 10; i++ {
		var id int64
		var provider, state string
		var attempts int
		err := a.db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `
				SELECT id, provider, state, attempts FROM publications
				WHERE state IN ('pending','failed') AND attempts < 5 AND (next_attempt_at IS NULL OR next_attempt_at <= now())
				ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
			`).Scan(&id, &provider, &state, &attempts); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE publications SET state = 'dispatching', attempts = attempts + 1, updated_at = now() WHERE id = $1`, id); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO publication_attempts (publication_id, attempt_number, phase, state_from, state_to) VALUES ($1, $2, 'execute', $3, 'dispatching')`, id, attempts+1, state)
			return err
		})
		if err == pgx.ErrNoRows {
			break
		}
		if err != nil {
			return
		}
		a.logger.Debug("publication claimed", "publication_id", id, "provider", provider, "attempt", attempts+1)
		err = a.executePublication(ctx, id, provider)
		if err == nil {
			_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state = CASE WHEN provider = 'astrobox' THEN 'waiting_external_review' ELSE 'published' END, error_message = '', next_attempt_at = NULL, updated_at = now() WHERE id = $1`, id)
			_, _ = a.db.Pool.Exec(ctx, `INSERT INTO publication_attempts (publication_id, attempt_number, phase, state_from, state_to) SELECT id, attempts, 'execute', 'dispatching', CASE WHEN provider = 'astrobox' THEN 'waiting_external_review' ELSE 'published' END FROM publications WHERE id = $1`, id)
			a.updatePublicationRollup(ctx, id)
			a.logger.Info("publication completed", "publication_id", id, "provider", provider)
			continue
		}
		attempts++
		delay := syndication.RetryDelay(attempts)
		stateTo := "failed"
		if attempts >= 5 {
			stateTo = "failed"
		}
		_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state = $2, error_message = $3, next_attempt_at = CASE WHEN $4 < 5 THEN now() + $5::interval ELSE NULL END, updated_at = now() WHERE id = $1`, id, stateTo, safePublicationError(err), attempts, fmt.Sprintf("%d seconds", int(delay.Seconds())))
		_, _ = a.db.Pool.Exec(ctx, `INSERT INTO publication_attempts (publication_id, attempt_number, phase, state_from, state_to, error_message) VALUES ($1, $2, 'execute', 'dispatching', 'failed', $3)`, id, attempts, safePublicationError(err))
		a.updatePublicationRollup(ctx, id)
		a.logger.Warn("publication failed", "publication_id", id, "provider", provider, "attempt", attempts, "error", observability.SafeError(err))
	}
	a.pollAstroBoxPublications(ctx)
}

func (a *application) pollAstroBoxPublications(ctx context.Context) {
	rows, err := a.db.Pool.Query(ctx, `
		SELECT p.id, p.external_id, r.owner_uid FROM publications p
		JOIN resource_revisions rr ON rr.id = p.revision_id JOIN resources r ON r.resource_id = rr.resource_id
		WHERE p.provider = 'astrobox' AND p.state IN ('waiting_external_review', 'external_changes_requested', 'external_fixed_waiting') AND p.external_id <> '' LIMIT 20
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var externalID string
		var ownerUID int
		if rows.Scan(&id, &externalID, &ownerUID) != nil {
			continue
		}
		var login string
		var encrypted []byte
		if a.db.Pool.QueryRow(ctx, `SELECT login, access_token_cipher FROM github_grants WHERE user_uid = $1`, ownerUID).Scan(&login, &encrypted) != nil {
			continue
		}
		token, err := a.cipher.Decrypt(encrypted)
		if err != nil {
			continue
		}
		pr, _ := strconv.Atoi(externalID)
		if pr <= 0 {
			continue
		}
		client := astrobox.Client{GitHub: syndication.GitHubClient{BaseURL: a.cfg.GitHub.APIURL}, RepoOwner: a.cfg.AstroBox.RepoOwner, RepoName: a.cfg.AstroBox.RepoName, RepoBranch: a.cfg.AstroBox.RepoBranch, CatalogPath: a.cfg.AstroBox.CatalogPath}
		merged, state, _, err := client.PRStatus(ctx, string(token), pr)
		if err != nil {
			continue
		}
		next := "waiting_external_review"
		if merged {
			next = "published"
		} else if strings.EqualFold(state, "closed") {
			next = "external_rejected"
		} else if comments, e := client.ReviewComments(ctx, string(token), pr); e == nil {
			reviewState, _ := astrobox.DeriveReviewStatus(comments)
			switch reviewState {
			case "changes_requested":
				next = "external_changes_requested"
			case "fixed_waiting":
				next = "external_fixed_waiting"
			}
		}
		if next != "waiting_external_review" {
			_, _ = a.db.Pool.Exec(ctx, `UPDATE publications SET state = $2, updated_at = now() WHERE id = $1`, id, next)
			a.updatePublicationRollup(ctx, id)
		}
	}
}

func (a *application) updatePublicationRollup(ctx context.Context, publicationID int64) {
	var revisionID int64
	if a.db.Pool.QueryRow(ctx, `SELECT revision_id FROM publications WHERE id = $1`, publicationID).Scan(&revisionID) != nil {
		return
	}
	rows, err := a.db.Pool.Query(ctx, `SELECT state FROM publications WHERE revision_id = $1`, revisionID)
	if err != nil {
		return
	}
	defer rows.Close()
	states := make([]string, 0, 4)
	for rows.Next() {
		var state string
		if rows.Scan(&state) == nil {
			states = append(states, state)
		}
	}
	_ = syndication.Rollup(states)
}

func safePublicationError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	return msg
}

func (a *application) executePublication(ctx context.Context, id int64, provider string) error {
	if provider == "astrobox" {
		return a.executeAstroBoxPublication(ctx, id)
	}
	if provider != "bandbbs" {
		return fmt.Errorf("unsupported publication provider %s", provider)
	}
	var (
		ownerUID                                                  int
		resourceID, title, summary, description, restype, version string
		downloadsRaw, configRaw                                   json.RawMessage
		categoryID                                                int
	)
	var iconSHA, coverSHA string
	var previewsRaw json.RawMessage
	err := a.db.Pool.QueryRow(ctx, `
		SELECT r.owner_uid, r.resource_id, r.title, rr.summary, r.description, r.restype, rr.version,
		       rr.downloads, p.config, r.icon_sha256, r.cover_sha256, r.preview_shas, p.category_id
		FROM publications p JOIN resource_revisions rr ON rr.id = p.revision_id JOIN resources r ON r.resource_id = rr.resource_id
		WHERE p.id = $1
	`, id).Scan(&ownerUID, &resourceID, &title, &summary, &description, &restype, &version, &downloadsRaw, &configRaw, &iconSHA, &coverSHA, &previewsRaw, &categoryID)
	if err != nil {
		return err
	}
	if categoryID <= 0 {
		return fmt.Errorf("bandbbs category mapping is missing")
	}
	var downloads map[string]struct {
		BlobSHA256   string `json:"blob_sha256"`
		OriginalName string `json:"original_name"`
	}
	if err := json.Unmarshal(downloadsRaw, &downloads); err != nil {
		return fmt.Errorf("invalid downloads: %w", err)
	}
	var pubCfg struct {
		DeviceCompatIDs  []string       `json:"device_compat_ids"`
		ResourcePrefixes map[string]int `json:"resource_prefixes"`
		ThreadPrefixes   map[string]int `json:"thread_prefixes"`
		Purchase         struct {
			Link     string  `json:"link"`
			Price    float64 `json:"price"`
			Currency string  `json:"currency"`
		} `json:"purchase"`
	}
	if err := json.Unmarshal(configRaw, &pubCfg); err != nil {
		return err
	}
	device := ""
	if pubCfg.Purchase.Link == "" {
		for _, candidate := range pubCfg.DeviceCompatIDs {
			if _, ok := downloads[candidate]; ok {
				device = candidate
				break
			}
		}
		if device == "" {
			return fmt.Errorf("bandbbs category has no matching device download")
		}
	}
	var data []byte
	var name string
	if device != "" {
		artifact := downloads[device]
		for _, candidate := range pubCfg.DeviceCompatIDs {
			other, ok := downloads[candidate]
			if ok && (other.BlobSHA256 != artifact.BlobSHA256 || other.OriginalName != artifact.OriginalName) {
				return fmt.Errorf("bandbbs category %d contains multiple incompatible device packages", categoryID)
			}
		}
		var readErr error
		data, name, readErr = a.readLocalBlob(artifact.BlobSHA256, artifact.OriginalName)
		if readErr != nil {
			return readErr
		}
	}
	var encrypted []byte
	if err := a.db.Pool.QueryRow(ctx, `SELECT access_token_cipher FROM oauth_grants WHERE user_uid = $1 AND provider IN ('bandbbs_write','bandbbs') ORDER BY CASE WHEN provider='bandbbs_write' THEN 0 ELSE 1 END LIMIT 1`, ownerUID).Scan(&encrypted); err != nil {
		return fmt.Errorf("bandbbs grant unavailable")
	}
	token, err := a.cipher.Decrypt(encrypted)
	if err != nil {
		return fmt.Errorf("bandbbs grant unavailable")
	}
	client := bandbbs.New(a.cfg.BandBBS.APIURL)
	var previewSHAs []string
	_ = json.Unmarshal(previewsRaw, &previewSHAs)
	previews := make([]bandbbs.PreviewImage, 0, len(previewSHAs))
	for i, sha := range previewSHAs {
		previewData, _, previewErr := a.readLocalBlob(sha, fmt.Sprintf("preview-%d.webp", i+1))
		if previewErr != nil {
			return previewErr
		}
		previews = append(previews, bandbbs.PreviewImage{Name: fmt.Sprintf("preview-%d.webp", i+1), Data: previewData})
	}
	if strings.TrimSpace(description) == "" {
		description = summary
	}
	in := bandbbs.PublishInput{CategoryID: categoryID, CreatorID: strconv.Itoa(ownerUID), Restype: restype, ResourcePrefixID: pubCfg.ResourcePrefixes[restype], ThreadPrefixID: pubCfg.ThreadPrefixes[restype], Title: title, TagLine: summary, Description: bandbbs.MarkdownToBBCode(description), Previews: previews, Version: version, VersionFileName: name, VersionData: data}
	if pubCfg.Purchase.Link != "" {
		in.External = &bandbbs.ExternalPurchase{Link: pubCfg.Purchase.Link, Price: pubCfg.Purchase.Price, Currency: pubCfg.Purchase.Currency}
	}
	var existingIDRaw, existingURL string
	existingID := 0
	if err := a.db.Pool.QueryRow(ctx, `SELECT external_id, external_url FROM external_bindings WHERE resource_id = $1 AND provider = 'bandbbs' AND category_id = $2`, resourceID, categoryID).Scan(&existingIDRaw, &existingURL); err != nil && err != pgx.ErrNoRows {
		return err
	}
	if existingIDRaw != "" {
		existingID, _ = strconv.Atoi(existingIDRaw)
	}
	in.ExistingID = existingID
	result, err := client.Publish(ctx, string(token), in)
	if err != nil {
		return err
	}
	if result.ResourceID == 0 {
		return fmt.Errorf("BandBBS returned no resource id")
	}
	_, err = a.db.Pool.Exec(ctx, `INSERT INTO external_bindings (resource_id, provider, category_id, external_id, external_url, meta, updated_at) VALUES ($1,'bandbbs',$2,$3,$4,'{}'::jsonb,now()) ON CONFLICT (resource_id,provider,category_id) DO UPDATE SET external_id = $3, external_url = $4, updated_at = now()`, resourceID, categoryID, strconv.Itoa(result.ResourceID), result.URL)
	if err != nil {
		return err
	}
	_, err = a.db.Pool.Exec(ctx, `UPDATE publications SET external_id = $2, external_url = $3, updated_at = now() WHERE id = $1`, id, strconv.Itoa(result.ResourceID), result.URL)
	return err
}

func (a *application) readLocalBlob(sha, name string) ([]byte, string, error) {
	if len(sha) != 64 {
		return nil, "", fmt.Errorf("invalid blob hash")
	}
	if _, err := hex.DecodeString(sha); err != nil {
		return nil, "", fmt.Errorf("invalid blob hash")
	}
	base := a.cfg.BlobStorageDir
	if base == "" {
		base = "/var/lib/oronbox/blobs"
	}
	path := filepath.Join(base, sha[:2], sha[2:4], sha)
	data, err := os.ReadFile(path)
	if err != nil {
		data, err = os.ReadFile(filepath.Join(base, sha))
	}
	if err != nil {
		return nil, "", fmt.Errorf("publication blob unavailable")
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		name = sha + ".bin"
	}
	return data, name, nil
}

func (a *application) executeAstroBoxPublication(ctx context.Context, id int64) error {
	var (
		ownerUID                                                   int
		resourceID, title, restype, description, version, paidType string
		downloadsRaw, tagsRaw, previewsRaw                         json.RawMessage
		iconSHA, coverSHA                                          string
	)
	if err := a.db.Pool.QueryRow(ctx, `
		SELECT r.owner_uid, r.resource_id, r.title, r.restype, r.description, rr.version, r.paid_type,
		       rr.downloads, r.tags, r.preview_shas, r.icon_sha256, r.cover_sha256
		FROM publications p JOIN resource_revisions rr ON rr.id = p.revision_id JOIN resources r ON r.resource_id = rr.resource_id
		WHERE p.id = $1
	`, id).Scan(&ownerUID, &resourceID, &title, &restype, &description, &version, &paidType, &downloadsRaw, &tagsRaw, &previewsRaw, &iconSHA, &coverSHA); err != nil {
		return err
	}
	var grantToken []byte
	var login string
	if err := a.db.Pool.QueryRow(ctx, `SELECT login, access_token_cipher FROM github_grants WHERE user_uid = $1`, ownerUID).Scan(&login, &grantToken); err != nil {
		return fmt.Errorf("github grant unavailable")
	}
	token, err := a.cipher.Decrypt(grantToken)
	if err != nil || strings.TrimSpace(login) == "" {
		return fmt.Errorf("github grant unavailable")
	}
	var downloads map[string]struct {
		BlobSHA256   string `json:"blob_sha256"`
		OriginalName string `json:"original_name"`
	}
	if err := json.Unmarshal(downloadsRaw, &downloads); err != nil || len(downloads) == 0 {
		return fmt.Errorf("invalid downloads")
	}
	manifest := syndication.Manifest{Downloads: map[string]syndication.DownloadEntry{}}
	manifest.Item.ID, manifest.Item.Name, manifest.Item.Restype, manifest.Item.Description = resourceID, title, restype, description
	manifest.Item.Icon = "media/icon.webp"
	files := map[string][]byte{}
	if iconSHA != "" {
		data, _, e := a.readLocalBlob(iconSHA, "icon.webp")
		if e != nil {
			return e
		}
		files["media/icon.webp"] = data
	}
	if coverSHA != "" {
		manifest.Item.Cover = "media/cover.webp"
		data, _, e := a.readLocalBlob(coverSHA, "cover.webp")
		if e != nil {
			return e
		}
		files["media/cover.webp"] = data
	}
	var previewSHAs []string
	_ = json.Unmarshal(previewsRaw, &previewSHAs)
	for i, sha := range previewSHAs {
		data, _, e := a.readLocalBlob(sha, fmt.Sprintf("preview-%d.webp", i+1))
		if e != nil {
			return e
		}
		path := fmt.Sprintf("media/preview-%d.webp", i+1)
		files[path] = data
		manifest.Item.Preview = append(manifest.Item.Preview, path)
	}
	for device, artifact := range downloads {
		data, name, e := a.readLocalBlob(artifact.BlobSHA256, artifact.OriginalName)
		if e != nil {
			return e
		}
		if name == "" {
			name = device + ".bin"
		}
		// Different device builds often share a filename; keep each artifact in
		// its own hash directory so one cannot overwrite another in the repo.
		path := "downloads/" + artifact.BlobSHA256 + "/" + filepath.Base(name)
		files[path] = data
		manifest.Downloads[device] = syndication.DownloadEntry{Version: version, FileName: path}
	}
	manifestRaw, err := astrobox.BuildManifestV2(manifest, syndication.Changelog{}, "")
	if err != nil {
		return err
	}
	files["manifest_v2.json"] = manifestRaw
	repoName := "oronbox-resource-" + strings.ToLower(strings.NewReplacer(".", "-", "/", "-", "_", "-").Replace(resourceID))
	client := astrobox.Client{GitHub: syndication.GitHubClient{BaseURL: a.cfg.GitHub.APIURL}, RepoOwner: a.cfg.AstroBox.RepoOwner, RepoName: a.cfg.AstroBox.RepoName, RepoBranch: a.cfg.AstroBox.RepoBranch, CatalogPath: a.cfg.AstroBox.CatalogPath}
	repo, err := client.EnsureResourceRepo(ctx, string(token), repoName, title, files)
	if err != nil {
		return err
	}
	var tags []string
	_ = json.Unmarshal(tagsRaw, &tags)
	entry := astrobox.CatalogEntry{ID: resourceID, Name: title, Restype: restype, RepoOwner: repo.Owner, RepoName: repo.Name, RepoCommitHash: repo.Commit, Icon: manifest.Item.Icon, Cover: manifest.Item.Cover, Tags: strings.Join(tags, ";"), Devices: strings.Join(astrobox.SortedDeviceKeys(manifest.Downloads), ";"), PaidType: paidType}
	upstream, commit, err := client.UpstreamCatalog(ctx, string(token))
	if err != nil {
		return fmt.Errorf("read AstroBox catalog: %w", err)
	}
	req := astrobox.DeriveSubmission(entry, upstream, commit)
	pr, url, _, err := client.Submit(ctx, string(token), login, repo, entry, req)
	if err != nil {
		return err
	}
	_, err = a.db.Pool.Exec(ctx, `INSERT INTO external_bindings (resource_id, provider, category_id, external_id, external_url, meta, updated_at) VALUES ($1,'astrobox',0,$2,$3,'{}'::jsonb,now()) ON CONFLICT (resource_id,provider,category_id) DO UPDATE SET external_id = $2, external_url = $3, updated_at = now()`, resourceID, strconv.Itoa(pr), url)
	if err != nil {
		return err
	}
	_, err = a.db.Pool.Exec(ctx, `UPDATE publications SET external_id = $2, external_url = $3, updated_at = now() WHERE id = $1`, id, strconv.Itoa(pr), url)
	return err
}
