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

const DefaultSystemPrompt = `你是一个严格的内容安全审查员，请对用户提交的文本进行安全合规审查
你必须严格输出且仅输出一个合法的 JSON 对象，不要输出任何 Markdown 格式或额外解释：
{"action": "pass" | "flag" | "block", "reason": "审核结论说明"}

判定规则：
- pass: 正常言论、健康讨论、客观批评或赞美
- flag: 轻微争议、疑似擦边、含有潜在外部联系方式或广告嫌疑，需人工复核
- block: 严重违规、政治敏感、违法暴恐、色情低俗、恶意辱骂人身攻击、垃圾引流欺诈广告`

// Review performs the configured first-pass review. The model is required to
// return JSON: {"action":"pass"|"flag"|"block","reason":"..."}. Disabled moderation is
// an explicit pass and never makes a request to an external service.
func Review(ctx context.Context, s *store.Store, text string) (action, reason, model string, err error) {
	var raw json.RawMessage
	if err = s.Pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key = 'ai_moderation'`).Scan(&raw); err != nil {
		return "pass", "", "", nil
	}
	var cfg config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return "error", "", "", fmt.Errorf("invalid ai moderation settings")
	}
	if !cfg.Enabled {
		return "pass", "", cfg.Model, nil
	}
	if strings.TrimSpace(cfg.APIURL) == "" || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "error", "", cfg.Model, fmt.Errorf("ai moderation is enabled but not configured")
	}

	prompt := strings.TrimSpace(cfg.SystemPrompt)
	if prompt == "" {
		prompt = DefaultSystemPrompt
	}

	body, _ := json.Marshal(map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": text},
		},
		"temperature": 0,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return "error", "", cfg.Model, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "error", "", cfg.Model, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "error", "", cfg.Model, fmt.Errorf("moderation upstream returned %s", resp.Status)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "error", "", cfg.Model, fmt.Errorf("invalid moderation response")
	}

	content := strings.TrimSpace(out.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var verdict struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Allow  *bool  `json:"allow,omitempty"`
	}
	if err := json.Unmarshal([]byte(content), &verdict); err != nil {
		return "error", "", cfg.Model, fmt.Errorf("invalid moderation verdict")
	}

	act := strings.ToLower(strings.TrimSpace(verdict.Action))
	if act == "" && verdict.Allow != nil {
		if *verdict.Allow {
			act = "pass"
		} else {
			act = "block"
		}
	}
	if act != "pass" && act != "flag" && act != "block" {
		act = "flag"
	}

	return act, strings.TrimSpace(verdict.Reason), cfg.Model, nil
}
