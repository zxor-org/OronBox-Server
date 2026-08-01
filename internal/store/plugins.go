package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type PluginRecord struct {
	ID               string
	UploaderID       string
	UploaderName     string
	Name             string
	Version          string
	Author           string
	Description      string
	Runtime          string
	Permissions      []string
	State            string
	ModerationReason string
	PackageSHA256    string
	IconSHA256       string
	PackageSize      int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

var ErrPluginNotFound = errors.New("plugin not found")

const pluginColumns = `p.id,p.uploader_id,COALESCE(u.username,''),p.name,p.version,p.author,p.description,p.runtime,p.permissions,p.state,p.moderation_reason,p.package_sha256,COALESCE(p.icon_sha256,''),COALESCE(b.size_bytes,0),p.created_at,p.updated_at`
const pluginTables = ` FROM plugins p LEFT JOIN users u ON u.id=p.uploader_id LEFT JOIN blobs b ON b.sha256=p.package_sha256`

func scanPlugin(row interface{ Scan(...any) error }) (PluginRecord, error) {
	var plugin PluginRecord
	var permissions []byte
	err := row.Scan(
		&plugin.ID, &plugin.UploaderID, &plugin.UploaderName, &plugin.Name,
		&plugin.Version, &plugin.Author, &plugin.Description, &plugin.Runtime,
		&permissions, &plugin.State, &plugin.ModerationReason,
		&plugin.PackageSHA256, &plugin.IconSHA256,
		&plugin.PackageSize, &plugin.CreatedAt, &plugin.UpdatedAt,
	)
	if err != nil {
		return PluginRecord{}, err
	}
	if len(permissions) > 0 {
		if err := json.Unmarshal(permissions, &plugin.Permissions); err != nil {
			return PluginRecord{}, err
		}
	}
	return plugin, nil
}

// ListPlugins returns the public catalog: every listed plugin plus the
// viewer's own plugins in any state so uploaders can track moderation.
func (s *Store) ListPlugins(ctx context.Context, viewerID string) ([]PluginRecord, error) {
	query := `SELECT ` + pluginColumns + pluginTables
	args := []any{}
	if viewerID == "" {
		query += ` WHERE p.state='listed'`
	} else {
		query += ` WHERE p.state='listed' OR p.uploader_id=$1`
		args = append(args, viewerID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY p.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plugins := []PluginRecord{}
	for rows.Next() {
		plugin, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}
	return plugins, rows.Err()
}

// AdminListPlugins returns every plugin regardless of state.
func (s *Store) AdminListPlugins(ctx context.Context) ([]PluginRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pluginColumns+pluginTables+` ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plugins := []PluginRecord{}
	for rows.Next() {
		plugin, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}
	return plugins, rows.Err()
}

func (s *Store) Plugin(ctx context.Context, id string) (PluginRecord, error) {
	plugin, err := scanPlugin(s.db.QueryRowContext(ctx, `SELECT `+pluginColumns+pluginTables+` WHERE p.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PluginRecord{}, ErrPluginNotFound
	}
	return plugin, err
}

// SetPluginState moves a plugin through the moderation states and returns the
// updated record.
func (s *Store) SetPluginState(ctx context.Context, id, state, reason string) (PluginRecord, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE plugins SET state=$2, moderation_reason=$3, updated_at=now() WHERE id=$1`, id, state, reason)
	if err != nil {
		return PluginRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return PluginRecord{}, err
	}
	if affected == 0 {
		return PluginRecord{}, ErrPluginNotFound
	}
	return s.Plugin(ctx, id)
}

// UpsertPlugin inserts a new plugin or replaces the existing row while it
// belongs to the same uploader. It reports false when the id is taken by
// someone else and nothing was written. Every uploaded package returns to
// pending review regardless of the previous state.
func (s *Store) UpsertPlugin(ctx context.Context, plugin PluginRecord) (bool, error) {
	var owner string
	err := s.db.QueryRowContext(ctx, `SELECT uploader_id FROM plugins WHERE id=$1`, plugin.ID).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil && owner != plugin.UploaderID {
		return false, nil
	}
	permissions, err := json.Marshal(plugin.Permissions)
	if err != nil {
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO plugins(id,uploader_id,name,version,author,description,runtime,permissions,package_sha256,icon_sha256)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''))
ON CONFLICT(id) DO UPDATE SET
 name=excluded.name,version=excluded.version,author=excluded.author,description=excluded.description,
 runtime=excluded.runtime,permissions=excluded.permissions,package_sha256=excluded.package_sha256,
 icon_sha256=excluded.icon_sha256,state='pending',moderation_reason='',updated_at=now()`,
		plugin.ID, plugin.UploaderID, plugin.Name, plugin.Version, plugin.Author,
		plugin.Description, plugin.Runtime, permissions, plugin.PackageSHA256, plugin.IconSHA256)
	return true, err
}

// DeletePlugin removes the plugin while it belongs to uploaderID and reports
// whether a row was removed.
func (s *Store) DeletePlugin(ctx context.Context, id, uploaderID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM plugins WHERE id=$1 AND uploader_id=$2`, id, uploaderID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
