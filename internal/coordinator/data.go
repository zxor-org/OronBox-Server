package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (c *Coordinator) snapshot(ctx context.Context, revisionID string) ([]byte, error) {
	result := map[string]any{}
	var name, summary, kind string
	if err := c.db.QueryRowContext(ctx, `SELECT rr.name,rr.summary,r.kind FROM resource_revisions rr JOIN resources r ON r.id=rr.resource_id WHERE rr.id=$1`, revisionID).Scan(&name, &summary, &kind); err != nil {
		return nil, err
	}
	result["revision"] = map[string]any{"name": name, "summary": summary}
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
	grant, err := c.store.OAuthGrant(ctx, userID, "bandbbs_publish")
	if err != nil {
		return "", "", err
	}
	if !config.HasScopes(config.ScopeString(grant.Scopes), c.cfg.BandBBS.PublishScopes) {
		return "", "", fmt.Errorf("BandBBS publishing permission is not authorized")
	}
	if grant.ExpiresAt == nil || grant.ExpiresAt.After(time.Now().UTC().Add(time.Minute)) {
		token, decryptErr := c.secrets.Decrypt(grant.AccessTokenCipher)
		return token, grant.Subject, decryptErr
	}
	refresh, err := c.secrets.Decrypt(grant.RefreshTokenCipher)
	if err != nil || refresh == "" {
		return "", "", fmt.Errorf("BandBBS publishing grant expired")
	}
	token, err := c.bandAuth.Refresh(ctx, refresh)
	if err != nil {
		c.log.Warn("BandBBS token refresh failed", "user_id", userID, "error", err)
		return "", "", err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refresh
	}
	subject, scopes, err := c.bandAuth.ValidateScopes(ctx, token, c.cfg.BandBBS.PublishScopes)
	if err != nil {
		return "", "", err
	}
	accessCipher, err := c.secrets.Encrypt(token.AccessToken)
	if err != nil {
		return "", "", err
	}
	refreshCipher, err := c.secrets.Encrypt(token.RefreshToken)
	if err != nil {
		return "", "", err
	}
	var expiresAt *time.Time
	if token.ExpiresIn > 0 {
		expiry := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
		expiresAt = &expiry
	}
	err = c.store.UpsertOAuthGrant(ctx, store.GrantParams{UserID: userID, Provider: "bandbbs_publish", Subject: subject, Scopes: config.ParseScopes(scopes), AccessTokenCipher: accessCipher, RefreshTokenCipher: refreshCipher, TokenType: token.TokenType, ExpiresAt: expiresAt})
	if err != nil {
		return "", "", err
	}
	c.log.Info("BandBBS token refreshed", "user_id", userID)
	if subject == "" {
		subject = grant.Subject
	}
	return token.AccessToken, subject, nil
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
	result, err := c.astro.Remove(ctx, token, itemID, name)
	if err != nil {
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
