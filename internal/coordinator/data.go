package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/publish/bandbbs"
)

func (c *Coordinator) snapshot(ctx context.Context, revisionID string, publicationID string) ([]byte, error) {
	result := map[string]any{}
	var name, summary, description, descriptionFormat, kind, purchaseLink, purchaseCurrency string
	var purchasePrice sql.NullFloat64
	if err := c.db.QueryRowContext(ctx, `SELECT rr.name,rr.summary,rr.description,rr.description_format,r.kind,COALESCE(rr.purchase_link,''),rr.purchase_price,CASE WHEN COALESCE(rr.purchase_link,'') <> '' THEN 'CNY' ELSE '' END FROM resource_revisions rr JOIN resources r ON r.id=rr.resource_id WHERE rr.id=$1`, revisionID).Scan(&name, &summary, &description, &descriptionFormat, &kind, &purchaseLink, &purchasePrice, &purchaseCurrency); err != nil {
		return nil, err
	}
	revision := map[string]any{"name": name, "summary": summary, "description": description, "description_format": descriptionFormat, "purchase_link": purchaseLink, "purchase_currency": purchaseCurrency}
	if purchasePrice.Valid {
		revision["purchase_price"] = purchasePrice.Float64
	}
	var frozen []byte
	if err := c.db.QueryRowContext(ctx, `SELECT approved_snapshot FROM publications WHERE id=$1`, publicationID).Scan(&frozen); err != nil {
		return nil, err
	}
	var approved map[string]any
	if json.Unmarshal(frozen, &approved) != nil {
		return nil, fmt.Errorf("invalid approved snapshot")
	}
	for _, key := range []string{"name", "summary", "description", "description_format"} {
		if value, ok := approved[key]; ok {
			revision[key] = value
		}
	}
	result["revision"] = revision
	linkRows, err := c.db.QueryContext(ctx, `SELECT title,url FROM revision_links WHERE revision_id=$1 ORDER BY position`, revisionID)
	if err != nil {
		return nil, err
	}
	var links []map[string]any
	for linkRows.Next() {
		var title, url string
		if err := linkRows.Scan(&title, &url); err != nil {
			linkRows.Close()
			return nil, err
		}
		links = append(links, map[string]any{"title": title, "url": url})
	}
	linkRows.Close()
	revision["links"] = links
	mediaRows, err := c.db.QueryContext(ctx, `SELECT blob_sha256,role,position FROM revision_media WHERE revision_id=$1 ORDER BY role,position`, revisionID)
	if err != nil {
		return nil, err
	}
	var media []map[string]any
	for mediaRows.Next() {
		var sha, role string
		var position int
		if err := mediaRows.Scan(&sha, &role, &position); err != nil {
			mediaRows.Close()
			return nil, err
		}
		media = append(media, map[string]any{"blob_sha256": sha, "role": role, "position": position})
	}
	mediaRows.Close()
	artifactRows, err := c.db.QueryContext(ctx, `SELECT a.id::text,a.blob_sha256,a.original_name,a.package_id,a.package_version FROM revision_artifacts a WHERE a.revision_id=$1 ORDER BY a.created_at`, revisionID)
	if err != nil {
		return nil, err
	}
	var artifacts []map[string]any
	for artifactRows.Next() {
		var id, sha, originalName, packageID, version string
		if err := artifactRows.Scan(&id, &sha, &originalName, &packageID, &version); err != nil {
			artifactRows.Close()
			return nil, err
		}
		rows, err := c.db.QueryContext(ctx, `SELECT COALESCE(NULLIF(d.astrobox_id,''),d.codename),d.display_name,d.platform FROM revision_artifact_devices b JOIN devices d ON d.id=b.device_id WHERE b.artifact_id=$1 ORDER BY COALESCE(NULLIF(d.astrobox_id,''),d.codename)`, id)
		if err != nil {
			artifactRows.Close()
			return nil, err
		}
		var devices []map[string]any
		for rows.Next() {
			var deviceID, deviceName, platform string
			if err := rows.Scan(&deviceID, &deviceName, &platform); err != nil {
				rows.Close()
				artifactRows.Close()
				return nil, err
			}
			if platform != "vela_os" || !astrobox.IsSupportedDeviceID(deviceID) {
				continue
			}
			devices = append(devices, map[string]any{"id": deviceID, "name": deviceName})
		}
		rows.Close()
		if len(devices) == 0 {
			continue
		}
		artifacts = append(artifacts, map[string]any{"blob_sha256": sha, "original_name": originalName, "package_id": packageID, "version": version, "kind": kind, "platform": "vela_os", "devices": devices})
	}
	artifactRows.Close()
	result["media"], result["artifacts"] = media, artifacts
	return json.Marshal(result)
}

func (c *Coordinator) bandBBSToken(ctx context.Context, userID string) (string, string, error) {
	token, subject, err := c.bandAuth.GrantToken(ctx, c.store, c.secrets, userID, "bandbbs_publish", "")
	return token.AccessToken, subject, err
}

// DeleteBandBBSResources removes the creator's BandBBS resources, tolerating
// ids that are already gone. Used by the creator delete endpoint so a local
// deletion never silently leaves BandBBS copies behind.
func (c *Coordinator) DeleteBandBBSResources(ctx context.Context, ownerID string, resourceIDs []string) error {
	token, _, err := c.bandBBSToken(ctx, ownerID)
	if err != nil {
		return err
	}
	for _, id := range resourceIDs {
		if err := c.band.DeleteResource(ctx, token, id); err != nil {
			if bandbbs.IsUnauthorized(err) {
				_, _, _ = c.bandAuth.GrantToken(ctx, c.store, c.secrets, ownerID, "bandbbs_publish", token)
			}
			return fmt.Errorf("delete BandBBS resource %s: %w", id, err)
		}
	}
	return nil
}

// RemoveAstroBoxItem submits a catalog removal PR for the creator's AstroBox
// item and returns the PR URL. Used by the creator delete endpoint when the
// creator explicitly asks to remove the item from the index.
func (c *Coordinator) RemoveAstroBoxItem(ctx context.Context, ownerID, itemID, name string) (string, error) {
	token, err := c.githubToken(ctx, ownerID)
	if err != nil {
		return "", err
	}
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT COALESCE(p.status_detail->>'pull_request_number','0') FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources r ON r.id=rr.resource_id JOIN external_bindings b ON b.resource_id=r.id AND b.provider='astrobox' WHERE r.owner_id=$1 AND b.external_id=$2 AND p.target='astrobox'`, ownerID, itemID)
	if err != nil {
		return "", err
	}
	var numbers []int
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return "", err
		}
		n, _ := strconv.Atoi(raw)
		if n > 0 {
			numbers = append(numbers, n)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	for _, number := range numbers {
		if err = c.astro.ClosePullRequest(ctx, token, number); err != nil {
			if astrobox.IsUnauthorized(err) {
				c.rejectGitHubToken(ctx, ownerID, token)
			}
			return "", err
		}
	}
	result, err := c.astro.Remove(ctx, token, itemID, name)
	if err != nil {
		if astrobox.IsUnauthorized(err) {
			c.rejectGitHubToken(ctx, ownerID, token)
		}
		return "", err
	}
	return result.PullRequest, nil
}

func (c *Coordinator) githubToken(ctx context.Context, userID string) (string, error) {
	var cipher []byte
	var scopes []string
	var scopesJSON []byte
	err := c.db.QueryRowContext(ctx, `SELECT access_token_cipher,to_json(scopes) FROM github_grants WHERE user_id=$1`, userID).Scan(&cipher, &scopesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("GitHub publishing permission is not authorized")
	}
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
		return "", fmt.Errorf("decode GitHub grant scopes: %w", err)
	}
	if !config.HasScopes(config.ScopeString(scopes), []string{"public_repo"}) {
		return "", fmt.Errorf("GitHub grant lacks public_repo scope")
	}
	return c.secrets.Decrypt(cipher)
}

func (c *Coordinator) AstroBoxRemovalStatus(ctx context.Context, ownerID, prURL string) (string, error) {
	u, err := url.Parse(prURL)
	if err != nil || u.Host != "github.com" {
		return "failed", fmt.Errorf("invalid removal PR")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != c.cfg.AstroBox.RepoOwner || parts[1] != c.cfg.AstroBox.RepoName || parts[2] != "pull" {
		return "failed", fmt.Errorf("invalid removal PR repository")
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return "failed", fmt.Errorf("invalid removal PR number")
	}
	token, err := c.githubToken(ctx, ownerID)
	if err != nil {
		return "failed", err
	}
	status, err := c.astro.PullRequest(ctx, token, n)
	if err != nil {
		return "awaiting_external", err
	}
	if status.Merged {
		return "completed", nil
	}
	if status.State == "closed" {
		return "closed_unmerged", nil
	}
	return "awaiting_external", nil
}

func (c *Coordinator) rejectGitHubToken(ctx context.Context, userID, token string) {
	cipher, err := c.store.GitHubAccessTokenCipher(ctx, userID)
	if err != nil {
		return
	}
	current, err := c.secrets.Decrypt(cipher)
	if err == nil && current == token {
		if err = c.store.RejectGitHubToken(ctx, userID, cipher); err != nil {
			c.log.Warn("GitHub rejected credential state update failed", "user_id", userID, "error", err)
		}
	}
}
