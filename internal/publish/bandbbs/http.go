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
)

func (c *Client) DeleteResource(ctx context.Context, token, resourceID string) error {
	err := c.request(ctx, token, http.MethodDelete, c.apiURL+"/resources/"+url.PathEscape(resourceID), nil, "", nil)
	var statusErr *statusError
	if errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *Client) request(ctx context.Context, token, method, endpoint string, body io.Reader, contentType string, destination any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &statusError{status: resp.StatusCode, message: fmt.Sprintf("BandBBS %s %s returned HTTP %d: %s", method, resp.Request.URL.Path, resp.StatusCode, apiErrorDetail(body))}
	}
	if destination == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(destination)
}

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string {
	return e.message
}

func apiErrorDetail(body []byte) string {
	var payload struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &payload) == nil && len(payload.Errors) > 0 {
		parts := make([]string, 0, len(payload.Errors))
		for _, item := range payload.Errors {
			parts = append(parts, item.Code+": "+item.Message)
		}
		return strings.Join(parts, "; ")
	}
	detail := strings.Join(strings.Fields(string(body)), " ")
	if len(detail) > 300 {
		detail = detail[:300] + "..."
	}
	if detail == "" {
		return "empty response body"
	}
	return detail
}

func IsUnauthorized(err error) bool {
	var status *statusError
	return errors.As(err, &status) && status.status == http.StatusUnauthorized
}
