package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type Service struct {
	Cfg    config.Config
	Store  *store.Store
	Cipher *auth.Cipher
	States *sync.Map
}

var oauthHTTPClient = &http.Client{Timeout: 15 * time.Second}

func NewService(cfg config.Config, s *store.Store, c *auth.Cipher, states *sync.Map) *Service {
	if states == nil {
		states = &sync.Map{}
	}
	return &Service{
		Cfg:    cfg,
		Store:  s,
		Cipher: c,
		States: states,
	}
}

func (s *Service) FetchBandBBSUserWithToken(ctx context.Context, code, redirect string) (string, string, string, string, string, int, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {s.Cfg.BandBBS.ClientID},
		"client_secret": {s.Cfg.BandBBS.ClientSecret},
		"redirect_uri":  {redirect},
		"code":          {code},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.Cfg.BandBBS.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", "", "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", "", "", "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", "", "", "", "", 0, fmt.Errorf("BandBBS token exchange: HTTP %d", resp.StatusCode)
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return "", "", "", "", "", 0, fmt.Errorf("BandBBS token exchange: invalid JSON response")
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return "", "", "", "", "", 0, fmt.Errorf("BandBBS token exchange: missing access or refresh credential")
	}

	meReq, err := http.NewRequestWithContext(ctx, "GET", s.Cfg.BandBBS.MeURL, nil)
	if err != nil {
		return "", "", "", "", "", 0, err
	}
	meReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	meReq.Header.Set("Accept", "application/json")

	meResp, err := oauthHTTPClient.Do(meReq)
	if err != nil {
		return "", "", "", "", "", 0, err
	}
	defer meResp.Body.Close()
	if meResp.StatusCode < http.StatusOK || meResp.StatusCode >= http.StatusMultipleChoices {
		return "", "", "", "", "", 0, fmt.Errorf("BandBBS profile: HTTP %d", meResp.StatusCode)
	}

	// XenForo profile payload compatibility
	var me struct {
		Me   *bandBBSProfile `json:"me"`
		User *bandBBSProfile `json:"user"`
		bandBBSProfile
	}
	if err := json.NewDecoder(io.LimitReader(meResp.Body, 1<<20)).Decode(&me); err != nil {
		return "", "", "", "", "", 0, fmt.Errorf("BandBBS profile: invalid JSON response (HTTP %d)", meResp.StatusCode)
	}
	profile := me.Me
	if profile == nil {
		profile = me.User
	}
	if profile == nil && (me.bandBBSProfile.ID > 0 || me.bandBBSProfile.AltID > 0) {
		profile = &me.bandBBSProfile
	}
	if profile != nil && profile.ID <= 0 && profile.AltID > 0 {
		profile.ID = profile.AltID
	}
	if profile == nil || profile.ID <= 0 || strings.TrimSpace(profile.Username) == "" {
		return "", "", "", "", "", 0, fmt.Errorf("BandBBS profile: missing identity in me/user envelope (HTTP %d)", meResp.StatusCode)
	}
	avatar := profile.AvatarURL
	if avatar == "" {
		for _, size := range []string{"l", "m", "h", "o", "s"} {
			if candidate := profile.AvatarURLs[size]; candidate != "" {
				avatar = candidate
				break
			}
		}
	}

	return strconv.FormatInt(profile.ID, 10), profile.Username, avatar, tok.AccessToken, tok.RefreshToken, tok.ExpiresIn, nil
}

type bandBBSProfile struct {
	ID         int64             `json:"user_id"`
	AltID      int64             `json:"id"`
	Username   string            `json:"username"`
	AvatarURL  string            `json:"avatar_url"`
	AvatarURLs map[string]string `json:"avatar_urls"`
}

func (s *Service) RevokeBandBBSToken(ctx context.Context, token string) error {
	if token == "" || s.Cfg.BandBBS.RevokeURL == "" {
		return nil
	}
	data := url.Values{
		"client_id":       {s.Cfg.BandBBS.ClientID},
		"client_secret":   {s.Cfg.BandBBS.ClientSecret},
		"token":           {token},
		"token_type_hint": {"access_token"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.Cfg.BandBBS.RevokeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
