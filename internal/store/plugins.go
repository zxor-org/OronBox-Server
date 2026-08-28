package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
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
	PendingVersionID string
	PendingState     string
	PendingReason    string
}

var ErrPluginNotFound = errors.New("plugin not found")

const pluginColumns = `p.id,p.uploader_id,COALESCE(u.username,''),p.name,p.version,p.author,p.description,p.runtime,p.permissions,p.state,p.moderation_reason,p.package_sha256,COALESCE(p.icon_sha256,''),COALESCE(b.size_bytes,0),p.created_at,p.updated_at,COALESCE(p.pending_version_id::text,''),COALESCE(pv.state,''),COALESCE(pv.moderation_reason,'')`
const pluginTables = ` FROM plugins p LEFT JOIN users u ON u.id=p.uploader_id LEFT JOIN blobs b ON b.sha256=p.package_sha256 LEFT JOIN plugin_versions pv ON pv.id=p.pending_version_id`

func scanPlugin(row interface{ Scan(...any) error }) (PluginRecord, error) {
	var plugin PluginRecord
	var permissions []byte
	err := row.Scan(
		&plugin.ID, &plugin.UploaderID, &plugin.UploaderName, &plugin.Name,
		&plugin.Version, &plugin.Author, &plugin.Description, &plugin.Runtime,
		&permissions, &plugin.State, &plugin.ModerationReason,
		&plugin.PackageSHA256, &plugin.IconSHA256,
		&plugin.PackageSize, &plugin.CreatedAt, &plugin.UpdatedAt,
		&plugin.PendingVersionID, &plugin.PendingState, &plugin.PendingReason,
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
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return PluginRecord{}, err
	}
	defer tx.Rollback()
	var pendingID, currentID, currentState string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(pending_version_id::text,''),COALESCE(current_version_id::text,''),state FROM plugins WHERE id=$1 FOR UPDATE`, id).Scan(&pendingID, &currentID, &currentState)
	if errors.Is(err, sql.ErrNoRows) {
		return PluginRecord{}, ErrPluginNotFound
	}
	if err != nil {
		return PluginRecord{}, err
	}
	if pendingID != "" && (state == "listed" || state == "rejected") {
		versionState := state
		if _, err = tx.ExecContext(ctx, `UPDATE plugin_versions SET state=$2,moderation_reason=$3,updated_at=now() WHERE id=$1 AND state='pending'`, pendingID, versionState, reason); err != nil {
			return PluginRecord{}, err
		}
		if state == "listed" {
			if _, err = tx.ExecContext(ctx, `UPDATE plugin_versions SET state='superseded',updated_at=now() WHERE plugin_id=$1 AND state='listed' AND id<>$2`, id, pendingID); err != nil {
				return PluginRecord{}, err
			}
			_, err = tx.ExecContext(ctx, `UPDATE plugins plugin SET name=version.name,version=version.version,author=version.author,description=version.description,runtime=version.runtime,permissions=version.permissions,package_sha256=version.package_sha256,icon_sha256=version.icon_sha256,state='listed',moderation_reason='',current_version_id=version.id,pending_version_id=NULL,updated_at=now() FROM plugin_versions version WHERE plugin.id=$1 AND version.id=$2`, id, pendingID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE plugins SET pending_version_id=NULL,state=CASE WHEN $3='' THEN 'rejected' ELSE state END,moderation_reason=$2,updated_at=now() WHERE id=$1`, id, reason, currentID)
		}
		if err == nil {
			// A new plugin, or a delisted plugin being restored, changes the
			// plugins.state column and is notified by the database trigger. An
			// update that stays listed/delisted does not fire that trigger, so it
			// still needs the explicit version-review notification below.
			triggerNotifies := (state == "listed" && currentState != "listed") ||
				(state == "rejected" && currentID == "" && currentState != "rejected")
			if !triggerNotifies {
				event := "plugin.approved"
				if state == "rejected" {
					event = "plugin.rejected"
				}
				_, err = tx.ExecContext(ctx, `INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) SELECT $2,uploader_id,'moderation',$3,jsonb_build_object('plugin_id',id,'state',$4::text,'reason',$5::text),'','',id FROM plugins WHERE id=$1`, id, uuid.NewString(), event, state, reason)
			}
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE plugins SET state=$2,moderation_reason=$3,updated_at=now() WHERE id=$1`, id, state, reason)
	}
	if err != nil {
		return PluginRecord{}, err
	}
	if err = tx.Commit(); err != nil {
		return PluginRecord{}, err
	}
	return s.Plugin(ctx, id)
}

// UpsertPlugin inserts a new plugin or replaces the existing row while it
// belongs to the same uploader. It reports false when the id is taken by
// someone else and nothing was written. Every uploaded package returns to
// pending review regardless of the previous state.
func (s *Store) UpsertPlugin(ctx context.Context, plugin PluginRecord) (bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var owner, currentID, pendingID string
	lookupErr := tx.QueryRowContext(ctx, `SELECT uploader_id::text,COALESCE(current_version_id::text,''),COALESCE(pending_version_id::text,'') FROM plugins WHERE id=$1 FOR UPDATE`, plugin.ID).Scan(&owner, &currentID, &pendingID)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return false, lookupErr
	}
	if lookupErr == nil && owner != plugin.UploaderID {
		return false, nil
	}
	permissions, err := json.Marshal(plugin.Permissions)
	if err != nil {
		return false, err
	}
	if errors.Is(lookupErr, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO plugins(id,uploader_id,name,version,author,description,runtime,permissions,package_sha256,icon_sha256,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),'pending')`, plugin.ID, plugin.UploaderID, plugin.Name, plugin.Version, plugin.Author, plugin.Description, plugin.Runtime, permissions, plugin.PackageSHA256, plugin.IconSHA256); err != nil {
			return false, err
		}
		currentID = ""
	} else if lookupErr != nil {
		return false, lookupErr
	}
	if pendingID != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE plugin_versions SET state='superseded',updated_at=now() WHERE id=$1 AND state='pending'`, pendingID); err != nil {
			return false, err
		}
	}
	var revisionNo int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM plugin_versions WHERE plugin_id=$1`, plugin.ID).Scan(&revisionNo); err != nil {
		return false, err
	}
	versionID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO plugin_versions(id,plugin_id,revision_no,version,name,author,description,runtime,permissions,package_sha256,icon_sha256,state,created_by,created_via,base_version_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),'pending',$12,'uploader',NULLIF($13,'')::uuid)`, versionID, plugin.ID, revisionNo, plugin.Version, plugin.Name, plugin.Author, plugin.Description, plugin.Runtime, permissions, plugin.PackageSHA256, plugin.IconSHA256, plugin.UploaderID, currentID); err != nil {
		return false, err
	}
	if currentID == "" {
		_, err = tx.ExecContext(ctx, `UPDATE plugins SET name=$2,version=$3,author=$4,description=$5,runtime=$6,permissions=$7,package_sha256=$8,icon_sha256=NULLIF($9,''),state='pending',moderation_reason='',pending_version_id=$10,updated_at=now() WHERE id=$1`, plugin.ID, plugin.Name, plugin.Version, plugin.Author, plugin.Description, plugin.Runtime, permissions, plugin.PackageSHA256, plugin.IconSHA256, versionID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE plugins SET pending_version_id=$2,updated_at=now() WHERE id=$1`, plugin.ID, versionID)
	}
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
