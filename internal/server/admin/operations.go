package admin

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIMessages(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminMessages(r.Context(), store.AdminMessageQuery{
		Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("messages_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "user_id": item.UserID, "username": item.Username, "kind": item.Kind,
			"title": item.Title, "body": item.Body, "ref": item.Ref, "read_at": item.ReadAt,
			"created_at": item.CreatedAt, "expires_at": item.ExpiresAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "messages_failed")
}

func (a *Handler) HandleAdminAPIAnnouncements(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminAnnouncementsPage(r.Context(), store.AdminAnnouncementQuery{
		Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("announcements_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "title": item.Title, "body": item.Body, "published_at": item.PublishedAt,
			"created_by": item.CreatedBy, "creator": item.Creator,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "announcements_failed")
}

func (a *Handler) HandleAdminAPIHome(w http.ResponseWriter, r *http.Request) {
	banners, err := a.store.ListHomeBanners(r.Context(), false)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	sections, err := a.store.ListHomeSections(r.Context(), false)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	out := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		cards, _ := a.store.ListHomeSectionCards(r.Context(), section.ID)
		out = append(out, map[string]any{"section": section, "cards": cards})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"banners": banners, "sections": out})
}

func (a *Handler) HandleAdminAPIHomeBanner(w http.ResponseWriter, r *http.Request) {
	var banner store.HomeBanner
	if err := httpx.ReadJSON(r, &banner); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	id := r.PathValue("banner")
	if id == "" {
		banner.ID = uuid.NewString()
		if err := a.store.CreateHomeBanner(r.Context(), banner); err != nil {
			a.audit(r, "home.banner.create", "failure", err.Error())
			httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
			return
		}
		a.audit(r, "home.banner.create", "success", "banner="+banner.ID)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": banner.ID})
		return
	}
	banner.ID = id
	if err := a.store.UpdateHomeBanner(r.Context(), banner); err != nil {
		a.audit(r, "home.banner.save", "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	a.audit(r, "home.banner.save", "success", "banner="+id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIHomeBannerDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteHomeBanner(r.Context(), r.PathValue("banner")); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	a.audit(r, "home.banner.delete", "success", "banner="+r.PathValue("banner"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIHomeMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Delta int `json:"delta"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil || (body.Delta != 1 && body.Delta != -1) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "delta must be 1 or -1"))
		return
	}
	kind := r.PathValue("kind")
	id := r.PathValue("id")
	var err error
	switch kind {
	case "banners":
		err = a.store.MoveHomeBanner(r.Context(), id, body.Delta)
	case "sections":
		err = a.store.MoveHomeSection(r.Context(), id, body.Delta)
	case "cards":
		err = a.store.MoveHomeSectionCard(r.Context(), id, r.URL.Query().Get("section_id"), body.Delta)
	default:
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "unknown home item"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIHomeSection(w http.ResponseWriter, r *http.Request) {
	var section store.HomeSection
	if err := httpx.ReadJSON(r, &section); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.store.CreateHomeSection(r.Context(), section); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	a.audit(r, "home.section.create", "success", "section="+section.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIHomeSectionDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteHomeSection(r.Context(), r.PathValue("section")); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIHomeCard(w http.ResponseWriter, r *http.Request) {
	var card store.HomeSectionCard
	if err := httpx.ReadJSON(r, &card); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	card.ID = uuid.NewString()
	if err := a.store.CreateHomeSectionCard(r.Context(), card); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": card.ID})
}

func (a *Handler) HandleAdminAPIHomeCardDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteHomeSectionCard(r.Context(), r.PathValue("card")); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("home_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIMessageSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserIDs []string `json:"user_ids"`
		Title   string   `json:"title"`
		Body    string   `json:"body"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	count, err := a.store.CreateAdminMessages(r.Context(), body.UserIDs, body.Title, body.Body)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("message_failed", err.Error()))
		return
	}
	a.audit(r, "message.send", "success", "count="+strconv.FormatInt(count, 10))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": count})
}

func (a *Handler) HandleAdminAPIAnnouncementCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.store.CreateAnnouncement(r.Context(), a.actor(r).UserID, body.Title, body.Body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("announcement_failed", err.Error()))
		return
	}
	a.audit(r, "announcement.publish", "success", body.Title)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIAnnouncementDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteAnnouncement(r.Context(), r.PathValue("announcement")); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("announcement_failed", err.Error()))
		return
	}
	a.audit(r, "announcement.delete", "success", r.PathValue("announcement"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
