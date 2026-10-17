package bandbbs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

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
