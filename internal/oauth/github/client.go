package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

type Client struct {
	cfg  config.GitHubConfig
	http *http.Client
}

func New(cfg config.GitHubConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) AuthorizeURL(state, codeChallenge string) string {
	query := url.Values{
		"client_id":             {c.cfg.ClientID},
		"redirect_uri":          {c.cfg.RedirectURI},
		"scope":                 {strings.Join(c.cfg.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	return c.cfg.AuthorizeURL + "?" + query.Encode()
}

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}
type Token struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

func (c *Client) Start(ctx context.Context) (DeviceCode, error) {
	form := url.Values{"client_id": {c.cfg.ClientID}, "scope": {strings.Join(c.cfg.Scopes, " ")}}
	var result DeviceCode
	err := c.form(ctx, c.cfg.DeviceCodeURL, form, &result)
	return result, err
}
func (c *Client) Poll(ctx context.Context, deviceCode string) (Token, error) {
	form := url.Values{"client_id": {c.cfg.ClientID}, "device_code": {deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	var result Token
	err := c.form(ctx, c.cfg.TokenURL, form, &result)
	if err == nil && result.Error != "" {
		return result, fmt.Errorf("%s: %s", result.Error, result.ErrorDescription)
	}
	return result, err
}

func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier string) (Token, error) {
	form := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {c.cfg.RedirectURI},
		"code_verifier": {codeVerifier},
	}
	var result Token
	err := c.form(ctx, c.cfg.TokenURL, form, &result)
	if err == nil && result.Error != "" {
		return result, fmt.Errorf("%s: %s", result.Error, result.ErrorDescription)
	}
	return result, err
}
func (c *Client) User(ctx context.Context, token string) (User, error) {
	var user User
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIURL+"/user", nil)
	if err != nil {
		return user, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		return user, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return user, fmt.Errorf("GitHub user API returned %d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&user)
	return user, err
}
func (c *Client) form(ctx context.Context, endpoint string, form url.Values, destination any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GitHub OAuth returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(destination)
}
