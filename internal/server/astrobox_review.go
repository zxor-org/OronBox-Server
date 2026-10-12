package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

func (a *application) astroboxReviewContext(r *http.Request) (int, int, string, string, astrobox.Client, error) {
	uid, err := httputil.AuthenticateUser(r.Context(), a.db, httputil.ExtractBearerToken(r))
	if err != nil || uid <= 0 {
		return 0, 0, "", "", astrobox.Client{}, err
	}
	revisionID, _ := strconv.ParseInt(pathPart(r.URL.Path, "submissions"), 10, 64)
	if revisionID <= 0 {
		return 0, 0, "", "", astrobox.Client{}, strconv.ErrSyntax
	}
	var ownerUID int
	var externalID, externalURL string
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT r.owner_uid, COALESCE(p.external_id,''), COALESCE(p.external_url,'') FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources r ON r.resource_id=rr.resource_id WHERE p.revision_id=$1 AND p.provider='astrobox'`, revisionID).Scan(&ownerUID, &externalID, &externalURL); err != nil {
		return 0, 0, "", "", astrobox.Client{}, err
	}
	if ownerUID != uid {
		return 0, 0, "", "", astrobox.Client{}, fmt.Errorf("forbidden")
	}
	pr, err := strconv.Atoi(externalID)
	if err != nil || pr <= 0 {
		return 0, 0, "", "", astrobox.Client{}, strconv.ErrSyntax
	}
	var login string
	var encrypted []byte
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT login, access_token_cipher FROM github_grants WHERE user_uid=$1`, uid).Scan(&login, &encrypted); err != nil {
		return 0, 0, "", "", astrobox.Client{}, err
	}
	token, err := a.cipher.Decrypt(encrypted)
	if err != nil {
		return 0, 0, "", "", astrobox.Client{}, err
	}
	client := astrobox.Client{GitHub: syndication.GitHubClient{BaseURL: a.cfg.GitHub.APIURL, Token: string(token)}, RepoOwner: a.cfg.AstroBox.RepoOwner, RepoName: a.cfg.AstroBox.RepoName, RepoBranch: a.cfg.AstroBox.RepoBranch, CatalogPath: a.cfg.AstroBox.CatalogPath}
	return uid, pr, string(token), externalURL, client, nil
}

func pathPart(path, marker string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if part == marker && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func lastPathPart(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (a *application) getAstroboxReview(w http.ResponseWriter, r *http.Request) {
	_, pr, token, externalURL, client, err := a.astroboxReviewContext(r)
	if err != nil {
		httputil.Error(w, http.StatusForbidden, "astrobox_unavailable", "无法读取 AstroBox 审核状态")
		return
	}
	merged, state, url, err := client.PRStatus(r.Context(), token, pr)
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "github_error", "无法读取 GitHub PR")
		return
	}
	if url == "" {
		url = externalURL
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"provider": "astrobox", "pr_number": pr, "external_url": url, "state": state, "merged": merged})
}

func (a *application) getAstroboxComments(w http.ResponseWriter, r *http.Request) {
	_, pr, _, _, client, err := a.astroboxReviewContext(r)
	if err != nil {
		httputil.Error(w, http.StatusForbidden, "astrobox_unavailable", "无法读取 AstroBox 评论")
		return
	}
	comments, err := client.GitHub.ListIssueComments(r.Context(), client.RepoOwner, client.RepoName, pr)
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "github_error", "无法读取 GitHub 评论")
		return
	}
	items := make([]map[string]any, 0, len(comments))
	for _, comment := range comments {
		items = append(items, map[string]any{"id": comment.ID, "body": comment.Body, "created_at": comment.CreatedAt, "author": comment.User.Login})
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *application) createAstroboxComment(w http.ResponseWriter, r *http.Request) {
	uid, pr, _, _, client, err := a.astroboxReviewContext(r)
	if err != nil {
		httputil.Error(w, http.StatusForbidden, "astrobox_unavailable", "无法操作 AstroBox 评论")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req) != nil || strings.TrimSpace(req.Body) == "" {
		httputil.Error(w, 400, "invalid_body", "评论内容不能为空")
		return
	}
	comment, err := client.GitHub.CreateIssueComment(r.Context(), client.RepoOwner, client.RepoName, pr, strings.TrimSpace(req.Body))
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "github_error", "无法提交 GitHub 评论")
		return
	}
	_ = uid
	httputil.JSON(w, http.StatusCreated, map[string]any{"id": comment.ID, "body": comment.Body, "created_at": comment.CreatedAt})
}

func (a *application) updateAstroboxComment(w http.ResponseWriter, r *http.Request) {
	_, _, _, _, client, err := a.astroboxReviewContext(r)
	if err != nil {
		httputil.Error(w, 403, "astrobox_unavailable", "无法操作 AstroBox 评论")
		return
	}
	id, _ := strconv.ParseInt(lastPathPart(r.URL.Path), 10, 64)
	if id <= 0 {
		httputil.Error(w, 400, "invalid_id", "评论ID无效")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req) != nil || strings.TrimSpace(req.Body) == "" {
		httputil.Error(w, 400, "invalid_body", "评论内容不能为空")
		return
	}
	comment, err := client.GitHub.GetIssueComment(r.Context(), client.RepoOwner, client.RepoName, id)
	if err != nil || comment.User.Login != "" && comment.User.Login != currentGithubLogin(a, r) {
		httputil.Error(w, 403, "forbidden", "只能修改自己的 GitHub 评论")
		return
	}
	if err := client.GitHub.UpdateIssueComment(r.Context(), client.RepoOwner, client.RepoName, id, strings.TrimSpace(req.Body)); err != nil {
		httputil.Error(w, 502, "github_error", "无法更新 GitHub 评论")
		return
	}
	httputil.JSON(w, 200, map[string]any{"id": id, "body": strings.TrimSpace(req.Body)})
}

func (a *application) deleteAstroboxComment(w http.ResponseWriter, r *http.Request) {
	_, _, _, _, client, err := a.astroboxReviewContext(r)
	if err != nil {
		httputil.Error(w, 403, "astrobox_unavailable", "无法操作 AstroBox 评论")
		return
	}
	id, _ := strconv.ParseInt(lastPathPart(r.URL.Path), 10, 64)
	if id <= 0 {
		httputil.Error(w, 400, "invalid_id", "评论ID无效")
		return
	}
	comment, err := client.GitHub.GetIssueComment(r.Context(), client.RepoOwner, client.RepoName, id)
	if err != nil || comment.User.Login != "" && comment.User.Login != currentGithubLogin(a, r) {
		httputil.Error(w, 403, "forbidden", "只能删除自己的 GitHub 评论")
		return
	}
	if err := client.GitHub.DeleteIssueComment(r.Context(), client.RepoOwner, client.RepoName, id); err != nil {
		httputil.Error(w, 502, "github_error", "无法删除 GitHub 评论")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// appendAstroboxFix accepts a bounded set of corrected staging files encoded as
// base64, commits them to the existing PR head, and optionally posts ABCC_FIXED.
func (a *application) appendAstroboxFix(w http.ResponseWriter, r *http.Request) {
	_, pr, _, _, client, err := a.astroboxReviewContext(r)
	if err != nil {
		httputil.Error(w, 403, "astrobox_unavailable", "无法提交 AstroBox 修正")
		return
	}
	var req struct {
		Files   map[string]string `json:"files"`
		Comment string            `json:"comment"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&req) != nil || len(req.Files) == 0 {
		httputil.Error(w, 400, "invalid_body", "缺少修正文件")
		return
	}
	files := make(map[string][]byte, len(req.Files))
	total := 0
	for name, encoded := range req.Files {
		if !strings.HasPrefix(name, "tmp/") || strings.Contains(name, "..") {
			httputil.Error(w, 400, "invalid_path", "修正文件路径无效")
			return
		}
		data, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil || len(data) > 10<<20 {
			httputil.Error(w, 400, "invalid_file", "修正文件无效")
			return
		}
		total += len(data)
		if total > 30<<20 {
			httputil.Error(w, 413, "payload_too_large", "修正文件过大")
			return
		}
		files[name] = data
	}
	if err := client.AppendPRFiles(r.Context(), client.GitHub.Token, pr, "fix: append AstroBox correction", files); err != nil {
		httputil.Error(w, 502, "github_error", "无法追加 AstroBox 修正提交")
		return
	}
	comment := strings.TrimSpace(req.Comment)
	if comment == "" {
		comment = "[ABCC_FIXED]"
	}
	_, _ = client.GitHub.CreateIssueComment(r.Context(), client.RepoOwner, client.RepoName, pr, comment)
	httputil.JSON(w, 200, map[string]any{"status": "external_fixed_waiting", "pr_number": pr})
}

func currentGithubLogin(a *application, r *http.Request) string {
	uid, _ := httputil.AuthenticateUser(r.Context(), a.db, httputil.ExtractBearerToken(r))
	var login string
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT login FROM github_grants WHERE user_uid=$1`, uid).Scan(&login)
	return login
}
