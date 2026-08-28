package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type AdminDeviceItem struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	Codename      string `json:"codename"`
	Platform      string `json:"platform"`
	Vendor        string `json:"vendor"`
	AstroBoxID    string `json:"astrobox_id"`
	Enabled       bool   `json:"enabled"`
	ResourceCount int    `json:"resource_count"`
	ArtifactCount int    `json:"artifact_count"`
}

type AdminDeviceQuery struct {
	Search   string
	Platform string
	Vendor   string
	State    string
	Sort     string
	Page     int
	PerPage  int
}

func (query AdminDeviceQuery) normalized() AdminDeviceQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Platform = strings.ToLower(strings.TrimSpace(query.Platform))
	query.Vendor = strings.ToLower(strings.TrimSpace(query.Vendor))
	query.State = strings.ToLower(strings.TrimSpace(query.State))
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	if query.Platform != "vela_os" && query.Platform != "zepp_os" {
		query.Platform = ""
	}
	if query.State != "enabled" && query.State != "disabled" {
		query.State = ""
	}
	switch query.Sort {
	case "name", "codename", "platform", "vendor", "resources_desc", "artifacts_desc":
	default:
		query.Sort = "name"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 25
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	return query
}

type AdminDevicePage struct {
	Items      []AdminDeviceItem `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
	TotalPages int               `json:"total_pages"`
	Query      AdminDeviceQuery  `json:"query"`
}

var ErrAdminDeviceNotFound = errors.New("device was not found")
var ErrAdminDeviceConflict = errors.New("device update conflicts with the catalog")

var deviceCodenamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type AdminDeviceInput struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"display_name"`
	Codename    string `json:"codename"`
	Platform    string `json:"platform"`
	Vendor      string `json:"vendor"`
	AstroBoxID  string `json:"astrobox_id"`
	Enabled     bool   `json:"enabled"`
}

func (input AdminDeviceInput) normalized() (AdminDeviceInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Codename = strings.ToLower(strings.TrimSpace(input.Codename))
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Vendor = strings.ToLower(strings.TrimSpace(input.Vendor))
	input.AstroBoxID = strings.TrimSpace(input.AstroBoxID)
	if input.DisplayName == "" || len(input.DisplayName) > 120 || !deviceCodenamePattern.MatchString(input.Codename) || (input.Platform != "vela_os" && input.Platform != "zepp_os") || len(input.Vendor) > 80 || len(input.AstroBoxID) > 120 {
		return input, fmt.Errorf("%w: invalid device fields", ErrAdminDeviceConflict)
	}
	return input, nil
}

const adminDeviceCountsSQL = `
LEFT JOIN (
 SELECT binding.device_id,
        count(DISTINCT revision.resource_id)::integer AS resource_count,
        count(DISTINCT binding.artifact_id)::integer AS artifact_count
 FROM revision_artifact_devices binding
 JOIN resource_revisions revision ON revision.id=binding.revision_id
 GROUP BY binding.device_id
) usage ON usage.device_id=device.id`

func (s *Store) AdminDevices(ctx context.Context, raw AdminDeviceQuery) (AdminDevicePage, error) {
	query := raw.normalized()
	filter := `($1='' OR device.display_name ILIKE '%'||$1||'%' OR device.codename ILIKE '%'||$1||'%' OR device.astrobox_id ILIKE '%'||$1||'%')
 AND ($2='' OR device.platform=$2)
 AND ($3='' OR lower(device.vendor)=$3)
 AND ($4='' OR ($4='enabled' AND device.enabled) OR ($4='disabled' AND NOT device.enabled))`

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM devices device WHERE `+filter,
		query.Search, query.Platform, query.Vendor, query.State).Scan(&total); err != nil {
		return AdminDevicePage{}, err
	}

	orderBy := map[string]string{
		"name":           "lower(device.display_name), device.codename",
		"codename":       "lower(device.codename), lower(device.display_name)",
		"platform":       "device.platform, lower(device.display_name), device.codename",
		"vendor":         "lower(device.vendor), lower(device.display_name), device.codename",
		"resources_desc": "COALESCE(usage.resource_count,0) DESC, lower(device.display_name), device.codename",
		"artifacts_desc": "COALESCE(usage.artifact_count,0) DESC, lower(device.display_name), device.codename",
	}[query.Sort]
	rows, err := s.db.QueryContext(ctx, `
SELECT device.id::text,device.display_name,device.codename,device.platform,device.vendor,device.astrobox_id,device.enabled,
       COALESCE(usage.resource_count,0),COALESCE(usage.artifact_count,0)
FROM devices device
`+adminDeviceCountsSQL+`
WHERE `+filter+`
ORDER BY `+orderBy+` LIMIT $5 OFFSET $6`,
		query.Search, query.Platform, query.Vendor, query.State, query.PerPage, (query.Page-1)*query.PerPage)
	if err != nil {
		return AdminDevicePage{}, err
	}
	defer rows.Close()

	page := AdminDevicePage{
		Items:   []AdminDeviceItem{},
		Total:   total,
		Page:    query.Page,
		PerPage: query.PerPage,
		Query:   query,
	}
	for rows.Next() {
		var item AdminDeviceItem
		if err := rows.Scan(&item.ID, &item.DisplayName, &item.Codename, &item.Platform, &item.Vendor, &item.AstroBoxID, &item.Enabled, &item.ResourceCount, &item.ArtifactCount); err != nil {
			return AdminDevicePage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminDevicePage{}, err
	}
	page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	return page, nil
}

func (s *Store) AdminDevice(ctx context.Context, id string) (AdminDeviceItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AdminDeviceItem{}, ErrAdminDeviceNotFound
	}
	var item AdminDeviceItem
	err := s.db.QueryRowContext(ctx, `
SELECT device.id::text,device.display_name,device.codename,device.platform,device.vendor,device.astrobox_id,device.enabled,
       COALESCE(usage.resource_count,0),COALESCE(usage.artifact_count,0)
FROM devices device
`+adminDeviceCountsSQL+`
WHERE device.id::text=$1 OR device.codename=$1`, id).Scan(
		&item.ID, &item.DisplayName, &item.Codename, &item.Platform, &item.Vendor, &item.AstroBoxID, &item.Enabled,
		&item.ResourceCount, &item.ArtifactCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminDeviceItem{}, ErrAdminDeviceNotFound
	}
	if err != nil {
		return AdminDeviceItem{}, err
	}
	return item, nil
}

func (s *Store) AdminSaveDevice(ctx context.Context, raw AdminDeviceInput) (AdminDeviceItem, error) {
	input, err := raw.normalized()
	if err != nil {
		return AdminDeviceItem{}, err
	}
	id := input.ID
	if id == "" {
		id = uuid.NewString()
	} else if _, err := uuid.Parse(id); err != nil {
		return AdminDeviceItem{}, ErrAdminDeviceNotFound
	}
	var result sql.Result
	if input.ID == "" {
		result, err = s.db.ExecContext(ctx, `INSERT INTO devices(id,codename,display_name,platform,astrobox_id,vendor,enabled) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, input.Codename, input.DisplayName, input.Platform, input.AstroBoxID, input.Vendor, input.Enabled)
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE devices SET codename=$2,display_name=$3,platform=$4,astrobox_id=$5,vendor=$6,enabled=$7,updated_at=now() WHERE id=$1`, id, input.Codename, input.DisplayName, input.Platform, input.AstroBoxID, input.Vendor, input.Enabled)
	}
	if err != nil {
		return AdminDeviceItem{}, fmt.Errorf("%w: codename or AstroBox ID is already used", ErrAdminDeviceConflict)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return AdminDeviceItem{}, ErrAdminDeviceNotFound
	}
	return s.AdminDevice(ctx, id)
}
