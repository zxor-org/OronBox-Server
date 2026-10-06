// Package bandbbs implements the BandBBS (米坛) Resource Manager API used by the
// publishing worker. It follows docs/bandbbs-res-api-docs: metadata, versions and
// updates are three separate flows, and the discussion-thread prefix is set via
// the threads endpoint (needs thread:write).
package bandbbs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	APIURL string
	HTTP   *http.Client
}

func New(apiURL string) *Client {
	return &Client{APIURL: strings.TrimRight(apiURL, "/"), HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

type statusError struct {
	Status  int
	Code    string
	Message string
}

func (e *statusError) Error() string { return e.Message }

// MissingScope reports whether the API rejected the call for a missing scope.
func MissingScope(err error) bool {
	se, ok := err.(*statusError)
	return ok && se.Code == "missing_scope"
}

func (c Client) request(ctx context.Context, token, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.APIURL+"/"+strings.TrimLeft(path, "/"), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return parseAPIError(method, path, resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func parseAPIError(method, path string, status int, body []byte) error {
	var payload struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	e := &statusError{Status: status, Message: fmt.Sprintf("BandBBS %s %s returned HTTP %d", method, path, status)}
	if json.Unmarshal(body, &payload) == nil && len(payload.Errors) > 0 {
		e.Code = payload.Errors[0].Code
		e.Message = fmt.Sprintf("BandBBS %s %s: %s: %s", method, path, payload.Errors[0].Code, payload.Errors[0].Message)
		return e
	}
	if detail := strings.TrimSpace(string(body)); detail != "" {
		if len(detail) > 300 {
			detail = detail[:300]
		}
		e.Message += ": " + detail
	}
	return e
}

// Category is one BandBBS resource category with its capabilities.
type Category struct {
	ID               int    `json:"resource_category_id"`
	Title            string `json:"title"`
	CanAdd           bool   `json:"can_add"`
	CanUploadImages  bool   `json:"can_upload_images"`
	AllowLocal       bool   `json:"allow_local"`
	AllowExternal    bool   `json:"allow_external"`
	AllowFileless    bool   `json:"allow_fileless"`
	EnableVersioning bool   `json:"enable_versioning"`
	MinTags          int    `json:"min_tags"`
}

func (c Client) Categories(ctx context.Context, token string) (map[int]Category, error) {
	var out struct {
		Categories []Category `json:"categories"`
	}
	if err := c.request(ctx, token, http.MethodGet, "resource-categories/", nil, "", &out); err != nil {
		return nil, err
	}
	m := map[int]Category{}
	for _, cat := range out.Categories {
		m[cat.ID] = cat
	}
	return m, nil
}

// NewAttachmentKey uploads a file and returns (key, directURL). `kind` is
// "resource_version" for the package and "resource_update" for description images.
func (c Client) NewAttachmentKey(ctx context.Context, token, kind string, contextFields map[string]string, filename string, data []byte) (string, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("type", kind)
	for k, v := range contextFields {
		_ = w.WriteField("context["+k+"]", v)
	}
	part, err := w.CreateFormFile("attachment", filename)
	if err != nil {
		return "", "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", "", err
	}
	_ = w.Close()
	var out struct {
		Key        string `json:"key"`
		Attachment struct {
			ID        int    `json:"attachment_id"`
			DirectURL string `json:"direct_url"`
		} `json:"attachment"`
	}
	if err := c.request(ctx, token, http.MethodPost, "attachments/new-key", &body, w.FormDataContentType(), &out); err != nil {
		return "", "", err
	}
	if out.Key == "" {
		return "", "", fmt.Errorf("BandBBS did not return an attachment key")
	}
	return out.Key, out.Attachment.DirectURL, nil
}

// CreateResource creates a resource and returns its id/view url.
func (c Client) CreateResource(ctx context.Context, token string, form url.Values) (int, string, error) {
	var out struct {
		Resource struct {
			ID      int    `json:"resource_id"`
			ViewURL string `json:"view_url"`
		} `json:"resource"`
	}
	if err := c.request(ctx, token, http.MethodPost, "resources/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &out); err != nil {
		return 0, "", err
	}
	return out.Resource.ID, out.Resource.ViewURL, nil
}

// UpdateResource edits resource metadata only (does not publish a new version).
func (c Client) UpdateResource(ctx context.Context, token string, resourceID int, form url.Values) error {
	return c.request(ctx, token, http.MethodPost, "resources/"+strconv.Itoa(resourceID)+"/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil)
}

func (c Client) DeleteResource(ctx context.Context, token string, resourceID int) error {
	err := c.request(ctx, token, http.MethodDelete, "resources/"+strconv.Itoa(resourceID), nil, "", nil)
	if se, ok := err.(*statusError); ok && se.Status == http.StatusNotFound {
		return nil
	}
	return err
}

type Version struct {
	ID            int    `json:"resource_version_id"`
	VersionString string `json:"version_string"`
	Files         []struct {
		ID       int    `json:"id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	} `json:"files"`
}

func (c Client) Versions(ctx context.Context, token string, resourceID int) ([]Version, error) {
	var out struct {
		Versions []Version `json:"versions"`
	}
	if err := c.request(ctx, token, http.MethodGet, "resources/"+strconv.Itoa(resourceID)+"/versions", nil, "", &out); err != nil {
		return nil, err
	}
	return out.Versions, nil
}

// CreateVersion returns the new version id and its file ids.
func (c Client) CreateVersion(ctx context.Context, token string, resourceID int, versionString, attachmentKey string) (int, []int, error) {
	form := url.Values{"resource_id": {strconv.Itoa(resourceID)}, "version_type": {"local"}, "version_attachment_key": {attachmentKey}}
	if strings.TrimSpace(versionString) != "" {
		form.Set("version_string", versionString)
	}
	var out struct {
		Version struct {
			ID    int `json:"resource_version_id"`
			Files []struct {
				ID int `json:"id"`
			} `json:"files"`
		} `json:"version"`
	}
	if err := c.request(ctx, token, http.MethodPost, "resource-versions/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &out); err != nil {
		return 0, nil, err
	}
	if out.Version.ID == 0 {
		return 0, nil, fmt.Errorf("BandBBS returned no resource version id")
	}
	files := make([]int, 0, len(out.Version.Files))
	for _, f := range out.Version.Files {
		files = append(files, f.ID)
	}
	return out.Version.ID, files, nil
}

func (c Client) DeleteVersion(ctx context.Context, token string, versionID int) error {
	q := url.Values{"reason": {"replaced by OronBox"}, "hard_delete": {"0"}}
	return c.request(ctx, token, http.MethodDelete, "resource-versions/"+strconv.Itoa(versionID)+"/?"+q.Encode(), nil, "", nil)
}

func (c Client) CreateUpdate(ctx context.Context, token string, resourceID int, title, message string) (int, error) {
	form := url.Values{"resource_id": {strconv.Itoa(resourceID)}, "title": {title}, "message": {message}}
	var out struct {
		Update struct {
			ID int `json:"resource_update_id"`
		} `json:"update"`
	}
	if err := c.request(ctx, token, http.MethodPost, "resource-updates/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &out); err != nil {
		return 0, err
	}
	if out.Update.ID == 0 {
		return 0, fmt.Errorf("BandBBS returned no resource update id")
	}
	return out.Update.ID, nil
}

type ResourceSummary struct {
	ID       int    `json:"resource_id"`
	Title    string `json:"title"`
	Category int    `json:"resource_category_id"`
	ViewURL  string `json:"view_url"`
}

// FindResource pages the creator's resources and matches category + title.
func (c Client) FindResource(ctx context.Context, token, creatorID string, categoryID int, title string) (ResourceSummary, error) {
	match := ResourceSummary{}
	for page := 1; ; page++ {
		q := url.Values{"creator_id": {creatorID}, "page": {strconv.Itoa(page)}, "order": {"last_update"}, "direction": {"desc"}}
		var out struct {
			Resources  []ResourceSummary `json:"resources"`
			Pagination struct {
				LastPage int `json:"last_page"`
			} `json:"pagination"`
		}
		if err := c.request(ctx, token, http.MethodGet, "resources/?"+q.Encode(), nil, "", &out); err != nil {
			return ResourceSummary{}, err
		}
		for _, r := range out.Resources {
			if r.Category != categoryID || r.Title != title || r.ID == 0 {
				continue
			}
			if match.ID != 0 && match.ID != r.ID {
				return ResourceSummary{}, fmt.Errorf("multiple BandBBS resources match category %d and title %q", categoryID, title)
			}
			match = r
		}
		last := out.Pagination.LastPage
		if last == 0 || page >= last {
			return match, nil
		}
	}
}

var threadLinkRe = regexp.MustCompile(`/threads/(\d+)/`)

// ResourcePageThreadID scrapes the public resource page for the discussion thread id
// (the API does not return it).
func (c Client) ResourcePageThreadID(ctx context.Context, token string, resourceID int) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+"/resources/"+strconv.Itoa(resourceID)+"/", nil)
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, &statusError{Status: resp.StatusCode, Message: fmt.Sprintf("BandBBS resource page HTTP %d", resp.StatusCode)}
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	m := threadLinkRe.FindSubmatch(raw)
	if m == nil {
		return 0, fmt.Errorf("no discussion thread found on resource %d page", resourceID)
	}
	return strconv.Atoi(string(m[1]))
}

// SetThreadPrefix sets the discussion thread prefix (requires thread:write).
func (c Client) SetThreadPrefix(ctx context.Context, token string, threadID, prefixID int) error {
	form := url.Values{"prefix_id": {strconv.Itoa(prefixID)}}
	return c.request(ctx, token, http.MethodPost, "threads/"+strconv.Itoa(threadID)+"/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil)
}
