package admin

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
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

const articleManifestPath = "articles/manifest.json"
const maxArticleBytes = 2 << 20

var articleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// BlogHandlers edits the community repository's article index and Markdown in
// one Gitea contents commit. The database is only used for admin authentication.
type BlogHandlers struct {
	Store            *store.Store
	Gitea            syndication.GiteaClient
	Owner, Repo, Ref string
}

func NewBlogHandlers(s *store.Store, g syndication.GiteaClient, owner, repo, ref string) *BlogHandlers {
	if strings.TrimSpace(ref) == "" {
		ref = "main"
	}
	return &BlogHandlers{Store: s, Gitea: g, Owner: owner, Repo: repo, Ref: ref}
}

type articleFile struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
	Type    string `json:"type"`
}
type articleChange struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	SHA       string `json:"sha,omitempty"`
	Content   string `json:"content,omitempty"`
}
type articleRequest struct {
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	Summary  string `json:"summary"`
	Content  string `json:"content"`
	Cover    string `json:"cover_url"`
	Category string `json:"category"`
	Status   string `json:"status"`
}
type articleAPIError struct{ Status int }

func (e articleAPIError) Error() string { return fmt.Sprintf("gitea status %d", e.Status) }

// request is deliberately local: Gitea's multi-file contents API is not the
// GitHub Git-data API, and its SHA preconditions must be preserved on updates.
func (h *BlogHandlers) request(ctx context.Context, method, suffix string, body any, out any) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	endpoint := strings.TrimRight(h.Gitea.BaseURL, "/") + "/api/v1/repos/" + url.PathEscape(h.Owner) + "/" + url.PathEscape(h.Repo) + "/" + suffix
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+h.Gitea.Token)
	req.Header.Set("Content-Type", "application/json")
	client := h.Gitea.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return articleAPIError{resp.StatusCode}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 4*maxArticleBytes)).Decode(out)
	}
	return nil
}
func (h *BlogHandlers) readFile(ctx context.Context, file, ref string) (articleFile, []byte, error) {
	var f articleFile
	err := h.request(ctx, http.MethodGet, "contents/"+file+"?ref="+url.QueryEscape(ref), nil, &f)
	if err != nil {
		return f, nil, err
	}
	if f.Type != "file" || f.SHA == "" {
		return f, nil, errors.New("invalid article file")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(f.Content, "\n", ""))
	return f, raw, err
}
func (h *BlogHandlers) readManifest(ctx context.Context) (map[string]any, []map[string]any, articleFile, string, error) {
	// Pin every read to one commit, including Markdown; changing branch tips must
	// never pair an old index with a newer article file.
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := h.request(ctx, http.MethodGet, "branches/"+url.PathEscape(h.Ref), nil, &branch); err != nil {
		return nil, nil, articleFile{}, "", err
	}
	if branch.Commit.ID == "" {
		return nil, nil, articleFile{}, "", errors.New("missing branch commit")
	}
	f, raw, err := h.readFile(ctx, articleManifestPath, branch.Commit.ID)
	if err != nil {
		return nil, nil, f, "", err
	}
	var root map[string]any
	if err = json.Unmarshal(raw, &root); err != nil {
		return nil, nil, f, "", err
	}
	values, ok := root["articles"].([]any)
	if !ok {
		return nil, nil, f, "", errors.New("invalid articles manifest")
	}
	entries := make([]map[string]any, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		a, ok := v.(map[string]any)
		id := articleString(a, "id")
		if !ok || !articleIDPattern.MatchString(id) || seen[id] {
			return nil, nil, f, "", errors.New("invalid or duplicate article id")
		}
		seen[id] = true
		if !validArticlePath(articleString(a, "file")) {
			return nil, nil, f, "", errors.New("invalid article path")
		}
		entries = append(entries, a)
	}
	return root, entries, f, branch.Commit.ID, nil
}
func articleString(m map[string]any, key string) string { v, _ := m[key].(string); return v }
func validArticlePath(p string) bool {
	return strings.HasPrefix(p, "articles/") && strings.HasSuffix(p, ".md") && path.Clean(p) == p && !strings.ContainsAny(p, "\\?#%\x00") && len(p) < 512
}
func (h *BlogHandlers) authorized(w http.ResponseWriter, r *http.Request) bool {
	if h.Store == nil {
		httputil.Error(w, 503, "unavailable", "管理员认证不可用")
		return false
	}
	if _, _, err := httputil.AuthenticateAdmin(r.Context(), h.Store, r); err != nil {
		httputil.Error(w, 401, "unauthorized", "未授权")
		return false
	}
	return true
}
func articleView(a map[string]any, content string) map[string]any {
	status := articleString(a, "status")
	if status == "" {
		status = "published"
	}
	return map[string]any{"id": a["id"], "slug": a["id"], "title": a["title"], "summary": a["summary"], "category": a["category"], "cover_url": a["cover_image"], "status": status, "content": content, "created_at": a["published_at"], "updated_at": a["updated_at"]}
}
func (h *BlogHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	_, entries, _, commit, err := h.readManifest(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	items := make([]map[string]any, 0, len(entries))
	for _, a := range entries {
		_, raw, err := h.readFile(r.Context(), articleString(a, "file"), commit)
		if err != nil {
			h.fail(w, err)
			return
		}
		items = append(items, articleView(a, string(raw)))
	}
	httputil.JSON(w, 200, map[string]any{"items": items})
}

// PublicList and PublicGet are read-only compatibility proxies for clients
// that cannot reach the raw Gitea host. Draft entries are never exposed.
func (h *BlogHandlers) PublicList(w http.ResponseWriter, r *http.Request) {
	_, entries, _, commit, err := h.readManifest(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, e := strconv.Atoi(value); e == nil && parsed > 0 && parsed < 100 {
			limit = parsed
		}
	}
	posts := make([]map[string]any, 0, min(limit, len(entries)))
	for _, a := range entries {
		if articleString(a, "status") == "draft" {
			continue
		}
		posts = append(posts, map[string]any{"slug": articleString(a, "id"), "type": articleString(a, "category"), "title": a["title"], "subtitle": a["summary"], "author": "OronBox Community", "status": "published", "cover_url": a["cover_image"], "published_at": a["published_at"]})
		if len(posts) >= limit {
			break
		}
	}
	_ = commit
	httputil.JSON(w, 200, map[string]any{"posts": posts})
}

func (h *BlogHandlers) PublicGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/blog/")
	if !articleIDPattern.MatchString(id) {
		httputil.Error(w, 400, "invalid_id", "文章ID格式错误")
		return
	}
	_, entries, _, commit, err := h.readManifest(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	for _, a := range entries {
		if articleString(a, "id") != id || articleString(a, "status") == "draft" {
			continue
		}
		_, raw, readErr := h.readFile(r.Context(), articleString(a, "file"), commit)
		if readErr != nil {
			h.fail(w, readErr)
			return
		}
		httputil.JSON(w, 200, map[string]any{"slug": id, "type": a["category"], "title": a["title"], "subtitle": a["summary"], "author": "OronBox Community", "status": "published", "body": string(raw), "cover_url": a["cover_image"], "published_at": a["published_at"]})
		return
	}
	httputil.Error(w, 404, "not_found", "文章不存在")
}
func (h *BlogHandlers) Create(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, "create") }
func (h *BlogHandlers) Update(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, "update") }
func (h *BlogHandlers) Delete(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, "delete") }
func (h *BlogHandlers) fail(w http.ResponseWriter, err error) {
	var upstream articleAPIError
	if errors.As(err, &upstream) && (upstream.Status == 409 || upstream.Status == 422) {
		httputil.Error(w, 409, "repository_conflict", "仓库内容已改变，请刷新后重试")
		return
	}
	httputil.Error(w, 502, "repository_error", "无法读取或更新社区仓库")
}
func (h *BlogHandlers) mutate(w http.ResponseWriter, r *http.Request, operation string) {
	if !h.authorized(w, r) {
		return
	}
	var req articleRequest
	id := strings.TrimPrefix(r.URL.Path, "/admin/api/blogs/")
	if operation != "delete" {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxArticleBytes))
		if err := decoder.Decode(&req); err != nil {
			httputil.Error(w, 400, "invalid_body", "文章格式或长度错误")
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			httputil.Error(w, 400, "invalid_body", "仅允许一个 JSON 对象")
			return
		}
		req.Title = strings.TrimSpace(req.Title)
		req.Slug = strings.TrimSpace(req.Slug)
		if req.Status == "" {
			req.Status = "published"
		}
		if req.Title == "" || len(req.Title) > 512 || len(req.Summary) > 8192 || (req.Status != "draft" && req.Status != "published") || len(req.Category) > 128 {
			httputil.Error(w, 400, "invalid_article", "文章标题或状态错误")
			return
		}
		if operation == "create" {
			id = req.Slug
			if id == "" {
				id = fmt.Sprintf("article-%d", time.Now().UnixNano())
			}
		}
	}
	if !articleIDPattern.MatchString(id) {
		httputil.Error(w, 400, "invalid_id", "文章 ID 格式错误")
		return
	}
	root, entries, manifest, commit, err := h.readManifest(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	index := -1
	for i, a := range entries {
		if articleString(a, "id") == id {
			index = i
			break
		}
	}
	if operation == "create" && index >= 0 {
		httputil.Error(w, 409, "article_exists", "文章 ID 已存在")
		return
	}
	if operation != "create" && index < 0 {
		httputil.Error(w, 404, "not_found", "文章不存在")
		return
	}
	var a map[string]any
	var changes []articleChange
	var existing []byte
	if index >= 0 {
		a = entries[index]
		file := articleString(a, "file")
		for i, other := range entries {
			if i != index && articleString(other, "file") == file {
				httputil.Error(w, 409, "shared_article_file", "多篇文章引用同一正文文件，无法编辑或删除")
				return
			}
		}
		existingFile, raw, err := h.readFile(r.Context(), file, commit)
		if err != nil {
			h.fail(w, err)
			return
		}
		existing = raw
		if operation == "delete" || req.Slug == "" || req.Slug == id {
			changes = append(changes, articleChange{Operation: operation, Path: file, SHA: existingFile.SHA})
		} else {
			if !articleIDPattern.MatchString(req.Slug) {
				httputil.Error(w, 400, "invalid_slug", "文章 slug 格式错误")
				return
			}
			for i, other := range entries {
				if i != index && articleString(other, "id") == req.Slug {
					httputil.Error(w, 409, "article_exists", "文章 slug 已存在")
					return
				}
			}
			newFile := "articles/" + req.Slug + ".md"
			changes = append(changes,
				articleChange{Operation: "delete", Path: file, SHA: existingFile.SHA},
				articleChange{Operation: "create", Path: newFile, Content: base64.StdEncoding.EncodeToString(existing)},
			)
			a["id"] = req.Slug
			a["file"] = newFile
			id = req.Slug
		}
	} else {
		a = map[string]any{"id": id, "file": "articles/" + id + ".md"}
		changes = append(changes, articleChange{Operation: "create", Path: articleString(a, "file")})
	}
	if operation == "delete" {
		entries = append(entries[:index], entries[index+1:]...)
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		a["title"] = req.Title
		a["summary"] = req.Summary
		a["cover_image"] = req.Cover
		a["category"] = req.Category
		a["status"] = req.Status
		a["updated_at"] = now
		if articleString(a, "published_at") == "" && req.Status == "published" {
			a["published_at"] = now
		}
		encodedContent := base64.StdEncoding.EncodeToString([]byte(req.Content))
		contentPath := articleString(a, "file")
		for i := range changes {
			if changes[i].Path == contentPath && changes[i].Operation != "delete" {
				changes[i].Content = encodedContent
			}
		}
		if operation == "create" {
			entries = append(entries, a)
		}
	}
	root["articles"] = entries
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		h.fail(w, err)
		return
	}
	changes = append(changes, articleChange{Operation: "update", Path: articleManifestPath, SHA: manifest.SHA, Content: base64.StdEncoding.EncodeToString(append(raw, '\n'))})
	// Existing blob SHA preconditions and non-forced push reject stale writes;
	// all files are committed together and unrelated repository files survive.
	err = h.request(r.Context(), http.MethodPost, "contents", map[string]any{"branch": h.Ref, "message": "admin: " + operation + " article " + id, "files": changes, "force_push": false}, nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	code := http.StatusOK
	if operation == "create" {
		code = http.StatusCreated
	}
	if operation == "delete" {
		httputil.JSON(w, code, map[string]any{"status": "deleted", "id": id})
		return
	}
	httputil.JSON(w, code, articleView(a, req.Content))
}
