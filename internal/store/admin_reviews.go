package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/reviewdiff"
)

const (
	adminReviewDefaultPerPage = 25
	adminReviewMaxPerPage     = 100
)

var ErrAdminReviewNotFound = errors.New("review case was not found")

var ErrAdminReviewConflict = errors.New("review case changed or is not eligible for this operation")

func (query AdminReviewQuery) normalized() AdminReviewQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Owner = strings.TrimSpace(query.Owner)
	if query.Kind != "quickapp" && query.Kind != "watchface" {
		query.Kind = ""
	}
	if query.Target != "oronbox" && query.Target != "bandbbs" && query.Target != "astrobox" {
		query.Target = ""
	}
	if query.State != "done" && query.State != "pending" && query.State != "approved" && query.State != "rejected" && query.State != "superseded" {
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
	if query.State == "done" {
		where = append(where, `review.state <> 'pending'`)
	} else if query.State != "" {
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
	Description       string
	DescriptionFormat string
	ID                string
	Number            int
	Name              string
	Summary           string
	PaidType          string
	State             string
	CreatedBy         string
	CreatedVia        string
	BaseRevisionID    string
	PublicationPlan   any
	Attributes        []string
	Links             []AdminLink
	Media             []AdminMedia
	Artifacts         []AdminArtifact
	Governance        AdminRevisionGovernance
	CreatedAt         time.Time
}

type AdminReviewDiffCount = reviewdiff.Count

type AdminReviewFieldChange = reviewdiff.FieldChange

type AdminReviewItemChange = reviewdiff.ItemChange

type AdminReviewDiff = reviewdiff.Diff

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

type adminReviewScanner interface {
	Scan(dest ...any) error
}
