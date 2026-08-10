package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrAdminResourceNotFound = errors.New("resource not found")
	ErrAdminResourceConflict = errors.New("resource action is not allowed")
)

type AdminResourceQuery struct {
	ResourceID        string
	Device            string
	Search            string
	Owner             string
	Kind              string
	Moderation        string
	RevisionState     string
	ReviewState       string
	PublicationTarget string
	PublicationState  string
	Sort              string
	Page              int
	PerPage           int
}

type AdminPublication struct {
	ID            string         `json:"id"`
	Target        string         `json:"target"`
	State         string         `json:"state"`
	Config        map[string]any `json:"config"`
	StatusDetail  map[string]any `json:"status_detail"`
	ExternalID    string         `json:"external_id"`
	ExternalURL   string         `json:"external_url"`
	ErrorMessage  string         `json:"error_message"`
	Attempts      int            `json:"attempts"`
	NextAttemptAt time.Time      `json:"next_attempt_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type AdminResourceItem struct {
	ID                    string
	OwnerID               string
	Owner                 string
	Slug                  string
	Platform              string
	Kind                  string
	ModerationState       string
	ModerationBy          string
	ModerationReason      string
	ModerationAt          *time.Time
	CurrentRevisionID     string
	CurrentRevisionNumber int
	CurrentRevisionName   string
	LatestRevisionID      string
	LatestRevisionNumber  int
	LatestRevisionName    string
	LatestRevisionState   string
	LatestReviewState     string
	Publications          []AdminPublication
	Name                  string
	RevisionNo            int
	RevisionState         string
	ReviewState           string
	Targets               []string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type AdminResourcePage struct {
	Items      []AdminResourceItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminResourceQuery
}

type AdminRevision struct {
	ID              string
	Number          int
	Name            string
	Summary         string
	PaidType        string
	Attributes      []string
	PublicationPlan []byte
	State           string
	CreatedBy       string
	CreatedVia      string
	BaseRevisionID  string
	ReviewID        string
	ReviewState     string
	ReviewNote      string
	ReviewItems     []string
	Reviewer        string
	ArtifactCount   int
	MediaCount      int
	Publications    []AdminPublication
	CreatedAt       time.Time
	ReviewUpdatedAt *time.Time
}

type AdminExternalBinding struct {
	Provider    string
	ExternalID  string
	ExternalURL string
	Repository  string
	Entries     []AdminExternalBindingEntry
	CreatedAt   time.Time
}

type AdminExternalBindingEntry struct {
	Label string
	Value string
	URL   string
}

type AdminResourceEvent struct {
	ID        int64
	Actor     string
	EventType string
	Payload   map[string]any
	CreatedAt time.Time
}

type AdminResourceDetail struct {
	Resource     AdminResourceItem
	Revisions    []AdminRevision
	Publications []AdminPublication
	Artifacts    []AdminArtifact
	Media        []AdminMedia
	Bindings     []AdminExternalBinding
	Events       []AdminResourceEvent
	Snapshot     string
}

type AdminLink struct {
	Position int
	Title    string
	URL      string
}

type AdminRevisionDetail struct {
	Resource  AdminResourceItem
	Revision  AdminRevision
	Artifacts []AdminArtifact
	Media     []AdminMedia
	Links     []AdminLink
}

type AdminArtifact struct {
	ID             string
	SHA256         string
	OriginalName   string
	PackageFormat  string
	PackageID      string
	Version        string
	SizeBytes      int64
	Analysis       map[string]any
	Devices        []string
	DeviceBindings []AdminArtifactDevice
	DuplicateCount int
}

type AdminArtifactDevice struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Codename    string `json:"codename"`
	Platform    string `json:"platform"`
}

type AdminMedia struct {
	ID        string
	SHA256    string
	Role      string
	Position  int
	Width     int
	Height    int
	SizeBytes int64
}

func (query AdminResourceQuery) normalized() AdminResourceQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Owner = strings.TrimSpace(query.Owner)
	query.ResourceID = strings.TrimSpace(query.ResourceID)
	query.Device = strings.TrimSpace(query.Device)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 25
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	if query.Kind != "quickapp" && query.Kind != "watchface" {
		query.Kind = ""
	}
	if query.Moderation != "visible" && query.Moderation != "suspended" && query.Moderation != "frozen" {
		query.Moderation = ""
	}
	if query.RevisionState != "submitted" && query.RevisionState != "approved" && query.RevisionState != "rejected" && query.RevisionState != "superseded" {
		query.RevisionState = ""
	}
	if query.ReviewState != "pending" && query.ReviewState != "approved" && query.ReviewState != "rejected" && query.ReviewState != "superseded" {
		query.ReviewState = ""
	}
	if query.PublicationTarget != "oronbox" && query.PublicationTarget != "bandbbs" && query.PublicationTarget != "astrobox" {
		query.PublicationTarget = ""
	}
	if query.PublicationState != "pending" && query.PublicationState != "running" && query.PublicationState != "published" && query.PublicationState != "reviewing" && query.PublicationState != "failed" && query.PublicationState != "cancelled" {
		query.PublicationState = ""
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "name", "owner":
	default:
		query.Sort = "updated_desc"
	}
	return query
}

func (s *Store) AdminResources(ctx context.Context, raw AdminResourceQuery) (AdminResourcePage, error) {
	query := raw.normalized()
	args := make([]any, 0, 12)
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.ResourceID != "" {
		add(`r.id=?`, query.ResourceID)
	}
	if query.Search != "" {
		add(`(r.id::text ILIKE '%'||?||'%' OR r.slug ILIKE '%'||?||'%' OR u.username ILIKE '%'||?||'%' OR COALESCE(latest.name,'') ILIKE '%'||?||'%' OR COALESCE(latest.summary,'') ILIKE '%'||?||'%')`, query.Search)
	}
	if query.Owner != "" {
		add(`(u.username ILIKE '%'||?||'%' OR r.owner_id::text=?)`, query.Owner)
	}
	if query.Kind != "" {
		add(`r.kind=?`, query.Kind)
	}
	if query.Device != "" {
		add(`EXISTS(
 SELECT 1 FROM resource_revisions device_revision
 JOIN revision_artifacts device_artifact ON device_artifact.revision_id=device_revision.id
 JOIN revision_artifact_devices device_binding ON device_binding.artifact_id=device_artifact.id
 JOIN devices filter_device ON filter_device.id=device_binding.device_id
 WHERE device_revision.resource_id=r.id AND (filter_device.id::text=? OR filter_device.codename=?)
)`, query.Device)
	}
	if query.Moderation != "" {
		add(`r.moderation_state=?`, query.Moderation)
	}
	if query.RevisionState != "" {
		add(`latest.state=?`, query.RevisionState)
	}
	if query.ReviewState != "" {
		add(`review.state=?`, query.ReviewState)
	}
	if query.PublicationTarget != "" && query.PublicationState != "" {
		args = append(args, query.PublicationTarget, query.PublicationState)
		where = append(where, fmt.Sprintf(`EXISTS(
 SELECT 1 FROM publications filter_publication
 WHERE filter_publication.revision_id=latest.id
   AND filter_publication.target=$%d
   AND filter_publication.state=$%d
)`, len(args)-1, len(args)))
	} else if query.PublicationTarget != "" {
		add(`EXISTS(SELECT 1 FROM publications filter_publication WHERE filter_publication.revision_id=latest.id AND filter_publication.target=?)`, query.PublicationTarget)
	} else if query.PublicationState != "" {
		add(`EXISTS(SELECT 1 FROM publications filter_publication WHERE filter_publication.revision_id=latest.id AND filter_publication.state=?)`, query.PublicationState)
	}
	order := "r.updated_at DESC, r.id DESC"
	switch query.Sort {
	case "updated_asc":
		order = "r.updated_at ASC, r.id ASC"
	case "created_desc":
		order = "r.created_at DESC, r.id DESC"
	case "name":
		order = "COALESCE(latest.name,r.slug) ASC, r.id ASC"
	case "owner":
		order = "u.username ASC, r.updated_at DESC"
	}
	base := `FROM resources r
JOIN users u ON u.id=r.owner_id
LEFT JOIN resource_revisions current_revision ON current_revision.id=r.current_revision_id
LEFT JOIN LATERAL (SELECT revision.* FROM resource_revisions revision WHERE revision.resource_id=r.id ORDER BY revision.revision_no DESC LIMIT 1) latest ON TRUE
LEFT JOIN review_cases review ON review.revision_id=latest.id
WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+base, args...).Scan(&total); err != nil {
		return AdminResourcePage{}, err
	}
	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	limitPosition, offsetPosition := len(args)-1, len(args)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT r.id::text,r.owner_id::text,u.username,r.slug,r.platform,r.kind,r.moderation_state,COALESCE(r.moderation_by,''),r.moderation_reason,r.moderation_at,
 COALESCE(current_revision.id::text,''),COALESCE(current_revision.revision_no,0),COALESCE(current_revision.name,''),
 COALESCE(latest.id::text,''),COALESCE(latest.revision_no,0),COALESCE(latest.name,''),COALESCE(latest.state,''),
 COALESCE(review.state,''),
 COALESCE((SELECT jsonb_agg(jsonb_build_object(
   'id',publication.id::text,'target',publication.target,'state',publication.state,
   'external_id',publication.external_id,'external_url',publication.external_url,
   'error_message',publication.error_message,'attempts',publication.attempts,'updated_at',publication.updated_at
 ) ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=latest.id),'[]'::jsonb),
 r.created_at,r.updated_at
%s
ORDER BY %s LIMIT $%d OFFSET $%d`, base, order, limitPosition, offsetPosition), args...)
	if err != nil {
		return AdminResourcePage{}, err
	}
	defer rows.Close()
	page := AdminResourcePage{Page: query.Page, PerPage: query.PerPage, Query: query, Items: []AdminResourceItem{}, Total: total}
	for rows.Next() {
		var item AdminResourceItem
		var publications []byte
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Owner, &item.Slug, &item.Platform, &item.Kind, &item.ModerationState, &item.ModerationBy, &item.ModerationReason, &item.ModerationAt, &item.CurrentRevisionID, &item.CurrentRevisionNumber, &item.CurrentRevisionName, &item.LatestRevisionID, &item.LatestRevisionNumber, &item.LatestRevisionName, &item.LatestRevisionState, &item.LatestReviewState, &publications, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return AdminResourcePage{}, err
		}
		if err := json.Unmarshal(publications, &item.Publications); err != nil {
			return AdminResourcePage{}, fmt.Errorf("decode resource publications: %w", err)
		}
		item.Name = item.LatestRevisionName
		if item.Name == "" {
			item.Name = item.Slug
		}
		item.RevisionNo = item.LatestRevisionNumber
		item.RevisionState = item.LatestRevisionState
		item.ReviewState = item.LatestReviewState
		for _, publication := range item.Publications {
			item.Targets = append(item.Targets, publication.Target)
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminResourcePage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

func (s *Store) AdminResource(ctx context.Context, id string) (AdminResourceDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminResourceDetail{}, ErrAdminResourceNotFound
	}
	page, err := s.AdminResources(ctx, AdminResourceQuery{ResourceID: id, Page: 1, PerPage: 1})
	if err != nil {
		return AdminResourceDetail{}, err
	}
	var summary AdminResourceItem
	for _, item := range page.Items {
		if item.ID == id {
			summary = item
			break
		}
	}
	if summary.ID == "" {
		return AdminResourceDetail{}, ErrAdminResourceNotFound
	}
	detail := AdminResourceDetail{Resource: summary, Revisions: []AdminRevision{}, Bindings: []AdminExternalBinding{}, Events: []AdminResourceEvent{}}
	rows, err := s.db.QueryContext(ctx, `SELECT revision.id::text,revision.revision_no,revision.name,revision.summary,revision.paid_type,
 COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=revision.id),'[]'::jsonb),revision.publication_plan,revision.state,
 COALESCE(revision.created_by::text,''),revision.created_via,COALESCE(revision.base_revision_id::text,''),
 COALESCE(review.id::text,''),COALESCE(review.state,''),COALESCE(review.note,''),COALESCE(review.items,'[]'::jsonb),
 COALESCE(reviewer.username,''),
 (SELECT count(*) FROM revision_artifacts WHERE revision_id=revision.id),
 (SELECT count(*) FROM revision_media WHERE revision_id=revision.id),
 COALESCE((SELECT jsonb_agg(jsonb_build_object(
	'id',publication.id::text,'target',publication.target,'state',publication.state,
	'config',publication.config,'status_detail',publication.status_detail,
   'external_id',publication.external_id,'external_url',publication.external_url,
	'error_message',publication.error_message,'attempts',publication.attempts,
	'next_attempt_at',publication.next_attempt_at,'updated_at',publication.updated_at
 ) ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=revision.id),'[]'::jsonb),
 revision.created_at,review.updated_at
FROM resource_revisions revision
LEFT JOIN review_cases review ON review.revision_id=revision.id
LEFT JOIN users reviewer ON reviewer.id=review.reviewer_id
WHERE revision.resource_id=$1 ORDER BY revision.revision_no DESC`, id)
	if err != nil {
		return AdminResourceDetail{}, err
	}
	for rows.Next() {
		var revision AdminRevision
		var attributes, reviewItems, publications []byte
		var reviewUpdated sql.NullTime
		if err := rows.Scan(&revision.ID, &revision.Number, &revision.Name, &revision.Summary, &revision.PaidType, &attributes, &revision.PublicationPlan, &revision.State, &revision.CreatedBy, &revision.CreatedVia, &revision.BaseRevisionID, &revision.ReviewID, &revision.ReviewState, &revision.ReviewNote, &reviewItems, &revision.Reviewer, &revision.ArtifactCount, &revision.MediaCount, &publications, &revision.CreatedAt, &reviewUpdated); err != nil {
			rows.Close()
			return AdminResourceDetail{}, err
		}
		_ = json.Unmarshal(attributes, &revision.Attributes)
		_ = json.Unmarshal(reviewItems, &revision.ReviewItems)
		if err := json.Unmarshal(publications, &revision.Publications); err != nil {
			rows.Close()
			return AdminResourceDetail{}, fmt.Errorf("decode revision publications: %w", err)
		}
		if reviewUpdated.Valid {
			revision.ReviewUpdatedAt = &reviewUpdated.Time
		}
		detail.Revisions = append(detail.Revisions, revision)
	}
	if err := rows.Close(); err != nil {
		return AdminResourceDetail{}, err
	}
	var artifactQuery, mediaQuery, snapshotID string
	if len(detail.Revisions) > 0 {
		latest := detail.Revisions[0]
		detail.Publications = append(detail.Publications, latest.Publications...)
		snapshotID = latest.ID
		artifactQuery = adminRevisionArtifactsSQL
		mediaQuery = adminRevisionMediaSQL
	}
	if snapshotID != "" {
		detail.Artifacts, err = s.adminArtifacts(ctx, artifactQuery, snapshotID)
		if err != nil {
			return AdminResourceDetail{}, err
		}
		detail.Media, err = s.adminMedia(ctx, mediaQuery, snapshotID)
		if err != nil {
			return AdminResourceDetail{}, err
		}
	}
	bindingRows, err := s.db.QueryContext(ctx, `SELECT provider,external_id,external_url,meta,created_at FROM external_bindings WHERE resource_id=$1 ORDER BY provider`, id)
	if err != nil {
		return AdminResourceDetail{}, err
	}
	for bindingRows.Next() {
		var binding AdminExternalBinding
		var meta []byte
		if err := bindingRows.Scan(&binding.Provider, &binding.ExternalID, &binding.ExternalURL, &meta, &binding.CreatedAt); err != nil {
			bindingRows.Close()
			return AdminResourceDetail{}, err
		}
		binding.present(meta)
		detail.Bindings = append(detail.Bindings, binding)
	}
	if err := bindingRows.Close(); err != nil {
		return AdminResourceDetail{}, err
	}
	eventRows, err := s.db.QueryContext(ctx, `SELECT event.id,COALESCE(actor.username,''),event.event_type,event.payload,event.created_at FROM resource_events event LEFT JOIN users actor ON actor.id=event.actor_id WHERE event.resource_id=$1 ORDER BY event.id DESC LIMIT 200`, id)
	if err != nil {
		return AdminResourceDetail{}, err
	}
	for eventRows.Next() {
		var event AdminResourceEvent
		var payload []byte
		if err := eventRows.Scan(&event.ID, &event.Actor, &event.EventType, &payload, &event.CreatedAt); err != nil {
			eventRows.Close()
			return AdminResourceDetail{}, err
		}
		_ = json.Unmarshal(payload, &event.Payload)
		detail.Events = append(detail.Events, event)
	}
	if err := eventRows.Close(); err != nil {
		return AdminResourceDetail{}, err
	}
	snapshot, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return AdminResourceDetail{}, err
	}
	detail.Snapshot = string(snapshot)
	return detail, nil
}

func (s *Store) AdminResourceRevision(ctx context.Context, resourceID, revisionID string) (AdminRevisionDetail, error) {
	if _, err := uuid.Parse(resourceID); err != nil {
		return AdminRevisionDetail{}, ErrAdminResourceNotFound
	}
	if _, err := uuid.Parse(revisionID); err != nil {
		return AdminRevisionDetail{}, ErrAdminResourceNotFound
	}
	resource, err := s.AdminResource(ctx, resourceID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	detail := AdminRevisionDetail{Resource: resource.Resource, Artifacts: []AdminArtifact{}, Media: []AdminMedia{}, Links: []AdminLink{}}
	for _, revision := range resource.Revisions {
		if revision.ID == revisionID {
			detail.Revision = revision
			break
		}
	}
	if detail.Revision.ID == "" {
		return AdminRevisionDetail{}, ErrAdminResourceNotFound
	}
	detail.Artifacts, err = s.adminArtifacts(ctx, adminRevisionArtifactsSQL, revisionID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	detail.Media, err = s.adminMedia(ctx, adminRevisionMediaSQL, revisionID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT position,title,url FROM revision_links WHERE revision_id=$1 ORDER BY position`, revisionID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var link AdminLink
		if err := rows.Scan(&link.Position, &link.Title, &link.URL); err != nil {
			return AdminRevisionDetail{}, err
		}
		detail.Links = append(detail.Links, link)
	}
	if err := rows.Err(); err != nil {
		return AdminRevisionDetail{}, err
	}
	return detail, nil
}

func (binding *AdminExternalBinding) present(rawMeta []byte) {
	if binding.Provider == "bandbbs" {
		ids := map[string]any{}
		if json.Unmarshal([]byte(binding.ExternalID), &ids) == nil {
			keys := make([]string, 0, len(ids))
			for categoryID := range ids {
				keys = append(keys, categoryID)
			}
			sort.Slice(keys, func(i, j int) bool {
				left, leftErr := strconv.Atoi(keys[i])
				right, rightErr := strconv.Atoi(keys[j])
				if leftErr == nil && rightErr == nil {
					return left < right
				}
				return keys[i] < keys[j]
			})
			for _, categoryID := range keys {
				resourceID := ""
				resourceURL := ""
				switch value := ids[categoryID].(type) {
				case string:
					resourceID = value
				case map[string]any:
					resourceID, _ = value["resource_id"].(string)
					resourceURL, _ = value["url"].(string)
				}
				if resourceURL == "" && resourceID != "" {
					resourceURL = "https://www.bandbbs.cn/resources/" + resourceID + "/"
				}
				binding.Entries = append(binding.Entries, AdminExternalBindingEntry{
					Label: "分区 " + categoryID,
					Value: "资源 " + resourceID,
					URL:   resourceURL,
				})
			}
		}
		return
	}
	binding.Entries = append(binding.Entries, AdminExternalBindingEntry{Label: "资源 ID", Value: binding.ExternalID})
	meta := map[string]string{}
	if json.Unmarshal(rawMeta, &meta) == nil {
		binding.Repository = strings.Trim(strings.TrimSpace(meta["repo_owner"])+"/"+strings.TrimSpace(meta["repo_name"]), "/")
	}
}

const adminRevisionArtifactsSQL = `SELECT artifact.id::text,artifact.blob_sha256,artifact.original_name,artifact.package_format,artifact.package_id,artifact.package_version,
 blob.size_bytes,artifact.analysis,
 COALESCE((SELECT jsonb_agg(device.display_name ORDER BY device.display_name) FROM revision_artifact_devices binding JOIN devices device ON device.id=binding.device_id WHERE binding.artifact_id=artifact.id),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',device.id::text,'display_name',device.display_name,'codename',device.codename,'platform',device.platform) ORDER BY device.display_name) FROM revision_artifact_devices binding JOIN devices device ON device.id=binding.device_id WHERE binding.artifact_id=artifact.id),'[]'::jsonb)
FROM revision_artifacts artifact JOIN blobs blob ON blob.sha256=artifact.blob_sha256 WHERE artifact.revision_id=$1 ORDER BY artifact.created_at`

const adminRevisionMediaSQL = `SELECT media.id::text,media.blob_sha256,media.role,media.position,media.width,media.height,blob.size_bytes FROM revision_media media JOIN blobs blob ON blob.sha256=media.blob_sha256 WHERE media.revision_id=$1 ORDER BY media.role,media.position`

func (s *Store) adminArtifacts(ctx context.Context, query, snapshotID string) ([]AdminArtifact, error) {
	rows, err := s.db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	artifacts := []AdminArtifact{}
	byContent := make(map[string]int)
	for rows.Next() {
		var artifact AdminArtifact
		var analysis, devices, deviceBindings []byte
		if err := rows.Scan(&artifact.ID, &artifact.SHA256, &artifact.OriginalName, &artifact.PackageFormat, &artifact.PackageID, &artifact.Version, &artifact.SizeBytes, &analysis, &devices, &deviceBindings); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(analysis, &artifact.Analysis)
		_ = json.Unmarshal(devices, &artifact.Devices)
		_ = json.Unmarshal(deviceBindings, &artifact.DeviceBindings)
		artifact.DuplicateCount = 1
		key := strings.Join([]string{artifact.SHA256, artifact.OriginalName, artifact.PackageFormat, artifact.PackageID, artifact.Version}, "\x00")
		if index, ok := byContent[key]; ok {
			artifacts[index].DuplicateCount++
			artifacts[index].Devices = mergeStringValues(artifacts[index].Devices, artifact.Devices)
			artifacts[index].DeviceBindings = mergeArtifactDevices(artifacts[index].DeviceBindings, artifact.DeviceBindings)
			continue
		}
		byContent[key] = len(artifacts)
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func mergeStringValues(left, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	merged := make([]string, 0, len(left)+len(right))
	for _, values := range [][]string{left, right} {
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				merged = append(merged, value)
			}
		}
	}
	sort.Strings(merged)
	return merged
}

func mergeArtifactDevices(left, right []AdminArtifactDevice) []AdminArtifactDevice {
	byID := make(map[string]AdminArtifactDevice, len(left)+len(right))
	for _, device := range append(left, right...) {
		byID[device.ID] = device
	}
	merged := make([]AdminArtifactDevice, 0, len(byID))
	for _, device := range byID {
		merged = append(merged, device)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].DisplayName < merged[j].DisplayName })
	return merged
}

func (s *Store) adminMedia(ctx context.Context, query, snapshotID string) ([]AdminMedia, error) {
	rows, err := s.db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	media := []AdminMedia{}
	for rows.Next() {
		var item AdminMedia
		if err := rows.Scan(&item.ID, &item.SHA256, &item.Role, &item.Position, &item.Width, &item.Height, &item.SizeBytes); err != nil {
			return nil, err
		}
		media = append(media, item)
	}
	return media, rows.Err()
}

type AdminResourceActionResult struct {
	ID                 string
	Slug               string
	OwnerID            string
	PreviousModeration string
	ModerationState    string
	Reason             string
	Deleted            bool
}

// AdminManageResource applies an admin moderation action. suspend and freeze
// hide the resource (freeze additionally locks the creator out of editing)
// and cancel its pending/running publications; restore and unfreeze return it
// to visible. delete physically removes a resource that has no revisions.
func (s *Store) AdminManageResource(ctx context.Context, id, action, reason string, actor AdminSession) (AdminResourceActionResult, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminResourceActionResult{}, ErrAdminResourceNotFound
	}
	reason = strings.TrimSpace(reason)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminResourceActionResult{}, err
	}
	defer tx.Rollback()
	var result AdminResourceActionResult
	var revisionCount int
	err = tx.QueryRowContext(ctx, `SELECT id::text,slug,owner_id::text,moderation_state,(SELECT count(*) FROM resource_revisions WHERE resource_id=resources.id) FROM resources WHERE id=$1 FOR UPDATE`, id).
		Scan(&result.ID, &result.Slug, &result.OwnerID, &result.PreviousModeration, &revisionCount)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminResourceActionResult{}, ErrAdminResourceNotFound
	}
	if err != nil {
		return AdminResourceActionResult{}, err
	}
	result.ModerationState = result.PreviousModeration
	eventType := ""
	switch action {
	case "suspend":
		result.ModerationState = "suspended"
		eventType = "resource.suspended"
	case "freeze":
		result.ModerationState = "frozen"
		eventType = "resource.frozen"
	case "restore", "unfreeze":
		result.ModerationState = "visible"
		eventType = "resource.restored"
		if action == "unfreeze" {
			eventType = "resource.unfrozen"
		}
	case "delete":
		if revisionCount != 0 {
			return AdminResourceActionResult{}, fmt.Errorf("%w: resources with immutable revisions must be suspended or frozen instead", ErrAdminResourceConflict)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM resources WHERE id=$1`, id); err != nil {
			return AdminResourceActionResult{}, err
		}
		result.Deleted = true
		return result, tx.Commit()
	default:
		return AdminResourceActionResult{}, fmt.Errorf("%w: unknown action", ErrAdminResourceConflict)
	}
	result.Reason = reason
	if result.ModerationState == result.PreviousModeration {
		return result, tx.Commit()
	}
	if result.ModerationState == "visible" {
		if _, err := tx.ExecContext(ctx, `UPDATE resources SET moderation_state='visible',moderation_by=NULL,moderation_reason='',moderation_at=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
			return AdminResourceActionResult{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE resources SET moderation_state=$2,moderation_by='admin',moderation_reason=$3,moderation_at=now(),updated_at=now() WHERE id=$1`, id, result.ModerationState, reason); err != nil {
			return AdminResourceActionResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE publications publication SET state='cancelled',error_message='cancelled by resource moderation',updated_at=now() FROM resource_revisions revision WHERE publication.revision_id=revision.id AND revision.resource_id=$1 AND publication.state='pending'`, id); err != nil {
			return AdminResourceActionResult{}, err
		}
	}
	payload, _ := json.Marshal(map[string]any{"admin": actor.Username, "admin_user_id": actor.UserID, "action": action, "reason": reason, "previous_moderation": result.PreviousModeration, "moderation": result.ModerationState})
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_events(resource_id,actor_id,event_type,payload) VALUES($1,NULLIF($2,'')::uuid,$3,$4)`, id, actor.UserID, eventType, payload); err != nil {
		return AdminResourceActionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminResourceActionResult{}, err
	}
	return result, nil
}
