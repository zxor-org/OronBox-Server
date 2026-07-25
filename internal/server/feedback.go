package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleCreateFeedback(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Kind         string `json:"kind"`
		Subject      string `json:"subject"`
		Message      string `json:"message"`
		TargetSource string `json:"target_source"`
		TargetID     string `json:"target_id"`
		TargetURL    string `json:"target_url"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	request.Kind = strings.TrimSpace(request.Kind)
	request.Subject = strings.TrimSpace(request.Subject)
	request.Message = strings.TrimSpace(request.Message)
	request.TargetSource = strings.TrimSpace(request.TargetSource)
	request.TargetID = strings.TrimSpace(request.TargetID)
	request.TargetURL = strings.TrimSpace(request.TargetURL)
	if request.Kind != "feedback" && request.Kind != "report" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_kind", "kind must be feedback or report"))
		return
	}
	if request.Subject == "" || request.Message == "" || len([]rune(request.Subject)) > 120 || len([]rune(request.Message)) > 10000 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "subject or message is empty or too long"))
		return
	}
	if len(request.TargetSource) > 80 || len(request.TargetID) > 512 || len(request.TargetURL) > 2048 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_target", "resource report target is too long"))
		return
	}
	if request.TargetURL != "" {
		targetURL, err := url.ParseRequestURI(request.TargetURL)
		if err != nil || (targetURL.Scheme != "http" && targetURL.Scheme != "https") || targetURL.Host == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid_target", "resource report URL must use HTTP or HTTPS"))
			return
		}
	}
	if request.Kind == "report" && request.TargetID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_target", "resource report requires a target"))
		return
	}
	if request.Kind == "report" {
		exists, err := a.store.FeedbackTargetExists(r.Context(), request.TargetSource, request.TargetID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("target_lookup_failed", err.Error()))
			return
		}
		if !exists {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid_target", "resource report target was not found"))
			return
		}
	}
	ticket, err := a.store.CreateFeedback(r.Context(), store.CreateFeedbackParams{UserID: currentUser(r).ID, Kind: request.Kind, Subject: request.Subject, Message: request.Message, TargetSource: request.TargetSource, TargetID: request.TargetID, TargetURL: request.TargetURL})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("feedback_create_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, ticket)
}

func (a *App) handleFeedbackList(w http.ResponseWriter, r *http.Request) {
	tickets, err := a.store.FeedbackList(r.Context(), currentUser(r).ID, false, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("feedback_list_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": tickets})
}

func (a *App) handleFeedback(w http.ResponseWriter, r *http.Request) {
	ticket, err := a.store.Feedback(r.Context(), r.PathValue("ticket"), currentUser(r).ID, false)
	if errors.Is(err, store.ErrFeedbackNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feedback_not_found", "feedback ticket was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("feedback_read_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (a *App) handleFeedbackReply(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" || len([]rune(request.Message)) > 10000 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_reply", "reply is empty or too long"))
		return
	}
	user := currentUser(r)
	ticket, err := a.store.Feedback(r.Context(), r.PathValue("ticket"), user.ID, false)
	if errors.Is(err, store.ErrFeedbackNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feedback_not_found", "feedback ticket was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("feedback_read_failed", err.Error()))
		return
	}
	if ticket.Status == "dismissed" {
		writeJSON(w, http.StatusConflict, errorBody("feedback_closed", "dismissed feedback cannot be reopened"))
		return
	}
	updated, err := a.store.UpdateFeedback(r.Context(), ticket.ID, store.FeedbackUpdate{
		Status:   "open",
		Reply:    request.Message,
		AuthorID: user.ID,
	})
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("feedback_reply_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
