package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AdminPluginQuery describes the complete administrative plugin catalogue.
// Uploader accepts either an uploader UUID or a partial username.
type AdminPluginQuery struct {
	Search   string
	State    string
	Uploader string
	Runtime  string
	Sort     string
	Page     int
	PerPage  int
}

func (query AdminPluginQuery) normalized() AdminPluginQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.State = strings.ToLower(strings.TrimSpace(query.State))
	query.Uploader = strings.TrimSpace(query.Uploader)
	query.Runtime = strings.ToLower(strings.TrimSpace(query.Runtime))
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	switch query.State {
	case "pending", "listed", "rejected", "delisted":
	default:
		query.State = ""
	}
	switch query.Runtime {
	case "js", "wasm", "hybrid":
	default:
		query.Runtime = ""
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "created_asc", "name", "uploader", "size_desc", "size_asc":
	default:
		query.Sort = "updated_desc"
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

func adminPluginOrder(sort string) string {
	return map[string]string{
		"updated_desc": "p.updated_at DESC, p.id ASC",
		"updated_asc":  "p.updated_at ASC, p.id ASC",
		"created_desc": "p.created_at DESC, p.id ASC",
		"created_asc":  "p.created_at ASC, p.id ASC",
		"name":         "lower(p.name), p.id ASC",
		"uploader":     "lower(COALESCE(u.username,'')), lower(p.name), p.id ASC",
		"size_desc":    "COALESCE(package_blob.size_bytes,0) DESC, p.updated_at DESC, p.id ASC",
		"size_asc":     "COALESCE(package_blob.size_bytes,0) ASC, p.updated_at DESC, p.id ASC",
	}[sort]
}

type AdminPluginItem struct {
	PluginRecord
	UploaderCreatedAt time.Time
	PackageMediaType  string
	IconSize          int64
	IconMediaType     string
	PendingVersionID  string
}

type AdminPluginPage struct {
	Items      []AdminPluginItem `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
	TotalPages int               `json:"total_pages"`
	Query      AdminPluginQuery  `json:"query"`
}

// AdminPluginVersion is an immutable plugin version snapshot.
type AdminPluginVersion struct {
	ID               string    `json:"id"`
	Number           int       `json:"number"`
	Version          string    `json:"version"`
	Name             string    `json:"name"`
	Author           string    `json:"author"`
	Description      string    `json:"description"`
	Runtime          string    `json:"runtime"`
	Permissions      []string  `json:"permissions"`
	State            string    `json:"state"`
	ModerationReason string    `json:"moderation_reason,omitempty"`
	PackageSHA256    string    `json:"package_sha256"`
	IconSHA256       string    `json:"icon_sha256,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	CreatedVia       string    `json:"created_via"`
}

type AdminPluginDetail struct {
	Plugin           AdminPluginItem      `json:"plugin"`
	Versions         []AdminPluginVersion `json:"versions"`
	HistorySupported bool                 `json:"history_supported"`
}

const adminPluginsV2FromSQL = `FROM plugins p
JOIN users u ON u.id=p.uploader_id
JOIN blobs package_blob ON package_blob.sha256=p.package_sha256
LEFT JOIN blobs icon_blob ON icon_blob.sha256=p.icon_sha256`

const adminPluginsV2Columns = `p.id,p.uploader_id,u.username,p.name,p.version,p.author,p.description,p.runtime,p.permissions,
p.state,p.moderation_reason,p.package_sha256,COALESCE(p.icon_sha256,''),package_blob.size_bytes,p.created_at,p.updated_at,
u.created_at,package_blob.media_type,COALESCE(icon_blob.size_bytes,0),COALESCE(icon_blob.media_type,''),COALESCE(p.pending_version_id::text,'')`

func scanAdminPlugin(row interface{ Scan(...any) error }) (AdminPluginItem, error) {
	var item AdminPluginItem
	var permissions []byte
	err := row.Scan(&item.ID, &item.UploaderID, &item.UploaderName, &item.Name, &item.Version,
		&item.Author, &item.Description, &item.Runtime, &permissions, &item.State,
		&item.ModerationReason, &item.PackageSHA256, &item.IconSHA256, &item.PackageSize,
		&item.CreatedAt, &item.UpdatedAt, &item.UploaderCreatedAt, &item.PackageMediaType,
		&item.IconSize, &item.IconMediaType, &item.PendingVersionID)
	if err != nil {
		return AdminPluginItem{}, err
	}
	if len(permissions) > 0 {
		if err := json.Unmarshal(permissions, &item.Permissions); err != nil {
			return AdminPluginItem{}, err
		}
	}
	return item, nil
}

func (s *Store) AdminPluginsV2(ctx context.Context, raw AdminPluginQuery) (AdminPluginPage, error) {
	query := raw.normalized()
	filter := `WHERE ($1='' OR p.id ILIKE '%'||$1||'%' OR p.name ILIKE '%'||$1||'%' OR p.author ILIKE '%'||$1||'%' OR p.description ILIKE '%'||$1||'%' OR p.version ILIKE '%'||$1||'%')
 AND ($2='' OR ($2='pending' AND p.pending_version_id IS NOT NULL) OR ($2<>'pending' AND p.state=$2))
 AND ($3='' OR p.uploader_id::text=$3 OR u.username ILIKE '%'||$3||'%')
 AND ($4='' OR p.runtime=$4)`
	args := []any{query.Search, query.State, query.Uploader, query.Runtime}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+adminPluginsV2FromSQL+` `+filter, args...).Scan(&total); err != nil {
		return AdminPluginPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+adminPluginsV2Columns+` `+adminPluginsV2FromSQL+` `+filter+
		` ORDER BY `+adminPluginOrder(query.Sort)+` LIMIT $5 OFFSET $6`, append(args, query.PerPage, (query.Page-1)*query.PerPage)...)
	if err != nil {
		return AdminPluginPage{}, err
	}
	defer rows.Close()
	page := AdminPluginPage{Items: []AdminPluginItem{}, Total: total, Page: query.Page, PerPage: query.PerPage, Query: query}
	for rows.Next() {
		item, err := scanAdminPlugin(rows)
		if err != nil {
			return AdminPluginPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminPluginPage{}, err
	}
	page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	return page, nil
}

func (s *Store) AdminPluginV2(ctx context.Context, id string) (AdminPluginDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AdminPluginDetail{}, ErrPluginNotFound
	}
	item, err := scanAdminPlugin(s.db.QueryRowContext(ctx, `SELECT `+adminPluginsV2Columns+` `+adminPluginsV2FromSQL+` WHERE p.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminPluginDetail{}, ErrPluginNotFound
	}
	if err != nil {
		return AdminPluginDetail{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,revision_no,version,name,author,description,runtime,permissions,state,moderation_reason,package_sha256,COALESCE(icon_sha256,''),created_at,updated_at,created_via FROM plugin_versions WHERE plugin_id=$1 ORDER BY revision_no DESC`, id)
	if err != nil {
		return AdminPluginDetail{}, err
	}
	defer rows.Close()
	versions := []AdminPluginVersion{}
	for rows.Next() {
		var version AdminPluginVersion
		var permissions []byte
		if err := rows.Scan(&version.ID, &version.Number, &version.Version, &version.Name, &version.Author, &version.Description, &version.Runtime, &permissions, &version.State, &version.ModerationReason, &version.PackageSHA256, &version.IconSHA256, &version.CreatedAt, &version.UpdatedAt, &version.CreatedVia); err != nil {
			return AdminPluginDetail{}, err
		}
		_ = json.Unmarshal(permissions, &version.Permissions)
		versions = append(versions, version)
	}
	return AdminPluginDetail{Plugin: item, Versions: versions, HistorySupported: true}, rows.Err()
}

type AdminPluginMetadataRevisionInput struct {
	Name        string `json:"name"`
	Author      string `json:"author"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by,omitempty"`
}

type AdminPluginPackageRevisionInput struct {
	Version       string   `json:"version"`
	Name          string   `json:"name"`
	Author        string   `json:"author"`
	Description   string   `json:"description"`
	Runtime       string   `json:"runtime"`
	PackageSHA256 string   `json:"package_sha256"`
	IconSHA256    string   `json:"icon_sha256"`
	CreatedBy     string   `json:"created_by,omitempty"`
	Permissions   []string `json:"permissions"`
}

func (s *Store) AdminCreatePluginPackageRevision(ctx context.Context, id string, input AdminPluginPackageRevisionInput) (AdminPluginVersion, error) {
	id = strings.TrimSpace(id)
	input.Name = strings.TrimSpace(input.Name)
	input.Version = strings.TrimSpace(input.Version)
	if id == "" || input.Name == "" || input.Version == "" || input.PackageSHA256 == "" {
		return AdminPluginVersion{}, fmt.Errorf("invalid plugin package revision")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminPluginVersion{}, err
	}
	defer tx.Rollback()
	var currentID, pendingID string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(current_version_id::text,''),COALESCE(pending_version_id::text,'') FROM plugins WHERE id=$1 FOR UPDATE`, id).Scan(&currentID, &pendingID); errors.Is(err, sql.ErrNoRows) {
		return AdminPluginVersion{}, ErrPluginNotFound
	} else if err != nil {
		return AdminPluginVersion{}, err
	}
	if pendingID != "" {
		return AdminPluginVersion{}, fmt.Errorf("plugin already has a pending version")
	}
	var number int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM plugin_versions WHERE plugin_id=$1`, id).Scan(&number); err != nil {
		return AdminPluginVersion{}, err
	}
	permissions, _ := json.Marshal(input.Permissions)
	version := AdminPluginVersion{ID: uuid.NewString(), Number: number, Version: input.Version, Name: input.Name, Author: strings.TrimSpace(input.Author), Description: strings.TrimSpace(input.Description), Runtime: input.Runtime, Permissions: input.Permissions, State: "pending", PackageSHA256: input.PackageSHA256, IconSHA256: input.IconSHA256, CreatedVia: "admin"}
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_versions(id,plugin_id,revision_no,version,name,author,description,runtime,permissions,package_sha256,icon_sha256,state,created_by,created_via,base_version_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),'pending',NULLIF($12,'')::uuid,'admin',NULLIF($13,'')::uuid)`, version.ID, id, number, version.Version, version.Name, version.Author, version.Description, version.Runtime, permissions, version.PackageSHA256, version.IconSHA256, input.CreatedBy, currentID)
	if err != nil {
		return AdminPluginVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plugins SET pending_version_id=$2,updated_at=now() WHERE id=$1`, id, version.ID); err != nil {
		return AdminPluginVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return AdminPluginVersion{}, err
	}
	return version, nil
}

func (input AdminPluginMetadataRevisionInput) normalized() (AdminPluginMetadataRevisionInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Author = strings.TrimSpace(input.Author)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 160 || len(input.Author) > 160 || len(input.Description) > 8000 {
		return input, fmt.Errorf("invalid plugin metadata")
	}
	return input, nil
}

func (s *Store) AdminCreatePluginMetadataRevision(ctx context.Context, id string, input AdminPluginMetadataRevisionInput) (AdminPluginVersion, error) {
	if strings.TrimSpace(id) == "" {
		return AdminPluginVersion{}, ErrPluginNotFound
	}
	normalized, err := input.normalized()
	if err != nil {
		return AdminPluginVersion{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminPluginVersion{}, err
	}
	defer tx.Rollback()
	var plugin PluginRecord
	var permissions []byte
	var currentID, pendingID string
	err = tx.QueryRowContext(ctx, `SELECT uploader_id::text,name,version,author,description,runtime,permissions,state,moderation_reason,package_sha256,COALESCE(icon_sha256,''),COALESCE(current_version_id::text,''),COALESCE(pending_version_id::text,'') FROM plugins WHERE id=$1 FOR UPDATE`, id).Scan(&plugin.UploaderID, &plugin.Name, &plugin.Version, &plugin.Author, &plugin.Description, &plugin.Runtime, &permissions, &plugin.State, &plugin.ModerationReason, &plugin.PackageSHA256, &plugin.IconSHA256, &currentID, &pendingID)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminPluginVersion{}, ErrPluginNotFound
	}
	if err != nil {
		return AdminPluginVersion{}, err
	}
	_ = json.Unmarshal(permissions, &plugin.Permissions)
	if pendingID != "" {
		return AdminPluginVersion{}, fmt.Errorf("plugin already has a pending version")
	}
	var number int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM plugin_versions WHERE plugin_id=$1`, id).Scan(&number); err != nil {
		return AdminPluginVersion{}, err
	}
	version := AdminPluginVersion{ID: uuid.NewString(), Number: number, Version: plugin.Version, Name: normalized.Name, Author: normalized.Author, Description: normalized.Description, Runtime: plugin.Runtime, Permissions: plugin.Permissions, State: "pending", PackageSHA256: plugin.PackageSHA256, IconSHA256: plugin.IconSHA256, CreatedVia: "admin"}
	if _, err = tx.ExecContext(ctx, `INSERT INTO plugin_versions(id,plugin_id,revision_no,version,name,author,description,runtime,permissions,package_sha256,icon_sha256,state,created_by,created_via,base_version_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),'pending',NULLIF($12,'')::uuid,'admin',NULLIF($13,'')::uuid)`, version.ID, id, number, version.Version, version.Name, version.Author, version.Description, version.Runtime, permissions, version.PackageSHA256, version.IconSHA256, normalized.CreatedBy, currentID); err != nil {
		return AdminPluginVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plugins SET pending_version_id=$2,updated_at=now() WHERE id=$1`, id, version.ID); err != nil {
		return AdminPluginVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return AdminPluginVersion{}, err
	}
	return version, nil
}
