package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleCoinAccount(w http.ResponseWriter, r *http.Request) {
	account, err := a.store.CoinAccount(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("coin_account_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (a *App) handleCoinCheckin(w http.ResponseWriter, r *http.Request) {
	result, err := a.store.CheckinCoins(r.Context(), currentUser(r).ID, time.Now())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("coin_checkin_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleResourceCoinStatus(w http.ResponseWriter, r *http.Request) {
	myCoins, err := a.store.UserResourceCoins(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("coin_status_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"my_coins": myCoins})
}

func (a *App) handleResourceCoin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Coins int `json:"coins"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "coins must be 1 or 2"))
		return
	}
	result, err := a.store.CoinResource(r.Context(), currentUser(r).ID, r.PathValue("id"), request.Coins)
	if err == nil {
		writeJSON(w, http.StatusOK, result)
		return
	}
	status, code := http.StatusConflict, "coin_vote_failed"
	switch {
	case errors.Is(err, store.ErrCoinBalance):
		code = "coin_balance_insufficient"
	case errors.Is(err, store.ErrCoinVoteLimit):
		code = "coin_resource_limit"
	case errors.Is(err, store.ErrCoinVoteOwn):
		code = "coin_own_resource"
	case errors.Is(err, store.ErrCoinVotingFrozen):
		code = "coin_voting_frozen"
	case errors.Is(err, store.ErrCoinAccountYoung):
		status, code = http.StatusForbidden, "coin_account_too_new"
	case errors.Is(err, store.ErrAdminResourceNotFound):
		status, code = http.StatusNotFound, "resource_not_found"
	default:
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, errorBody(code, err.Error()))
}

func (a *App) handleCreatorCoinStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.CreatorCoinStats(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("creator_coin_stats_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *App) handleAdminCoinOverview(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.AdminCoinStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("admin_coin_failed", err.Error()))
		return
	}
	ledger, err := a.store.AdminCoinLedger(r.Context(), r.URL.Query().Get("user"), 200)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("admin_coin_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats, "ledger": ledger})
}

func (a *App) handleAdminCoinUser(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action     string `json:"action"`
		DeltaUnits int64  `json:"delta_units"`
		Reason     string `json:"reason"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	actor := currentAdmin(r)
	userID := r.PathValue("user")
	result, err := a.applyAdminCoinUserAction(r.Context(), userID, request.Action, request.DeltaUnits, request.Reason, actor.UserID)
	if err != nil {
		if err.Error() == "unknown coin action" {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "unknown coin action"))
			return
		}
		_ = a.store.RecordAudit(r.Context(), actor, "coins."+request.Action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("admin_coin_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "coins."+request.Action, "success", a.clientIP(r), r.UserAgent(), "user="+userID+" reason="+request.Reason)
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleAdminCoinInvalidate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ResourceID string `json:"resource_id"`
		UserID     string `json:"user_id"`
		Reason     string `json:"reason"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	actor := currentAdmin(r)
	if err := a.store.AdminInvalidateCoinVote(r.Context(), request.ResourceID, request.UserID, request.Reason, actor.UserID); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "coins.invalidate", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("admin_coin_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "coins.invalidate", "success", a.clientIP(r), r.UserAgent(), "resource="+request.ResourceID+" user="+request.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
