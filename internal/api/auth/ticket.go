package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
)

type TicketExchangeRequest struct {
	Ticket string `json:"ticket"`
}

type TicketExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserUID      int    `json:"user_uid"`
	ExpiresIn    int    `json:"expires_in"`
}

// HandleTicketExchange implements POST /api/v1/auth/ticket/exchange (Spec §5.3)
func (s *Service) HandleTicketExchange(w http.ResponseWriter, r *http.Request) {
	var req TicketExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Ticket == "" {
		httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少 ticket 字段")
		return
	}

	ticketHash := sha256.Sum256([]byte(req.Ticket))

	var userUID int
	var expiresAt time.Time
	err := s.Store.Pool.QueryRow(r.Context(), `
		SELECT user_uid, expires_at FROM login_tickets 
		WHERE ticket_hash = $1 AND used_at IS NULL
	`, ticketHash[:]).Scan(&userUID, &expiresAt)

	if err == pgx.ErrNoRows || time.Now().After(expiresAt) {
		httputil.Error(w, http.StatusUnauthorized, "invalid_ticket", "票据无效或已过期")
		return
	} else if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Mark consumed
	_, _ = s.Store.Pool.Exec(r.Context(), `UPDATE login_tickets SET used_at = now() WHERE ticket_hash = $1`, ticketHash[:])

	// Create session
	tokenBytes := make([]byte, 32)
	_, _ = rand.Read(tokenBytes)
	accessToken := hex.EncodeToString(tokenBytes)

	refreshBytes := make([]byte, 32)
	_, _ = rand.Read(refreshBytes)
	refreshToken := hex.EncodeToString(refreshBytes)

	tokenHash := sha256.Sum256([]byte(accessToken))
	refreshHash := sha256.Sum256([]byte(refreshToken))

	_, err = s.Store.Pool.Exec(r.Context(), `
		INSERT INTO sessions (user_uid, access_hash, refresh_hash, access_expires_at, refresh_expires_at, platform)
		VALUES ($1, $2, $3, now() + interval '30 days', now() + interval '60 days', 'app')
	`, userUID, tokenHash[:], refreshHash[:])
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	httputil.JSON(w, http.StatusOK, TicketExchangeResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		UserUID:      userUID,
		ExpiresIn:    2592000,
	})
}

type SessionRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// HandleSessionRefresh implements POST /api/v1/auth/session/refresh (Spec §5.4)
func (s *Service) HandleSessionRefresh(w http.ResponseWriter, r *http.Request) {
	var req SessionRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少 refresh_token 字段")
		return
	}

	refreshHash := sha256.Sum256([]byte(req.RefreshToken))

	var sessionID int64
	var userUID int
	err := s.Store.Pool.QueryRow(r.Context(), `
		SELECT id, user_uid FROM sessions 
		WHERE refresh_hash = $1 AND refresh_expires_at > now() AND revoked_at IS NULL
	`, refreshHash[:]).Scan(&sessionID, &userUID)

	if err == pgx.ErrNoRows {
		httputil.Error(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌无效或已过期")
		return
	} else if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	tokenBytes := make([]byte, 32)
	_, _ = rand.Read(tokenBytes)
	newAccessToken := hex.EncodeToString(tokenBytes)
	newTokenHash := sha256.Sum256([]byte(newAccessToken))

	// Bug 2.2: sessions has last_seen_at, NOT updated_at
	_, err = s.Store.Pool.Exec(r.Context(), `
		UPDATE sessions 
		SET access_hash = $1, access_expires_at = now() + interval '30 days', last_seen_at = now()
		WHERE id = $2
	`, newTokenHash[:], sessionID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"access_token": newAccessToken,
		"expires_in":   2592000,
	})
}
