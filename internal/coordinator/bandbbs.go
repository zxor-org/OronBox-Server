package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/publish/bandbbs"
)

func (c *Coordinator) publishBandBBS(ctx context.Context, item publication, snapshot []byte) error {
	token, subject, err := c.bandBBSToken(ctx, item.OwnerID)
	if err != nil {
		return err
	}
	var publishConfig struct {
		Targets []struct {
			CategoryID int `json:"category_id"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(item.Config, &publishConfig); err != nil || len(publishConfig.Targets) == 0 {
		return fmt.Errorf("invalid BandBBS publication targets")
	}
	var bound, boundURL string
	_ = c.db.QueryRowContext(ctx, `SELECT external_id,external_url FROM external_bindings WHERE resource_id=$1 AND provider='bandbbs'`, item.ResourceID).Scan(&bound, &boundURL)
	targetIDs := make([]int, 0, len(publishConfig.Targets))
	for _, target := range publishConfig.Targets {
		targetIDs = append(targetIDs, target.CategoryID)
	}
	existing, err := parseBandBBSBinding(bound, targetIDs)
	if err != nil {
		return err
	}
	if bound != "" {
		normalized, marshalErr := json.Marshal(existing)
		if marshalErr != nil {
			return marshalErr
		}
		if string(normalized) != bound {
			if _, err := c.db.ExecContext(ctx, `UPDATE external_bindings SET external_id=$2 WHERE resource_id=$1 AND provider='bandbbs'`, item.ResourceID, normalized); err != nil {
				return err
			}
			bound = string(normalized)
		}
	}
	config, progress := filterBandBBSConfig(item.Config, item.StatusDetail)
	if len(config) == 0 {
		for category, id := range existing {
			if _, ok := progress[category]; !ok {
				progress[category] = bandbbs.CategoryResult{ResourceID: id, URL: "https://www.bandbbs.cn/resources/" + id + "/"}
			}
		}
		return c.finish(ctx, item, "published", bound, boundURL, map[string]any{"resources": progress}, nil)
	}
	result, err := c.band.PublishWithProgress(ctx, token, subject, existing, progress, snapshot, config)
	if bandbbs.IsUnauthorized(err) {
		// Preserve partial progress below. Rotate for the next task attempt rather
		// than replaying writes that might already have reached the upstream.
		if _, _, refreshErr := c.bandAuth.GrantToken(ctx, c.store, c.secrets, item.OwnerID, "bandbbs_publish", token); refreshErr != nil {
			err = fmt.Errorf("%w; authorization refresh: %v", err, refreshErr)
		}
	}
	if err != nil {
		if len(result.Resources) > 0 {
			if persistErr := c.recordBandBBSPartial(ctx, item, result.Resources); persistErr != nil {
				return fmt.Errorf("%w; persist BandBBS partial progress: %v", err, persistErr)
			}
		}
		return err
	}
	canonical := make(map[string]string, len(result.Resources))
	for categoryID, resourceID := range existing {
		canonical[categoryID] = resourceID
	}
	for categoryID, resource := range result.Resources {
		canonical[categoryID] = resource.ResourceID
	}
	externalID, _ := json.Marshal(canonical)
	externalURL := boundURL
	if len(publishConfig.Targets) > 0 {
		if resource, ok := result.Resources[strconv.Itoa(publishConfig.Targets[0].CategoryID)]; ok {
			externalURL = resource.URL
		}
	}
	if err := c.finish(ctx, item, "published", string(externalID), externalURL, map[string]any{"resources": result.Resources}, nil); err != nil {
		return err
	}
	c.log.Info("publication completed", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_url", externalURL, "categories", len(result.Resources))
	return nil
}

func filterBandBBSConfig(raw, detail []byte) ([]byte, map[string]bandbbs.CategoryResult) {
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		return raw, nil
	}
	var targets []map[string]json.RawMessage
	targetsRaw, _ := json.Marshal(config["targets"])
	if json.Unmarshal(targetsRaw, &targets) != nil {
		return raw, nil
	}
	completed := map[string]bandbbs.CategoryResult{}
	versionTitle, _ := config["version_title"].(string)
	versionMessage, _ := config["version_message"].(string)
	// The publish client fills a missing title/notes side from the revision,
	// so either side present means an update will be created on dispatch.
	needsUpdate := strings.TrimSpace(versionTitle) != "" || strings.TrimSpace(versionMessage) != ""
	var status struct {
		Completed map[string]any `json:"completed_categories"`
	}
	if json.Unmarshal(detail, &status) == nil {
		for category, rawResult := range status.Completed {
			encoded, err := json.Marshal(rawResult)
			if err != nil {
				continue
			}
			var result bandbbs.CategoryResult
			if json.Unmarshal(encoded, &result) == nil && result.ResourceID != "" {
				completed[category] = result
			}
		}
	}
	if len(completed) == 0 {
		return raw, completed
	}
	kept := targets[:0]
	for _, target := range targets {
		var categoryID int
		if json.Unmarshal(target["category_id"], &categoryID) != nil {
			return raw, completed
		}
		result, done := completed[strconv.Itoa(categoryID)]
		fullyDone := done && result.VersionID != "" && (!needsUpdate || result.UpdateID != "")
		if !fullyDone {
			kept = append(kept, target)
		}
	}
	if len(kept) == 0 {
		return nil, completed
	}
	config["targets"] = kept
	filtered, err := json.Marshal(config)
	if err != nil {
		return raw, completed
	}
	return filtered, completed
}

func (c *Coordinator) recordBandBBSPartial(ctx context.Context, item publication, resources map[string]bandbbs.CategoryResult) error {
	completed := map[string]any{}
	var prior struct {
		Completed map[string]any `json:"completed_categories"`
	}
	if json.Unmarshal(item.StatusDetail, &prior) == nil {
		for category, result := range prior.Completed {
			completed[category] = result
		}
	}
	for category, result := range resources {
		completed[category] = result
	}
	raw, _ := json.Marshal(map[string]any{"completed_categories": completed})
	external := map[string]string{}
	for category, result := range resources {
		external[category] = result.ResourceID
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous, previousURL string
	_ = tx.QueryRowContext(ctx, `SELECT external_id,external_url FROM external_bindings WHERE resource_id=$1 AND provider='bandbbs' FOR UPDATE`, item.ResourceID).Scan(&previous, &previousURL)
	merged := map[string]string{}
	_ = json.Unmarshal([]byte(previous), &merged)
	for category, resourceID := range external {
		merged[category] = resourceID
	}
	mergedID, _ := json.Marshal(merged)
	if err := c.upsertBandBBSBinding(ctx, tx, item.ResourceID, string(mergedID), previousURL, []byte("{}")); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE publications SET status_detail=$2 WHERE id=$1 AND state='running'`, item.ID, raw); err != nil {
		return err
	}
	return tx.Commit()
}

func parseBandBBSBinding(raw string, targetCategoryIDs []int) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	canonical := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &canonical); err == nil {
		return canonical, nil
	}
	var historical map[string]struct {
		ResourceID string `json:"resource_id"`
	}
	if err := json.Unmarshal([]byte(raw), &historical); err == nil {
		result := make(map[string]string, len(historical))
		for categoryID, resource := range historical {
			if strings.TrimSpace(resource.ResourceID) == "" {
				return nil, fmt.Errorf("invalid BandBBS binding for category %s", categoryID)
			}
			result[categoryID] = resource.ResourceID
		}
		return result, nil
	}
	if len(targetCategoryIDs) == 1 {
		if _, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return map[string]string{strconv.Itoa(targetCategoryIDs[0]): raw}, nil
		}
	}
	return nil, fmt.Errorf("invalid BandBBS binding; refusing to create duplicate resources")
}

func (c *Coordinator) upsertBandBBSBinding(ctx context.Context, tx *sql.Tx, resourceID, externalID, externalURL string, meta []byte) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-bandbbs-binding'))`); err != nil {
		return fmt.Errorf("lock BandBBS bindings: %w", err)
	}
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT external_id FROM external_bindings WHERE resource_id=$1 AND provider='bandbbs' FOR UPDATE`, resourceID).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	newMap := map[string]string{}
	if err := json.Unmarshal([]byte(externalID), &newMap); err != nil || len(newMap) == 0 {
		return fmt.Errorf("invalid BandBBS binding returned by publisher")
	}
	if existing != "" {
		oldMap := map[string]string{}
		if err := json.Unmarshal([]byte(existing), &oldMap); err != nil {
			return fmt.Errorf("invalid existing BandBBS binding")
		}
		for categoryID, oldResourceID := range oldMap {
			if newMap[categoryID] != oldResourceID {
				return fmt.Errorf("BandBBS category %s is already bound to resource %s", categoryID, oldResourceID)
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT resource_id::text,external_id FROM external_bindings WHERE provider='bandbbs' AND resource_id<>$1`, resourceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var otherResourceID, raw string
		if err := rows.Scan(&otherResourceID, &raw); err != nil {
			return err
		}
		otherMap := map[string]string{}
		if json.Unmarshal([]byte(raw), &otherMap) != nil {
			continue
		}
		for categoryID, externalResourceID := range newMap {
			if otherMap[categoryID] == externalResourceID {
				return fmt.Errorf("BandBBS category %s resource %s is already bound to OronBox resource %s", categoryID, externalResourceID, otherResourceID)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO external_bindings(id,resource_id,provider,external_id,external_url,meta) VALUES(gen_random_uuid(),$1,'bandbbs',$2,$3,$4) ON CONFLICT(resource_id,provider) DO UPDATE SET external_id=excluded.external_id,external_url=excluded.external_url,meta=external_bindings.meta||excluded.meta`, resourceID, externalID, externalURL, meta)
	return err
}
