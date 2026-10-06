package syndication

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GiteaClient struct {
	BaseURL, Token string
	HTTP           *http.Client
}

func (c GiteaClient) request(ctx context.Context, method, path string, payload any, out any) error {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		body = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+"/api/v1/"+strings.TrimLeft(path, "/"), body)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitea %s: %s", resp.Status, readBody(resp.Body))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
func (c GiteaClient) CreateRepository(ctx context.Context, owner, name string, private bool) error {
	path := "user/repos"
	if owner != "" {
		path = "orgs/" + owner + "/repos"
	}
	return c.request(ctx, "POST", path, map[string]any{"name": name, "private": private}, nil)
}
func (c GiteaClient) CreatePullRequest(ctx context.Context, owner, repo, title, head, base, body string) (int, error) {
	var out struct {
		Number int `json:"number"`
	}
	e := c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/pulls", map[string]any{"title": title, "head": head, "base": base, "body": body}, &out)
	return out.Number, e
}
func (c GiteaClient) SetPrivate(ctx context.Context, owner, repo string, private bool) error {
	return c.request(ctx, "PATCH", "repos/"+owner+"/"+repo, map[string]any{"private": private}, nil)
}

// MergePullRequest merges a PR (server-driven; no webhook is used).
func (c GiteaClient) MergePullRequest(ctx context.Context, owner, repo string, index int) error {
	return c.request(ctx, "POST", fmt.Sprintf("repos/%s/%s/pulls/%d/merge", owner, repo, index), map[string]any{}, nil)
}
func (c GiteaClient) CreateFile(ctx context.Context, owner, repo, file, branch, message string, data []byte) error {
	return c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/contents/"+strings.TrimLeft(file, "/"), map[string]any{"branch": branch, "message": message, "content": base64.StdEncoding.EncodeToString(data)}, nil)
}
func (c GiteaClient) UpsertFile(ctx context.Context, owner, repo, file, branch, message string, data []byte) error {
	path := "repos/" + owner + "/" + repo + "/contents/" + strings.TrimLeft(file, "/")
	var current struct {
		SHA string `json:"sha"`
	}
	if err := c.request(ctx, "GET", path+"?ref="+branch, nil, &current); err == nil && current.SHA != "" {
		return c.request(ctx, "PUT", path, map[string]any{"branch": branch, "message": message, "sha": current.SHA, "content": base64.StdEncoding.EncodeToString(data)}, nil)
	}
	return c.CreateFile(ctx, owner, repo, file, branch, message, data)
}
func (c GiteaClient) CreateBranch(ctx context.Context, owner, repo, name, from string) error {
	return c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/branches", map[string]any{"new_branch_name": name, "old_branch_name": from}, nil)
}
func (c GiteaClient) ReadFile(ctx context.Context, owner, repo, file, ref string) ([]byte, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if e := c.request(ctx, "GET", "repos/"+owner+"/"+repo+"/contents/"+strings.TrimLeft(file, "/")+"?ref="+ref, nil, &out); e != nil {
		return nil, e
	}
	decoded, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if e != nil {
		return nil, e
	}
	return decoded, nil
}

type IssueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

func (c GiteaClient) ListIssueComments(ctx context.Context, owner, repo string, index int) ([]IssueComment, error) {
	out := []IssueComment{}
	if index <= 0 {
		return out, nil
	}
	e := c.request(ctx, "GET", fmt.Sprintf("repos/%s/%s/issues/%d/comments", owner, repo, index), nil, &out)
	return out, e
}
func (c GiteaClient) CreateIssueComment(ctx context.Context, owner, repo string, index int, body string) error {
	return c.request(ctx, "POST", fmt.Sprintf("repos/%s/%s/issues/%d/comments", owner, repo, index), map[string]any{"body": body}, nil)
}
func (c GiteaClient) GetBranchCommit(ctx context.Context, owner, repo, branch string) (string, error) {
	var out struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if e := c.request(ctx, "GET", "repos/"+owner+"/"+repo+"/branches/"+branch, nil, &out); e != nil {
		return "", e
	}
	return out.Commit.ID, nil
}
func (c GiteaClient) CommitTree(ctx context.Context, owner, repo, commit string) (string, error) {
	var out struct {
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	if e := c.request(ctx, "GET", "repos/"+owner+"/"+repo+"/git/commits/"+commit, nil, &out); e != nil {
		return "", e
	}
	return out.Commit.Tree.SHA, nil
}

// treeEntry is one entry of a Git tree (Gitea and GitHub share this shape).
type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// CommitFilesTree replaces the *entire* tree at `branch` with `files` in a single
// atomic commit. Files not present in `files` disappear (strict mirror), so
// multi-file writes never produce N per-file commits. Returns the new commit SHA.
func (c GiteaClient) CommitFilesTree(ctx context.Context, owner, repo, branch, message string, files map[string][]byte) (string, error) {
	return commitFilesTree(ctx, c, owner, repo, branch, message, "", files)
}

// CommitFilesTreeBase overlays `files` on top of `baseTree` (an existing commit's
// tree SHA) in a single commit, leaving other files untouched. Returns the new
// commit SHA. Used for fork submissions where only a few files are added.
func (c GiteaClient) CommitFilesTreeBase(ctx context.Context, owner, repo, branch, message, baseTree string, files map[string][]byte) (string, error) {
	return commitFilesTree(ctx, c, owner, repo, branch, message, baseTree, files)
}

// gitData is the shared Git-data API surface implemented by both GiteaClient and
// GitHubClient (identical blob/tree/commit/ref shapes).
type gitData interface {
	GetBranchCommit(ctx context.Context, owner, repo, branch string) (string, error)
	CreateBlob(ctx context.Context, owner, repo string, content []byte) (string, error)
	CreateTree(ctx context.Context, owner, repo string, entries []treeEntry, baseTree string) (string, error)
	CreateCommit(ctx context.Context, owner, repo, message, tree string, parents []string) (string, error)
	CreateRef(ctx context.Context, owner, repo, ref, sha string) error
	UpdateRef(ctx context.Context, owner, repo, branch, sha string) error
}

// commitFilesTree builds blobs, one tree (optionally based on baseTree), a commit
// and updates the branch ref. baseTree "" means a brand-new tree (strict mirror).
func commitFilesTree(ctx context.Context, g gitData, owner, repo, branch, message, baseTree string, files map[string][]byte) (string, error) {
	parent, _ := g.GetBranchCommit(ctx, owner, repo, branch) // "" on a fresh repo
	entries := make([]treeEntry, 0, len(files))
	for path, content := range files {
		sha, err := g.CreateBlob(ctx, owner, repo, content)
		if err != nil {
			return "", err
		}
		entries = append(entries, treeEntry{Path: path, Mode: "100644", Type: "blob", SHA: sha})
	}
	tree, err := g.CreateTree(ctx, owner, repo, entries, baseTree)
	if err != nil {
		return "", err
	}
	var parents []string
	if parent != "" {
		parents = []string{parent}
	}
	commit, err := g.CreateCommit(ctx, owner, repo, message, tree, parents)
	if err != nil {
		return "", err
	}
	if parent == "" {
		if err := g.CreateRef(ctx, owner, repo, "refs/heads/"+branch, commit); err != nil {
			return "", err
		}
	} else if err := g.UpdateRef(ctx, owner, repo, branch, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func (c GiteaClient) CreateBlob(ctx context.Context, owner, repo string, content []byte) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	e := c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/git/blobs", map[string]any{"content": base64.StdEncoding.EncodeToString(content), "encoding": "base64"}, &out)
	return out.SHA, e
}
func (c GiteaClient) CreateTree(ctx context.Context, owner, repo string, entries []treeEntry, baseTree string) (string, error) {
	payload := map[string]any{"tree": entries}
	if baseTree != "" {
		payload["base_tree"] = baseTree
	}
	var out struct {
		SHA string `json:"sha"`
	}
	e := c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/git/trees", payload, &out)
	return out.SHA, e
}
func (c GiteaClient) CreateCommit(ctx context.Context, owner, repo, message, tree string, parents []string) (string, error) {
	payload := map[string]any{"message": message, "tree": tree}
	if len(parents) > 0 {
		payload["parents"] = parents
	}
	var out struct {
		SHA string `json:"sha"`
	}
	e := c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/git/commits", payload, &out)
	return out.SHA, e
}
func (c GiteaClient) CreateRef(ctx context.Context, owner, repo, ref, sha string) error {
	return c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/git/refs", map[string]any{"ref": ref, "sha": sha}, nil)
}
func (c GiteaClient) UpdateRef(ctx context.Context, owner, repo, branch, sha string) error {
	return c.request(ctx, "PATCH", "repos/"+owner+"/"+repo+"/git/refs/heads/"+branch, map[string]any{"sha": sha, "force": true}, nil)
}
func readBody(r interface{ Read([]byte) (int, error) }) string {
	b := make([]byte, 1024)
	n, _ := r.Read(b)
	return strings.TrimSpace(string(b[:n]))
}
