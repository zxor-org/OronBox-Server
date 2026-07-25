package bandbbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/model"
)

type Client struct {
	cfg    config.BandBBSConfig
	client *http.Client
}

type IntrospectResponse struct {
	Active    bool   `json:"active"`
	Scope     string `json:"scope"`
	ClientID  string `json:"client_id"`
	Username  string `json:"username"`
	TokenType string `json:"token_type"`
	Exp       int64  `json:"exp"`
	Iat       int64  `json:"iat"`
	Sub       string `json:"sub"`
	Iss       string `json:"iss"`
}

type TokenInfoResponse struct {
	UserID    int64           `json:"user_id"`
	Scope     map[string]bool `json:"scope"`
	ExpiresIn int64           `json:"expires_in"`
	IssueDate int64           `json:"issue_date"`
}

type User struct {
	UserID     int64             `json:"user_id"`
	Username   string            `json:"username"`
	AvatarURLs map[string]string `json:"avatar_urls"`
}

func (c *Client) Me(ctx context.Context, accessToken string) (User, error) {
	var envelope struct {
		Me User `json:"me"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.MeURL, nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.client.Do(req)
	if err != nil {
		return User{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return User{}, fmt.Errorf("BandBBS me endpoint returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return User{}, err
	}
	if envelope.Me.UserID == 0 || envelope.Me.Username == "" {
		return User{}, errors.New("BandBBS me response is missing identity")
	}
	return envelope.Me, nil
}

func NewClient(cfg config.BandBBSConfig, timeout time.Duration) *Client {
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) AuthorizeURL(state string, scopes []string) string {
	values := url.Values{}
	values.Set("type", "authorization_code")
	values.Set("client_id", c.cfg.ClientID)
	values.Set("redirect_uri", c.cfg.RedirectURI)
	values.Set("response_type", "code")
	values.Set("scope", config.ScopeString(scopes))
	values.Set("state", state)
	return c.cfg.AuthorizeURL + "?" + values.Encode()
}

func (c *Client) ExchangeCode(ctx context.Context, code string) (model.TokenPayload, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", c.cfg.RedirectURI)
	return c.postToken(ctx, form)
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (model.TokenPayload, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("redirect_uri", c.cfg.RedirectURI)
	return c.postToken(ctx, form)
}

func (c *Client) postToken(ctx context.Context, form url.Values) (model.TokenPayload, error) {
	var token model.TokenPayload
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return token, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return token, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return token, upstreamError("token", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return token, err
	}
	if token.AccessToken == "" {
		return token, errors.New("missing access_token")
	}
	return token, nil
}

func upstreamError(endpoint string, status int, payload []byte) error {
	var body map[string]any
	if json.Unmarshal(payload, &body) == nil {
		code, _ := body["error"].(string)
		description, _ := body["error_description"].(string)
		if description == "" {
			description, _ = body["message"].(string)
		}
		if code != "" || description != "" {
			return fmt.Errorf("BandBBS %s endpoint returned %d (%s: %s)", endpoint, status, code, description)
		}
	}
	return fmt.Errorf("BandBBS %s endpoint returned %d", endpoint, status)
}

func (c *Client) ValidateScopes(ctx context.Context, token model.TokenPayload, required []string) (string, string, error) {
	expected := config.ScopeString(required)
	if token.Scope != "" && !config.HasScopes(token.Scope, required) {
		return "", token.Scope, fmt.Errorf("token response scope mismatch: expected %q got %q", expected, token.Scope)
	}

	introspect, err := c.Introspect(ctx, token.AccessToken)
	if err == nil {
		actual := introspect.Scope
		if actual == "" {
			actual = token.Scope
		}
		if !introspect.Active {
			return introspect.Sub, actual, errors.New("token is not active")
		}
		if introspect.ClientID != "" && introspect.ClientID != c.cfg.ClientID {
			return introspect.Sub, actual, fmt.Errorf("token client_id mismatch: %s", introspect.ClientID)
		}
		if introspect.TokenType != "" && strings.ToLower(introspect.TokenType) != "bearer" {
			return introspect.Sub, actual, fmt.Errorf("unexpected token type: %s", introspect.TokenType)
		}
		if !config.HasScopes(actual, required) {
			return introspect.Sub, actual, fmt.Errorf("scope mismatch: expected %q got %q", expected, actual)
		}
		return introspect.Sub, actual, nil
	}

	info, err := c.TokenInfo(ctx, token.AccessToken)
	if err != nil {
		return "", token.Scope, err
	}
	actual := scopesFromMap(info.Scope)
	if !config.HasScopes(actual, required) {
		return fmt.Sprint(info.UserID), actual, fmt.Errorf("scope mismatch: expected %q got %q", expected, actual)
	}
	return fmt.Sprint(info.UserID), actual, nil
}

func (c *Client) Introspect(ctx context.Context, accessToken string) (IntrospectResponse, error) {
	var out IntrospectResponse
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("token", accessToken)
	form.Set("token_type_hint", "access_token")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.IntrospectURL, strings.NewReader(form.Encode()))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("introspect endpoint returned %d", resp.StatusCode)
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) TokenInfo(ctx context.Context, accessToken string) (TokenInfoResponse, error) {
	var out TokenInfoResponse
	u, _ := url.Parse(c.cfg.TokenURL)
	q := u.Query()
	q.Set("token", accessToken)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return out, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("token info endpoint returned %d", resp.StatusCode)
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) Revoke(ctx context.Context, token string, hint string) error {
	if token == "" {
		return nil
	}
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("token", token)
	form.Set("token_type_hint", hint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("revoke endpoint returned %d", resp.StatusCode)
	}
	return nil
}

func scopesFromMap(values map[string]bool) string {
	var scopes []string
	for key, enabled := range values {
		if enabled {
			scopes = append(scopes, key)
		}
	}
	return config.ScopeString(scopes)
}
