package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type blogCard struct {
	Slug        string     `json:"slug"`
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Subtitle    string     `json:"subtitle"`
	Author      string     `json:"author"`
	CoverSHA256 string     `json:"cover_sha256"`
	PublishedAt *time.Time `json:"published_at"`
}

type homeBannerJSON struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	CoverSHA256 string `json:"cover_sha256"`
	ResourceID  string `json:"resource_id,omitempty"`
	BlogSlug    string `json:"blog_slug,omitempty"`
	LinkURL     string `json:"link_url,omitempty"`
}

type homeCardJSON struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Resource *creator.HomeResourceCard `json:"resource,omitempty"`
	Blog     *blogCard                 `json:"blog,omitempty"`
}

type homeSectionJSON struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Cards       []homeCardJSON `json:"cards"`
}

func publicBlogCard(post store.BlogPost) blogCard {
	return blogCard{
		Slug: post.Slug, Type: post.Type, Title: post.Title, Subtitle: post.Subtitle,
		Author: post.Author, CoverSHA256: post.CoverSHA256, PublishedAt: post.PublishedAt,
	}
}

func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	banners, err := a.store.ListHomeBanners(r.Context(), true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
		return
	}
	sections, err := a.store.ListHomeSections(r.Context(), true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
		return
	}
	posts, err := a.store.ListPublishedBlogPosts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
		return
	}
	postBySlug := make(map[string]store.BlogPost, len(posts))
	for _, post := range posts {
		postBySlug[post.Slug] = post
	}
	cardsBySection := make(map[string][]store.HomeSectionCard, len(sections))
	resourceIDs := []string{}
	for _, section := range sections {
		cards, err := a.store.ListHomeSectionCards(r.Context(), section.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
			return
		}
		cardsBySection[section.ID] = cards
		for _, card := range cards {
			if card.Type == "resource" && card.ResourceID != "" {
				resourceIDs = append(resourceIDs, card.ResourceID)
			}
		}
	}
	resources, err := a.creator.HomeResources(r.Context(), resourceIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
		return
	}
	resourceByID := make(map[string]creator.HomeResourceCard, len(resources))
	for _, resource := range resources {
		resourceByID[resource.ID] = resource
	}
	bannerJSON := make([]homeBannerJSON, 0, len(banners))
	for _, banner := range banners {
		bannerJSON = append(bannerJSON, homeBannerJSON{
			ID: banner.ID, Type: banner.Type, Title: banner.Title, Subtitle: banner.Subtitle,
			CoverSHA256: banner.CoverSHA256, ResourceID: banner.ResourceID, BlogSlug: banner.BlogSlug, LinkURL: banner.LinkURL,
		})
	}
	sectionJSON := make([]homeSectionJSON, 0, len(sections))
	for _, section := range sections {
		out := homeSectionJSON{ID: section.ID, Name: section.Name, Description: section.Description, Cards: []homeCardJSON{}}
		for _, card := range cardsBySection[section.ID] {
			entry := homeCardJSON{ID: card.ID, Type: card.Type}
			if card.Type == "resource" {
				resource, ok := resourceByID[card.ResourceID]
				if !ok {
					continue
				}
				entry.Resource = &resource
			} else if card.Type == "blog" {
				post, ok := postBySlug[card.BlogSlug]
				if !ok {
					continue
				}
				card := publicBlogCard(post)
				entry.Blog = &card
			} else {
				continue
			}
			out.Cards = append(out.Cards, entry)
		}
		sectionJSON = append(sectionJSON, out)
	}
	writeJSON(w, http.StatusOK, map[string]any{"banners": bannerJSON, "sections": sectionJSON})
}

func (a *App) handleBlogList(w http.ResponseWriter, r *http.Request) {
	posts, err := a.store.ListPublishedBlogPosts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("blog_failed", err.Error()))
		return
	}
	items := make([]blogCard, 0, len(posts))
	for _, post := range posts {
		items = append(items, publicBlogCard(post))
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": items})
}

func (a *App) handleBlogPost(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.BlogPost(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrBlogPostNotFound) || (err == nil && !post.Published) {
		writeJSON(w, http.StatusNotFound, errorBody("post_not_found", "blog post was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("blog_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug": post.Slug, "type": post.Type, "title": post.Title, "subtitle": post.Subtitle,
		"author": post.Author, "cover_sha256": post.CoverSHA256, "body": post.Body, "published_at": post.PublishedAt,
	})
}
