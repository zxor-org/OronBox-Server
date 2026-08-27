package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	adminReviewDefaultPerPage = 25
	adminReviewMaxPerPage     = 100
)

var ErrAdminReviewNotFound = errors.New("review case was not found")
var ErrAdminReviewConflict = errors.New("review case changed or is not eligible for this operation")

type AdminReviewQuery struct {
	Search  string
	Kind    string
	Target  string
	Owner   string
	State   string
	From    *time.Time
	To      *time.Time
	Sort    string
	Page    int
	PerPage int
}

type AdminReviewItem struct {
	ID               string
	State            string
	Note             string
	Items            []string
	ReviewerID       string
	Reviewer         string
	ResourceID       string
	ResourceSlug     string
	ResourceKind     string
	ResourceState    string
	ResourcePlatform string
	// CurationGrade is the grade the resource carries today. The decision form
	// preselects it so approving a featured resource cannot silently demote it.
	CurationGrade  string
	RevisionID     string
	RevisionNumber int
	RevisionName   string
	RevisionState  string
	OwnerID        string
	Owner          string
	Targets        []string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// Priority is 0..3, highest last, and DueAt is the SLA deadline the queue
	// sorts by. FirstSubmittedAt survives resubmission so a case that has been
	// bounced back and forth does not look freshly filed.
	Priority         int
	DueAt            time.Time
	FirstSubmittedAt time.Time
	// OwnerRejections and Reports are the risk signals a reviewer needs before
	// opening a case: a creator with a long rejection history and a resource
	// that is already being reported deserve a closer look.
	OwnerRejections int
	Reports         int
}

// Overdue reports whether a pending case has passed its SLA deadline.
func (item AdminReviewItem) Overdue() bool {
	return item.State == "pending" && !item.DueAt.IsZero() && time.Now().After(item.DueAt)
}

// WaitingFor is how long the case has been in the queue, used for the "waiting
// 3 days" style badge rather than making the reviewer subtract timestamps.
func (item AdminReviewItem) WaitingFor() time.Duration {
	since := item.CreatedAt
	if !item.FirstSubmittedAt.IsZero() {
		since = item.FirstSubmittedAt
	}
	if since.IsZero() {
		return 0
	}
	return time.Since(since)
}

func (item AdminReviewItem) PriorityLabel() string {
	switch item.Priority {
	case 3:
		return "紧急"
	case 2:
		return "高"
	case 1:
		return "关注"
	default:
		return "普通"
	}
}

type AdminReviewPage struct {
	Items      []AdminReviewItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminReviewQuery
}

type AdminReviewRevisionSnapshot struct {
	ID              string
	Number          int
	Name            string
	Summary         string
	PaidType        string
	State           string
	CreatedBy       string
	CreatedVia      string
	BaseRevisionID  string
	PublicationPlan any
	Attributes      []string
	Links           []AdminLink
	Media           []AdminMedia
	Artifacts       []AdminArtifact
	Governance      AdminRevisionGovernance
	CreatedAt       time.Time
}

type AdminReviewDiffCount struct {
	Added   int
	Removed int
	Changed int
}

type AdminReviewFieldChange struct {
	Field  string
	Label  string
	Before string
	After  string
}

type AdminReviewItemChange struct {
	Key    string
	Label  string
	Change string
	Before string
	After  string
}

type AdminReviewDiff struct {
	HasBase         bool
	MetadataChanged bool
	MetadataFields  []string
	Metadata        []AdminReviewFieldChange
	Attributes      AdminReviewDiffCount
	Links           AdminReviewDiffCount
	Media           AdminReviewDiffCount
	Artifacts       AdminReviewDiffCount
	Devices         AdminReviewDiffCount
	AttributeItems  []AdminReviewItemChange
	LinkItems       []AdminReviewItemChange
	MediaItems      []AdminReviewItemChange
	ArtifactItems   []AdminReviewItemChange
	DeviceItems     []AdminReviewItemChange
}

type AdminReviewDetail struct {
	Review  AdminReviewItem
	Current AdminReviewRevisionSnapshot
	Base    AdminReviewRevisionSnapshot
	Diff    AdminReviewDiff
}

type AdminReviewerOption struct {
	ID       string
	Username string
	Role     string
}

func (s *Store) AdminReviewers(ctx context.Context) ([]AdminReviewerOption, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,username,role FROM users WHERE role IN ('reviewer','admin') AND banned_at IS NULL ORDER BY username,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AdminReviewerOption{}
	for rows.Next() {
		var item AdminReviewerOption
		if err := rows.Scan(&item.ID, &item.Username, &item.Role); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeReviewChecklist(items []string) []string {
	result, seen := make([]string, 0, len(items)), map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func (s *Store) AdminSaveReviewChecklist(ctx context.Context, reviewID, actorID string, items []string) error {
	if _, err := uuid.Parse(reviewID); err != nil {
		return ErrAdminReviewNotFound
	}
	checklist := normalizeReviewChecklist(items)
	raw, _ := json.Marshal(checklist)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET items=$2,updated_at=now() WHERE id=$1 AND state='pending'`, reviewID, raw)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrAdminReviewConflict
	}
	if err := AppendReviewCaseEvent(ctx, tx, reviewID, actorID, "checklist_saved", "", checklist, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeReviewIDs(ids []string) ([]string, error) {
	result, seen := make([]string, 0, len(ids)), map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			return nil, ErrAdminReviewNotFound
		}
		seen[id] = true
		result = append(result, id)
	}
	if len(result) == 0 || len(result) > 100 {
		return nil, ErrAdminReviewConflict
	}
	return result, nil
}

func (s *Store) AdminAssignReviews(ctx context.Context, ids []string, reviewerID, actorID string) error {
	ids, err := normalizeReviewIDs(ids)
	if err != nil {
		return err
	}
	if reviewerID != "" {
		if _, err := uuid.Parse(reviewerID); err != nil {
			return ErrAdminReviewConflict
		}
		var valid bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role IN ('reviewer','admin') AND banned_at IS NULL)`, reviewerID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrAdminReviewConflict
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET reviewer_id=NULLIF($2,'')::uuid,updated_at=now() WHERE id=ANY($1::uuid[]) AND state='pending'`, ids, reviewerID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); int(count) != len(ids) {
		return ErrAdminReviewConflict
	}
	event := "assigned"
	if reviewerID == "" {
		event = "unassigned"
	}
	for _, id := range ids {
		if err := AppendReviewCaseEvent(ctx, tx, id, actorID, event, "", nil, map[string]any{"reviewer_id": reviewerID}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AdminSetReviewPriority retunes the queue. The SLA deadline moves with the
// priority so an urgent case does not keep a deadline set for a routine one.
func (s *Store) AdminSetReviewPriority(ctx context.Context, ids []string, priority int, actorID string) error {
	ids, err := normalizeReviewIDs(ids)
	if err != nil {
		return err
	}
	if priority < 0 || priority > 3 {
		return ErrAdminReviewConflict
	}
	hours := map[int]int{0: 48, 1: 24, 2: 12, 3: 4}[priority]
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET priority=$2,due_at=COALESCE(first_submitted_at,created_at)+make_interval(hours=>$3),updated_at=now() WHERE id=ANY($1::uuid[]) AND state='pending'`, ids, priority, hours)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); int(count) != len(ids) {
		return ErrAdminReviewConflict
	}
	for _, id := range ids {
		if err := AppendReviewCaseEvent(ctx, tx, id, actorID, "priority_changed", "", nil, map[string]any{"priority": priority, "sla_hours": hours}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (query AdminReviewQuery) normalized() AdminReviewQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Owner = strings.TrimSpace(query.Owner)
	if query.Kind != "quickapp" && query.Kind != "watchface" {
		query.Kind = ""
	}
	if query.Target != "oronbox" && query.Target != "bandbbs" && query.Target != "astrobox" {
		query.Target = ""
	}
	if query.State != "pending" && query.State != "approved" && query.State != "rejected" && query.State != "superseded" {
		query.State = ""
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		query.From, query.To = nil, nil
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "created_asc", "revision_desc", "owner", "sla", "priority":
	default:
		query.Sort = "updated_desc"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = adminReviewDefaultPerPage
	}
	if query.PerPage > adminReviewMaxPerPage {
		query.PerPage = adminReviewMaxPerPage
	}
	return query
}

func adminReviewOrder(value string) string {
	switch value {
	case "updated_asc":
		return "review.updated_at ASC,review.id ASC"
	case "created_desc":
		return "review.created_at DESC,review.id DESC"
	case "created_asc":
		return "review.created_at ASC,review.id ASC"
	case "revision_desc":
		return "revision.revision_no DESC,review.updated_at DESC,review.id DESC"
	case "owner":
		return "owner.username ASC,review.updated_at DESC,review.id DESC"
	case "sla":
		// Cases without a deadline sort last so the ones actually at risk stay
		// at the top of the queue.
		return "review.due_at ASC NULLS LAST,review.priority DESC,review.created_at ASC,review.id ASC"
	case "priority":
		return "review.priority DESC,review.due_at ASC NULLS LAST,review.created_at ASC,review.id ASC"
	default:
		return "review.updated_at DESC,review.id DESC"
	}
}

func (s *Store) AdminReviews(ctx context.Context, raw AdminReviewQuery) (AdminReviewPage, error) {
	query := raw.normalized()
	args := make([]any, 0, 9)
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.Search != "" {
		add(`concat_ws(' ',review.id::text,review.note,revision.id::text,revision.name,revision.summary,resource.id::text,resource.slug,owner.id::text,owner.username,COALESCE(reviewer.username,'')) ILIKE '%'||?||'%'`, query.Search)
	}
	if query.Kind != "" {
		add(`resource.kind=?`, query.Kind)
	}
	if query.Target != "" {
		add(`EXISTS(SELECT 1 FROM publications publication_filter WHERE publication_filter.revision_id=revision.id AND publication_filter.target=?)`, query.Target)
	}
	if query.Owner != "" {
		add(`(owner.id::text=? OR owner.username ILIKE '%'||?||'%')`, query.Owner)
	}
	if query.State != "" {
		add(`review.state=?`, query.State)
	}
	if query.From != nil {
		add(`review.created_at>=?`, *query.From)
	}
	if query.To != nil {
		add(`review.created_at<=?`, *query.To)
	}
	base := `FROM review_cases review
JOIN resource_revisions revision ON revision.id=review.revision_id
JOIN resources resource ON resource.id=revision.resource_id
JOIN users owner ON owner.id=resource.owner_id
LEFT JOIN users reviewer ON reviewer.id=review.reviewer_id
WHERE ` + strings.Join(where, " AND ")
	page := AdminReviewPage{Items: []AdminReviewItem{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+base, args...).Scan(&page.Total); err != nil {
		return AdminReviewPage{}, err
	}
	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT
 review.id::text,review.state,review.note,review.items,COALESCE(review.reviewer_id::text,''),COALESCE(reviewer.username,''),
 resource.id::text,resource.slug,resource.kind,resource.moderation_state,resource.platform,resource.curation_grade,
 revision.id::text,revision.revision_no,revision.name,revision.state,
 owner.id::text,owner.username,
 COALESCE((SELECT jsonb_agg(publication.target ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=revision.id),'[]'::jsonb),
 review.created_at,review.updated_at,
 review.priority,review.due_at,review.first_submitted_at,
 (SELECT count(*) FROM review_cases history JOIN resource_revisions history_revision ON history_revision.id=history.revision_id JOIN resources history_resource ON history_resource.id=history_revision.resource_id WHERE history_resource.owner_id=resource.owner_id AND history.state='rejected'),
 (SELECT count(*) FROM feedback_tickets report WHERE report.kind='resource_report' AND report.target_id=resource.id::text AND report.status IN ('open','investigating'))
%s ORDER BY %s LIMIT $%d OFFSET $%d`, base, adminReviewOrder(query.Sort), len(args)-1, len(args)), args...)
	if err != nil {
		return AdminReviewPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanAdminReview(rows)
		if err != nil {
			return AdminReviewPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminReviewPage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

type adminReviewScanner interface {
	Scan(dest ...any) error
}

func scanAdminReview(scanner adminReviewScanner) (AdminReviewItem, error) {
	var item AdminReviewItem
	var reviewItems, targets []byte
	if err := scanner.Scan(
		&item.ID, &item.State, &item.Note, &reviewItems, &item.ReviewerID, &item.Reviewer,
		&item.ResourceID, &item.ResourceSlug, &item.ResourceKind, &item.ResourceState, &item.ResourcePlatform, &item.CurationGrade,
		&item.RevisionID, &item.RevisionNumber, &item.RevisionName, &item.RevisionState,
		&item.OwnerID, &item.Owner, &targets, &item.CreatedAt, &item.UpdatedAt,
		&item.Priority, &item.DueAt, &item.FirstSubmittedAt, &item.OwnerRejections, &item.Reports,
	); err != nil {
		return AdminReviewItem{}, err
	}
	if err := json.Unmarshal(reviewItems, &item.Items); err != nil {
		return AdminReviewItem{}, fmt.Errorf("decode review items: %w", err)
	}
	if err := json.Unmarshal(targets, &item.Targets); err != nil {
		return AdminReviewItem{}, fmt.Errorf("decode review targets: %w", err)
	}
	return item, nil
}

func (s *Store) AdminReview(ctx context.Context, id string) (AdminReviewDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminReviewDetail{}, ErrAdminReviewNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT
 review.id::text,review.state,review.note,review.items,COALESCE(review.reviewer_id::text,''),COALESCE(reviewer.username,''),
 resource.id::text,resource.slug,resource.kind,resource.moderation_state,resource.platform,resource.curation_grade,
 revision.id::text,revision.revision_no,revision.name,revision.state,
 owner.id::text,owner.username,
 COALESCE((SELECT jsonb_agg(publication.target ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=revision.id),'[]'::jsonb),
 review.created_at,review.updated_at,
 review.priority,review.due_at,review.first_submitted_at,
 (SELECT count(*) FROM review_cases history JOIN resource_revisions history_revision ON history_revision.id=history.revision_id JOIN resources history_resource ON history_resource.id=history_revision.resource_id WHERE history_resource.owner_id=resource.owner_id AND history.state='rejected'),
 (SELECT count(*) FROM feedback_tickets report WHERE report.kind='resource_report' AND report.target_id=resource.id::text AND report.status IN ('open','investigating'))
FROM review_cases review
JOIN resource_revisions revision ON revision.id=review.revision_id
JOIN resources resource ON resource.id=revision.resource_id
JOIN users owner ON owner.id=resource.owner_id
LEFT JOIN users reviewer ON reviewer.id=review.reviewer_id
WHERE review.id=$1`, id)
	item, err := scanAdminReview(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminReviewDetail{}, ErrAdminReviewNotFound
	}
	if err != nil {
		return AdminReviewDetail{}, err
	}
	detail := AdminReviewDetail{Review: item}
	detail.Current, err = s.adminReviewRevisionSnapshot(ctx, item.RevisionID)
	if err != nil {
		return AdminReviewDetail{}, err
	}
	baseID := detail.Current.BaseRevisionID
	if baseID == "" {
		err = s.db.QueryRowContext(ctx, `SELECT id::text FROM resource_revisions WHERE resource_id=$1 AND revision_no<$2 ORDER BY revision_no DESC LIMIT 1`, item.ResourceID, item.RevisionNumber).Scan(&baseID)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		if err != nil {
			return AdminReviewDetail{}, err
		}
	}
	if baseID != "" {
		detail.Base, err = s.adminReviewRevisionSnapshot(ctx, baseID)
		if err != nil {
			return AdminReviewDetail{}, err
		}
	}
	detail.Diff = summarizeAdminReviewDiff(detail.Base, detail.Current)
	return detail, nil
}

func (s *Store) adminReviewRevisionSnapshot(ctx context.Context, id string) (AdminReviewRevisionSnapshot, error) {
	var snapshot AdminReviewRevisionSnapshot
	var publicationPlan, attributes []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision.id::text,revision.revision_no,revision.name,revision.summary,revision.paid_type,revision.state,
 COALESCE(revision.created_by::text,''),revision.created_via,COALESCE(revision.base_revision_id::text,''),revision.publication_plan,
 COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=revision.id),'[]'::jsonb),revision.created_at
FROM resource_revisions revision WHERE revision.id=$1`, id).Scan(
		&snapshot.ID, &snapshot.Number, &snapshot.Name, &snapshot.Summary, &snapshot.PaidType, &snapshot.State,
		&snapshot.CreatedBy, &snapshot.CreatedVia, &snapshot.BaseRevisionID, &publicationPlan, &attributes, &snapshot.CreatedAt,
	)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	if err := json.Unmarshal(publicationPlan, &snapshot.PublicationPlan); err != nil {
		return AdminReviewRevisionSnapshot{}, fmt.Errorf("decode revision publication plan: %w", err)
	}
	if err := json.Unmarshal(attributes, &snapshot.Attributes); err != nil {
		return AdminReviewRevisionSnapshot{}, fmt.Errorf("decode revision attributes: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT position,title,url FROM revision_links WHERE revision_id=$1 ORDER BY position`, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	for rows.Next() {
		var link AdminLink
		if err := rows.Scan(&link.Position, &link.Title, &link.URL); err != nil {
			rows.Close()
			return AdminReviewRevisionSnapshot{}, err
		}
		snapshot.Links = append(snapshot.Links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminReviewRevisionSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	snapshot.Media, err = s.adminMedia(ctx, adminRevisionMediaSQL, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	snapshot.Artifacts, err = s.adminArtifacts(ctx, adminRevisionArtifactsSQL, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	snapshot.Governance, err = s.AdminRevisionGovernance(ctx, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	return snapshot, nil
}

func summarizeAdminReviewDiff(base, current AdminReviewRevisionSnapshot) AdminReviewDiff {
	diff := AdminReviewDiff{HasBase: base.ID != "", MetadataFields: []string{}, Metadata: []AdminReviewFieldChange{}}
	metadata := []struct {
		name, label   string
		base, current any
		pretty        bool
	}{
		{"name", "名称", base.Name, current.Name, false},
		{"summary", "简介", base.Summary, current.Summary, false},
		{"paid_type", "付费类型", base.PaidType, current.PaidType, false},
		{"publication_plan", "发布计划", base.PublicationPlan, current.PublicationPlan, true},
		{"governance", "治理信息", base.Governance, current.Governance, true},
	}
	for _, field := range metadata {
		before, after := stringifyReviewValue(field.base, field.pretty), stringifyReviewValue(field.current, field.pretty)
		if before == after {
			continue
		}
		diff.MetadataFields = append(diff.MetadataFields, field.name)
		diff.Metadata = append(diff.Metadata, AdminReviewFieldChange{Field: field.name, Label: field.label, Before: before, After: after})
	}
	diff.MetadataChanged = len(diff.MetadataFields) > 0
	diff.Attributes = diffStringSets(base.Attributes, current.Attributes)
	diff.Links = diffKeyedValues(linkValues(base.Links), linkValues(current.Links))
	diff.Media = diffKeyedValues(mediaValues(base.Media), mediaValues(current.Media))
	diff.Artifacts = diffKeyedValues(artifactValues(base.Artifacts), artifactValues(current.Artifacts))
	diff.Devices = diffStringSets(artifactDevices(base.Artifacts), artifactDevices(current.Artifacts))
	diff.AttributeItems = stringSetItemChanges(base.Attributes, current.Attributes)
	diff.LinkItems = keyedItemChanges(linkValues(base.Links), linkValues(current.Links), func(key, value string) string {
		title, url, _ := strings.Cut(value, "\x00")
		if title == "" {
			return url
		}
		return title + " · " + url
	})
	diff.MediaItems = keyedItemChanges(mediaValues(base.Media), mediaValues(current.Media), func(key, value string) string {
		sha, _, _ := strings.Cut(value, ":")
		return key + " · " + sha
	})
	diff.ArtifactItems = keyedItemChanges(artifactValues(base.Artifacts), artifactValues(current.Artifacts), func(key, value string) string {
		name := key
		if _, rest, ok := strings.Cut(key, "\x00"); ok {
			name = rest
		}
		sha, _, _ := strings.Cut(value, ":")
		return name + " · " + sha
	})
	diff.DeviceItems = stringSetItemChanges(artifactDevices(base.Artifacts), artifactDevices(current.Artifacts))
	return diff
}

func stringifyReviewValue(value any, pretty bool) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	}
	if pretty {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Sprint(value)
		}
		text := strings.TrimSpace(string(raw))
		if text == "null" || text == "\"\"" {
			return ""
		}
		return text
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

func stringSetItemChanges(base, current []string) []AdminReviewItemChange {
	left, right := map[string]struct{}{}, map[string]struct{}{}
	for _, value := range base {
		left[value] = struct{}{}
	}
	for _, value := range current {
		right[value] = struct{}{}
	}
	changes := []AdminReviewItemChange{}
	for _, value := range current {
		if _, ok := left[value]; !ok {
			changes = append(changes, AdminReviewItemChange{Key: value, Label: value, Change: "added", After: value})
		}
	}
	for _, value := range base {
		if _, ok := right[value]; !ok {
			changes = append(changes, AdminReviewItemChange{Key: value, Label: value, Change: "removed", Before: value})
		}
	}
	return changes
}

func keyedItemChanges(base, current map[string]string, label func(key, value string) string) []AdminReviewItemChange {
	changes := []AdminReviewItemChange{}
	for key, value := range current {
		old, ok := base[key]
		switch {
		case !ok:
			changes = append(changes, AdminReviewItemChange{Key: key, Label: label(key, value), Change: "added", After: label(key, value)})
		case old != value:
			changes = append(changes, AdminReviewItemChange{Key: key, Label: label(key, value), Change: "changed", Before: label(key, old), After: label(key, value)})
		}
	}
	for key, value := range base {
		if _, ok := current[key]; !ok {
			changes = append(changes, AdminReviewItemChange{Key: key, Label: label(key, value), Change: "removed", Before: label(key, value)})
		}
	}
	return changes
}

func diffStringSets(base, current []string) AdminReviewDiffCount {
	left, right := map[string]struct{}{}, map[string]struct{}{}
	for _, value := range base {
		left[value] = struct{}{}
	}
	for _, value := range current {
		right[value] = struct{}{}
	}
	var result AdminReviewDiffCount
	for value := range right {
		if _, ok := left[value]; !ok {
			result.Added++
		}
	}
	for value := range left {
		if _, ok := right[value]; !ok {
			result.Removed++
		}
	}
	return result
}

func diffKeyedValues(base, current map[string]string) AdminReviewDiffCount {
	var result AdminReviewDiffCount
	for key, value := range current {
		old, ok := base[key]
		if !ok {
			result.Added++
		} else if old != value {
			result.Changed++
		}
	}
	for key := range base {
		if _, ok := current[key]; !ok {
			result.Removed++
		}
	}
	return result
}

func linkValues(items []AdminLink) map[string]string {
	values := map[string]string{}
	for _, item := range items {
		values[fmt.Sprintf("%d", item.Position)] = item.Title + "\x00" + item.URL
	}
	return values
}

func mediaValues(items []AdminMedia) map[string]string {
	values := map[string]string{}
	for _, item := range items {
		key := fmt.Sprintf("%s:%d", item.Role, item.Position)
		values[key] = fmt.Sprintf("%s:%d:%d:%d", item.SHA256, item.Width, item.Height, item.SizeBytes)
	}
	return values
}

func artifactValues(items []AdminArtifact) map[string]string {
	values := map[string]string{}
	for _, item := range items {
		devices := append([]string(nil), item.Devices...)
		sort.Strings(devices)
		analysis, _ := json.Marshal(item.Analysis)
		key := item.PackageID + "\x00" + item.OriginalName
		values[key] = fmt.Sprintf("%s:%s:%s:%d:%s:%s", item.SHA256, item.PackageFormat, item.Version, item.SizeBytes, analysis, strings.Join(devices, "\x00"))
	}
	return values
}

func artifactDevices(items []AdminArtifact) []string {
	seen := map[string]struct{}{}
	for _, item := range items {
		for _, device := range item.Devices {
			seen[device] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for device := range seen {
		values = append(values, device)
	}
	return values
}
