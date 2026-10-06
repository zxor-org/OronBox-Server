package syndication

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHubClient talks to the GitHub REST API. It implements the same Git-data
// surface as GiteaClient (blob/tree/commit/ref), so commitFilesTree is shared.
type GitHubClient struct {
	BaseURL, Token string
	HTTP           *http.Client
}

type githubError struct {
	Status  int
	Message string
}

func (e *githubError) Error() string { return e.Message }

func githubStatus(err error) int {
	var ge *githubError
	if errors.As(err, &ge) {
		return ge.Status
	}
	return 0
}

func (c GitHubClient) base() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return "https://api.github.com"
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c GitHubClient) request(ctx context.Context, method, path string, payload any, out any) error {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
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
	req, e := http.NewRequestWithContext(ctx, method, c.base()+"/"+strings.TrimLeft(path, "/"), body)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &githubError{Status: resp.StatusCode, Message: fmt.Sprintf("github %s %s: %s", method, path, resp.Status)}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// ---- Git data ----

func (c GitHubClient) GetBranchCommit(ctx context.Context, owner, repo, branch string) (string, error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if e := c.request(ctx, "GET", "repos/"+owner+"/"+repo+"/git/ref/heads/"+encodeRef(branch), nil, &out); e != nil {
		return "", e
	}
	return out.Object.SHA, nil
}
func (c GitHubClient) CreateBlob(ctx context.Context, owner, repo string, content []byte) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	e := c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/git/blobs", map[string]any{"content": base64.StdEncoding.EncodeToString(content), "encoding": "base64"}, &out)
	return out.SHA, e
}
func (c GitHubClient) CreateTree(ctx context.Context, owner, repo string, entries []treeEntry, baseTree string) (string, error) {
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
func (c GitHubClient) CreateCommit(ctx context.Context, owner, repo, message, tree string, parents []string) (string, error) {
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
func (c GitHubClient) CreateRef(ctx context.Context, owner, repo, ref, sha string) error {
	return c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/git/refs", map[string]any{"ref": ref, "sha": sha}, nil)
}

// UpdateRef patches a branch ref. It tries a fast-forward first, falls back to a
// force update on 409/422, and creates the ref on 404 (mirrors ABCC semantics).
func (c GitHubClient) UpdateRef(ctx context.Context, owner, repo, branch, sha string) error {
	path := "repos/" + owner + "/" + repo + "/git/refs/heads/" + encodeRef(branch)
	err := c.request(ctx, "PATCH", path, map[string]any{"sha": sha, "force": false}, nil)
	switch githubStatus(err) {
	case 0:
		return err
	case http.StatusNotFound:
		return c.CreateRef(ctx, owner, repo, "refs/heads/"+branch, sha)
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return c.request(ctx, "PATCH", path, map[string]any{"sha": sha, "force": true}, nil)
	default:
		return err
	}
}

func (c GitHubClient) CommitFilesTree(ctx context.Context, owner, repo, branch, message string, files map[string][]byte) (string, error) {
	return commitFilesTree(ctx, c, owner, repo, branch, message, "", files)
}
func (c GitHubClient) CommitFilesTreeBase(ctx context.Context, owner, repo, branch, message, baseTree string, files map[string][]byte) (string, error) {
	return commitFilesTree(ctx, c, owner, repo, branch, message, baseTree, files)
}

// CommitTree returns the tree SHA of a commit (used as base_tree for overlays).
func (c GitHubClient) CommitTree(ctx context.Context, owner, repo, commit string) (string, error) {
	var out struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if e := c.request(ctx, "GET", "repos/"+owner+"/"+repo+"/git/commits/"+commit, nil, &out); e != nil {
		return "", e
	}
	return out.Tree.SHA, nil
}

// ---- Repos / forks / refs ----

func (c GitHubClient) CurrentUser(ctx context.Context) (string, error) {
	var out struct {
		Login string `json:"login"`
	}
	if e := c.request(ctx, "GET", "user", nil, &out); e != nil {
		return "", e
	}
	return out.Login, nil
}

// EnsureUserRepo creates a repo under the token's user (auto-init so it has a
// default branch), tolerating an already-existing name.
func (c GitHubClient) EnsureUserRepo(ctx context.Context, name, description string) (owner, repoName, defaultBranch string, err error) {
	var out struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	e := c.request(ctx, "POST", "user/repos", map[string]any{"name": name, "description": description, "auto_init": true}, &out)
	if e != nil && githubStatus(e) != http.StatusUnprocessableEntity {
		return "", "", "", e
	}
	if out.Name == "" {
		user, uerr := c.CurrentUser(ctx)
		if uerr != nil {
			return "", "", "", uerr
		}
		if e := c.request(ctx, "GET", "repos/"+user+"/"+name, nil, &out); e != nil {
			return "", "", "", e
		}
	}
	if out.DefaultBranch == "" {
		out.DefaultBranch = "main"
	}
	return out.Owner.Login, out.Name, out.DefaultBranch, nil
}

func (c GitHubClient) ForkRepo(ctx context.Context, owner, repo string) (forkOwner, forkName, defaultBranch string, err error) {
	var out struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if e := c.request(ctx, "POST", "repos/"+owner+"/"+repo+"/forks", map[string]any{}, &out); e != nil {
		return "", "", "", e
	}
	if out.DefaultBranch == "" {
		out.DefaultBranch = "main"
	}
	return out.Owner.Login, out.Name, out.DefaultBranch, nil
}

// GetRefSHA reads a ref SHA, e.g. ref="heads/main".
func (c GitHubClient) GetRefSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if e := c.request(ctx, "GET", "repos/"+owner+"/"+repo+"/git/ref/"+ref, nil, &out); e != nil {
		return "", e
	}
	return out.Object.SHA, nil
}

func (c GitHubClient) CreateBranch(ctx context.Context, owner, repo, branch, sha string) error {
	return c.CreateRef(ctx, owner, repo, "refs/heads/"+branch, sha)
}

// SyncForkDefaultBranch best-effort aligns a fork's branch to upstream (merge-upstream,
// then a forced ref update). Never fatal; callers re-check HEAD equality.
func (c GitHubClient) SyncForkDefaultBranch(ctx context.Context, forkOwner, forkRepo, branch, upstreamOwner, upstreamRepo string) {
	_ = c.request(ctx, "POST", "repos/"+forkOwner+"/"+forkRepo+"/merge-upstream", map[string]any{"branch": branch}, nil)
	upstreamSHA, e1 := c.GetRefSHA(ctx, upstreamOwner, upstreamRepo, "heads/"+branch)
	forkSHA, e2 := c.GetRefSHA(ctx, forkOwner, forkRepo, "heads/"+branch)
	if e1 != nil || e2 != nil || upstreamSHA == "" || upstreamSHA == forkSHA {
		return
	}
	_ = c.request(ctx, "PATCH", "repos/"+forkOwner+"/"+forkRepo+"/git/refs/heads/"+encodeRef(branch), map[string]any{"sha": upstreamSHA, "force": true}, nil)
}

// ---- Contents / PRs / comments ----

func (c GitHubClient) ReadFile(ctx context.Context, owner, repo, file, ref string) ([]byte, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	path := "repos/" + owner + "/" + repo + "/contents/" + file + "?ref=" + url.QueryEscape(ref)
	if e := c.request(ctx, "GET", path, nil, &out); e != nil {
		return nil, e
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
}

func (c GitHubClient) CreatePullRequest(ctx context.Context, baseOwner, baseRepo, title, headOwner, headBranch, baseBranch, body string) (int, string, error) {
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	e := c.request(ctx, "POST", "repos/"+baseOwner+"/"+baseRepo+"/pulls", map[string]any{
		"title": title, "body": body, "base": baseBranch, "head": headOwner + ":" + headBranch,
	}, &out)
	return out.Number, out.HTMLURL, e
}

func (c GitHubClient) MergePullRequest(ctx context.Context, owner, repo string, number int) error {
	return c.request(ctx, "PUT", fmt.Sprintf("repos/%s/%s/pulls/%d/merge", owner, repo, number), map[string]any{}, nil)
}

// GitHubIssueComment carries the fields needed to derive review status.
type GitHubIssueComment struct {
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (c GitHubClient) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]GitHubIssueComment, error) {
	out := []GitHubIssueComment{}
	if number <= 0 {
		return out, nil
	}
	e := c.request(ctx, "GET", fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100&sort=created&direction=asc", owner, repo, number), nil, &out)
	return out, e
}

type GitHubPullRequest struct {
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	HTMLURL string `json:"html_url"`
}

func (c GitHubClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (GitHubPullRequest, error) {
	var out GitHubPullRequest
	e := c.request(ctx, "GET", fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, number), nil, &out)
	return out, e
}

// encodeRef percent-encodes each segment of a ref name (GitHub/Gitea accept
// unencoded slashes but encoding the branch name is safer for odd characters).
func encodeRef(branch string) string {
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
