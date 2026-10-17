package store

import (
	"context"
	"time"
)

func (s *Store) ApplyBlogSnapshot(ctx context.Context, posts []BlogPost) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := []string{}
	for _, p := range posts {
		ids = append(ids, p.Slug)
		if p.PublishedAt == nil {
			now := time.Now()
			p.PublishedAt = &now
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO blog_posts(slug,type,title,subtitle,author,body,published,published_at,source_sha,source_body_sha,source_commit,cover_url)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)ON CONFLICT(slug)DO UPDATE SET type=excluded.type,title=excluded.title,subtitle=excluded.subtitle,author=excluded.author,body=excluded.body,published=excluded.published,published_at=excluded.published_at,source_sha=excluded.source_sha,source_body_sha=excluded.source_body_sha,source_commit=excluded.source_commit,cover_url=excluded.cover_url,cover_sha256=NULL,updated_at=now()`, p.Slug, p.Type, p.Title, p.Subtitle, p.Author, p.Body, p.Published, p.PublishedAt, p.SourceSHA, p.SourceBodySHA, p.SourceCommit, p.CoverURL)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE blog_posts SET published=false WHERE source_commit<>'' AND NOT(slug=ANY($1))`, ids); err != nil {
		return err
	}
	return tx.Commit()
}
