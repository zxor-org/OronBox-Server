package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
)

func (c *Coordinator) publishAstroBox(ctx context.Context, item publication, snapshot []byte) error {
	token, err := c.githubToken(ctx, item.OwnerID)
	if err != nil {
		return err
	}
	// Imported resources carry their existing AstroBox identity in the
	// binding; fill any gaps in the publication config from it so the
	// publication updates that repo instead of creating a duplicate.
	var publishConfig struct {
		ItemID   string `json:"item_id"`
		RepoName string `json:"repo_name"`
	}
	if err := json.Unmarshal(item.Config, &publishConfig); err == nil && (strings.TrimSpace(publishConfig.ItemID) == "" || strings.TrimSpace(publishConfig.RepoName) == "") {
		var boundID, boundMeta string
		bindErr := c.db.QueryRowContext(ctx, `SELECT external_id,meta::text FROM external_bindings WHERE resource_id=$1 AND provider='astrobox'`, item.ResourceID).Scan(&boundID, &boundMeta)
		if bindErr == nil {
			config := map[string]any{}
			_ = json.Unmarshal(item.Config, &config)
			meta := map[string]string{}
			_ = json.Unmarshal([]byte(boundMeta), &meta)
			if strings.TrimSpace(publishConfig.ItemID) == "" && boundID != "" {
				config["item_id"] = boundID
			}
			if strings.TrimSpace(publishConfig.RepoName) == "" && meta["repo_name"] != "" {
				config["repo_name"] = meta["repo_name"]
			}
			if merged, marshalErr := json.Marshal(config); marshalErr == nil {
				item.Config = merged
			}
		}
	}
	result, err := c.astro.Publish(ctx, token, item.OwnerName, snapshot, item.Config)
	if err != nil {
		if astrobox.IsUnauthorized(err) {
			c.rejectGitHubToken(ctx, item.OwnerID, token)
		}
		return err
	}
	detail := map[string]any{
		"pull_request_number":     result.PullRequestNumber,
		"repository":              result.Repository,
		"pull_request_repository": "https://github.com/" + c.cfg.AstroBox.RepoOwner + "/" + c.cfg.AstroBox.RepoName,
		"submission_protocol":     result.SubmissionProtocol,
		"submission_path":         result.SubmissionPath,
		"catalog_row":             result.CatalogRow,
		"catalog_commit":          result.CatalogCommit,
	}
	var astroConfig struct {
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(item.Config, &astroConfig); err != nil || strings.TrimSpace(astroConfig.ItemID) == "" {
		return fmt.Errorf("AstroBox item_id is missing")
	}
	meta := map[string]string{}
	if name := strings.TrimPrefix(result.Repository, "https://github.com/"); name != result.Repository {
		if slash := strings.IndexByte(name, '/'); slash > 0 {
			meta["repo_owner"], meta["repo_name"] = name[:slash], name[slash+1:]
		}
	}
	if err := c.finish(ctx, item, "reviewing", astroConfig.ItemID, result.PullRequest, detail, meta); err != nil {
		return err
	}
	c.log.Info("publication submitted for review", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_id", astroConfig.ItemID, "external_url", result.PullRequest)
	return nil
}
