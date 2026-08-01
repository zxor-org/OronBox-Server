package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type BlogPost struct {
	Slug        string
	Type        string
	Title       string
	Subtitle    string
	Author      string
	CoverSHA256 string
	Body        string
	Published   bool
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type HomeBanner struct {
	ID          string
	Type        string
	Title       string
	Subtitle    string
	CoverSHA256 string
	ResourceID  string
	BlogSlug    string
	LinkURL     string
	Position    int
	Enabled     bool
}

type HomeSection struct {
	ID          string
	Name        string
	Description string
	Position    int
	Enabled     bool
}

type HomeSectionCard struct {
	ID         string
	SectionID  string
	Type       string
	ResourceID string
	BlogSlug   string
	Position   int
}

var ErrBlogPostNotFound = errors.New("blog post not found")

const blogColumns = `slug,type,title,subtitle,author,COALESCE(cover_sha256,''),body,published,published_at,created_at,updated_at`

func scanBlogPost(row interface{ Scan(...any) error }) (BlogPost, error) {
	var post BlogPost
	err := row.Scan(
		&post.Slug, &post.Type, &post.Title, &post.Subtitle, &post.Author,
		&post.CoverSHA256, &post.Body, &post.Published, &post.PublishedAt,
		&post.CreatedAt, &post.UpdatedAt,
	)
	return post, err
}

func (s *Store) ListBlogPosts(ctx context.Context) ([]BlogPost, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+blogColumns+` FROM blog_posts ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	posts := []BlogPost{}
	for rows.Next() {
		post, err := scanBlogPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, rows.Err()
}

func (s *Store) ListPublishedBlogPosts(ctx context.Context) ([]BlogPost, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+blogColumns+` FROM blog_posts WHERE published ORDER BY published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	posts := []BlogPost{}
	for rows.Next() {
		post, err := scanBlogPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, rows.Err()
}

func (s *Store) BlogPost(ctx context.Context, slug string) (BlogPost, error) {
	post, err := scanBlogPost(s.db.QueryRowContext(ctx, `SELECT `+blogColumns+` FROM blog_posts WHERE slug=$1`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return BlogPost{}, ErrBlogPostNotFound
	}
	return post, err
}

// UpsertBlogPost creates or replaces the editable fields of a post. The
// publish state is managed separately by SetBlogPostPublished.
func (s *Store) UpsertBlogPost(ctx context.Context, post BlogPost) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO blog_posts(slug,type,title,subtitle,author,cover_sha256,body)
VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7)
ON CONFLICT(slug) DO UPDATE SET
 type=excluded.type,title=excluded.title,subtitle=excluded.subtitle,author=excluded.author,
 cover_sha256=excluded.cover_sha256,body=excluded.body,updated_at=now()`,
		post.Slug, post.Type, post.Title, post.Subtitle, post.Author, post.CoverSHA256, post.Body)
	return err
}

// SetBlogPostPublished toggles visibility and stamps published_at on the
// first publish.
func (s *Store) SetBlogPostPublished(ctx context.Context, slug string, published bool) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE blog_posts SET published=$2,
 published_at=CASE WHEN $2 AND published_at IS NULL THEN now() ELSE published_at END,
 updated_at=now()
WHERE slug=$1`, slug, published)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrBlogPostNotFound
	}
	return nil
}

func (s *Store) DeleteBlogPost(ctx context.Context, slug string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM blog_posts WHERE slug=$1`, slug)
	return err
}

func (s *Store) ListHomeBanners(ctx context.Context, enabledOnly bool) ([]HomeBanner, error) {
	query := `SELECT id::text,type,title,subtitle,COALESCE(cover_sha256,''),COALESCE(resource_id::text,''),COALESCE(blog_slug,''),link_url,position,enabled FROM home_banners`
	if enabledOnly {
		query += ` WHERE enabled`
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	banners := []HomeBanner{}
	for rows.Next() {
		var banner HomeBanner
		if err := rows.Scan(&banner.ID, &banner.Type, &banner.Title, &banner.Subtitle, &banner.CoverSHA256, &banner.ResourceID, &banner.BlogSlug, &banner.LinkURL, &banner.Position, &banner.Enabled); err != nil {
			return nil, err
		}
		banners = append(banners, banner)
	}
	return banners, rows.Err()
}

func (s *Store) CreateHomeBanner(ctx context.Context, banner HomeBanner) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO home_banners(id,type,title,subtitle,cover_sha256,resource_id,blog_slug,link_url,position,enabled)
VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')::uuid,NULLIF($7,''),$8,
 COALESCE((SELECT max(position)+1 FROM home_banners),0),$9)`,
		banner.ID, banner.Type, banner.Title, banner.Subtitle, banner.CoverSHA256,
		banner.ResourceID, banner.BlogSlug, banner.LinkURL, banner.Enabled)
	return err
}

func (s *Store) UpdateHomeBanner(ctx context.Context, banner HomeBanner) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE home_banners SET type=$2,title=$3,subtitle=$4,cover_sha256=NULLIF($5,''),
 resource_id=NULLIF($6,'')::uuid,blog_slug=NULLIF($7,''),link_url=$8,enabled=$9,updated_at=now()
WHERE id=$1`,
		banner.ID, banner.Type, banner.Title, banner.Subtitle, banner.CoverSHA256,
		banner.ResourceID, banner.BlogSlug, banner.LinkURL, banner.Enabled)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return fmt.Errorf("home banner not found: %s", banner.ID)
	}
	return nil
}

func (s *Store) DeleteHomeBanner(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM home_banners WHERE id=$1`, id)
	return err
}

func (s *Store) ListHomeSections(ctx context.Context, enabledOnly bool) ([]HomeSection, error) {
	query := `SELECT id,name,description,position,enabled FROM home_sections`
	if enabledOnly {
		query += ` WHERE enabled`
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sections := []HomeSection{}
	for rows.Next() {
		var section HomeSection
		if err := rows.Scan(&section.ID, &section.Name, &section.Description, &section.Position, &section.Enabled); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func (s *Store) CreateHomeSection(ctx context.Context, section HomeSection) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO home_sections(id,name,description,position,enabled)
VALUES($1,$2,$3,COALESCE((SELECT max(position)+1 FROM home_sections),0),$4)`,
		section.ID, section.Name, section.Description, section.Enabled)
	return err
}

func (s *Store) UpdateHomeSection(ctx context.Context, section HomeSection) error {
	result, err := s.db.ExecContext(ctx, `UPDATE home_sections SET name=$2,description=$3,enabled=$4,updated_at=now() WHERE id=$1`,
		section.ID, section.Name, section.Description, section.Enabled)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return fmt.Errorf("home section not found: %s", section.ID)
	}
	return nil
}

func (s *Store) DeleteHomeSection(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM home_sections WHERE id=$1`, id)
	return err
}

func (s *Store) ListHomeSectionCards(ctx context.Context, sectionID string) ([]HomeSectionCard, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id::text,section_id,type,COALESCE(resource_id::text,''),COALESCE(blog_slug,''),position
FROM home_section_cards WHERE section_id=$1 ORDER BY position`, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []HomeSectionCard{}
	for rows.Next() {
		var card HomeSectionCard
		if err := rows.Scan(&card.ID, &card.SectionID, &card.Type, &card.ResourceID, &card.BlogSlug, &card.Position); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

func (s *Store) CreateHomeSectionCard(ctx context.Context, card HomeSectionCard) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO home_section_cards(id,section_id,type,resource_id,blog_slug,position)
VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,''),
 COALESCE((SELECT max(position)+1 FROM home_section_cards WHERE section_id=$2),0))`,
		card.ID, card.SectionID, card.Type, card.ResourceID, card.BlogSlug)
	return err
}

func (s *Store) DeleteHomeSectionCard(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM home_section_cards WHERE id=$1`, id)
	return err
}

// moveHomeRow swaps the position of a row with its adjacent neighbour within
// its ordering scope. scopeColumn may be empty for globally ordered tables.
func (s *Store) moveHomeRow(ctx context.Context, table, scopeColumn, scopeValue, id string, delta int) error {
	// home_section_cards has no updated_at column
	touch := ",updated_at=now()"
	if table == "home_section_cards" {
		touch = ""
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	scope := ""
	args := []any{id}
	if scopeColumn != "" {
		scope = ` AND ` + scopeColumn + `=$2`
		args = append(args, scopeValue)
	}
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT position FROM `+table+` WHERE id=$1`+scope+` FOR UPDATE`, args...).Scan(&position); err != nil {
		return err
	}
	direction := ">"
	order := "ASC"
	if delta < 0 {
		direction = "<"
		order = "DESC"
	}
	neighbourArgs := []any{position}
	if scopeColumn != "" {
		neighbourArgs = append(neighbourArgs, scopeValue)
	}
	var neighbourID string
	var neighbourPosition int
	err = tx.QueryRowContext(ctx, `SELECT id::text,position FROM `+table+` WHERE position`+direction+`$1`+scope+` ORDER BY position `+order+` LIMIT 1 FOR UPDATE`, neighbourArgs...).Scan(&neighbourID, &neighbourPosition)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET position=$1`+touch+` WHERE id=$2`, neighbourPosition, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET position=$1 WHERE id=$2`, position, neighbourID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MoveHomeBanner(ctx context.Context, id string, delta int) error {
	return s.moveHomeRow(ctx, "home_banners", "", "", id, delta)
}

func (s *Store) MoveHomeSection(ctx context.Context, id string, delta int) error {
	return s.moveHomeRow(ctx, "home_sections", "", "", id, delta)
}

func (s *Store) MoveHomeSectionCard(ctx context.Context, id, sectionID string, delta int) error {
	return s.moveHomeRow(ctx, "home_section_cards", "section_id", sectionID, id, delta)
}
