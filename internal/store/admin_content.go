package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	adminContentDefaultPerPage = 25
	adminContentMaxPerPage     = 100
)

var (
	ErrAdminBlogConflict = errors.New("blog post state transition is not allowed")
	ErrAdminBlogInvalid  = errors.New("blog post state is invalid")
)

type AdminBlogQuery struct {
	Search    string
	Published string
	Sort      string
	Page      int
	PerPage   int
}

type AdminBlogPage struct {
	Items      []BlogPost
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminBlogQuery
}

func (query AdminBlogQuery) normalized() AdminBlogQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Published = strings.ToLower(strings.TrimSpace(query.Published))
	if query.Published != "published" && query.Published != "draft" {
		query.Published = ""
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "created_asc", "published_desc", "published_asc", "title_asc", "title_desc":
	default:
		query.Sort = "updated_desc"
	}
	query.Page, query.PerPage = normalizeAdminContentPage(query.Page, query.PerPage)
	return query
}

func adminBlogOrder(sort string) string {
	switch sort {
	case "updated_asc":
		return "post.updated_at ASC, post.slug ASC"
	case "created_desc":
		return "post.created_at DESC, post.slug DESC"
	case "created_asc":
		return "post.created_at ASC, post.slug ASC"
	case "published_desc":
		return "post.published_at DESC NULLS LAST, post.slug DESC"
	case "published_asc":
		return "post.published_at ASC NULLS LAST, post.slug ASC"
	case "title_asc":
		return "post.title ASC, post.slug ASC"
	case "title_desc":
		return "post.title DESC, post.slug DESC"
	default:
		return "post.updated_at DESC, post.slug DESC"
	}
}

func (s *Store) AdminBlogPosts(ctx context.Context, raw AdminBlogQuery) (AdminBlogPage, error) {
	query := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',post.slug,post.type,post.title,post.subtitle,post.author,post.body) ILIKE '%'||$1||'%')
 AND ($2='' OR ($2='published' AND post.published) OR ($2='draft' AND NOT post.published))`
	page := AdminBlogPage{Items: []BlogPost{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM blog_posts post WHERE `+filter, query.Search, query.Published).Scan(&page.Total); err != nil {
		return AdminBlogPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM blog_posts post WHERE %s ORDER BY %s LIMIT $3 OFFSET $4`, blogColumns, filter, adminBlogOrder(query.Sort)), query.Search, query.Published, query.PerPage, (query.Page-1)*query.PerPage)
	if err != nil {
		return AdminBlogPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		post, err := scanBlogPost(rows)
		if err != nil {
			return AdminBlogPage{}, err
		}
		page.Items = append(page.Items, post)
	}
	if err := rows.Err(); err != nil {
		return AdminBlogPage{}, err
	}
	page.TotalPages = adminContentTotalPages(page.Total, page.PerPage)
	return page, nil
}

func (s *Store) AdminBlogPost(ctx context.Context, slug string) (BlogPost, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return BlogPost{}, ErrBlogPostNotFound
	}
	post, err := scanBlogPost(s.db.QueryRowContext(ctx, `SELECT `+blogColumns+` FROM blog_posts WHERE slug=$1`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return BlogPost{}, ErrBlogPostNotFound
	}
	return post, err
}

// AdminSetBlogPostState performs a compare-and-set lifecycle transition.
// Publishing is only valid for drafts; withdrawing is only valid for
// published posts. The first publication timestamp is retained on withdrawal
// and any later republish.
func (s *Store) AdminSetBlogPostState(ctx context.Context, slug, action string) (BlogPost, error) {
	slug = strings.TrimSpace(slug)
	action = strings.ToLower(strings.TrimSpace(action))
	if slug == "" {
		return BlogPost{}, ErrBlogPostNotFound
	}
	var query string
	switch action {
	case "publish":
		query = `UPDATE blog_posts SET published=true,published_at=COALESCE(published_at,now()),updated_at=now() WHERE slug=$1 AND NOT published RETURNING ` + blogColumns
	case "withdraw":
		query = `UPDATE blog_posts SET published=false,updated_at=now() WHERE slug=$1 AND published RETURNING ` + blogColumns
	default:
		return BlogPost{}, ErrAdminBlogInvalid
	}
	post, err := scanBlogPost(s.db.QueryRowContext(ctx, query, slug))
	if err == nil {
		return post, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BlogPost{}, err
	}
	if _, lookupErr := s.AdminBlogPost(ctx, slug); errors.Is(lookupErr, ErrBlogPostNotFound) {
		return BlogPost{}, ErrBlogPostNotFound
	} else if lookupErr != nil {
		return BlogPost{}, lookupErr
	}
	return BlogPost{}, ErrAdminBlogConflict
}

type AdminAnnouncementQuery struct {
	Search  string
	From    *time.Time
	To      *time.Time
	Page    int
	PerPage int
}

type AdminAnnouncementItem struct {
	Announcement
	CreatedBy string
	Creator   string
}

type AdminAnnouncementPage struct {
	Items      []AdminAnnouncementItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminAnnouncementQuery
}

func (query AdminAnnouncementQuery) normalized() AdminAnnouncementQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Page, query.PerPage = normalizeAdminContentPage(query.Page, query.PerPage)
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		query.From, query.To = query.To, query.From
	}
	return query
}

func (s *Store) AdminAnnouncementsPage(ctx context.Context, raw AdminAnnouncementQuery) (AdminAnnouncementPage, error) {
	query := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',announcement.id::text,announcement.title,announcement.body,creator.username) ILIKE '%'||$1||'%')
 AND ($2::timestamptz IS NULL OR announcement.published_at >= $2)
 AND ($3::timestamptz IS NULL OR announcement.published_at <= $3)`
	page := AdminAnnouncementPage{Items: []AdminAnnouncementItem{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM announcements announcement LEFT JOIN users creator ON creator.id=announcement.created_by WHERE `+filter, query.Search, query.From, query.To).Scan(&page.Total); err != nil {
		return AdminAnnouncementPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT announcement.id::text,announcement.title,announcement.body,announcement.published_at,COALESCE(announcement.created_by::text,''),COALESCE(creator.username,'') FROM announcements announcement LEFT JOIN users creator ON creator.id=announcement.created_by WHERE `+filter+` ORDER BY announcement.published_at DESC,announcement.id DESC LIMIT $4 OFFSET $5`, query.Search, query.From, query.To, query.PerPage, (query.Page-1)*query.PerPage)
	if err != nil {
		return AdminAnnouncementPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AdminAnnouncementItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &item.PublishedAt, &item.CreatedBy, &item.Creator); err != nil {
			return AdminAnnouncementPage{}, err
		}
		item.Type = "announcement"
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminAnnouncementPage{}, err
	}
	page.TotalPages = adminContentTotalPages(page.Total, page.PerPage)
	return page, nil
}

func normalizeAdminContentPage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = adminContentDefaultPerPage
	}
	if perPage > adminContentMaxPerPage {
		perPage = adminContentMaxPerPage
	}
	return page, perPage
}

func adminContentTotalPages(total, perPage int) int {
	if total == 0 {
		return 0
	}
	return (total + perPage - 1) / perPage
}
