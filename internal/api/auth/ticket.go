package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
)

type TicketExchangeRequest struct {
	Ticket string `json:"ticket"`
}

type TicketExchangeResponse struct {
	AccessToken  string                `json:"access_token"`
	RefreshToken string                `json:"refresh_token"`
	UserUID      int                   `json:"user_uid"`
	ExpiresIn    int                   `json:"expires_in"`
	User         TicketUser            `json:"user"`
	BandBBS      *BandBBSTokenResponse `json:"bandbbs,omitempty"`
}

type BandBBSTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

type TicketUser struct {
	BandBBSUID int    `json:"bandbbs_uid"`
	Username   string `json:"username"`
	AvatarURL  string `json:"avatar_url"`
	Role       string `json:"role"`
}

// HandleTicketExchange implements POST /api/v1/auth/ticket/exchange (Spec §5.3)
func (s *Service) HandleTicketExchange(w http.ResponseWriter, r *http.Request) {
	var req TicketExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Ticket == "" {
		httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少 ticket 字段")
		return
	}

	ticketHash := sha256.Sum256([]byte(req.Ticket))
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "token_generation_failed", "无法生成安全令牌")
		return
	}
	accessToken := hex.EncodeToString(tokenBytes)
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "token_generation_failed", "无法生成安全令牌")
		return
	}
	refreshToken := hex.EncodeToString(refreshBytes)
	tokenHash := sha256.Sum256([]byte(accessToken))
	refreshHash := sha256.Sum256([]byte(refreshToken))

	var userUID int
	var ticketUser TicketUser
	// Consume the ticket and create the session in one transaction so a failed
	// session insert does not permanently burn a valid login ticket.
	err := s.Store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			UPDATE login_tickets
			SET used_at = now()
			WHERE ticket_hash = $1 AND used_at IS NULL AND expires_at > now()
			RETURNING user_uid
		`, ticketHash[:]).Scan(&userUID); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			SELECT bandbbs_uid, username, COALESCE(avatar_url, ''), role
			FROM users WHERE bandbbs_uid = $1
		`, userUID).Scan(&ticketUser.BandBBSUID, &ticketUser.Username, &ticketUser.AvatarURL, &ticketUser.Role); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO sessions (user_uid, access_hash, refresh_hash, access_expires_at, refresh_expires_at, platform)
			VALUES ($1, $2, $3, $4, $5, 'app')
		`, userUID, tokenHash[:], refreshHash[:], time.Now().Add(accessTTL(s.Cfg)), time.Now().Add(refreshTTL(s.Cfg)))
		return err
	})

	if err == pgx.ErrNoRows {
		httputil.Error(w, http.StatusUnauthorized, "invalid_ticket", "票据无效或已过期")
		return
	} else if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	var bandbbs *BandBBSTokenResponse
	if s.Cipher != nil {
		var encrypted []byte
		var expiresAt time.Time
		var scopes []string
		if scanErr := s.Store.Pool.QueryRow(r.Context(), `SELECT access_token_cipher, COALESCE(expires_at, now() + interval '1 hour'), scopes FROM oauth_grants WHERE user_uid = $1 AND provider = 'bandbbs'`, userUID).Scan(&encrypted, &expiresAt, &scopes); scanErr == nil {
			if raw, decryptErr := s.Cipher.DecryptString(string(encrypted)); decryptErr == nil && raw != "" {
				seconds := int(time.Until(expiresAt).Seconds())
				if seconds < 1 {
					seconds = 1
				}
				bandbbs = &BandBBSTokenResponse{AccessToken: raw, TokenType: "bearer", ExpiresIn: seconds, Scope: strings.Join(scopes, " ")}
			}
		}
	}
	httputil.JSON(w, http.StatusOK, TicketExchangeResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		UserUID:      userUID,
		ExpiresIn:    int(accessTTL(s.Cfg).Seconds()),
		User:         ticketUser,
		BandBBS:      bandbbs,
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

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "token_generation_failed", "无法生成安全令牌")
		return
	}
	newAccessToken := hex.EncodeToString(tokenBytes)
	newTokenHash := sha256.Sum256([]byte(newAccessToken))

	var userUID int
	err := s.Store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var sessionID int64
		if err := tx.QueryRow(r.Context(), `
			SELECT id, user_uid FROM sessions
			WHERE refresh_hash = $1 AND refresh_expires_at > now() AND revoked_at IS NULL
			FOR UPDATE
		`, refreshHash[:]).Scan(&sessionID, &userUID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			UPDATE sessions SET access_hash = $1, access_expires_at = $3, last_seen_at = now()
			WHERE id = $2
		`, newTokenHash[:], sessionID, time.Now().Add(accessTTL(s.Cfg)))
		return err
	})
	if err == pgx.ErrNoRows {
		httputil.Error(w, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌无效或已过期")
		return
	}
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"access_token": newAccessToken,
		"expires_in":   int(accessTTL(s.Cfg).Seconds()),
	})
}

func accessTTL(cfg config.Config) time.Duration {
	if cfg.AccessTokenTTL > 0 {
		return cfg.AccessTokenTTL
	}
	return 15 * time.Minute
}

func refreshTTL(cfg config.Config) time.Duration {
	if cfg.RefreshTokenTTL > 0 {
		return cfg.RefreshTokenTTL
	}
	return 30 * 24 * time.Hour
}
