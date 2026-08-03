package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// Bounds match the CHECK constraints on blog_posts.slug and home_sections.id.
var blogSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
var homeSectionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

func blogUploadMediaType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

// handleAdminBlobUpload stores a single image for blog covers, banner covers
// and markdown inline media. Returns the SHA-256 used to reference it.
func (a *App) handleAdminBlobUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "multipart form is invalid or too large"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "file field is required"))
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > 8<<20 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "image must be between 1 B and 8 MiB"))
		return
	}
	head := make([]byte, 512)
	read, _ := io.ReadFull(file, head)
	head = head[:read]
	if !blogUploadMediaType(http.DetectContentType(head)) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "only PNG, JPEG, WebP and GIF images are allowed"))
		return
	}
	object, err := a.blobs.Put(r.Context(), io.MultiReader(bytes.NewReader(head), file))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("upload_failed", err.Error()))
		return
	}
	mediaType := http.DetectContentType(head)
	if err := a.store.EnsureBlob(r.Context(), object.SHA256, object.Size, mediaType, object.Key); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("upload_failed", err.Error()))
		return
	}
	actor := currentAdmin(r)
	_ = a.store.RecordAudit(r.Context(), actor, "home.blob.upload", "success", a.clientIP(r), r.UserAgent(), "sha256="+object.SHA256)
	writeJSON(w, http.StatusOK, map[string]any{"sha256": object.SHA256})
}

func (a *App) handleAdminHomePage(w http.ResponseWriter, r *http.Request) {
	banners, err := a.store.ListHomeBanners(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sections, err := a.store.ListHomeSections(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cards := make(map[string][]store.HomeSectionCard, len(sections))
	for _, section := range sections {
		items, err := a.store.ListHomeSectionCards(r.Context(), section.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cards[section.ID] = items
	}
	posts, err := a.store.ListBlogPosts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resources, err := a.store.AdminResources(r.Context(), store.AdminResourceQuery{Page: 1, PerPage: 100, Sort: "updated_desc"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_home", map[string]any{
		"Title": "首页编排", "Banners": banners, "Sections": sections, "Cards": cards,
		"Posts": posts, "Resources": resources.Items,
		"Action": r.URL.Query().Get("action"),
	})
}

func validBannerForm(r *http.Request) (store.HomeBanner, string) {
	banner := store.HomeBanner{
		Type:        strings.TrimSpace(r.FormValue("type")),
		Title:       strings.TrimSpace(r.FormValue("title")),
		Subtitle:    strings.TrimSpace(r.FormValue("subtitle")),
		CoverSHA256: strings.TrimSpace(r.FormValue("cover_sha256")),
		ResourceID:  strings.TrimSpace(r.FormValue("resource_id")),
		BlogSlug:    strings.TrimSpace(r.FormValue("blog_slug")),
		LinkURL:     strings.TrimSpace(r.FormValue("link_url")),
		Enabled:     r.FormValue("enabled") == "on",
	}
	if banner.Title == "" {
		return banner, "标题必填"
	}
	switch banner.Type {
	case "resource":
		if banner.ResourceID == "" {
			return banner, "资源 Banner 必须填写资源 ID"
		}
	case "blog":
		if banner.BlogSlug == "" {
			return banner, "博客 Banner 必须填写文章 Slug"
		}
	case "link":
		if banner.LinkURL == "" {
			return banner, "链接 Banner 必须填写链接"
		}
	default:
		return banner, "未知的 Banner 类型"
	}
	if banner.CoverSHA256 != "" && !sha256Pattern.MatchString(banner.CoverSHA256) {
		return banner, "封面必须是合法的 SHA-256"
	}
	return banner, ""
}

func (a *App) handleAdminBannerCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	banner, problem := validBannerForm(r)
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	banner.ID = uuid.NewString()
	actor := currentAdmin(r)
	if err := a.store.CreateHomeBanner(r.Context(), banner); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.banner.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.banner.create", "success", a.clientIP(r), r.UserAgent(), "banner="+banner.ID+" title="+banner.Title)
	http.Redirect(w, r, "/admin/home?action=banner", http.StatusFound)
}

func (a *App) handleAdminBannerSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	banner, problem := validBannerForm(r)
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	banner.ID = r.PathValue("banner")
	actor := currentAdmin(r)
	if err := a.store.UpdateHomeBanner(r.Context(), banner); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.banner.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.banner.save", "success", a.clientIP(r), r.UserAgent(), "banner="+banner.ID)
	http.Redirect(w, r, "/admin/home?action=banner", http.StatusFound)
}

func (a *App) handleAdminBannerDelete(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	id := r.PathValue("banner")
	if err := a.store.DeleteHomeBanner(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.banner.delete", "success", a.clientIP(r), r.UserAgent(), "banner="+id)
	http.Redirect(w, r, "/admin/home?action=banner", http.StatusFound)
}

func (a *App) handleAdminBannerMove(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	delta, _ := strconv.Atoi(r.FormValue("delta"))
	if delta != 1 && delta != -1 {
		http.Error(w, "invalid direction", http.StatusBadRequest)
		return
	}
	if err := a.store.MoveHomeBanner(r.Context(), r.PathValue("banner"), delta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/home?action=banner", http.StatusFound)
}

func (a *App) handleAdminSectionCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	section := store.HomeSection{
		ID:          strings.TrimSpace(r.FormValue("id")),
		Name:        strings.TrimSpace(r.FormValue("name")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Enabled:     r.FormValue("enabled") == "on",
	}
	if !homeSectionIDPattern.MatchString(section.ID) {
		http.Error(w, "分区 ID 必须是小写字母、数字和中划线", http.StatusBadRequest)
		return
	}
	if section.Name == "" {
		http.Error(w, "分区名称必填", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	if err := a.store.CreateHomeSection(r.Context(), section); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.section.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.section.create", "success", a.clientIP(r), r.UserAgent(), "section="+section.ID)
	http.Redirect(w, r, "/admin/home?action=section", http.StatusFound)
}

func (a *App) handleAdminSectionSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	section := store.HomeSection{
		ID:          r.PathValue("section"),
		Name:        strings.TrimSpace(r.FormValue("name")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Enabled:     r.FormValue("enabled") == "on",
	}
	if section.Name == "" {
		http.Error(w, "分区名称必填", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	if err := a.store.UpdateHomeSection(r.Context(), section); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.section.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.section.save", "success", a.clientIP(r), r.UserAgent(), "section="+section.ID)
	http.Redirect(w, r, "/admin/home?action=section", http.StatusFound)
}

func (a *App) handleAdminSectionDelete(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	id := r.PathValue("section")
	if err := a.store.DeleteHomeSection(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.section.delete", "success", a.clientIP(r), r.UserAgent(), "section="+id)
	http.Redirect(w, r, "/admin/home?action=section", http.StatusFound)
}

func (a *App) handleAdminSectionMove(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	delta, _ := strconv.Atoi(r.FormValue("delta"))
	if delta != 1 && delta != -1 {
		http.Error(w, "invalid direction", http.StatusBadRequest)
		return
	}
	if err := a.store.MoveHomeSection(r.Context(), r.PathValue("section"), delta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/home?action=section", http.StatusFound)
}

func (a *App) handleAdminCardCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	card := store.HomeSectionCard{
		ID:         uuid.NewString(),
		SectionID:  strings.TrimSpace(r.FormValue("section_id")),
		Type:       strings.TrimSpace(r.FormValue("type")),
		ResourceID: strings.TrimSpace(r.FormValue("resource_id")),
		BlogSlug:   strings.TrimSpace(r.FormValue("blog_slug")),
	}
	if card.SectionID == "" {
		http.Error(w, "缺少分区", http.StatusBadRequest)
		return
	}
	switch card.Type {
	case "resource":
		if card.ResourceID == "" {
			http.Error(w, "资源卡片必须填写资源 ID", http.StatusBadRequest)
			return
		}
	case "blog":
		if card.BlogSlug == "" {
			http.Error(w, "博客卡片必须填写文章 Slug", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "未知的卡片类型", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	if err := a.store.CreateHomeSectionCard(r.Context(), card); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.card.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.card.create", "success", a.clientIP(r), r.UserAgent(), "card="+card.ID+" section="+card.SectionID+" type="+card.Type)
	http.Redirect(w, r, "/admin/home?action=card", http.StatusFound)
}

func (a *App) handleAdminCardDelete(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	id := r.PathValue("card")
	if err := a.store.DeleteHomeSectionCard(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.card.delete", "success", a.clientIP(r), r.UserAgent(), "card="+id)
	http.Redirect(w, r, "/admin/home?action=card", http.StatusFound)
}

func (a *App) handleAdminCardMove(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	delta, _ := strconv.Atoi(r.FormValue("delta"))
	if delta != 1 && delta != -1 {
		http.Error(w, "invalid direction", http.StatusBadRequest)
		return
	}
	if err := a.store.MoveHomeSectionCard(r.Context(), r.PathValue("card"), r.FormValue("section_id"), delta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/home?action=card", http.StatusFound)
}

func (a *App) handleAdminBlogList(w http.ResponseWriter, r *http.Request) {
	posts, err := a.store.ListBlogPosts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_blog", map[string]any{"Title": "Blog 管理", "Posts": posts, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminBlogCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if !blogSlugPattern.MatchString(slug) {
		http.Error(w, "Slug 必须是小写字母、数字和中划线", http.StatusBadRequest)
		return
	}
	post := store.BlogPost{Slug: slug, Type: "announcement", Title: slug}
	switch strings.TrimSpace(r.FormValue("type")) {
	case "recommendation":
		post.Type = "recommendation"
	case "docs":
		post.Type = "docs"
	}
	actor := currentAdmin(r)
	if _, err := a.store.BlogPost(r.Context(), slug); err == nil {
		http.Error(w, "Slug 已存在", http.StatusConflict)
		return
	} else if !errors.Is(err, store.ErrBlogPostNotFound) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.store.UpsertBlogPost(r.Context(), post); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "blog.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "blog.create", "success", a.clientIP(r), r.UserAgent(), "slug="+slug)
	http.Redirect(w, r, "/admin/blog/"+slug, http.StatusFound)
}

func (a *App) handleAdminBlogEdit(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.BlogPost(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrBlogPostNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_blog_edit", map[string]any{"Title": "编辑文章", "Post": post, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminBlogSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	slug := r.PathValue("slug")
	post, err := a.store.BlogPost(r.Context(), slug)
	if errors.Is(err, store.ErrBlogPostNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	post.Type = strings.TrimSpace(r.FormValue("type"))
	switch post.Type {
	case "announcement", "recommendation", "docs":
	default:
		http.Error(w, "未知的文章类型", http.StatusBadRequest)
		return
	}
	post.Title = strings.TrimSpace(r.FormValue("title"))
	post.Subtitle = strings.TrimSpace(r.FormValue("subtitle"))
	post.Author = strings.TrimSpace(r.FormValue("author"))
	post.CoverSHA256 = strings.TrimSpace(r.FormValue("cover_sha256"))
	post.Body = r.FormValue("body")
	if post.Title == "" {
		http.Error(w, "标题必填", http.StatusBadRequest)
		return
	}
	if post.CoverSHA256 != "" && !sha256Pattern.MatchString(post.CoverSHA256) {
		http.Error(w, "封面必须是合法的 SHA-256", http.StatusBadRequest)
		return
	}
	published := post.Published
	switch r.FormValue("publication_action") {
	case "publish":
		published = true
	case "unpublish":
		published = false
	case "save", "":
	default:
		http.Error(w, "未知的文章操作", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	if err := a.store.UpsertBlogPost(r.Context(), post); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "blog.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if published != post.Published {
		if err := a.store.SetBlogPostPublished(r.Context(), slug, published); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	_ = a.store.RecordAudit(r.Context(), actor, "blog.save", "success", a.clientIP(r), r.UserAgent(), "slug="+slug+" published="+strconv.FormatBool(published))
	http.Redirect(w, r, "/admin/blog/"+slug+"?action=saved", http.StatusFound)
}

func (a *App) handleAdminBlogDelete(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	slug := r.PathValue("slug")
	if err := a.store.DeleteBlogPost(r.Context(), slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "blog.delete", "success", a.clientIP(r), r.UserAgent(), "slug="+slug)
	http.Redirect(w, r, "/admin/blog?action=deleted", http.StatusFound)
}
