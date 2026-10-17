package astrobox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (c *Client) currentUser(ctx context.Context, token string) (string, error) {
	var response struct {
		Login string `json:"login"`
	}
	_, err := c.request(ctx, token, http.MethodGet, c.api+"/user", nil, &response)
	return response.Login, err
}

func stringPtr(value string) *string {
	return &value
}

// uploadFiles lands every file in one Git Data API commit, the same way
// AstroBox Creator Console publishes (blobs -> tree -> commit -> ref update).
func (c *Client) uploadFiles(ctx context.Context, token string, target repo, message string, files map[string][]byte) (string, error) {
	parentSHA, err := c.refSHA(ctx, token, target.Owner, target.Name, target.Branch)
	if err != nil {
		first := ""
		var firstData []byte
		for filePath, data := range files {
			first, firstData = filePath, data
			break
		}
		if err := c.putContent(ctx, token, target.Owner, target.Name, first, target.Branch, "Initialize repository", firstData); err != nil {
			return "", err
		}
		delete(files, first)
		if parentSHA, err = c.refSHA(ctx, token, target.Owner, target.Name, target.Branch); err != nil {
			return "", err
		}
	}
	baseTree, err := c.commitTree(ctx, token, target.Owner, target.Name, parentSHA)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	entries := make([]treeEntry, 0, len(files))
	for _, filePath := range paths {
		sha, err := c.createBlob(ctx, token, target.Owner, target.Name, files[filePath])
		if err != nil {
			return "", err
		}
		entries = append(entries, treeEntry{Path: filePath, Mode: "100644", Type: "blob", SHA: sha})
	}
	treeSHA, err := c.createTree(ctx, token, target.Owner, target.Name, baseTree, entries)
	if err != nil {
		return "", err
	}
	commitSHA, err := c.createCommit(ctx, token, target.Owner, target.Name, message, treeSHA, parentSHA)
	if err != nil {
		return "", err
	}
	if err := c.updateRef(ctx, token, target.Owner, target.Name, target.Branch, commitSHA); err != nil {
		return "", err
	}
	return commitSHA, nil
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (c *Client) createBlob(ctx context.Context, token, owner, repoName string, data []byte) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/blobs", c.api, owner, repoName), map[string]any{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"}, &response)
	return response.SHA, err
}

func (c *Client) createTree(ctx context.Context, token, owner, repoName, baseTree string, entries []treeEntry) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	body := map[string]any{"tree": entries}
	if baseTree != "" {
		body["base_tree"] = baseTree
	}
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/trees", c.api, owner, repoName), body, &response)
	return response.SHA, err
}

func (c *Client) createCommit(ctx context.Context, token, owner, repoName, message, tree, parent string) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	parents := []string{}
	if parent != "" {
		parents = []string{parent}
	}
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/commits", c.api, owner, repoName), map[string]any{"message": message, "tree": tree, "parents": parents}, &response)
	return response.SHA, err
}

func (c *Client) updateRef(ctx context.Context, token, owner, repoName, branch, sha string) error {
	_, err := c.request(ctx, token, http.MethodPatch, fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", c.api, owner, repoName, url.PathEscape(branch)), map[string]any{"sha": sha, "force": true}, nil)
	return err
}

func (c *Client) commitTree(ctx context.Context, token, owner, repoName, commitSHA string) (string, error) {
	var response struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/git/commits/%s", c.api, owner, repoName, commitSHA), nil, &response)
	return response.Tree.SHA, err
}

func (c *Client) getContent(ctx context.Context, token, owner, repoName, filePath, branch string) ([]byte, string, error) {
	var response struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.api, owner, repoName, filePath, url.QueryEscape(branch))
	_, err := c.request(ctx, token, http.MethodGet, endpoint, nil, &response)
	if err != nil {
		return nil, "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	return data, response.SHA, err
}

func (c *Client) putContent(ctx context.Context, token, owner, repoName, filePath, branch, message string, data []byte) error {
	_, sha, err := c.getContent(ctx, token, owner, repoName, filePath, branch)
	if err != nil {
		status, ok := githubStatus(err)
		if !ok || status != http.StatusNotFound {
			return err
		}
	}
	return c.putContentWithSHA(ctx, token, owner, repoName, filePath, branch, message, data, sha)
}

func (c *Client) putContentWithSHA(ctx context.Context, token, owner, repoName, filePath, branch, message string, data []byte, sha string) error {
	body := map[string]any{"message": message, "content": base64.StdEncoding.EncodeToString(data), "branch": branch}
	if sha != "" {
		body["sha"] = sha
	}
	_, err := c.request(ctx, token, http.MethodPut, fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.api, owner, repoName, filePath), body, nil)
	return err
}

type pr struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
}

func (c *Client) createPR(ctx context.Context, token, title, head, base, body string) (pr, error) {
	var response pr
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/pulls", c.api, c.cfg.RepoOwner, c.cfg.RepoName), map[string]any{"title": title, "head": head, "base": base, "body": body}, &response)
	return response, err
}

func (c *Client) readBlob(ctx context.Context, sha string) (store.BlobRecord, []byte, error) {
	record, err := c.store.Blob(ctx, sha)
	if err != nil {
		return record, nil, err
	}
	reader, err := c.blobs.Open(ctx, record.LocalKey)
	if err != nil {
		return record, nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	return record, data, err
}

func (c *Client) request(ctx context.Context, token, method, endpoint string, body any, destination any) (int, error) {
	var stream io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		stream = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, stream)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return resp.StatusCode, &githubRequestError{
			status:   resp.StatusCode,
			method:   method,
			endpoint: endpoint,
			body:     string(body),
		}
	}
	if destination != nil {
		return resp.StatusCode, json.NewDecoder(resp.Body).Decode(destination)
	}
	return resp.StatusCode, nil
}

func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func uniqueDevices(artifacts []artifact) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range artifacts {
		for _, device := range supportedArtifactDevices(item.Devices) {
			if !seen[device.ID] {
				seen[device.ID] = true
				result = append(result, device.ID)
			}
		}
	}
	return result
}

type githubRequestError struct {
	status   int
	method   string
	endpoint string
	body     string
}

func (e *githubRequestError) Error() string {
	displayEndpoint := e.endpoint
	if parsed, err := url.Parse(e.endpoint); err == nil {
		displayEndpoint = parsed.Path
		if parsed.RawQuery != "" {
			displayEndpoint += "?" + parsed.RawQuery
		}
	}
	detail := strings.TrimSpace(e.body)
	if detail == "" {
		detail = "no response body"
	}
	if len(detail) > 512 {
		detail = detail[:512] + "..."
	}
	return fmt.Sprintf("GitHub API %s %s returned HTTP %d: %s", e.method, displayEndpoint, e.status, detail)
}

func githubStatus(err error) (int, bool) {
	var requestError *githubRequestError
	if !errors.As(err, &requestError) {
		return 0, false
	}
	return requestError.status, true
}

// Validate before writing copies and again immediately before appending a commit.
func (c *Client) writableSubmissionPR(ctx context.Context, token string, number int) (existingPullRequest, error) {
	var pr existingPullRequest
	if _, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.api, c.cfg.RepoOwner, c.cfg.RepoName, number), nil, &pr); err != nil {
		return pr, fmt.Errorf("read existing AstroBox pull request: %w", err)
	}
	if pr.State != "open" || pr.Merged || pr.Head.Ref == "" || pr.Head.Repo.Name == "" || pr.Head.Repo.Owner.Login == "" {
		return pr, fmt.Errorf("AstroBox pull request %d is not open or has no writable branch", number)
	}
	return pr, nil
}

// IsUnauthorized identifies a rejected GitHub credential without conflating it
// with OronBox session expiry or transient upstream errors.
func IsUnauthorized(err error) bool {
	status, ok := githubStatus(err)
	return ok && status == http.StatusUnauthorized
}
