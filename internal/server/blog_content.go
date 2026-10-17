package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/gitea"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

var blogSlugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

var markdownImage = regexp.MustCompile(`!\[[^\]]*\]\(([^\s)]+)\)`)

type articleEntry struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Category    string     `json:"category"`
	Author      string     `json:"author"`
	File        string     `json:"file"`
	Status      string     `json:"status"`
	Cover       string     `json:"cover_image"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

type articleIndex struct {
	Articles []articleEntry `json:"articles"`
}

func (a *App) articleIndex(ctx context.Context) (gitea.Client, articleIndex, string, string, error) {
	c := gitea.Client{Config: a.cfg.DeviceCatalog}
	head, err := c.Head(ctx)
	if err != nil {
		return c, articleIndex{}, "", "", err
	}
	raw, sha, err := c.File(ctx, "articles/manifest.json", head)
	var upstream gitea.Error
	if errors.As(err, &upstream) && upstream.Status == 404 {
		return c, articleIndex{Articles: []articleEntry{}}, "", head, nil
	}
	if err != nil {
		return c, articleIndex{}, "", "", err
	}
	var index articleIndex
	if json.Unmarshal(raw, &index) != nil || index.Articles == nil || len(index.Articles) > 1000 {
		return c, index, "", "", fmt.Errorf("invalid article manifest")
	}
	seen := map[string]bool{}
	for _, e := range index.Articles {
		if !blogSlugPattern.MatchString(e.ID) || seen[e.ID] || !gitea.ValidPath(e.File) || !strings.HasPrefix(e.File, "articles/") || !strings.HasSuffix(e.File, ".md") || (e.Status != "draft" && e.Status != "published") {
			return c, index, "", "", fmt.Errorf("invalid article entry")
		}
		seen[e.ID] = true
	}
	return c, index, sha, head, nil
}

func (a *App) giteaImageURL(value, commit, relativeTo string) (string, error) {
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	base, _ := url.Parse(a.cfg.DeviceCatalog.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (!u.IsAbs() && u.Host != "") {
		return "", fmt.Errorf("invalid content image URL")
	}
	if u.IsAbs() {
		if u.Scheme != "https" || u.Host != base.Host {
			return "", fmt.Errorf("content images must be hosted on Gitea")
		}
		return value, nil
	}
	p := path.Join(path.Dir(relativeTo), value)
	if !gitea.ValidPath(p) || !strings.HasPrefix(p, "articles/") {
		return "", fmt.Errorf("image path escapes article directory")
	}
	return strings.TrimRight(a.cfg.DeviceCatalog.BaseURL, "/") + "/" + a.cfg.DeviceCatalog.Repository + "/raw/commit/" + commit + "/" + p, nil
}

func (a *App) syncBlogContent(ctx context.Context) error {
	c, index, sha, head, err := a.articleIndex(ctx)
	if err != nil {
		return err
	}
	if sha == "" {
		return nil
	}
	posts := []store.BlogPost{}
	total := 0
	for _, e := range index.Articles {
		body, bodySHA, err := c.File(ctx, e.File, head)
		if err != nil {
			return err
		}
		total += len(body)
		if len(body) > 2<<20 || total > 16<<20 {
			return fmt.Errorf("article content exceeds limit")
		}
		cover, err := a.giteaImageURL(e.Cover, head, e.File)
		if err != nil {
			return err
		}
		var imageErr error
		rendered := markdownImage.ReplaceAllStringFunc(string(body), func(match string) string {
			m := markdownImage.FindStringSubmatch(match)
			resolved, err := a.giteaImageURL(m[1], head, e.File)
			if err != nil {
				imageErr = err
				return match
			}
			return strings.Replace(match, m[1], resolved, 1)
		})
		if imageErr != nil {
			return imageErr
		}
		posts = append(posts, store.BlogPost{Slug: e.ID, Type: e.Category, Title: e.Title, Subtitle: e.Summary, Author: e.Author, Body: rendered, Published: e.Status == "published", PublishedAt: e.PublishedAt, SourceSHA: sha, SourceBodySHA: bodySHA, SourceCommit: head, CoverURL: cover})
	}
	return a.store.ApplyBlogSnapshot(ctx, posts)
}

func (a *App) handleBlogManifest(w http.ResponseWriter, r *http.Request) {
	_, index, sha, head, err := a.articleIndex(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", err.Error()))
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": index.Articles, "source_sha": sha, "source_commit": head})
}

func (a *App) writeBlogContent(w http.ResponseWriter, r *http.Request, remove bool) {
	var in store.BlogPost
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid article"))
		return
	}
	if slug := r.PathValue("slug"); slug != "" {
		in.Slug = slug
	}
	if !blogSlugPattern.MatchString(in.Slug) || (!remove && (strings.TrimSpace(in.Title) == "" || len(in.Body) > 2<<20)) {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_article", "invalid article fields"))
		return
	}
	c, index, sha, head, err := a.articleIndex(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", err.Error()))
		return
	}
	if sha != in.SourceSHA {
		httpx.WriteJSON(w, 409, httpx.ErrorBody("repository_conflict", "article index changed; reload before saving"))
		return
	}
	found := -1
	entry := articleEntry{ID: in.Slug, File: "articles/" + in.Slug + ".md"}
	for i, e := range index.Articles {
		if e.ID == in.Slug {
			found = i
			entry = e
			break
		}
	}
	bodySHA := ""
	if found >= 0 {
		_, bodySHA, err = c.File(r.Context(), entry.File, head)
		if err != nil {
			httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", err.Error()))
			return
		}
		if bodySHA != in.SourceBodySHA {
			httpx.WriteJSON(w, 409, httpx.ErrorBody("repository_conflict", "article body changed; reload before saving"))
			return
		}
	}
	files := []gitea.Change{}
	if remove {
		if found < 0 {
			httpx.WriteJSON(w, 404, httpx.ErrorBody("not_found", "article not found"))
			return
		}
		index.Articles = append(index.Articles[:found], index.Articles[found+1:]...)
		files = append(files, gitea.Change{Operation: "delete", Path: entry.File, SHA: bodySHA})
	} else {
		if _, err = a.giteaImageURL(in.CoverURL, head, entry.File); err != nil {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_image", err.Error()))
			return
		}
		for _, image := range markdownImage.FindAllStringSubmatch(in.Body, -1) {
			if _, err = a.giteaImageURL(image[1], head, entry.File); err != nil {
				httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_image", err.Error()))
				return
			}
		}
		entry.Title = in.Title
		entry.Summary = in.Subtitle
		entry.Category = in.Type
		entry.Author = in.Author
		entry.Cover = in.CoverURL
		entry.Status = "draft"
		if in.Published {
			entry.Status = "published"
			if entry.PublishedAt == nil {
				now := time.Now().UTC()
				entry.PublishedAt = &now
			}
		}
		if found < 0 {
			index.Articles = append(index.Articles, entry)
		} else {
			index.Articles[found] = entry
		}
		files = append(files, gitea.FileChange(entry.File, bodySHA, []byte(in.Body)))
	}
	raw, _ := json.MarshalIndent(index, "", "  ")
	files = append(files, gitea.FileChange("articles/manifest.json", sha, raw))
	if err = c.Commit(r.Context(), "Update article "+in.Slug, files); err != nil {
		var upstream gitea.Error
		status := 502
		if errors.As(err, &upstream) && (upstream.Status == 409 || upstream.Status == 422) {
			status = 409
		}
		httpx.WriteJSON(w, status, httpx.ErrorBody("repository_conflict", err.Error()))
		return
	}
	if err = a.syncBlogContent(r.Context()); err != nil {
		httpx.WriteJSON(w, 202, map[string]any{"saved": true, "cache_pending": true})
		return
	}
	a.audit(r, "blog.gitea.save", "success", in.Slug)
	post, _ := a.store.AdminBlogPost(r.Context(), in.Slug)
	httpx.WriteJSON(w, 200, post)
}

func (a *App) handleAdminAPIBlogGet(w http.ResponseWriter, r *http.Request) {
	c, index, sha, head, err := a.articleIndex(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", "content repository unavailable"))
		return
	}
	for _, entry := range index.Articles {
		if entry.ID != r.PathValue("slug") {
			continue
		}
		body, bodySHA, err := c.File(r.Context(), entry.File, head)
		if err != nil {
			httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", "article body unavailable"))
			return
		}
		httpx.WriteJSON(w, 200, store.BlogPost{Slug: entry.ID, Title: entry.Title, Subtitle: entry.Summary, Type: entry.Category, Author: entry.Author, Body: string(body), Published: entry.Status == "published", PublishedAt: entry.PublishedAt, SourceSHA: sha, SourceBodySHA: bodySHA, SourceCommit: head, CoverURL: entry.Cover})
		return
	}
	httpx.WriteJSON(w, 404, httpx.ErrorBody("not_found", "article not found in content repository"))
}

func (a *App) handleAdminAPIBlogSave(w http.ResponseWriter, r *http.Request) {
	a.writeBlogContent(w, r, false)
}

func (a *App) handleAdminAPIBlogDelete(w http.ResponseWriter, r *http.Request) {
	a.writeBlogContent(w, r, true)
}
