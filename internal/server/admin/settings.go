package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIDevices(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{
		Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("devices_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.DisplayName, "display_name": item.DisplayName, "codename": item.Codename,
			"platform": item.Platform, "vendor": item.Vendor, "astrobox_id": item.AstroBoxID,
			"enabled": item.Enabled, "resource_count": item.ResourceCount, "artifact_count": item.ArtifactCount,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "devices_failed")
}

func (a *Handler) HandleAdminAPITickets(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{
		Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("q"), Kind: r.URL.Query().Get("kind"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("tickets_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "kind": item.Kind, "subject": item.Subject, "status": item.Status,
			"username": item.Username, "user_id": item.UserID, "message": item.Message,
			"target_source": item.TargetSource, "target_id": item.TargetID, "target_url": item.TargetURL,
			"resolution": item.Resolution, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt, "closed_at": item.ClosedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "tickets_failed")
}

func (a *Handler) HandleAdminAPIBlog(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminBlogPosts(r.Context(), store.AdminBlogQuery{
		Category: r.URL.Query().Get("category"), Published: r.URL.Query().Get("published"), Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("blog_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"slug": item.Slug, "type": item.Type, "title": item.Title, "subtitle": item.Subtitle,
			"author": item.Author, "published": item.Published, "published_at": item.PublishedAt,
			"created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "blog_failed")
}

func (a *Handler) HandleAdminAPISettings(w http.ResponseWriter, r *http.Request) {
	var attributes any
	if a.creator != nil {
		attributes, _ = a.creator.Attributes(r.Context(), true)
	}
	moderationPrompt := moderation.DefaultPrompt
	if a.store != nil {
		moderationPrompt, _ = a.store.Setting(r.Context(), "moderation.prompt", moderation.DefaultPrompt)
	}
	ranking := a.RankingWeights(r)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{{
			"bandbbs_client_id": a.cfg.BandBBS.ClientID,
			"github_client_id":  a.cfg.GitHub.ClientID,
			"public_url":        a.cfg.PublicURL,
		}},
		"total": 1, "page": 1, "per_page": 1, "attributes": attributes,
		"moderation_prompt": moderationPrompt,
		"ranking":           ranking,
	})
}

// RankingWeights resolves the effective recommendation multipliers: env
// defaults from config, overlaid by server_settings overrides.
func (a *Handler) RankingWeights(r *http.Request) map[string]float64 {
	ranking := map[string]float64{
		"coin_extra_weight":    a.cfg.Ranking.CoinExtraWeight,
		"download_weight":      a.cfg.Ranking.DownloadWeight,
		"freshness_amplitude":  a.cfg.Ranking.FreshnessAmplitude,
		"freshness_decay_days": a.cfg.Ranking.FreshnessDecayDays,
		"featured_boost":       a.cfg.Ranking.FeaturedBoost,
		"jitter_base":          a.cfg.Ranking.JitterBase,
	}
	if a.creator != nil {
		effective := a.creator.RankingSettings(r.Context())
		ranking["coin_extra_weight"] = effective.CoinExtraWeight
		ranking["download_weight"] = effective.DownloadWeight
		ranking["freshness_amplitude"] = effective.FreshnessAmplitude
		ranking["freshness_decay_days"] = effective.FreshnessDecayDays
		ranking["featured_boost"] = effective.FeaturedBoost
		ranking["jitter_base"] = effective.JitterBase
	}
	return ranking
}

func (a *Handler) HandleAdminAPITicketAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
		Reply  string `json:"reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if strings.TrimSpace(body.Status) == "replied" && strings.TrimSpace(body.Reply) == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "回复不能为空"))
		return
	}
	actor := a.actor(r)
	if _, err := a.store.UpdateFeedback(r.Context(), r.PathValue("ticket"), store.FeedbackUpdate{
		Status: body.Status, Reply: body.Reply, AuthorID: actor.UserID,
	}); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("ticket_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIAnalytics(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	if rangeName != "90d" && rangeName != "12m" {
		rangeName = "30d"
	}
	data, err := a.store.AdminAnalytics(r.Context(), rangeName)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("analytics_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, data)
}

func (a *Handler) HandleAdminAPIOverview(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.AdminOverview(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("overview_failed", "overview temporarily unavailable"))
		return
	}
	httpx.WriteJSON(w, 200, data)
}

func (a *Handler) HandleAdminAPICatalog(w http.ResponseWriter, r *http.Request) {
	attributes, _ := a.creator.Attributes(r.Context(), true)
	devices, _ := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Page: 1, PerPage: 200})
	reviewers, _ := a.store.AdminReviewers(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"attributes": attributes, "devices": devices.Items, "reviewers": reviewers, "checklist": defaultReviewChecklist})
}

func (a *Handler) HandleAdminAPIDeviceSaveJSON(w http.ResponseWriter, r *http.Request) {
	var input store.AdminDeviceInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if id := r.PathValue("device"); id != "" && id != "new" {
		input.ID = id
	}
	item, err := a.store.AdminSaveDevice(r.Context(), input)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("device_failed", err.Error()))
		return
	}
	a.audit(r, "device.save", "success", "device="+item.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (a *Handler) HandleAdminAPIRankingSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CoinExtraWeight    *float64 `json:"coin_extra_weight"`
		DownloadWeight     *float64 `json:"download_weight"`
		FreshnessAmplitude *float64 `json:"freshness_amplitude"`
		FreshnessDecayDays *float64 `json:"freshness_decay_days"`
		FeaturedBoost      *float64 `json:"featured_boost"`
		JitterBase         *float64 `json:"jitter_base"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	weights := store.RankingWeights{
		CoinExtraWeight:    valueOr(body.CoinExtraWeight, a.cfg.Ranking.CoinExtraWeight),
		DownloadWeight:     valueOr(body.DownloadWeight, a.cfg.Ranking.DownloadWeight),
		FreshnessAmplitude: valueOr(body.FreshnessAmplitude, a.cfg.Ranking.FreshnessAmplitude),
		FreshnessDecayDays: valueOr(body.FreshnessDecayDays, a.cfg.Ranking.FreshnessDecayDays),
		FeaturedBoost:      valueOr(body.FeaturedBoost, a.cfg.Ranking.FeaturedBoost),
		JitterBase:         valueOr(body.JitterBase, a.cfg.Ranking.JitterBase),
	}
	if err := a.store.SaveRankingWeights(r.Context(), weights); err != nil {
		a.audit(r, "ranking.save", "failure", err.Error())
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("ranking_failed", err.Error()))
		return
	}
	a.audit(r, "ranking.save", "success", "")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ranking": a.RankingWeights(r)})
}

func valueOr(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func (a *Handler) HandleAdminAPIAttribute(w http.ResponseWriter, r *http.Request) {
	var item creator.ResourceAttribute
	if err := httpx.ReadJSON(r, &item); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.creator.UpsertAttribute(r.Context(), item); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("attribute_failed", err.Error()))
		return
	}
	a.audit(r, "resource_attribute.save", "success", item.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIAttributeDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.DisableAttribute(r.Context(), r.PathValue("attribute")); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("attribute_failed", err.Error()))
		return
	}
	a.audit(r, "resource_attribute.disable", "success", r.PathValue("attribute"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPITicketDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminFeedbackDetail(r.Context(), r.PathValue("ticket"))
	if err != nil {
		if errors.Is(err, store.ErrFeedbackNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "ticket was not found"))
		} else {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("ticket_failed", err.Error()))
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}
