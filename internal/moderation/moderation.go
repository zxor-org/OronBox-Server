// Package moderation reviews user-generated content with an OpenAI-compatible
// chat model. A primary provider (DeepSeek by default) is tried first and a
// fallback (GLM) takes over when the primary fails.
package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Endpoint struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string
}

type Verdict struct {
	Action     string         `json:"action"`
	Categories []string       `json:"categories"`
	Reason     string         `json:"reason"`
	Provider   string         `json:"-"`
	Model      string         `json:"-"`
	Raw        map[string]any `json:"-"`
}

// ErrUnavailable means every configured provider failed; callers are expected
// to fail closed.
var ErrUnavailable = errors.New("moderation providers unavailable")

type Service struct {
	primary  Endpoint
	fallback Endpoint
	timeout  time.Duration
	client   *http.Client
}

func New(primary, fallback Endpoint, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	return &Service{primary: primary, fallback: fallback, timeout: timeout, client: &http.Client{Timeout: timeout + time.Second}}
}

// Enabled reports whether AI moderation is configured at all. Without an API
// key the service is skipped instead of blocking content.
func (s *Service) Enabled() bool {
	return s.primary.APIKey != "" || s.fallback.APIKey != ""
}

func (s *Service) Review(ctx context.Context, prompt, text string) (Verdict, error) {
	if s.primary.APIKey != "" {
		verdict, err := s.reviewWith(ctx, s.primary, prompt, text)
		if err == nil {
			return verdict, nil
		}
		if s.fallback.APIKey == "" {
			return Verdict{}, errors.Join(ErrUnavailable, err)
		}
	}
	if s.fallback.APIKey != "" {
		verdict, err := s.reviewWith(ctx, s.fallback, prompt, text)
		if err == nil {
			return verdict, nil
		}
		return Verdict{}, errors.Join(ErrUnavailable, err)
	}
	return Verdict{}, ErrUnavailable
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Service) reviewWith(ctx context.Context, endpoint Endpoint, prompt, text string) (Verdict, error) {
	payload, err := json.Marshal(chatRequest{
		Model: endpoint.Model,
		Messages: []chatMessage{
			{Role: "system", Content: prompt},
			{Role: "user", Content: text},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
	})
	if err != nil {
		return Verdict{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Verdict{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	response, err := s.client.Do(request)
	if err != nil {
		return Verdict{}, fmt.Errorf("%s request: %w", endpoint.Name, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Verdict{}, err
	}
	if response.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("%s returned HTTP %d", endpoint.Name, response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil || len(completion.Choices) == 0 {
		return Verdict{}, fmt.Errorf("%s returned an unexpected payload", endpoint.Name)
	}
	var verdict Verdict
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &raw); err != nil {
		return Verdict{}, fmt.Errorf("%s returned non-JSON verdict: %w", endpoint.Name, err)
	}
	_ = json.Unmarshal([]byte(completion.Choices[0].Message.Content), &verdict)
	if verdict.Action != "pass" && verdict.Action != "review" && verdict.Action != "block" {
		return Verdict{}, fmt.Errorf("%s returned unknown action %q", endpoint.Name, verdict.Action)
	}
	verdict.Provider = endpoint.Name
	verdict.Model = endpoint.Model
	verdict.Raw = raw
	return verdict, nil
}
