package bandbbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

var ErrReauthorization = errors.New("BandBBS authorization required")

func HasWriteScopes(scopes string) bool {
	for _, scope := range config.ParseScopes(scopes) {
		if strings.HasSuffix(scope, ":write") {
			return true
		}
	}
	return false
}

type pendingToken struct {
	Token      model.TokenPayload
	ReceivedAt time.Time
}

// GrantToken serializes refresh and authorization replacement across HTTP,
// publishing workers and server processes. rejectedAccess is the AT that got
// a 401; a newer stored AT can be reused without rotating its RT again.
func (c *Client) GrantToken(ctx context.Context, st *store.Store, secrets *auth.Secrets, userID, provider, rejectedAccess string) (model.TokenPayload, string, error) {
	var result model.TokenPayload
	var subject string
	err := st.WithOAuthGrantLock(ctx, userID, provider, func(locked *store.Store) error {
		grant, err := locked.OAuthGrant(ctx, userID, provider)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReauthorization
		}
		if err != nil {
			return err
		}
		if grant.Invalid {
			return ErrReauthorization
		}
		user, err := locked.UserByID(ctx, userID)
		if err != nil {
			return err
		}
		if user.BannedAt != nil {
			return ErrReauthorization
		}
		subject = strconv.FormatInt(user.BandBBSUserID, 10)
		required := c.cfg.Scopes
		if provider == "bandbbs_publish" {
			required = append(append([]string{}, required...), c.cfg.PublishScopes...)
		}
		invalidate := func() error {
			if err := locked.InvalidateOAuthGrant(ctx, userID, provider); err != nil {
				return err
			}
			return ErrReauthorization
		}
		if grant.Subject != "" && grant.Subject != subject {
			return invalidate()
		}
		if !config.HasScopes(config.ScopeString(grant.Scopes), required) || provider == "bandbbs" && HasWriteScopes(config.ScopeString(grant.Scopes)) {
			return invalidate()
		}
		access, err := secrets.Decrypt(grant.AccessTokenCipher)
		if err != nil {
			return err
		}
		if len(grant.PendingCipher) == 0 && (grant.ExpiresAt == nil && provider == "bandbbs_publish" || grant.ExpiresAt != nil && grant.ExpiresAt.After(time.Now().Add(2*time.Minute))) && (rejectedAccess == "" || rejectedAccess != access) {
			var remaining int64
			if grant.ExpiresAt != nil {
				remaining = int64(time.Until(*grant.ExpiresAt).Seconds())
			}
			result = model.TokenPayload{AccessToken: access, TokenType: grant.TokenType, Scope: config.ScopeString(grant.Scopes), ExpiresIn: remaining}
			return nil
		}
		rotate := func(refresh string) (pendingToken, error) {
			if refresh == "" {
				return pendingToken{}, invalidate()
			}
			next, err := c.Refresh(ctx, refresh)
			if err != nil {
				var rejected *TokenError
				if errors.As(err, &rejected) && rejected.Code == "invalid_grant" {
					return pendingToken{}, invalidate()
				}
				return pendingToken{}, err
			}
			if next.RefreshToken == "" {
				next.RefreshToken = refresh
			}
			value := pendingToken{Token: next, ReceivedAt: time.Now().UTC()}
			raw, err := json.Marshal(value)
			if err != nil {
				return value, err
			}
			cipher, err := secrets.Encrypt(string(raw))
			if err != nil {
				return value, err
			}
			// Keep the rotated RT even if the requesting client disconnects.
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = locked.SavePendingOAuthToken(persistCtx, userID, provider, cipher)
			return value, err
		}
		var pending pendingToken
		if len(grant.PendingCipher) > 0 {
			raw, err := secrets.Decrypt(grant.PendingCipher)
			if err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(raw), &pending); err != nil {
				return err
			}
			if pending.Token.ExpiresIn > 0 && !pending.ReceivedAt.Add(time.Duration(pending.Token.ExpiresIn)*time.Second).After(time.Now()) {
				pending, err = rotate(pending.Token.RefreshToken)
				if err != nil {
					return err
				}
			}
		} else {
			refresh, err := secrets.Decrypt(grant.RefreshTokenCipher)
			if err != nil {
				return err
			}
			pending, err = rotate(refresh)
			if err != nil {
				return err
			}
		}
		next := pending.Token
		actualSubject, scopes, err := c.ValidateScopes(ctx, next, required)
		if errors.Is(err, ErrAccessRejected) {
			pending, err = rotate(pending.Token.RefreshToken)
			if err != nil {
				return err
			}
			next = pending.Token
			actualSubject, scopes, err = c.ValidateScopes(ctx, next, required)
		}
		if err != nil {
			return err
		}
		if actualSubject != "" && actualSubject != subject || provider == "bandbbs" && HasWriteScopes(scopes) {
			return invalidate()
		}
		next.Scope = scopes
		accessCipher, err := secrets.Encrypt(next.AccessToken)
		if err != nil {
			return err
		}
		refreshCipher, err := secrets.Encrypt(next.RefreshToken)
		if err != nil {
			return err
		}
		var expiry *time.Time
		if next.ExpiresIn > 0 {
			value := pending.ReceivedAt.Add(time.Duration(next.ExpiresIn) * time.Second)
			expiry = &value
		}
		if err = locked.UpsertOAuthGrant(ctx, store.GrantParams{UserID: userID, Provider: provider, Subject: subject, Scopes: config.ParseScopes(scopes), AccessTokenCipher: accessCipher, RefreshTokenCipher: refreshCipher, TokenType: next.TokenType, ExpiresAt: expiry}); err != nil {
			return err
		}
		if expiry != nil {
			next.ExpiresIn = int64(time.Until(*expiry).Seconds())
			if next.ExpiresIn <= 0 {
				return fmt.Errorf("refreshed BandBBS access token has expired; retry refresh")
			}
		}
		next.RefreshToken = ""
		result = next
		return nil
	})
	return result, subject, err
}
