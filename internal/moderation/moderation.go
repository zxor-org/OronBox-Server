package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

type config struct {
	Enabled      bool   `json:"enabled"`
	APIURL       string `json:"api_url"`
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
}

// Review performs the configured first-pass review. The model is required to
// return JSON: {"allow":true|false,"reason":"..."}. Disabled moderation is
// an explicit pass and never makes a request to an external service.
func Review(ctx context.Context, s *store.Store, text string) (allow bool, action, reason, model string, err error) {
	var raw json.RawMessage
	if err = s.Pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key = 'ai_moderation'`).Scan(&raw); err != nil {
		// Missing settings are equivalent to the documented disabled default.
		return true, "pass", "", "", nil
	}
	var cfg config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return false, "error", "", "", fmt.Errorf("invalid ai moderation settings")
	}
	if !cfg.Enabled {
		return true, "pass", "", cfg.Model, nil
	}
	if strings.TrimSpace(cfg.APIURL) == "" || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return false, "error", "", cfg.Model, fmt.Errorf("ai moderation is enabled but not configured")
	}
	prompt := cfg.SystemPrompt + `
仅输出 JSON，不要输出 Markdown：{"allow":true,"reason":"简短原因"}`
	body, _ := json.Marshal(map[string]any{"model": cfg.Model, "messages": []map[string]string{{"role": "system", "content": prompt}, {"role": "user", "content": text}}, "temperature": 0})
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return false, "error", "", cfg.Model, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return false, "error", "", cfg.Model, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, "error", "", cfg.Model, fmt.Errorf("moderation upstream returned %s", resp.Status)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || len(out.Choices) == 0 {
		return false, "error", "", cfg.Model, fmt.Errorf("invalid moderation response")
	}
	var verdict struct {
		Allow  bool   `json:"allow"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.Choices[0].Message.Content)), &verdict); err != nil {
		return false, "error", "", cfg.Model, fmt.Errorf("invalid moderation verdict")
	}
	if !verdict.Allow {
		return false, "reject", strings.TrimSpace(verdict.Reason), cfg.Model, nil
	}
	return true, "pass", strings.TrimSpace(verdict.Reason), cfg.Model, nil
}
