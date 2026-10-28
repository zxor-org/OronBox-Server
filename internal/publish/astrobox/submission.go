package astrobox

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

func submissionBranch(key string) string {
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("oronbox-resource-%x", digest[:16])
}

// Recover a PR after a lost create response, and reject competing submissions.
func (c *Client) pendingSubmission(ctx context.Context, token, login, repoName, itemID string) (*existingPullRequest, error) {
	wantedPath, err := c.submissionPath(login, repoName)
	if err != nil {
		return nil, err
	}
	var recovered *existingPullRequest
	for page := 1; page <= 100; page++ {
		var pulls []existingPullRequest
		_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&per_page=100&page=%d", c.api, c.cfg.RepoOwner, c.cfg.RepoName, page), nil, &pulls)
		if err != nil {
			return nil, err
		}
		for _, pull := range pulls {
			if pull.Head.Repo.Owner.Login == "" || pull.Head.Repo.Name == "" || pull.Head.Ref == "" {
				continue
			}
			for filePage := 1; ; filePage++ {
				if filePage > 100 {
					return nil, fmt.Errorf("AstroBox submission has too many files")
				}
				var files []struct {
					Name string `json:"filename"`
				}
				_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", c.api, c.cfg.RepoOwner, c.cfg.RepoName, pull.Number, filePage), nil, &files)
				if err != nil {
					return nil, err
				}
				for _, file := range files {
					if !strings.HasPrefix(file.Name, "tmp/") || path.Base(file.Name) != submissionCSVFileName {
						continue
					}
					data, _, err := c.getContent(ctx, token, pull.Head.Repo.Owner.Login, pull.Head.Repo.Name, file.Name, pull.Head.Ref)
					if err != nil {
						return nil, fmt.Errorf("read existing AstroBox submission: %w", err)
					}
					rows, parseErr := csv.NewReader(strings.NewReader(string(data))).ReadAll()
					sameID := parseErr == nil && len(rows) == 2 && len(rows[1]) > 0 && strings.EqualFold(strings.TrimSpace(rows[1][0]), strings.TrimSpace(itemID))
					samePath := path.Dir(file.Name) == wantedPath
					if !sameID && !samePath {
						continue
					}
					if !sameID || !samePath || !strings.EqualFold(pull.Head.Repo.Owner.Login, login) {
						return nil, fmt.Errorf("AstroBox resource already has a conflicting submission: PR #%d", pull.Number)
					}
					if recovered != nil && recovered.Number != pull.Number {
						return nil, fmt.Errorf("AstroBox resource has multiple pending submissions")
					}
					current, err := c.writableSubmissionPR(ctx, token, pull.Number)
					if err != nil {
						return nil, err
					}
					recovered = &current
				}
				if len(files) < 100 {
					break
				}
			}
		}
		if len(pulls) < 100 {
			return recovered, nil
		}
	}
	return nil, fmt.Errorf("AstroBox has too many pending submissions")
}

func (c *Client) existingManifest(ctx context.Context, token string, target repo, ref string) (map[string]any, error) {
	data, _, err := c.getContent(ctx, token, target.Owner, target.Name, "manifest_v2.json", ref)
	if status, ok := githubStatus(err); ok && status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil || manifest == nil {
		return nil, fmt.Errorf("existing AstroBox manifest is invalid")
	}
	return manifest, nil
}

func preserveManifest(existing, generated map[string]any) map[string]any {
	if existing == nil {
		return generated
	}
	for key, value := range existing {
		if key == "ext" {
			generated[key] = value
			continue
		}
		if _, managed := generated[key]; !managed {
			generated[key] = value
		}
	}
	if old, ok := existing["item"].(map[string]any); ok {
		item := generated["item"].(map[string]any)
		for key, value := range old {
			if _, managed := item[key]; !managed {
				item[key] = value
			}
		}
	}
	if old, ok := existing["downloads"].(map[string]any); ok {
		if downloads, ok := generated["downloads"].(map[string]map[string]any); ok {
			for device, current := range downloads {
				if previous, ok := old[device].(map[string]any); ok {
					for key, value := range previous {
						if _, managed := current[key]; !managed {
							current[key] = value
						}
					}
				}
			}
		}
	}
	return generated
}

func (c *Client) ensureSubmissionBranch(ctx context.Context, token string, fork repo, branch, base string) error {
	_, err := c.refSHA(ctx, token, fork.Owner, fork.Name, branch)
	if err == nil {
		return nil
	}
	if status, ok := githubStatus(err); !ok || status != http.StatusNotFound {
		return err
	}
	return c.createRef(ctx, token, fork.Owner, fork.Name, branch, base)
}

func (c *Client) validatePublishedVersions(ctx context.Context, token string, row []string, downloads map[string]map[string]any) error {
	if len(row) < 6 {
		return fmt.Errorf("invalid published AstroBox row")
	}
	manifest, err := c.existingManifest(ctx, token, repo{Owner: row[3], Name: row[4]}, row[5])
	if err != nil {
		return err
	}
	oldDownloads, _ := manifest["downloads"].(map[string]any)
	for device, next := range downloads {
		old, _ := oldDownloads[device].(map[string]any)
		previous, ok := old["versionCode"].(float64)
		current, _ := next["versionCode"].(int64)
		if !ok {
			continue
		}
		changed := false
		if current == int64(previous) {
			if hash, ok := old["sha256"].(string); ok && hash != "" {
				changed = hash != next["sha256"]
			} else {
				filename, _ := old["file_name"].(string)
				if filename == "" {
					return fmt.Errorf("published AstroBox package has no file name")
				}
				var info struct {
					SHA string `json:"sha"`
				}
				endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.api, row[3], row[4], filename, url.QueryEscape(row[5]))
				if _, err := c.request(ctx, token, http.MethodGet, endpoint, nil, &info); err != nil {
					return fmt.Errorf("read published package identity: %w", err)
				}
				changed = info.SHA != next["git_blob_sha"]
			}
		}
		if current < int64(previous) || changed {
			return fmt.Errorf("AstroBox %s package versionCode must exceed the published version", device)
		}
	}
	return nil
}

// Head query is also used after PR creation fails: GitHub may have accepted it.
func (c *Client) submissionByHead(ctx context.Context, token, head string) (*existingPullRequest, error) {
	var pulls []existingPullRequest
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&head=%s", c.api, c.cfg.RepoOwner, c.cfg.RepoName, url.QueryEscape(head)), nil, &pulls)
	if err != nil || len(pulls) == 0 {
		return nil, err
	}
	current, err := c.writableSubmissionPR(ctx, token, pulls[0].Number)
	return &current, err
}
