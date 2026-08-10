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

const (
	adminPublicationDefaultPerPage = 25
	adminPublicationMaxPerPage     = 100
)

var ErrAdminPublicationNotFound = errors.New("publication was not found")
var ErrAdminPublicationConflict = errors.New("publication state transition is not allowed")

type AdminPublicationQuery struct {
	Search   string
	Target   string
	State    string
	Resource string
	Owner    string
	Sort     string
	Page     int
	PerPage  int
}

type AdminPublicationDevice struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Codename    string `json:"codename"`
	Platform    string `json:"platform"`
}

type AdminPublicationItem struct {
	AdminPublication
	ResourceID       string
	ResourceSlug     string
	ResourceKind     string
	ResourcePlatform string
	RevisionID       string
	RevisionNumber   int
	RevisionName     string
	RevisionState    string
	OwnerID          string
	Owner            string
	Devices          []AdminPublicationDevice
	CreatedAt        time.Time
	History          []AdminPublicationAttempt
}

type AdminPublicationAttempt struct {
	ID            int64
	AttemptNumber int
	Phase         string
	Event         string
	StateFrom     string
	StateTo       string
	ErrorMessage  string
	Detail        json.RawMessage
	CreatedAt     time.Time
}

type AdminPublicationPage struct {
	Items      []AdminPublicationItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminPublicationQuery
}

type AdminPublicationBatchResult struct {
	Matched int
	Retried int
}

func (query AdminPublicationQuery) normalized() AdminPublicationQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Resource = strings.TrimSpace(query.Resource)
	query.Owner = strings.TrimSpace(query.Owner)
	if query.Target != "oronbox" && query.Target != "bandbbs" && query.Target != "astrobox" {
		query.Target = ""
	}
	if query.State != "pending" && query.State != "running" && query.State != "published" && query.State != "reviewing" && query.State != "failed" && query.State != "cancelled" {
		query.State = ""
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "created_asc", "attempts_desc", "attempts_asc":
	default:
		query.Sort = "updated_desc"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = adminPublicationDefaultPerPage
	}
	if query.PerPage > adminPublicationMaxPerPage {
		query.PerPage = adminPublicationMaxPerPage
	}
	return query
}

func adminPublicationOrder(sort string) string {
	switch sort {
	case "updated_asc":
		return "p.updated_at ASC, p.id ASC"
	case "created_desc":
		return "p.created_at DESC, p.id DESC"
	case "created_asc":
		return "p.created_at ASC, p.id ASC"
	case "attempts_desc":
		return "p.attempts DESC, p.updated_at DESC, p.id DESC"
	case "attempts_asc":
		return "p.attempts ASC, p.updated_at DESC, p.id DESC"
	default:
		return "p.updated_at DESC, p.id DESC"
	}
}

func (s *Store) AdminPublications(ctx context.Context, raw AdminPublicationQuery) (AdminPublicationPage, error) {
	query := raw.normalized()
	args := make([]any, 0, 7)
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.Search != "" {
		add(`concat_ws(' ',p.id::text,p.target,p.state,p.external_id,p.external_url,p.error_message,r.id::text,r.slug,rr.id::text,rr.name,u.id::text,u.username) ILIKE '%'||?||'%'`, query.Search)
	}
	if query.Target != "" {
		add(`p.target=?`, query.Target)
	}
	if query.State != "" {
		add(`p.state=?`, query.State)
	}
	if query.Resource != "" {
		add(`(r.id::text=? OR r.slug ILIKE '%'||?||'%' OR rr.name ILIKE '%'||?||'%')`, query.Resource)
	}
	if query.Owner != "" {
		add(`(u.id::text=? OR u.username ILIKE '%'||?||'%')`, query.Owner)
	}

	base := `FROM publications p
JOIN resource_revisions rr ON rr.id=p.revision_id
JOIN resources r ON r.id=rr.resource_id
JOIN users u ON u.id=r.owner_id
WHERE ` + strings.Join(where, " AND ")
	page := AdminPublicationPage{Items: []AdminPublicationItem{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+base, args...).Scan(&page.Total); err != nil {
		return AdminPublicationPage{}, err
	}

	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT p.id::text,p.target,p.state,p.config,p.status_detail,p.external_id,p.external_url,p.error_message,
 p.attempts,p.next_attempt_at,p.created_at,p.updated_at,
 r.id::text,r.slug,r.kind,r.platform,
 rr.id::text,rr.revision_no,rr.name,rr.state,
 u.id::text,u.username,
 COALESCE((
  SELECT jsonb_agg(DISTINCT jsonb_build_object(
   'id',d.id::text,'display_name',d.display_name,'codename',d.codename,'platform',d.platform
  ))
  FROM revision_artifacts ra
  JOIN revision_artifact_devices rad ON rad.artifact_id=ra.id
  JOIN devices d ON d.id=rad.device_id
  WHERE ra.revision_id=rr.id
 ),'[]'::jsonb)
%s
ORDER BY %s LIMIT $%d OFFSET $%d`, base, adminPublicationOrder(query.Sort), len(args)-1, len(args)), args...)
	if err != nil {
		return AdminPublicationPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanAdminPublication(rows)
		if err != nil {
			return AdminPublicationPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminPublicationPage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

func (s *Store) AdminPublication(ctx context.Context, id string) (AdminPublicationItem, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminPublicationItem{}, ErrAdminPublicationNotFound
	}
	row := s.db.QueryRowContext(ctx, `
SELECT p.id::text,p.target,p.state,p.config,p.status_detail,p.external_id,p.external_url,p.error_message,
 p.attempts,p.next_attempt_at,p.created_at,p.updated_at,
 r.id::text,r.slug,r.kind,r.platform,
 rr.id::text,rr.revision_no,rr.name,rr.state,
 u.id::text,u.username,
 COALESCE((
  SELECT jsonb_agg(DISTINCT jsonb_build_object(
   'id',d.id::text,'display_name',d.display_name,'codename',d.codename,'platform',d.platform
  ))
  FROM revision_artifacts ra
  JOIN revision_artifact_devices rad ON rad.artifact_id=ra.id
  JOIN devices d ON d.id=rad.device_id
  WHERE ra.revision_id=rr.id
 ),'[]'::jsonb)
FROM publications p
JOIN resource_revisions rr ON rr.id=p.revision_id
JOIN resources r ON r.id=rr.resource_id
JOIN users u ON u.id=r.owner_id
WHERE p.id=$1`, id)
	item, err := scanAdminPublication(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminPublicationItem{}, ErrAdminPublicationNotFound
	}
	if err != nil {
		return item, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,attempt_number,phase,event,state_from,state_to,error_message,detail,created_at FROM publication_attempts WHERE publication_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return AdminPublicationItem{}, err
	}
	defer rows.Close()
	item.History = []AdminPublicationAttempt{}
	for rows.Next() {
		var attempt AdminPublicationAttempt
		if err := rows.Scan(&attempt.ID, &attempt.AttemptNumber, &attempt.Phase, &attempt.Event, &attempt.StateFrom, &attempt.StateTo, &attempt.ErrorMessage, &attempt.Detail, &attempt.CreatedAt); err != nil {
			return AdminPublicationItem{}, err
		}
		item.History = append(item.History, attempt)
	}
	return item, rows.Err()
}

type adminPublicationScanner interface {
	Scan(dest ...any) error
}

func scanAdminPublication(scanner adminPublicationScanner) (AdminPublicationItem, error) {
	var item AdminPublicationItem
	var config, statusDetail, devices []byte
	if err := scanner.Scan(
		&item.ID, &item.Target, &item.State, &config, &statusDetail,
		&item.ExternalID, &item.ExternalURL, &item.ErrorMessage,
		&item.Attempts, &item.NextAttemptAt, &item.CreatedAt, &item.UpdatedAt,
		&item.ResourceID, &item.ResourceSlug, &item.ResourceKind, &item.ResourcePlatform,
		&item.RevisionID, &item.RevisionNumber, &item.RevisionName, &item.RevisionState,
		&item.OwnerID, &item.Owner, &devices,
	); err != nil {
		return AdminPublicationItem{}, err
	}
	if err := json.Unmarshal(config, &item.Config); err != nil {
		return AdminPublicationItem{}, fmt.Errorf("decode publication config: %w", err)
	}
	if err := json.Unmarshal(statusDetail, &item.StatusDetail); err != nil {
		return AdminPublicationItem{}, fmt.Errorf("decode publication status detail: %w", err)
	}
	if err := json.Unmarshal(devices, &item.Devices); err != nil {
		return AdminPublicationItem{}, fmt.Errorf("decode publication devices: %w", err)
	}
	return item, nil
}

func (s *Store) AdminManagePublication(ctx context.Context, id, action string) (AdminPublicationItem, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminPublicationItem{}, ErrAdminPublicationNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPublicationItem{}, err
	}
	defer tx.Rollback()
	var state string
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT state,attempts FROM publications WHERE id=$1 FOR UPDATE`, id).Scan(&state, &attempts); errors.Is(err, sql.ErrNoRows) {
		return AdminPublicationItem{}, ErrAdminPublicationNotFound
	} else if err != nil {
		return AdminPublicationItem{}, err
	}
	var result sql.Result
	var nextState, event, historyError string
	switch strings.TrimSpace(action) {
	case "retry", "requeue":
		nextState, event = "pending", "requeued"
		result, err = tx.ExecContext(ctx, `UPDATE publications publication SET state='pending',error_message='',next_attempt_at=now(),updated_at=now() FROM resource_revisions revision WHERE publication.id=$1 AND revision.id=publication.revision_id AND revision.state='approved' AND publication.state IN ('failed','cancelled')`, id)
	case "cancel":
		nextState, event, historyError = "cancelled", "cancelled", "cancelled by administrator"
		result, err = tx.ExecContext(ctx, `UPDATE publications SET state='cancelled',error_message='cancelled by administrator',lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND state IN ('pending','failed','reviewing') AND (lease_token IS NULL OR lease_expires_at<=now())`, id)
	default:
		return AdminPublicationItem{}, ErrAdminPublicationConflict
	}
	if err != nil {
		return AdminPublicationItem{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return AdminPublicationItem{}, ErrAdminPublicationConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,error_message,detail) VALUES($1,$2,'admin',$3,$4,$5,$6,jsonb_build_object('action',$3::text))`, id, attempts, event, state, nextState, historyError); err != nil {
		return AdminPublicationItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPublicationItem{}, err
	}
	return s.AdminPublication(ctx, id)
}

// AdminRetryFailedPublications requeues every failed publication matching the
// current list filters. Matching rows are locked and revalidated in one
// serializable transaction so a coordinator or another administrator cannot
// cause a partial or stale state transition.
func (s *Store) AdminRetryFailedPublications(ctx context.Context, raw AdminPublicationQuery) (AdminPublicationBatchResult, error) {
	query := raw.normalized()
	query.State = "failed"
	args := make([]any, 0, 5)
	where := []string{"p.state='failed'"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.Search != "" {
		add(`concat_ws(' ',p.id::text,p.target,p.state,p.external_id,p.external_url,p.error_message,r.id::text,r.slug,rr.id::text,rr.name,u.id::text,u.username) ILIKE '%'||?||'%'`, query.Search)
	}
	if query.Target != "" {
		add(`p.target=?`, query.Target)
	}
	if query.Resource != "" {
		add(`(r.id::text=? OR r.slug ILIKE '%'||?||'%' OR rr.name ILIKE '%'||?||'%')`, query.Resource)
	}
	if query.Owner != "" {
		add(`(u.id::text=? OR u.username ILIKE '%'||?||'%')`, query.Owner)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminPublicationBatchResult{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT p.id::text,p.attempts,rr.state
FROM publications p
JOIN resource_revisions rr ON rr.id=p.revision_id
JOIN resources r ON r.id=rr.resource_id
JOIN users u ON u.id=r.owner_id
WHERE `+strings.Join(where, " AND ")+`
ORDER BY p.id
FOR UPDATE OF p`, args...)
	if err != nil {
		return AdminPublicationBatchResult{}, err
	}
	type candidate struct {
		id, revisionState string
		attempts          int
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.attempts, &item.revisionState); err != nil {
			rows.Close()
			return AdminPublicationBatchResult{}, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return AdminPublicationBatchResult{}, err
	}
	result := AdminPublicationBatchResult{Matched: len(candidates)}
	for _, item := range candidates {
		if item.revisionState != "approved" {
			continue
		}
		updated, err := tx.ExecContext(ctx, `UPDATE publications SET state='pending',error_message='',next_attempt_at=now(),updated_at=now() WHERE id=$1 AND state='failed'`, item.id)
		if err != nil {
			return AdminPublicationBatchResult{}, err
		}
		if count, _ := updated.RowsAffected(); count != 1 {
			return AdminPublicationBatchResult{}, ErrAdminPublicationConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,detail) VALUES($1,$2,'admin','batch_requeued','failed','pending',jsonb_build_object('action','batch_retry_filtered'))`, item.id, item.attempts); err != nil {
			return AdminPublicationBatchResult{}, err
		}
		result.Retried++
	}
	if err := tx.Commit(); err != nil {
		return AdminPublicationBatchResult{}, err
	}
	return result, nil
}
