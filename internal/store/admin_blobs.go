package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	adminBlobDefaultPerPage = 25
	adminBlobMaxPerPage     = 100
)

var (
	ErrAdminBlobNotFound        = errors.New("blob was not found")
	ErrAdminBlobReplicaConflict = errors.New("blob replica state transition is not allowed")
	adminBlobSHA256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type AdminBlobQuery struct {
	Search       string
	MediaType    string
	ReplicaState string
	Referenced   string
	Sort         string
	Page         int
	PerPage      int
}

type AdminBlobItem struct {
	SHA256          string     `json:"sha256"`
	SizeBytes       int64      `json:"size_bytes"`
	MediaType       string     `json:"media_type"`
	LocalKey        string     `json:"local_key"`
	LocalAvailable  bool       `json:"local_available"`
	R2ObjectKey     string     `json:"r2_object_key"`
	R2State         string     `json:"r2_state"`
	R2ErrorMessage  string     `json:"r2_error_message,omitempty"`
	R2Attempts      int        `json:"r2_attempts"`
	R2NextAttemptAt *time.Time `json:"r2_next_attempt_at,omitempty"`
	R2UpdatedAt     *time.Time `json:"r2_updated_at,omitempty"`
	Referenced      bool       `json:"referenced"`
	ReferenceCount  int        `json:"reference_count"`
	CreatedAt       time.Time  `json:"created_at"`
}

type AdminBlobPage struct {
	Items      []AdminBlobItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminBlobQuery
}

type AdminBlobResourceReference struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Kind     string `json:"kind"`
	Platform string `json:"platform"`
	Name     string `json:"name"`
}

type AdminBlobRevisionReference struct {
	ID             string `json:"id"`
	ResourceID     string `json:"resource_id"`
	ResourceSlug   string `json:"resource_slug"`
	RevisionNumber int    `json:"revision_number"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Usages         string `json:"usages"`
}

type AdminBlobBlogReference struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Published bool   `json:"published"`
	Usage     string `json:"usage"`
}

type AdminBlobBannerReference struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Enabled  bool   `json:"enabled"`
	Position int    `json:"position"`
}

type AdminBlobDetail struct {
	Blob      AdminBlobItem                `json:"blob"`
	Resources []AdminBlobResourceReference `json:"resources"`
	Revisions []AdminBlobRevisionReference `json:"revisions"`
	Blogs     []AdminBlobBlogReference     `json:"blogs"`
	Banners   []AdminBlobBannerReference   `json:"banners"`
}

func (query AdminBlobQuery) normalized() AdminBlobQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.MediaType = strings.ToLower(strings.TrimSpace(query.MediaType))
	query.ReplicaState = strings.ToLower(strings.TrimSpace(query.ReplicaState))
	query.Referenced = strings.ToLower(strings.TrimSpace(query.Referenced))
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	switch query.ReplicaState {
	case "missing", "pending", "uploading", "ready", "failed":
	default:
		query.ReplicaState = ""
	}
	if query.Referenced != "referenced" && query.Referenced != "unreferenced" {
		query.Referenced = ""
	}
	switch query.Sort {
	case "created_asc", "size_desc", "size_asc", "media_type", "sha256":
	default:
		query.Sort = "created_desc"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = adminBlobDefaultPerPage
	}
	if query.PerPage > adminBlobMaxPerPage {
		query.PerPage = adminBlobMaxPerPage
	}
	return query
}

func adminBlobOrder(sort string) string {
	switch sort {
	case "created_asc":
		return "blob.created_at ASC, blob.sha256 ASC"
	case "size_desc":
		return "blob.size_bytes DESC, blob.created_at DESC, blob.sha256 DESC"
	case "size_asc":
		return "blob.size_bytes ASC, blob.created_at DESC, blob.sha256 DESC"
	case "media_type":
		return "lower(blob.media_type), blob.created_at DESC, blob.sha256 DESC"
	case "sha256":
		return "blob.sha256 ASC"
	default:
		return "blob.created_at DESC, blob.sha256 DESC"
	}
}

const adminBlobReferencesSQL = `
LEFT JOIN LATERAL (
 SELECT count(*)::integer AS reference_count
 FROM (
  SELECT media.id::text FROM revision_media media WHERE media.blob_sha256=blob.sha256
  UNION ALL
  SELECT artifact.id::text FROM revision_artifacts artifact WHERE artifact.blob_sha256=blob.sha256
  UNION ALL
  SELECT post.slug FROM blog_posts post WHERE post.cover_sha256=blob.sha256 OR post.body LIKE '%'||blob.sha256||'%'
  UNION ALL
  SELECT banner.id::text FROM home_banners banner WHERE banner.cover_sha256=blob.sha256
 ) reference
) usage ON TRUE`

func (s *Store) AdminBlobs(ctx context.Context, raw AdminBlobQuery) (AdminBlobPage, error) {
	query := raw.normalized()
	args := make([]any, 0, 7)
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.Search != "" {
		add(`concat_ws(' ',blob.sha256,blob.media_type,blob.local_key,replica.object_key,replica.error_message) ILIKE '%'||?||'%'`, query.Search)
	}
	if query.MediaType != "" {
		add(`lower(blob.media_type)=?`, query.MediaType)
	}
	if query.ReplicaState == "missing" {
		where = append(where, `replica.blob_sha256 IS NULL`)
	} else if query.ReplicaState != "" {
		add(`replica.state=?`, query.ReplicaState)
	}
	if query.Referenced == "referenced" {
		where = append(where, `usage.reference_count>0`)
	} else if query.Referenced == "unreferenced" {
		where = append(where, `usage.reference_count=0`)
	}

	base := `FROM blobs blob
LEFT JOIN blob_replicas replica ON replica.blob_sha256=blob.sha256 AND replica.backend='r2'
` + adminBlobReferencesSQL + `
WHERE ` + strings.Join(where, " AND ")
	page := AdminBlobPage{Items: []AdminBlobItem{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+base, args...).Scan(&page.Total); err != nil {
		return AdminBlobPage{}, err
	}
	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT blob.sha256,blob.size_bytes,blob.media_type,blob.local_key,
       COALESCE(replica.object_key,''),COALESCE(replica.state,'missing'),
       COALESCE(replica.error_message,''),COALESCE(replica.attempts,0),
       replica.next_attempt_at,replica.updated_at,
       usage.reference_count>0,usage.reference_count,blob.created_at
%s ORDER BY %s LIMIT $%d OFFSET $%d`, base, adminBlobOrder(query.Sort), len(args)-1, len(args)), args...)
	if err != nil {
		return AdminBlobPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanAdminBlob(rows)
		if err != nil {
			return AdminBlobPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminBlobPage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

type adminBlobScanner interface {
	Scan(dest ...any) error
}

func scanAdminBlob(scanner adminBlobScanner) (AdminBlobItem, error) {
	var item AdminBlobItem
	if err := scanner.Scan(
		&item.SHA256, &item.SizeBytes, &item.MediaType, &item.LocalKey,
		&item.R2ObjectKey, &item.R2State, &item.R2ErrorMessage, &item.R2Attempts,
		&item.R2NextAttemptAt, &item.R2UpdatedAt, &item.Referenced, &item.ReferenceCount, &item.CreatedAt,
	); err != nil {
		return AdminBlobItem{}, err
	}
	item.LocalAvailable = item.LocalKey != ""
	return item, nil
}

func (s *Store) AdminBlob(ctx context.Context, rawSHA256 string) (AdminBlobDetail, error) {
	sha256 := strings.ToLower(strings.TrimSpace(rawSHA256))
	if !adminBlobSHA256Pattern.MatchString(sha256) {
		return AdminBlobDetail{}, ErrAdminBlobNotFound
	}
	row := s.db.QueryRowContext(ctx, `
SELECT blob.sha256,blob.size_bytes,blob.media_type,blob.local_key,
       COALESCE(replica.object_key,''),COALESCE(replica.state,'missing'),
       COALESCE(replica.error_message,''),COALESCE(replica.attempts,0),
       replica.next_attempt_at,replica.updated_at,
       usage.reference_count>0,usage.reference_count,blob.created_at
FROM blobs blob
LEFT JOIN blob_replicas replica ON replica.blob_sha256=blob.sha256 AND replica.backend='r2'
`+adminBlobReferencesSQL+`
WHERE blob.sha256=$1`, sha256)
	item, err := scanAdminBlob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminBlobDetail{}, ErrAdminBlobNotFound
	}
	if err != nil {
		return AdminBlobDetail{}, err
	}
	detail := AdminBlobDetail{
		Blob: item, Resources: []AdminBlobResourceReference{}, Revisions: []AdminBlobRevisionReference{},
		Blogs: []AdminBlobBlogReference{}, Banners: []AdminBlobBannerReference{},
	}
	if err := s.loadAdminBlobResources(ctx, sha256, &detail); err != nil {
		return AdminBlobDetail{}, err
	}
	if err := s.loadAdminBlobBlogs(ctx, sha256, &detail); err != nil {
		return AdminBlobDetail{}, err
	}
	if err := s.loadAdminBlobBanners(ctx, sha256, &detail); err != nil {
		return AdminBlobDetail{}, err
	}
	return detail, nil
}

func (s *Store) loadAdminBlobResources(ctx context.Context, sha256 string, detail *AdminBlobDetail) error {
	rows, err := s.db.QueryContext(ctx, `
WITH reference AS (
 SELECT media.revision_id,('media:'||media.role)::text AS usage FROM revision_media media WHERE media.blob_sha256=$1
 UNION ALL
 SELECT artifact.revision_id,('artifact:'||artifact.original_name)::text AS usage FROM revision_artifacts artifact WHERE artifact.blob_sha256=$1
)
SELECT resource.id::text,resource.slug,resource.kind,resource.platform,
       COALESCE(current_revision.name,resource.draft_name),
       revision.id::text,revision.revision_no,revision.name,revision.state,string_agg(reference.usage,', ' ORDER BY reference.usage)
FROM reference
JOIN resource_revisions revision ON revision.id=reference.revision_id
JOIN resources resource ON resource.id=revision.resource_id
LEFT JOIN resource_revisions current_revision ON current_revision.id=resource.current_revision_id
GROUP BY resource.id,resource.slug,resource.kind,resource.platform,current_revision.name,resource.draft_name,
         revision.id,revision.revision_no,revision.name,revision.state
ORDER BY resource.updated_at DESC,revision.revision_no DESC`, sha256)
	if err != nil {
		return err
	}
	defer rows.Close()
	seenResources := map[string]bool{}
	for rows.Next() {
		var resource AdminBlobResourceReference
		var revision AdminBlobRevisionReference
		if err := rows.Scan(&resource.ID, &resource.Slug, &resource.Kind, &resource.Platform, &resource.Name,
			&revision.ID, &revision.RevisionNumber, &revision.Name, &revision.State, &revision.Usages); err != nil {
			return err
		}
		revision.ResourceID = resource.ID
		revision.ResourceSlug = resource.Slug
		detail.Revisions = append(detail.Revisions, revision)
		if !seenResources[resource.ID] {
			seenResources[resource.ID] = true
			detail.Resources = append(detail.Resources, resource)
		}
	}
	return rows.Err()
}

func (s *Store) loadAdminBlobBlogs(ctx context.Context, sha256 string, detail *AdminBlobDetail) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT post.slug,post.title,post.published,
       concat_ws(', ',CASE WHEN post.cover_sha256=$1 THEN 'cover' END,CASE WHEN post.body LIKE '%'||$1||'%' THEN 'body' END)
FROM blog_posts post
WHERE post.cover_sha256=$1 OR post.body LIKE '%'||$1||'%'
ORDER BY post.updated_at DESC,post.slug`, sha256)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var reference AdminBlobBlogReference
		if err := rows.Scan(&reference.Slug, &reference.Title, &reference.Published, &reference.Usage); err != nil {
			return err
		}
		detail.Blogs = append(detail.Blogs, reference)
	}
	return rows.Err()
}

func (s *Store) loadAdminBlobBanners(ctx context.Context, sha256 string, detail *AdminBlobDetail) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT banner.id::text,banner.title,banner.enabled,banner.position
FROM home_banners banner WHERE banner.cover_sha256=$1
ORDER BY banner.position,banner.updated_at DESC`, sha256)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var reference AdminBlobBannerReference
		if err := rows.Scan(&reference.ID, &reference.Title, &reference.Enabled, &reference.Position); err != nil {
			return err
		}
		detail.Banners = append(detail.Banners, reference)
	}
	return rows.Err()
}

// AdminRequeueBlobReplica safely makes a failed R2 replica eligible for the
// coordinator's next upload attempt. Other states are deliberately immutable.
func (s *Store) AdminRequeueBlobReplica(ctx context.Context, rawSHA256 string) (AdminBlobDetail, error) {
	sha256 := strings.ToLower(strings.TrimSpace(rawSHA256))
	if !adminBlobSHA256Pattern.MatchString(sha256) {
		return AdminBlobDetail{}, ErrAdminBlobNotFound
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE blob_replicas SET state='pending',error_message='',next_attempt_at=now(),updated_at=now()
WHERE blob_sha256=$1 AND backend='r2' AND state='failed'`, sha256)
	if err != nil {
		return AdminBlobDetail{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blobs WHERE sha256=$1)`, sha256).Scan(&exists); err != nil {
			return AdminBlobDetail{}, err
		}
		if !exists {
			return AdminBlobDetail{}, ErrAdminBlobNotFound
		}
		return AdminBlobDetail{}, ErrAdminBlobReplicaConflict
	}
	return s.AdminBlob(ctx, sha256)
}
