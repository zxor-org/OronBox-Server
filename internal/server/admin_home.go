package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
	_ "golang.org/x/image/webp"
)

// Bounds match the CHECK constraints on blog_posts.slug and home_sections.id.
var blogSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
var homeSectionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

type adminFormField struct {
	Name   string
	Values []string
}

// renderAdminFormErrorRequest keeps the submitted fields on the error page so
// an administrator can retry the same POST without losing a long edit.
func (a *App) renderAdminFormErrorRequest(w http.ResponseWriter, r *http.Request, title, message, retryURL, backURL string, status int) {
	fields := make([]adminFormField, 0, len(r.PostForm))
	for name, values := range r.PostForm {
		if name == "file" {
			continue
		}
		copied := append([]string(nil), values...)
		fields = append(fields, adminFormField{Name: name, Values: copied})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	http.Error(w, message, status)
}

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
	if err := a.parseAdminUpload(w, r, 8<<20); err != nil {
		if errors.Is(err, errAdminCSRF) {
			writeJSON(w, http.StatusForbidden, errorBody("forbidden", "admin request rejected"))
			return
		}
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
	payload, err := io.ReadAll(io.LimitReader(file, 8<<20+1))
	if err != nil || len(payload) == 0 || len(payload) > 8<<20 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "image must be between 1 B and 8 MiB"))
		return
	}
	head := payload
	if len(head) > 512 {
		head = head[:512]
	}
	if !blogUploadMediaType(http.DetectContentType(head)) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "only PNG, JPEG, WebP and GIF images are allowed"))
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 1500 || config.Height > 1500 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_upload", "image dimensions must be between 1 and 1500 pixels"))
		return
	}
	object, err := a.blobs.Put(r.Context(), bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("upload_failed", err.Error()))
		return
	}
	mediaType := "image/" + format
	if err := a.store.EnsureBlob(r.Context(), object.SHA256, object.Size, mediaType, object.Key); err != nil {
		// The object is content-addressed, so only remove it when the catalog
		// confirms that this request did not register a row. If another request
		// won the race and registered the same digest, retain the shared object.
		if _, lookupErr := a.store.Blob(r.Context(), object.SHA256); errors.Is(lookupErr, sql.ErrNoRows) {
			_ = a.blobs.Delete(r.Context(), object.Key)
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("upload_failed", err.Error()))
		return
	}
	actor := currentAdmin(r)
	_ = a.store.RecordAudit(r.Context(), actor, "home.blob.upload", "success", a.clientIP(r), r.UserAgent(), "sha256="+object.SHA256)
	writeJSON(w, http.StatusOK, map[string]any{"sha256": object.SHA256})
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
		if _, err := uuid.Parse(banner.ResourceID); err != nil {
			return banner, "资源 Banner 的资源 ID 无效"
		}
		banner.BlogSlug, banner.LinkURL = "", ""
	case "blog":
		if banner.BlogSlug == "" {
			return banner, "博客 Banner 必须填写文章 Slug"
		}
		if !blogSlugPattern.MatchString(banner.BlogSlug) {
			return banner, "博客 Banner 的文章 Slug 无效"
		}
		banner.ResourceID, banner.LinkURL = "", ""
	case "link":
		if banner.LinkURL == "" {
			return banner, "链接 Banner 必须填写链接"
		}
		link, err := url.ParseRequestURI(banner.LinkURL)
		if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Host == "" {
			return banner, "链接 Banner 只支持有效的 HTTP 或 HTTPS 地址"
		}
		banner.ResourceID, banner.BlogSlug = "", ""
	default:
		return banner, "未知的 Banner 类型"
	}
	if banner.CoverSHA256 != "" && !sha256Pattern.MatchString(banner.CoverSHA256) {
		return banner, "封面必须是合法的 SHA-256"
	}
	return banner, ""
}

func validHomeCardForm(r *http.Request) (store.HomeSectionCard, string) {
	card := store.HomeSectionCard{
		SectionID:  strings.TrimSpace(r.FormValue("section_id")),
		Type:       strings.TrimSpace(r.FormValue("type")),
		ResourceID: strings.TrimSpace(r.FormValue("resource_id")),
		BlogSlug:   strings.TrimSpace(r.FormValue("blog_slug")),
	}
	if !homeSectionIDPattern.MatchString(card.SectionID) {
		return card, "分区无效"
	}
	switch card.Type {
	case "resource":
		if _, err := uuid.Parse(card.ResourceID); err != nil {
			return card, "资源卡片必须选择有效的资源"
		}
		card.BlogSlug = ""
	case "blog":
		if !blogSlugPattern.MatchString(card.BlogSlug) {
			return card, "文章卡片必须选择有效的文章 Slug"
		}
		card.ResourceID = ""
	default:
		return card, "未知的卡片类型"
	}
	return card, ""
}

func (a *App) validateHomeTarget(ctx context.Context, targetType, resourceID, blogSlug string) string {
	switch targetType {
	case "resource":
		detail, err := a.store.AdminResource(ctx, resourceID)
		if errors.Is(err, store.ErrAdminResourceNotFound) {
			return "目标资源不存在"
		}
		if err != nil {
			return "目标资源暂时无法读取"
		}
		if detail.Resource.ModerationState != "visible" ||
			detail.Resource.CurrentRevisionID == "" ||
			detail.Resource.CurrentRevisionState != "approved" {
			return "目标资源必须是已发布且可见的资源"
		}
	case "blog":
		post, err := a.store.BlogPost(ctx, blogSlug)
		if errors.Is(err, store.ErrBlogPostNotFound) || (err == nil && !post.Published) {
			return "目标文章不存在或尚未发布"
		}
		if err != nil {
			return "目标文章暂时无法读取"
		}
	}
	return ""
}

func (a *App) handleAdminBannerCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	banner, problem := validBannerForm(r)
	if problem != "" {
		a.renderAdminFormErrorRequest(w, r, "Banner 提交失败", problem, "/admin/home/banners", "/admin/home", http.StatusUnprocessableEntity)
		return
	}
	if problem = a.validateHomeTarget(r.Context(), banner.Type, banner.ResourceID, banner.BlogSlug); problem != "" {
		a.renderAdminFormErrorRequest(w, r, "Banner 提交失败", problem, "/admin/home/banners", "/admin/home", http.StatusUnprocessableEntity)
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
		a.renderAdminFormErrorRequest(w, r, "Banner 保存失败", problem, "/admin/home/banners/"+url.PathEscape(r.PathValue("banner"))+"/save", "/admin/home", http.StatusUnprocessableEntity)
		return
	}
	if problem = a.validateHomeTarget(r.Context(), banner.Type, banner.ResourceID, banner.BlogSlug); problem != "" {
		a.renderAdminFormErrorRequest(w, r, "Banner 保存失败", problem, "/admin/home/banners/"+url.PathEscape(r.PathValue("banner"))+"/save", "/admin/home", http.StatusUnprocessableEntity)
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
	delta, err := strconv.Atoi(strings.TrimSpace(r.FormValue("delta")))
	if err != nil || (delta != 1 && delta != -1) {
		http.Error(w, "invalid direction", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	id := r.PathValue("banner")
	if err := a.store.MoveHomeBanner(r.Context(), id, delta); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.banner.move", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.banner.move", "success", a.clientIP(r), r.UserAgent(), "banner="+id+" delta="+strconv.Itoa(delta))
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
		a.renderAdminFormErrorRequest(w, r, "首页分区提交失败", "分区 ID 必须是小写字母、数字和中划线", "/admin/home/sections", "/admin/home", http.StatusUnprocessableEntity)
		return
	}
	if section.Name == "" {
		a.renderAdminFormErrorRequest(w, r, "首页分区提交失败", "分区名称必填", "/admin/home/sections", "/admin/home", http.StatusUnprocessableEntity)
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
		a.renderAdminFormErrorRequest(w, r, "首页分区保存失败", "分区名称必填", "/admin/home/sections/"+url.PathEscape(section.ID)+"/save", "/admin/home", http.StatusUnprocessableEntity)
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
	delta, err := strconv.Atoi(strings.TrimSpace(r.FormValue("delta")))
	if err != nil || (delta != 1 && delta != -1) {
		http.Error(w, "invalid direction", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	id := r.PathValue("section")
	if err := a.store.MoveHomeSection(r.Context(), id, delta); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.section.move", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.section.move", "success", a.clientIP(r), r.UserAgent(), "section="+id+" delta="+strconv.Itoa(delta))
	http.Redirect(w, r, "/admin/home?action=section", http.StatusFound)
}

func (a *App) handleAdminCardCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	card, problem := validHomeCardForm(r)
	if problem != "" {
		a.renderAdminFormErrorRequest(w, r, "首页卡片提交失败", problem, "/admin/home/cards", "/admin/home", http.StatusUnprocessableEntity)
		return
	}
	if problem = a.validateHomeTarget(r.Context(), card.Type, card.ResourceID, card.BlogSlug); problem != "" {
		a.renderAdminFormErrorRequest(w, r, "首页卡片提交失败", problem, "/admin/home/cards", "/admin/home", http.StatusUnprocessableEntity)
		return
	}
	sectionExists, err := a.store.HomeSectionExists(r.Context(), card.SectionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !sectionExists {
		a.renderAdminFormErrorRequest(w, r, "首页卡片提交失败", "目标分区不存在，请刷新后重试", "/admin/home/cards", "/admin/home", http.StatusUnprocessableEntity)
		return
	}
	card.ID = uuid.NewString()
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
	delta, err := strconv.Atoi(strings.TrimSpace(r.FormValue("delta")))
	if err != nil || (delta != 1 && delta != -1) {
		http.Error(w, "invalid direction", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	id := r.PathValue("card")
	section := r.FormValue("section_id")
	if err := a.store.MoveHomeSectionCard(r.Context(), id, section, delta); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "home.card.move", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "home.card.move", "success", a.clientIP(r), r.UserAgent(), "card="+id+" section="+section+" delta="+strconv.Itoa(delta))
	http.Redirect(w, r, "/admin/home?action=card", http.StatusFound)
}
func (a *App) handleAdminBlogCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if !blogSlugPattern.MatchString(slug) {
		a.renderAdminFormErrorRequest(w, r, "文章创建失败", "Slug 必须是小写字母、数字和中划线", "/admin/blog", "/admin/blog", http.StatusUnprocessableEntity)
		return
	}
	post := store.BlogPost{Slug: slug, Type: strings.TrimSpace(r.FormValue("type")), Title: slug}
	switch post.Type {
	case "", "announcement":
		post.Type = "announcement"
	case "recommendation":
	case "docs":
	default:
		a.renderAdminFormErrorRequest(w, r, "文章创建失败", "未知的文章类型", "/admin/blog", "/admin/blog", http.StatusUnprocessableEntity)
		return
	}
	actor := currentAdmin(r)
	if _, err := a.store.BlogPost(r.Context(), slug); err == nil {
		a.renderAdminFormErrorRequest(w, r, "文章创建失败", "Slug 已存在", "/admin/blog", "/admin/blog", http.StatusConflict)
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
	rawUpdatedAt := strings.TrimSpace(r.FormValue("updated_at"))
	updatedAt, parseErr := time.Parse(time.RFC3339Nano, rawUpdatedAt)
	if parseErr != nil {
		a.renderAdminFormErrorRequest(w, r, "文章保存失败", "编辑页面版本信息缺失或无效，请返回后重新加载", "/admin/blog/"+url.PathEscape(slug), "/admin/blog/"+url.PathEscape(slug), http.StatusConflict)
		return
	}
	post.UpdatedAt = updatedAt
	post.Type = strings.TrimSpace(r.FormValue("type"))
	switch post.Type {
	case "announcement", "recommendation", "docs":
	default:
		a.renderAdminFormErrorRequest(w, r, "文章保存失败", "未知的文章类型", "/admin/blog/"+url.PathEscape(slug), "/admin/blog/"+url.PathEscape(slug), http.StatusUnprocessableEntity)
		return
	}
	post.Title = strings.TrimSpace(r.FormValue("title"))
	post.Subtitle = strings.TrimSpace(r.FormValue("subtitle"))
	post.Author = strings.TrimSpace(r.FormValue("author"))
	post.CoverSHA256 = strings.TrimSpace(r.FormValue("cover_sha256"))
	post.Body = r.FormValue("body")
	if post.Title == "" {
		a.renderAdminFormErrorRequest(w, r, "文章保存失败", "标题必填", "/admin/blog/"+url.PathEscape(slug), "/admin/blog/"+url.PathEscape(slug), http.StatusUnprocessableEntity)
		return
	}
	if post.CoverSHA256 != "" && !sha256Pattern.MatchString(post.CoverSHA256) {
		a.renderAdminFormErrorRequest(w, r, "文章保存失败", "封面必须是合法的 SHA-256", "/admin/blog/"+url.PathEscape(slug), "/admin/blog/"+url.PathEscape(slug), http.StatusUnprocessableEntity)
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
		a.renderAdminFormErrorRequest(w, r, "文章保存失败", "未知的文章操作", "/admin/blog/"+url.PathEscape(slug), "/admin/blog/"+url.PathEscape(slug), http.StatusUnprocessableEntity)
		return
	}
	post.Published = published
	actor := currentAdmin(r)
	if err := a.store.SaveBlogPost(r.Context(), post); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "blog.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		if errors.Is(err, store.ErrBlogPostConflict) {
			a.renderAdminFormErrorRequest(w, r, "文章保存失败", "文章已被删除或被其他管理员更新，请返回后重新加载", "/admin/blog/"+url.PathEscape(slug), "/admin/blog/"+url.PathEscape(slug), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
