package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
)

var creatorGitHubHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (a *App) handleCreatorAstroBox(w http.ResponseWriter, r *http.Request) {
	binding, err := a.creator.AstroBoxPublication(r.Context(), currentUser(r).ID, r.PathValue("review"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	result := map[string]any{
		"publication":  binding,
		"capabilities": map[string]bool{"can_comment": true, "can_edit_comment": true, "can_delete_comment": true, "can_update_submission": false, "can_reopen_submission": false},
	}
	if path, pathErr := a.creatorAstroBoxPath(r); pathErr == nil {
		pullPath := strings.Replace(path, "/issues/", "/pulls/", 1)
		if status, data, requestErr := a.githubCreatorJSON(r, http.MethodGet, pullPath, nil); requestErr == nil && status >= 200 && status < 300 {
			var pull map[string]any
			if json.Unmarshal(data, &pull) == nil {
				result["pull_request"] = map[string]any{"number": pull["number"], "state": pull["state"], "merged": pull["merged"], "updated_at": pull["updated_at"], "html_url": pull["html_url"], "repository": binding["repository"], "changed_files": pull["changed_files"]}
				result["capabilities"] = map[string]bool{"can_comment": pull["state"] == "open", "can_edit_comment": true, "can_delete_comment": true, "can_update_submission": pull["state"] == "open" && pull["merged"] != true, "can_reopen_submission": pull["state"] == "closed" && pull["merged"] != true}
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (a *App) githubCreatorRequest(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	status, data, err := a.githubCreatorJSON(r, method, path, body)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func (a *App) githubCreatorJSON(r *http.Request, method, path string, body []byte) (int, []byte, error) {
	token, _, err := a.githubCreatorToken(r)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), method, strings.TrimRight(a.cfg.GitHub.APIURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := creatorGitHubHTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		if cipher, err := a.store.GitHubAccessTokenCipher(r.Context(), currentUser(r).ID); err == nil {
			if current, err := a.secrets.Decrypt(cipher); err == nil && current == token {
				if err = a.store.RejectGitHubToken(r.Context(), currentUser(r).ID, cipher); err != nil {
					return 0, nil, err
				}
			}
		}
		body, _ := json.Marshal(httpx.ErrorBody("github_reauthorization_required", "请重新授权 GitHub 账号"))
		return http.StatusForbidden, body, nil
	}

	return resp.StatusCode, data, nil
}

func (a *App) githubCreatorToken(r *http.Request) (string, string, error) {
	userID := currentUser(r).ID
	var cipher []byte
	value, err := a.store.GitHubAccessTokenCipher(r.Context(), userID)
	if err != nil {
		return "", "", fmt.Errorf("github grant unavailable: %w", err)
	}
	cipher = value
	scopes, err := a.store.GitHubGrantScopes(r.Context(), userID)
	if err != nil || !config.HasScopes(config.ScopeString(scopes), []string{"public_repo"}) {
		return "", "", fmt.Errorf("GitHub publishing permission is not authorized")
	}
	token, err := a.secrets.Decrypt(cipher)
	if err != nil {
		return "", "", err
	}
	login, err := a.store.GitHubLogin(r.Context(), userID)
	return token, login, err
}

func (a *App) creatorAstroBoxPath(r *http.Request) (string, error) {
	binding, err := a.creator.AstroBoxPublication(r.Context(), currentUser(r).ID, r.PathValue("review"))
	if err != nil {
		return "", err
	}
	repositoryValue, ok := binding["repository"].(string)
	if !ok {
		return "", fmt.Errorf("%w: invalid AstroBox repository binding", creator.ErrInvalid)
	}
	repository := strings.TrimPrefix(repositoryValue, "https://github.com/")
	repository = strings.TrimSuffix(repository, ".git")
	pr, ok := binding["pull_request_number"].(string)
	validRepo := strings.Count(repository, "/") == 1
	for _, part := range strings.Split(repository, "/") {
		if part == "" || strings.IndexFunc(part, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.')
		}) >= 0 {
			validRepo = false
		}
	}
	if _, parseErr := strconv.ParseInt(pr, 10, 64); parseErr != nil || repository == "" || !validRepo || pr == "" {
		return "", fmt.Errorf("%w: incomplete AstroBox publication binding", creator.ErrInvalid)
	}
	return "/repos/" + repository + "/issues/" + pr, nil
}

func (a *App) handleCreatorAstroBoxComments(w http.ResponseWriter, r *http.Request) {
	base, err := a.creatorAstroBoxPath(r)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		_, login, tokenErr := a.githubCreatorToken(r)
		if tokenErr != nil {
			a.writeCreatorError(w, tokenErr)
			return
		}
		var upstreamStatus int
		var upstreamBody []byte
		timeline, err := astrobox.LoadTimeline(base, login, func(path string) ([]byte, error) {
			status, data, err := a.githubCreatorJSON(r, http.MethodGet, path, nil)
			if err != nil {
				return nil, err
			}
			if status < 200 || status >= 300 {
				upstreamStatus, upstreamBody = status, data
				return nil, fmt.Errorf("GitHub review timeline returned HTTP %d", status)
			}
			return data, nil
		})
		if err != nil {
			if upstreamStatus != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(upstreamStatus)
				_, _ = w.Write(upstreamBody)
			} else {
				a.writeCreatorError(w, err)
			}
			return
		}
		if err := a.creator.RecordAstroBoxReview(r.Context(), currentUser(r).ID, r.PathValue("review"), timeline); err != nil {
			a.writeCreatorError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, timeline)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	path := base + "/comments"
	a.githubCreatorRequest(w, r, r.Method, path, body)
}

func (a *App) handleCreatorAstroBoxComment(w http.ResponseWriter, r *http.Request) {
	if _, err := strconv.ParseInt(r.PathValue("comment"), 10, 64); err != nil {
		a.writeCreatorError(w, fmt.Errorf("%w: invalid comment id", creator.ErrInvalid))
		return
	}
	base, err := a.creatorAstroBoxPath(r)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	repositoryPath := strings.SplitN(base, "/issues/", 2)[0]
	commentPath := repositoryPath + "/issues/comments/" + r.PathValue("comment")
	if r.Method == http.MethodPatch || r.Method == http.MethodDelete {
		if err := a.requireOwnGitHubComment(r, commentPath, base); err != nil {
			a.writeCreatorError(w, err)
			return
		}
	}
	a.githubCreatorRequest(w, r, r.Method, commentPath, body)
}

func (a *App) requireOwnGitHubComment(r *http.Request, path, issuePath string) error {
	token, login, err := a.githubCreatorToken(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(a.cfg.GitHub.APIURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := creatorGitHubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub comment is not available")
	}
	var comment struct {
		IssueURL string `json:"issue_url"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&comment); err != nil {
		return err
	}
	if comment.IssueURL != strings.TrimRight(a.cfg.GitHub.APIURL, "/")+issuePath {
		return fmt.Errorf("comment belongs to a different PR")
	}
	if login == "" || comment.User.Login == "" || !strings.EqualFold(login, comment.User.Login) {
		return fmt.Errorf("cannot modify another user's GitHub comment")
	}
	return nil
}

func (a *App) handleCreatorAstroBoxReopen(w http.ResponseWriter, r *http.Request) {
	base, err := a.creatorAstroBoxPath(r)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	state := "requested"
	err = a.creator.RequestAstroBoxReopen(r.Context(), currentUser(r).ID, r.PathValue("review"), func() error {
		pullPath := strings.Replace(base, "/issues/", "/pulls/", 1)
		status, data, err := a.githubCreatorJSON(r, http.MethodGet, pullPath, nil)
		if err != nil {
			return err
		}
		var pull struct {
			State  string `json:"state"`
			Merged bool   `json:"merged"`
		}
		if status != http.StatusOK || json.Unmarshal(data, &pull) != nil {
			return fmt.Errorf("read AstroBox submission: HTTP %d", status)
		}
		if pull.Merged {
			return fmt.Errorf("%w: AstroBox submission is already merged", creator.ErrConflict)
		}
		if pull.State == "open" {
			state = "open"
			return nil
		}
		status, _, err = a.githubCreatorJSON(r, http.MethodPatch, pullPath, []byte(`{"state":"open"}`))
		if err != nil {
			return err
		}
		if status != http.StatusOK && status != http.StatusForbidden && status != http.StatusUnprocessableEntity {
			return fmt.Errorf("reopen AstroBox submission: HTTP %d", status)
		}
		if status == http.StatusOK {
			state = "open"
		}
		body, _ := json.Marshal(map[string]string{"body": "[ABCC_REOPEN] Creator requests another review"})
		status, _, err = a.githubCreatorJSON(r, http.MethodPost, base+"/comments", body)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("request AstroBox review: HTTP %d", status)
		}
		return nil
	})
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"state": state})
}
