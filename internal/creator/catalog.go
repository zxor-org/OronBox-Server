package creator

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var attributePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (s *Service) Attributes(ctx context.Context, includeDisabled bool) ([]ResourceAttribute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT attribute.id,attribute.name_zh,attribute.name_en,attribute.coefficient::float8,attribute.enabled,attribute.position,count(binding.revision_id)::integer FROM resource_attributes attribute LEFT JOIN resource_revision_attributes binding ON binding.attribute=attribute.id WHERE ($1 OR attribute.enabled) GROUP BY attribute.id ORDER BY attribute.position,attribute.name_zh,attribute.id`, includeDisabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ResourceAttribute, 0)
	for rows.Next() {
		var item ResourceAttribute
		if err := rows.Scan(&item.ID, &item.NameZH, &item.NameEN, &item.Coefficient, &item.Enabled, &item.Position, &item.UsageCount); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// PublicTags counts only the current, publicly visible revision. Draft/history
// bindings must not make an otherwise unused filter visible.
func (s *Service) PublicTags(ctx context.Context, forCreator bool) ([]ResourceAttribute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.name_zh,a.name_en,a.coefficient::float8,a.enabled,a.position,
 (SELECT count(DISTINCT r.id)::integer FROM resource_revision_attributes b JOIN resources r ON r.current_revision_id=b.revision_id WHERE b.attribute=a.id AND r.moderation_state='visible') AS uses
 FROM resource_attributes a WHERE a.enabled AND ($1 OR EXISTS(SELECT 1 FROM resource_revision_attributes b JOIN resources r ON r.current_revision_id=b.revision_id WHERE b.attribute=a.id AND r.moderation_state='visible')) ORDER BY a.position,a.name_zh,a.id`, forCreator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ResourceAttribute, 0)
	for rows.Next() {
		var item ResourceAttribute
		if err := rows.Scan(&item.ID, &item.NameZH, &item.NameEN, &item.Coefficient, &item.Enabled, &item.Position, &item.UsageCount); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) UpsertAttribute(ctx context.Context, item ResourceAttribute) error {
	item.ID = strings.TrimSpace(item.ID)
	item.NameZH = strings.TrimSpace(item.NameZH)
	item.NameEN = strings.TrimSpace(item.NameEN)
	if !attributePattern.MatchString(item.ID) || item.NameZH == "" || item.Coefficient <= 0 || item.Coefficient > 10 {
		return fmt.Errorf("%w: resource attribute", ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO resource_attributes(id,name_zh,name_en,coefficient,enabled,position) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET name_zh=excluded.name_zh,name_en=excluded.name_en,coefficient=excluded.coefficient,enabled=excluded.enabled,position=excluded.position,updated_at=now()`, item.ID, item.NameZH, item.NameEN, item.Coefficient, item.Enabled, item.Position)
	return err
}

func (s *Service) DisableAttribute(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE resource_attributes SET enabled=false,updated_at=now() WHERE id=$1`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) attributesExist(ctx context.Context, attributes []string, enabledOnly bool) bool {
	if len(attributes) == 0 {
		return true
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_attributes WHERE id=ANY($1::text[]) AND ($2=false OR enabled)`, attributes, enabledOnly).Scan(&count)
	return err == nil && count == len(attributes)
}

func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,codename,display_name,platform,astrobox_id,vendor FROM devices WHERE enabled AND platform='vela_os' ORDER BY vendor,display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Device
	for rows.Next() {
		var device Device
		if err := rows.Scan(&device.ID, &device.Codename, &device.Name, &device.Platform, &device.AstroBoxID, &device.Vendor); err != nil {
			return nil, err
		}
		result = append(result, device)
	}
	return result, rows.Err()
}
