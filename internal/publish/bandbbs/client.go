package bandbbs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	VersionID  string `json:"version_id,omitempty"`
	UpdateID   string `json:"update_id,omitempty"`
}

func New(apiURL string, blobs blob.Store, storage *store.Store) *Client {
	return &Client{apiURL: strings.TrimRight(apiURL, "/"), http: &http.Client{Timeout: 5 * time.Minute}, blobs: blobs, store: storage}
}

type snapshot struct {
	Revision struct {
		Name             string   `json:"name"`
		Summary          string   `json:"summary"`
		PurchaseLink     string   `json:"purchase_link"`
		PurchasePrice    *float64 `json:"purchase_price"`
		PurchaseCurrency string   `json:"purchase_currency"`
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
	Description       string   `json:"description"`
	VersionTitle      string   `json:"version_title"`
	VersionMessage    string   `json:"version_message"`
	OverwritePrevious bool     `json:"overwrite_previous_version"`
	Agreement         bool     `json:"agreement"`
	Price             float64  `json:"price"`
	Targets           []target `json:"targets"`
}

const externalPurchaseCurrency = "CNY"

// Publish fans one revision out to one BandBBS resource per configured
// category. existing maps a decimal category id to the bound resource id.
func (c *Client) Publish(ctx context.Context, token string, existing map[string]string, rawSnapshot, rawConfig []byte) (Result, error) {
	return c.PublishWithProgress(ctx, token, "", existing, nil, rawSnapshot, rawConfig)
}

// PublishWithProgress resumes a multi-step publication using the external
// resource, version, and update IDs already persisted by the coordinator.
// Replaying a failed attempt therefore edits the resource and skips completed
// side effects instead of creating duplicate versions or updates.
func (c *Client) PublishWithProgress(ctx context.Context, token, creatorID string, existing map[string]string, progress map[string]CategoryResult, rawSnapshot, rawConfig []byte) (Result, error) {
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
	cfg.VersionTitle = strings.TrimSpace(cfg.VersionTitle)
	cfg.VersionMessage = strings.TrimSpace(cfg.VersionMessage)
	purchaseLink := strings.TrimSpace(snap.Revision.PurchaseLink)
	externalPurchase := purchaseLink != ""
	if externalPurchase {
		if snap.Revision.PurchasePrice == nil || math.IsNaN(*snap.Revision.PurchasePrice) || math.IsInf(*snap.Revision.PurchasePrice, 0) || *snap.Revision.PurchasePrice <= 0 {
			return Result{}, fmt.Errorf("BandBBS external purchase amount is missing from resource metadata")
		}
		if math.IsNaN(cfg.Price) || math.IsInf(cfg.Price, 0) || (cfg.Price != 0 && math.Abs(cfg.Price-*snap.Revision.PurchasePrice) > 0.005) {
			return Result{}, fmt.Errorf("BandBBS external purchase amount does not match resource metadata")
		}
		cfg.Price = *snap.Revision.PurchasePrice
		if cfg.Price <= 0 {
			return Result{}, fmt.Errorf("BandBBS external purchase amount must be a positive CNY value")
		}
	}
	if (cfg.VersionTitle == "") != (cfg.VersionMessage == "") {
		return Result{}, fmt.Errorf("BandBBS version title and update notes must be provided together")
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
		if artifact < 0 && !externalPurchase {
			return Result{}, fmt.Errorf("package %s is not among the snapshot artifacts", target.PackageID)
		}
		var pack struct {
			Blob      string
			Name      string
			PackageID string
			Version   string
		}
		if artifact >= 0 {
			pack.Blob = snap.Artifacts[artifact].Blob
			pack.Name = snap.Artifacts[artifact].Name
			pack.PackageID = snap.Artifacts[artifact].PackageID
			pack.Version = snap.Artifacts[artifact].Version
		}
		packSize := c.blobSize(ctx, pack.Blob)
		categoryResult := progress[category]
		reconcileVersion := categoryResult.ResourceID != "" && categoryResult.VersionID == ""
		reconcileUpdate := categoryResult.ResourceID != "" && categoryResult.UpdateID == ""
		attachContext := map[string]string{"resource_category_id": category}
		existingID := existing[category]
		if existingID == "" {
			existingID = categoryResult.ResourceID
		}
		if existingID == "" && creatorID != "" {
			resource, findErr := c.findResource(ctx, token, creatorID, target.CategoryID, snap.Revision.Name)
			if findErr != nil {
				return result, fmt.Errorf("find existing resource in category %d: %w", target.CategoryID, findErr)
			}
			if resource.ID != 0 {
				existingID = strconv.Itoa(resource.ID)
			}
		}
		if existingID != "" {
			attachContext = map[string]string{"resource_id": existingID}
		}
		categoryResult.ResourceID = existingID
		createdResource := existingID == ""
		if categoryResult.ResourceID != "" {
			result.Resources[category] = categoryResult
		}
		versionKey := ""
		if categoryResult.VersionID == "" && !externalPurchase {
			var err error
			versionKey, _, err = c.newAttachmentKey(ctx, token, "resource_version", pack.Blob, pack.Name, attachContext)
			if err != nil {
				if categoryResult.ResourceID != "" {
					result.Resources[category] = categoryResult
				}
				return result, fmt.Errorf("upload version attachment for package %s: %w", target.PackageID, err)
			}
		}
		form := url.Values{"title": {snap.Revision.Name}, "tag_line": {truncateRunes(strings.TrimSpace(snap.Revision.Summary), 100)}, "description": {description}}
		if target.PrefixID > 0 {
			form.Set("prefix_id", strconv.Itoa(target.PrefixID))
		}
		if existingID == "" {
			form.Set("resource_category_id", category)
			if externalPurchase {
				form.Set("resource_type", "external_purchase")
				form.Set("external_purchase_url", purchaseLink)
				form.Set("price", strconv.FormatFloat(cfg.Price, 'f', 2, 64))
				form.Set("currency", externalPurchaseCurrency)
			} else {
				form.Set("resource_type", "download_local")
				form.Set("version_attachment_key", versionKey)
			}
			if pack.Version != "" {
				form.Set("version_string", pack.Version)
			}
		} else if externalPurchase {
			form.Set("resource_type", "external_purchase")
			form.Set("external_purchase_url", purchaseLink)
			form.Set("price", strconv.FormatFloat(cfg.Price, 'f', 2, 64))
			form.Set("currency", externalPurchaseCurrency)
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
		if response.Resource.ID == 0 {
			return result, fmt.Errorf("BandBBS returned no resource id for category %d", target.CategoryID)
		}
		resourceID := strconv.Itoa(response.Resource.ID)
		categoryResult.ResourceID = resourceID
		if createdResource && categoryResult.VersionID == "" {
			// Resource creation includes its first version. The API does not
			// consistently expose that version ID in the resource response, so
			// retain an opaque completion marker for retry reconciliation.
			categoryResult.VersionID = "initial"
		}
		if response.Resource.ViewURL != "" {
			categoryResult.URL = response.Resource.ViewURL
		}
		result.Resources[category] = categoryResult
		if categoryResult.VersionID == "" && !externalPurchase {
			version, versionErr := c.replaceVersion(ctx, token, resourceID, pack.Version, pack.Name, packSize, versionKey, reconcileVersion, cfg.OverwritePrevious)
			if versionErr != nil {
				return result, fmt.Errorf("create version in category %d: %w", target.CategoryID, versionErr)
			}
			categoryResult.VersionID = version.ID
		}
		if cfg.VersionTitle != "" && categoryResult.UpdateID == "" {
			update, updateErr := c.createUpdate(ctx, token, resourceID, cfg.VersionTitle, cfg.VersionMessage, reconcileUpdate)
			if updateErr != nil {
				return result, fmt.Errorf("create update in category %d: %w", target.CategoryID, updateErr)
			}
			categoryResult.UpdateID = update.ID
		}
		result.Resources[category] = categoryResult
	}
	return result, nil
}

type versionResult struct {
	ID string
}

type resourceSummary struct {
	ID       int    `json:"resource_id"`
	Title    string `json:"title"`
	Category int    `json:"resource_category_id"`
	ViewURL  string `json:"view_url"`
}

func (c *Client) findResource(ctx context.Context, token, creatorID string, categoryID int, title string) (resourceSummary, error) {
	var match resourceSummary
	for page := 1; ; page++ {
		var response struct {
			Resources  []resourceSummary `json:"resources"`
			Pagination struct {
				LastPage int `json:"last_page"`
			} `json:"pagination"`
		}
		query := url.Values{
			"creator_id": {creatorID},
			"page":       {strconv.Itoa(page)},
			"order":      {"last_update"},
			"direction":  {"desc"},
		}
		endpoint := c.apiURL + "/resources/?" + query.Encode()
		if err := c.request(ctx, token, http.MethodGet, endpoint, nil, "", &response); err != nil {
			return resourceSummary{}, err
		}
		for _, resource := range response.Resources {
			if resource.Category != categoryID || resource.Title != title || resource.ID == 0 {
				continue
			}
			if match.ID != 0 && match.ID != resource.ID {
				return resourceSummary{}, fmt.Errorf("multiple BandBBS resources match category %d and title %q", categoryID, title)
			}
			match = resource
		}
		lastPage := response.Pagination.LastPage
		if lastPage == 0 || page >= lastPage {
			return match, nil
		}
	}
}

type versionSummary struct {
	ID            int    `json:"resource_version_id"`
	VersionString string `json:"version_string"`
	Files         []struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	} `json:"files"`
}

func (c *Client) replaceVersion(ctx context.Context, token, resourceID, versionString, filename string, size int64, attachmentKey string, reconcile, overwritePrevious bool) (versionResult, error) {
	oldVersions, err := c.listVersions(ctx, token, resourceID)
	if err != nil {
		return versionResult{}, fmt.Errorf("list existing versions: %w", err)
	}
	if reconcile {
		if !overwritePrevious {
			if existing := findMatchingVersion(oldVersions, versionString, filename, size); existing != nil {
				if err := c.removePreviousVersions(ctx, token, oldVersions, versionString, existing.ID); err != nil {
					return versionResult{}, err
				}
				return versionResult{ID: strconv.Itoa(existing.ID)}, nil
			}
		}
	}
	created, err := c.createVersion(ctx, token, resourceID, versionString, attachmentKey)
	if err != nil {
		return versionResult{}, err
	}
	if overwritePrevious {
		if previous := latestVersion(oldVersions); previous != nil {
			if err := c.deleteVersion(ctx, token, strconv.Itoa(previous.ID)); err != nil {
				return created, fmt.Errorf("remove previous version %s: %w", strconv.Itoa(previous.ID), err)
			}
		}
		return created, nil
	}
	if err := c.removePreviousVersions(ctx, token, oldVersions, versionString, createdID(created)); err != nil {
		return created, err
	}
	return created, nil
}

func latestVersion(versions []versionSummary) *versionSummary {
	var latest *versionSummary
	for index := range versions {
		version := &versions[index]
		if latest == nil || version.ID > latest.ID {
			latest = version
		}
	}
	return latest
}

func createdID(version versionResult) int {
	id, _ := strconv.Atoi(version.ID)
	return id
}

func (c *Client) removePreviousVersions(ctx context.Context, token string, versions []versionSummary, versionString string, keepID int) error {
	if versionString == "" {
		return nil
	}
	for _, old := range versions {
		if old.VersionString != versionString || old.ID == keepID {
			continue
		}
		if err := c.deleteVersion(ctx, token, strconv.Itoa(old.ID)); err != nil {
			return fmt.Errorf("remove previous version %s: %w", strconv.Itoa(old.ID), err)
		}
	}
	return nil
}

func findMatchingVersion(versions []versionSummary, versionString, filename string, size int64) *versionSummary {
	if versionString == "" {
		return nil
	}
	for index := range versions {
		version := &versions[index]
		if version.VersionString != versionString {
			continue
		}
		for _, file := range version.Files {
			if file.Filename == filename && (size <= 0 || file.Size <= 0 || file.Size == size) {
				return version
			}
		}
	}
	return nil
}

func (c *Client) blobSize(ctx context.Context, sha string) int64 {
	if c.store == nil {
		return 0
	}
	record, err := c.store.Blob(ctx, sha)
	if err != nil {
		return 0
	}
	return record.Size
}

func (c *Client) listVersions(ctx context.Context, token, resourceID string) ([]versionSummary, error) {
	var response struct {
		Versions []versionSummary `json:"versions"`
	}
	if err := c.request(ctx, token, http.MethodGet, c.apiURL+"/resources/"+url.PathEscape(resourceID)+"/versions", nil, "", &response); err != nil {
		return nil, err
	}
	return response.Versions, nil
}

func (c *Client) deleteVersion(ctx context.Context, token, versionID string) error {
	query := url.Values{"reason": {"replaced by OronBox"}, "hard_delete": {"0"}}
	endpoint := c.apiURL + "/resource-versions/" + url.PathEscape(versionID) + "/?" + query.Encode()
	return c.request(ctx, token, http.MethodDelete, endpoint, nil, "", nil)
}

func (c *Client) createVersion(ctx context.Context, token, resourceID, versionString, attachmentKey string) (versionResult, error) {
	form := url.Values{
		"resource_id":            {resourceID},
		"version_type":           {"local"},
		"version_attachment_key": {attachmentKey},
	}
	if versionString != "" {
		form.Set("version_string", versionString)
	}
	var response struct {
		Version struct {
			ID int `json:"resource_version_id"`
		} `json:"version"`
	}
	if err := c.request(ctx, token, http.MethodPost, c.apiURL+"/resource-versions/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &response); err != nil {
		return versionResult{}, err
	}
	if response.Version.ID == 0 {
		return versionResult{}, fmt.Errorf("BandBBS returned no resource version id")
	}
	return versionResult{ID: strconv.Itoa(response.Version.ID)}, nil
}

type updateResult struct {
	ID string
}

type updateSummary struct {
	ID      int    `json:"resource_update_id"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

func (c *Client) createUpdate(ctx context.Context, token, resourceID, title, message string, reconcile bool) (updateResult, error) {
	if reconcile {
		updates, err := c.listUpdates(ctx, token, resourceID)
		if err != nil {
			return updateResult{}, fmt.Errorf("list existing updates: %w", err)
		}
		if existing := findMatchingUpdate(updates, title, message); existing != nil {
			return updateResult{ID: strconv.Itoa(existing.ID)}, nil
		}
	}
	form := url.Values{
		"resource_id": {resourceID},
		"title":       {title},
		"message":     {message},
	}
	var response struct {
		Update struct {
			ID int `json:"resource_update_id"`
		} `json:"update"`
	}
	if err := c.request(ctx, token, http.MethodPost, c.apiURL+"/resource-updates/", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &response); err != nil {
		return updateResult{}, err
	}
	if response.Update.ID == 0 {
		return updateResult{}, fmt.Errorf("BandBBS returned no resource update id")
	}
	return updateResult{ID: strconv.Itoa(response.Update.ID)}, nil
}

func (c *Client) listUpdates(ctx context.Context, token, resourceID string) ([]updateSummary, error) {
	var response struct {
		Updates []updateSummary `json:"updates"`
	}
	endpoint := c.apiURL + "/resources/" + url.PathEscape(resourceID) + "/updates?page=1"
	if err := c.request(ctx, token, http.MethodGet, endpoint, nil, "", &response); err != nil {
		return nil, err
	}
	return response.Updates, nil
}

func findMatchingUpdate(updates []updateSummary, title, message string) *updateSummary {
	for index := range updates {
		update := &updates[index]
		if update.Title == title && update.Message == message {
			return update
		}
	}
	return nil
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
