package bandbbs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type Client struct {
	apiURL string
	http   *http.Client
	blobs  blob.Store
	store  *store.Store
}
type Result struct {
	// Resources maps the decimal category id to the published resource.
	Resources map[string]CategoryResult
}

type CategoryResult struct {
	ResourceID string `json:"resource_id"`
	URL        string `json:"url"`
}

func New(apiURL string, blobs blob.Store, storage *store.Store) *Client {
	return &Client{apiURL: strings.TrimRight(apiURL, "/"), http: &http.Client{Timeout: 5 * time.Minute}, blobs: blobs, store: storage}
}

type snapshot struct {
	Revision struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	} `json:"revision"`
	Media []struct {
		Blob string `json:"blob_sha256"`
		Role string `json:"role"`
	} `json:"media"`
	Artifacts []struct {
		Blob      string `json:"blob_sha256"`
		Name      string `json:"original_name"`
		PackageID string `json:"package_id"`
		Version   string `json:"version"`
	} `json:"artifacts"`
}

type target struct {
	CategoryID int    `json:"category_id"`
	PrefixID   int    `json:"prefix_id"`
	PackageID  string `json:"package_id"`
}

type config struct {
	Description string   `json:"description"`
	Agreement   bool     `json:"agreement"`
	Targets     []target `json:"targets"`
}

// Publish fans one revision out to one BandBBS resource per configured
// category. existing maps a decimal category id to the bound resource id.
func (c *Client) Publish(ctx context.Context, token string, existing map[string]string, rawSnapshot, rawConfig []byte) (Result, error) {
	var snap snapshot
	var cfg config
	if err := json.Unmarshal(rawSnapshot, &snap); err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return Result{}, err
	}
	if !cfg.Agreement || len(cfg.Targets) == 0 {
		return Result{}, fmt.Errorf("BandBBS targets and agreement are required")
	}
	var previews []struct {
		Blob string `json:"blob_sha256"`
		Role string `json:"role"`
	}
	for _, media := range snap.Media {
		if media.Role == "preview" {
			previews = append(previews, media)
		}
	}
	description := strings.TrimSpace(cfg.Description)
	if description == "" {
		description = snap.Revision.Summary
	}
	// Previews are shared by every category, so upload them once against the
	// first target. RM MarketPlace registers no attachment content type for
	// description images, so previews go up as plain attachments and embed by
	// URL.
	previewContext := map[string]string{"resource_category_id": strconv.Itoa(cfg.Targets[0].CategoryID)}
	if existingID := existing[strconv.Itoa(cfg.Targets[0].CategoryID)]; existingID != "" {
		previewContext = map[string]string{"resource_id": existingID}
	}
	for index, media := range previews {
		ext := mediaExtension(c.store, ctx, media.Blob)
		name := fmt.Sprintf("preview-%d%s", index+1, ext)
		_, directURL, uploadErr := c.newAttachmentKey(ctx, token, "resource_version", media.Blob, name, previewContext)
		if uploadErr != nil {
			return Result{}, fmt.Errorf("upload preview attachment: %w", uploadErr)
		}
		if directURL != "" {
			description += fmt.Sprintf("\n\n[IMG]%s[/IMG]", directURL)
		}
	}
	result := Result{Resources: make(map[string]CategoryResult, len(cfg.Targets))}
	for _, target := range cfg.Targets {
		category := strconv.Itoa(target.CategoryID)
		artifact := -1
		for index := range snap.Artifacts {
			if snap.Artifacts[index].PackageID == target.PackageID {
				artifact = index
				break
			}
		}
		if artifact < 0 {
			return Result{}, fmt.Errorf("package %s is not among the snapshot artifacts", target.PackageID)
		}
		pack := snap.Artifacts[artifact]
		attachContext := map[string]string{"resource_category_id": category}
		existingID := existing[category]
		if existingID != "" {
			attachContext = map[string]string{"resource_id": existingID}
		}
		versionKey, _, err := c.newAttachmentKey(ctx, token, "resource_version", pack.Blob, pack.Name, attachContext)
		if err != nil {
			return Result{}, fmt.Errorf("upload version attachment for package %s: %w", target.PackageID, err)
		}
		form := url.Values{"resource_category_id": {category}, "title": {snap.Revision.Name}, "tag_line": {truncateRunes(strings.TrimSpace(snap.Revision.Summary), 100)}, "description": {description}, "resource_type": {"download_local"}, "version_attachment_key": {versionKey}}
		if target.PrefixID > 0 {
			form.Set("prefix_id", strconv.Itoa(target.PrefixID))
		}
		if pack.Version != "" {
			form.Set("version_string", pack.Version)
		}
		var response struct {
			Resource struct {
				ID      int    `json:"resource_id"`
				ViewURL string `json:"view_url"`
			} `json:"resource"`
		}
		endpoint := c.apiURL + "/resources/"
		if existingID != "" {
			endpoint += url.PathEscape(existingID) + "/"
		}
		if err := c.request(ctx, token, http.MethodPost, endpoint, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &response); err != nil {
			return Result{}, fmt.Errorf("create resource in category %d: %w", target.CategoryID, err)
		}
		if response.Resource.ID == 0 && existingID != "" {
			response.Resource.ID, _ = strconv.Atoi(existingID)
		}
		result.Resources[category] = CategoryResult{ResourceID: strconv.Itoa(response.Resource.ID), URL: response.Resource.ViewURL}
	}
	return result, nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (c *Client) DeleteResource(ctx context.Context, token, resourceID string) error {
	err := c.request(ctx, token, http.MethodDelete, c.apiURL+"/resources/"+url.PathEscape(resourceID), nil, "", nil)
	var statusErr *statusError
	if errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *Client) newAttachmentKey(ctx context.Context, token, kind, sha, name string, contextFields map[string]string) (string, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("type", kind)
	for key, value := range contextFields {
		_ = writer.WriteField("context["+key+"]", value)
	}
	if err := writeAttachmentPart(writer, c, ctx, sha, name); err != nil {
		return "", "", err
	}
	writer.Close()
	var response struct {
		Key        string `json:"key"`
		Attachment struct {
			DirectURL string `json:"direct_url"`
		} `json:"attachment"`
	}
	err := c.request(ctx, token, http.MethodPost, c.apiURL+"/attachments/new-key", &body, writer.FormDataContentType(), &response)
	if err != nil {
		return "", "", err
	}
	if response.Key == "" {
		return "", "", fmt.Errorf("BandBBS did not return an attachment key")
	}
	return response.Key, response.Attachment.DirectURL, nil
}

func writeAttachmentPart(writer *multipart.Writer, c *Client, ctx context.Context, sha, name string) error {
	record, err := c.store.Blob(ctx, sha)
	if err != nil {
		return err
	}
	file, err := c.blobs.Open(ctx, record.LocalKey)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile("attachment", name)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}

func mediaExtension(storage *store.Store, ctx context.Context, sha string) string {
	record, err := storage.Blob(ctx, sha)
	if err != nil {
		return ".bin"
	}
	switch record.MediaType {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
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
