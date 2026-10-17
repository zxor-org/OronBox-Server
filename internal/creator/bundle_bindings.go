package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (s *Service) verifyBundleBindings(ctx context.Context, tx *sql.Tx, artifacts []verifiedArtifact) error {
	claimed := map[string]bool{}
	for _, artifact := range artifacts {
		for _, deviceID := range unique(artifact.deviceIDs) {
			if claimed[deviceID] {
				return fmt.Errorf("%w: device %s is bound to more than one resource file", ErrInvalid, deviceID)
			}
			claimed[deviceID] = true
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1 AND platform='vela_os' AND codename NOT IN ('m66','n69'))`, deviceID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: unknown VelaOS device %s", ErrInvalid, deviceID)
			}
		}
	}
	return nil
}

func validateManifestBindings(bindings []publishBindingRef) error {
	if len(bindings) > 8 {
		return fmt.Errorf("%w: external bindings", ErrInvalid)
	}
	seen := map[string]bool{}
	for index := range bindings {
		binding := &bindings[index]
		binding.Provider = strings.TrimSpace(binding.Provider)
		binding.ExternalID = strings.TrimSpace(binding.ExternalID)
		binding.ExternalURL = strings.TrimSpace(binding.ExternalURL)
		if seen[binding.Provider] || (binding.Provider != "bandbbs" && binding.Provider != "astrobox") {
			return fmt.Errorf("%w: external bindings", ErrInvalid)
		}
		seen[binding.Provider] = true
		switch binding.Provider {
		case "bandbbs":
			// BandBBS binds one resource per device category as a JSON map
			// of category id to resource id.
			targets := map[string]string{}
			if err := json.Unmarshal([]byte(binding.ExternalID), &targets); err != nil || len(targets) == 0 || len(targets) > 64 {
				return fmt.Errorf("%w: BandBBS binding must map category ids to resource ids", ErrInvalid)
			}
			for categoryID, resourceID := range targets {
				if !digitsOnly(categoryID) || !digitsOnly(resourceID) {
					return fmt.Errorf("%w: BandBBS binding ids", ErrInvalid)
				}
			}
		case "astrobox":
			if binding.ExternalID == "" || len(binding.ExternalID) > 128 || strings.ContainsAny(binding.ExternalID, ",\r\n\x00") {
				return fmt.Errorf("%w: AstroBox binding id", ErrInvalid)
			}
		}
		if binding.ExternalURL != "" {
			parsed, parseErr := url.ParseRequestURI(binding.ExternalURL)
			if parseErr != nil || len(binding.ExternalURL) > 2048 || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("%w: external binding url", ErrInvalid)
			}
		}
		if len(binding.Meta) > 8 {
			return fmt.Errorf("%w: external binding meta", ErrInvalid)
		}
		for key, value := range binding.Meta {
			if len(key) > 64 || len(value) > 256 || strings.ContainsAny(key, "\r\n\x00") || strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("%w: external binding meta", ErrInvalid)
			}
		}
	}
	return nil
}

// saveManifestBindings records imported external identities. An identity
// already bound to another resource is rejected so one external resource can
// never be claimed by two OronBox resources.
func (s *Service) saveManifestBindings(ctx context.Context, tx *sql.Tx, resourceID, ownerID string, bindings []publishBindingRef) error {
	for _, binding := range bindings {
		var holder string
		err := tx.QueryRowContext(ctx, `SELECT resource_id::text FROM external_bindings WHERE provider=$1 AND external_id=$2`, binding.Provider, binding.ExternalID).Scan(&holder)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if holder != "" && holder != resourceID {
			return fmt.Errorf("%w: external %s identity is already bound to another resource", ErrConflict, binding.Provider)
		}
		meta := []byte("{}")
		if len(binding.Meta) > 0 {
			meta, err = json.Marshal(binding.Meta)
			if err != nil {
				return fmt.Errorf("%w: external binding meta", ErrInvalid)
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO external_bindings(id,resource_id,provider,external_id,external_url,meta,origin,verified_at,verified_by) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,'imported',now(),$6) ON CONFLICT(resource_id,provider) DO UPDATE SET external_id=excluded.external_id,external_url=excluded.external_url,meta=excluded.meta,verified_at=now(),verified_by=excluded.verified_by`, resourceID, binding.Provider, binding.ExternalID, binding.ExternalURL, meta, ownerID); err != nil {
			var state interface{ SQLState() string }
			if errors.As(err, &state) && state.SQLState() == "23505" {
				return fmt.Errorf("%w: external identity is already bound", ErrConflict)
			}
			return err
		}
	}
	return nil
}

func digitsOnly(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *Service) bindAstroBoxPlan(ctx context.Context, ownerID, resourceID string, plans []PublicationRequest) error {
	for i := range plans {
		p := &plans[i]
		if p.Target != PublishAstroBox {
			continue
		}
		if p.Config == nil {
			p.Config = map[string]any{}
		}
		requested, _ := p.Config["update_existing"].(bool)
		delete(p.Config, "pull_request_number")
		p.Config["update_existing"] = false
		var raw []byte
		err := s.db.QueryRowContext(ctx, `SELECT p.status_detail FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources r ON r.id=rr.resource_id WHERE r.id=$1 AND r.owner_id=$2 AND p.target='astrobox' AND p.state='reviewing' ORDER BY p.updated_at DESC LIMIT 1`, resourceID, ownerID).Scan(&raw)
		if err == nil {
			var detail struct {
				Number int `json:"pull_request_number"`
			}
			if json.Unmarshal(raw, &detail) != nil || detail.Number <= 0 {
				return fmt.Errorf("%w: incomplete AstroBox PR binding", ErrConflict)
			}
			p.Config["update_existing"] = true
			p.Config["pull_request_number"] = detail.Number
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if requested {
			return fmt.Errorf("%w: no active AstroBox submission to update", ErrConflict)
		}
		var item string
		var metaRaw []byte
		err = s.db.QueryRowContext(ctx, `SELECT b.external_id,b.meta FROM external_bindings b JOIN resources r ON r.id=b.resource_id WHERE r.id=$1 AND r.owner_id=$2 AND b.provider='astrobox'`, resourceID, ownerID).Scan(&item, &metaRaw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		meta := map[string]string{}
		if err = json.Unmarshal(metaRaw, &meta); err != nil {
			return err
		}
		for key, value := range map[string]string{"item_id": item, "repo_name": meta["repo_name"], "repo_owner": meta["repo_owner"]} {
			if value == "" {
				continue
			}
			if submitted, ok := p.Config[key].(string); ok && submitted != "" && !strings.EqualFold(submitted, value) {
				return fmt.Errorf("%w: bound AstroBox %s cannot be replaced", ErrConflict, key)
			}
			p.Config[key] = value
		}
	}
	return nil
}
