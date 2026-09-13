package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/creator"
)

var creatorGitHubHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (a *App) handleCreatorCollectionList(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.ListCollections(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) handleCreatorCollectionCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Slug    string               `json:"slug"`
		Name    string               `json:"name"`
		Summary string               `json:"summary"`
		Kind    creator.ResourceKind `json:"kind"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	item, err := a.creator.CreateCollection(r.Context(), currentUser(r).ID, request.Slug, request.Name, request.Summary, request.Kind)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *App) handleCreatorCollection(w http.ResponseWriter, r *http.Request) {
	item, err := a.creator.Collection(r.Context(), currentUser(r).ID, r.PathValue("collection"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleCreatorCollectionUpdate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	item, err := a.creator.UpdateCollectionMetadata(r.Context(), currentUser(r).ID, r.PathValue("collection"), request.Name, request.Summary)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleCreatorCollectionResources(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ResourceIDs              []string `json:"resource_ids"`
		RepresentativeResourceID string   `json:"representative_resource_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.SetCollectionResources(r.Context(), currentUser(r).ID, r.PathValue("collection"), request.RepresentativeResourceID, request.ResourceIDs); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorCollectionDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.DeleteCollection(r.Context(), currentUser(r).ID, r.PathValue("collection")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorRelationships(w http.ResponseWriter, r *http.Request) {
	resourceID := r.PathValue("resource")
	if _, err := a.creator.Workspace(r.Context(), currentUser(r).ID, resourceID); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	collaborators, source, err := a.creator.ResourceRelationships(r.Context(), resourceID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collaborators": collaborators, "source": source})
}

func (a *App) handleCollaborationInvitations(w http.ResponseWriter, r *http.Request) {
	invitations, err := a.creator.CollaborationInvitations(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collaborations_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (a *App) handleCreatorCollaboratorInvite(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BandBBSUserID int64 `json:"bandbbs_user_id"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.InviteCollaborator(r.Context(), currentUser(r).ID, r.PathValue("resource"), request.BandBBSUserID); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCollaboratorAccept(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.AcceptCollaborator(r.Context(), currentUser(r).ID, r.PathValue("resource")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCollaboratorDecline(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := a.creator.RemoveCollaborator(r.Context(), user.ID, r.PathValue("resource"), user.ID); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCollaboratorRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.RemoveCollaborator(r.Context(), currentUser(r).ID, r.PathValue("resource"), r.PathValue("user")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorSource(w http.ResponseWriter, r *http.Request) {
	var request creator.ResourceSource
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.SetResourceSource(r.Context(), currentUser(r).ID, r.PathValue("resource"), request); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorList(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.List(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": items})
}

func (a *App) handleCreatorReviewList(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	page, err := a.creator.ReviewPage(r.Context(), currentUser(r).ID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *App) handleCreatorReview(w http.ResponseWriter, r *http.Request) {
	workspace, err := a.creator.ReviewWorkspace(r.Context(), currentUser(r).ID, r.PathValue("review"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *App) handleCreatorAstroBox(w http.ResponseWriter, r *http.Request) {
	binding, err := a.creator.AstroBoxPublication(r.Context(), currentUser(r).ID, r.PathValue("review"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	result := map[string]any{
		"publication":  binding,
		"capabilities": map[string]bool{"can_comment": true, "can_edit_comment": true, "can_delete_comment": true, "can_update_submission": true},
	}
	if path, pathErr := a.creatorAstroBoxPath(r); pathErr == nil {
		pullPath := strings.Replace(path, "/issues/", "/pulls/", 1)
		if status, data, requestErr := a.githubCreatorJSON(r, http.MethodGet, pullPath, nil); requestErr == nil && status >= 200 && status < 300 {
			var pull map[string]any
			if json.Unmarshal(data, &pull) == nil {
				result["pull_request"] = map[string]any{"number": pull["number"], "state": pull["state"], "updated_at": pull["updated_at"], "html_url": pull["html_url"], "repository": binding["repository"], "changed_files": pull["changed_files"]}
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
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
		all := make([]json.RawMessage, 0)
		for page := 1; page <= 20; page++ {
			status, data, requestErr := a.githubCreatorJSON(r, http.MethodGet, fmt.Sprintf("%s/comments?per_page=100&sort=created&direction=asc&page=%d", base, page), nil)
			if requestErr != nil {
				a.writeCreatorError(w, requestErr)
				return
			}
			if status < 200 || status >= 300 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write(data)
				return
			}
			var items []json.RawMessage
			if json.Unmarshal(data, &items) != nil {
				a.writeCreatorError(w, fmt.Errorf("invalid GitHub comments response"))
				return
			}
			all = append(all, items...)
			if len(items) < 100 {
				break
			}
		}
		writeJSON(w, http.StatusOK, all)
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
		if err := a.requireOwnGitHubComment(r, commentPath); err != nil {
			a.writeCreatorError(w, err)
			return
		}
	}
	a.githubCreatorRequest(w, r, r.Method, commentPath, body)
}

func (a *App) requireOwnGitHubComment(r *http.Request, path string) error {
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
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&comment); err != nil {
		return err
	}
	if login == "" || comment.User.Login == "" || !strings.EqualFold(login, comment.User.Login) {
		return fmt.Errorf("cannot modify another user's GitHub comment")
	}
	return nil
}

func (a *App) handleCreatorCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Slug string               `json:"slug"`
		Name string               `json:"name"`
		Kind creator.ResourceKind `json:"kind"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	workspace, err := a.creator.Create(r.Context(), currentUser(r).ID, request.Slug, request.Name, request.Kind)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (a *App) handleCreatorWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace, err := a.creator.Workspace(r.Context(), currentUser(r).ID, r.PathValue("resource"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

// handleCreatorPublish accepts one zip bundle (manifest.json + payloads) and
// atomically creates the next revision with its review case and publications.
func (a *App) handleCreatorPublish(w http.ResponseWriter, r *http.Request) {
	limit := a.cfg.Limits.UploadMaxBytes + 1
	bundle, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if int64(len(bundle)) > a.cfg.Limits.UploadMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("creator_invalid", "publish bundle exceeds the upload limit"))
		return
	}
	workspace, err := a.creator.Publish(r.Context(), currentUser(r).ID, r.PathValue("resource"), bundle)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (a *App) handleCreatorDraft(w http.ResponseWriter, r *http.Request) {
	limit := a.cfg.Limits.UploadMaxBytes + 1
	bundle, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if int64(len(bundle)) > a.cfg.Limits.UploadMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("creator_invalid", "draft bundle exceeds the upload limit"))
		return
	}
	workspace, err := a.creator.SaveDraft(r.Context(), currentUser(r).ID, r.PathValue("resource"), bundle)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *App) handleCreatorTakedown(w http.ResponseWriter, r *http.Request) {
	a.writeCreatorModeration(w, r, "takedown")
}

func (a *App) handleCreatorRestore(w http.ResponseWriter, r *http.Request) {
	a.writeCreatorModeration(w, r, "restore")
}

func (a *App) writeCreatorModeration(w http.ResponseWriter, r *http.Request, action string) {
	workspace, err := a.creator.SetModeration(r.Context(), currentUser(r).ID, r.PathValue("resource"), action)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *App) handleCreatorDelete(w http.ResponseWriter, r *http.Request) {
	var deleteExternal []string
	for _, provider := range strings.Split(r.URL.Query().Get("delete_external"), ",") {
		if provider = strings.TrimSpace(provider); provider != "" {
			deleteExternal = append(deleteExternal, provider)
		}
	}
	result, err := a.creator.Delete(r.Context(), currentUser(r).ID, r.PathValue("resource"), deleteExternal)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) writeCreatorError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "creator_failed"
	switch {
	case errors.Is(err, creator.ErrNotFound):
		status, code = http.StatusNotFound, "creator_not_found"
	case errors.Is(err, creator.ErrConflict):
		status, code = http.StatusConflict, "creator_conflict"
	case errors.Is(err, creator.ErrInvalid):
		status, code = http.StatusBadRequest, "creator_invalid"
	}
	writeJSON(w, status, errorBody(code, err.Error()))
}

func (a *App) handleCreatorBlob(w http.ResponseWriter, r *http.Request) {
	reader, mediaType, err := a.creator.OpenBlob(r.Context(), currentUser(r).ID, r.PathValue("resource"), r.PathValue("sha256"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, r.PathValue("sha256"), time.Time{}, reader)
}
