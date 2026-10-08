package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type ModerationResult struct {
	Action     string   `json:"action"` // pass, flag, block
	Reason     string   `json:"reason"`
	Model      string   `json:"model"`
	Categories []string `json:"categories"`
}

var moderationHTTPClient = &http.Client{
	Timeout: 4 * time.Second,
}

// moderateText performs AI text moderation via DeepSeek or OpenAI-compatible API
// If no API key is configured or the request fails, it falls back to built-in rules
func (a *application) moderateText(ctx context.Context, text string) ModerationResult {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ModerationResult{Action: "pass", Reason: "空内容", Model: "built-in"}
	}

	apiKey := ""
	apiURL := ""
	model := ""
	customPrompt := ""
	enabled := true

	// 1. Try reading from server_settings database table
	var rawSettings []byte
	if err := a.db.Pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key='ai_moderation'`).Scan(&rawSettings); err == nil && len(rawSettings) > 0 {
		var cfg struct {
			Enabled      *bool  `json:"enabled"`
			APIKey       string `json:"api_key"`
			APIURL       string `json:"api_url"`
			Model        string `json:"model"`
			SystemPrompt string `json:"system_prompt"`
		}
		if json.Unmarshal(rawSettings, &cfg) == nil {
			if cfg.Enabled != nil && !*cfg.Enabled {
				enabled = false
			}
			apiKey = strings.TrimSpace(cfg.APIKey)
			apiURL = strings.TrimSpace(cfg.APIURL)
			model = strings.TrimSpace(cfg.Model)
			customPrompt = strings.TrimSpace(cfg.SystemPrompt)
		}
	}

	if !enabled {
		return checkBuiltinRules(trimmed)
	}

	// 2. Fall back to environment variables
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("MODERATION_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}

	if apiKey == "" {
		return checkBuiltinRules(trimmed)
	}

	if apiURL == "" {
		apiURL = strings.TrimSpace(os.Getenv("MODERATION_API_URL"))
	}
	if apiURL == "" {
		apiURL = "https://api.deepseek.com/chat/completions"
	}
	if model == "" {
		model = strings.TrimSpace(os.Getenv("MODERATION_MODEL"))
	}
	if model == "" {
		model = "deepseek-v4-flash"
	}

	systemPrompt := customPrompt
	if systemPrompt == "" {
		systemPrompt = `你是一个专业的内容安全审核员，请对用户评论进行审查
判断内容是否存在：政治敏感、违法暴恐、色情低俗、恶意辱骂人身攻击、垃圾引流欺诈广告
你必须严格输出且仅输出一个合法的 JSON 对象，格式如下：
{"action": "pass" | "flag" | "block", "reason": "审核结论说明", "categories": ["违规类型标签"]}
action 规则：
- pass: 正常言论、健康讨论、客观批评或赞美
- flag: 轻微争议、疑似擦边或需要人工复核
- block: 严重违规、明显人身攻击污言秽语、政治敏感、广告骚扰欺诈`
	}

	reqBody, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": trimmed},
		},
		"temperature": 0.0,
		"max_tokens":  256,
		"response_format": map[string]string{
			"type": "json_object",
		},
	})
	if err != nil {
		return checkBuiltinRules(trimmed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return checkBuiltinRules(trimmed)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := moderationHTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return checkBuiltinRules(trimmed)
	}
	defer resp.Body.Close()

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil || len(chatResp.Choices) == 0 {
		return checkBuiltinRules(trimmed)
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	var result struct {
		Action     string   `json:"action"`
		Reason     string   `json:"reason"`
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return checkBuiltinRules(trimmed)
	}

	action := strings.ToLower(result.Action)
	if action != "pass" && action != "flag" && action != "block" {
		action = "pass"
	}
	reason := result.Reason
	if reason == "" {
		if action == "pass" {
			reason = "正常内容"
		} else {
			reason = "违规内容"
		}
	}

	return ModerationResult{
		Action:     action,
		Reason:     reason,
		Model:      model,
		Categories: result.Categories,
	}
}

func checkBuiltinRules(text string) ModerationResult {
	lower := strings.ToLower(text)
	blockKeywords := []string{
		"办证", "代开发票", "加微信", "兼职刷单", "成人网站",
	}
	for _, kw := range blockKeywords {
		if strings.Contains(lower, kw) {
			return ModerationResult{
				Action:     "block",
				Reason:     "命中广告与欺诈关键词: " + kw,
				Model:      "rules-engine",
				Categories: []string{"spam"},
			}
		}
	}

	flagKeywords := []string{
		"傻逼", "弱智", "脑瘫", "死全家", "妈逼",
	}
	for _, kw := range flagKeywords {
		if strings.Contains(lower, kw) {
			return ModerationResult{
				Action:     "flag",
				Reason:     "疑似不文明用语: " + kw,
				Model:      "rules-engine",
				Categories: []string{"harassment"},
			}
		}
	}

	return ModerationResult{
		Action:     "pass",
		Reason:     "正常内容",
		Model:      "rules-engine",
		Categories: []string{},
	}
}
