package astrobox

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type repo struct {
	Owner  string
	Name   string
	Branch string
}

type catalogSnapshot struct {
	Fork     repo
	Catalog  []byte
	FileSHA  string
	Commit   string
	ForkBase string
}

func (c *Client) mainResourceBranch(ctx context.Context, token string, target repo) (repo, error) {
	if target.Branch == "main" {
		return target, nil
	}
	if _, err := c.refSHA(ctx, token, target.Owner, target.Name, "main"); err != nil {
		status, known := githubStatus(err)
		if !known || (status != 404 && status != 409) {
			return repo{}, err
		}
		sha, err := c.refSHA(ctx, token, target.Owner, target.Name, target.Branch)
		if err != nil {
			return repo{}, err
		}
		if err = c.createRef(ctx, token, target.Owner, target.Name, "main", sha); err != nil {
			if _, checkErr := c.refSHA(ctx, token, target.Owner, target.Name, "main"); checkErr != nil {
				return repo{}, err
			}
		}
	}
	target.Branch = "main"
	return target, nil
}

func (c *Client) updateResourceAbout(ctx context.Context, token string, target repo, itemID, restype string) {
	base := fmt.Sprintf("%s/repos/%s/%s", c.api, target.Owner, target.Name)
	topic := restype
	if topic == "quick_app" {
		topic = "quickapp"
	}
	if _, err := c.request(ctx, token, http.MethodPut, base+"/topics", map[string]any{"names": []string{"astrobox-resource", topic}}, nil); err != nil {
		slog.Warn("AstroBox repository topics update failed", "owner", target.Owner, "repo", target.Name)
	}
	homepage := "https://abox.run/open?source=resv2&id=" + url.QueryEscape(itemID) + "&provider=OfficialV2"
	if _, err := c.request(ctx, token, http.MethodPatch, base, map[string]string{"homepage": homepage}, nil); err != nil {
		slog.Warn("AstroBox repository homepage update failed", "owner", target.Owner, "repo", target.Name)
	}
}

// prepareCatalogSnapshot reads a stable upstream catalog snapshot and a
// readable fork base for the submission branch. Fork-main synchronization is
// best-effort: AstroBox Creator Console continues from the fork's current HEAD
// when GitHub refuses to update the default ref, because v2 submissions carry
// their catalog row and upstream base commit in tmp/.../request.json instead of
// rewriting the fork catalog itself.
func (c *Client) prepareCatalogSnapshot(ctx context.Context, token string) (catalogSnapshot, error) {
	fork, err := c.ensureFork(ctx, token)
	if err != nil {
		return catalogSnapshot{}, fmt.Errorf("prepare AstroBox catalog fork: %w", err)
	}
	// Match AstroBox Creator Console: default-branch synchronization is a
	// convenience, not a prerequisite for creating the staging branch.
	_ = c.syncFork(ctx, token, fork)
	for attempt := 0; attempt < 3; attempt++ {
		commit, err := c.refSHA(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.RepoBranch)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("read AstroBox upstream branch: %w", err)
		}
		forkBase, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("read AstroBox catalog fork branch: %w", err)
		}
		catalog, fileSHA, err := c.getContent(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.CatalogPath, commit)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("read AstroBox catalog: %w", err)
		}
		latestCommit, err := c.refSHA(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.RepoBranch)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("recheck AstroBox upstream branch: %w", err)
		}
		if latestCommit != commit {
			continue
		}
		return catalogSnapshot{Fork: fork, Catalog: catalog, FileSHA: fileSHA, Commit: commit, ForkBase: forkBase}, nil
	}
	return catalogSnapshot{}, fmt.Errorf("AstroBox catalog changed while preparing a submission; retry the publication")
}

func (c *Client) publicationRepository(ctx context.Context, token, owner, name, description string) (repo, error) {
	if owner == "" {
		return c.ensureRepo(ctx, token, name, description)
	}
	var existing struct {
		Name   string `json:"name"`
		Branch string `json:"default_branch"`
		Owner  struct {
			Login string `json:"login"`
		} `json:"owner"`
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s", c.api, owner, name), nil, &existing)
	if err != nil {
		return repo{}, fmt.Errorf("read bound AstroBox repository: %w", err)
	}
	if !existing.Permissions.Push || existing.Owner.Login == "" || existing.Name == "" || existing.Branch == "" {
		return repo{}, fmt.Errorf("bound AstroBox repository is not writable")
	}
	return repo{Owner: existing.Owner.Login, Name: existing.Name, Branch: existing.Branch}, nil
}

func (c *Client) ensureRepo(ctx context.Context, token, name, description string) (repo, error) {
	var response struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	status, err := c.request(ctx, token, http.MethodPost, c.api+"/user/repos", map[string]any{"name": name, "description": description, "auto_init": true}, &response)
	if err != nil && status != 422 {
		return repo{}, err
	}
	if status == 422 {
		user, err := c.currentUser(ctx, token)
		if err != nil {
			return repo{}, err
		}
		_, err = c.request(ctx, token, http.MethodGet, c.api+"/repos/"+user+"/"+name, nil, &response)
		if err != nil {
			return repo{}, err
		}
	}
	if response.DefaultBranch == "" {
		response.DefaultBranch = "main"
	}
	return repo{Owner: response.Owner.Login, Name: response.Name, Branch: response.DefaultBranch}, nil
}

func forkForBranch(value repo, branch string) repo {
	value.Branch = branch
	return value
}

func (c *Client) ensureFork(ctx context.Context, token string) (repo, error) {
	var response struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	status, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/forks", c.api, c.cfg.RepoOwner, c.cfg.RepoName), map[string]any{}, &response)
	if err != nil && status != http.StatusUnprocessableEntity {
		return repo{}, err
	}
	if status == http.StatusUnprocessableEntity {
		owner, err := c.currentUser(ctx, token)
		if err != nil {
			return repo{}, err
		}
		if _, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s", c.api, owner, c.cfg.RepoName), nil, &response); err != nil {
			return repo{}, err
		}
	}
	if response.DefaultBranch == "" {
		response.DefaultBranch = c.cfg.RepoBranch
	}
	fork := repo{Owner: response.Owner.Login, Name: response.Name, Branch: response.DefaultBranch}
	// The fork is synchronized immediately before every submission. Creating
	// the temporary branch from the fork HEAD keeps the commit in the fork's
	// object database and avoids GitHub's 404 for an upstream-only SHA.
	var lastErr error
	for i := 0; i < 10; i++ {
		if _, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch); err == nil {
			return fork, nil
		} else {
			status, ok := githubStatus(err)
			if !ok || (status != http.StatusNotFound && status != http.StatusConflict) {
				return repo{}, fmt.Errorf("wait for AstroBox catalog fork: %w", err)
			}
			lastErr = err
		}
		time.Sleep(1500 * time.Millisecond)
	}
	if lastErr != nil {
		return repo{}, fmt.Errorf("AstroBox catalog fork did not become ready: %w", lastErr)
	}
	return repo{}, fmt.Errorf("AstroBox catalog fork did not become ready")
}

func (c *Client) syncFork(ctx context.Context, token string, fork repo) error {
	// Match AstroBox Creator Console: try the regular GitHub sync first, but
	// continue to the explicit ref alignment when GitHub reports a conflict or
	// the fork has diverged.
	_, mergeErr := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/merge-upstream", c.api, fork.Owner, fork.Name), map[string]string{"branch": fork.Branch}, nil)
	upstreamSHA, err := c.refSHA(ctx, token, c.cfg.RepoOwner, c.cfg.RepoName, c.cfg.RepoBranch)
	if err != nil {
		return fmt.Errorf("read AstroBox upstream branch after synchronization: %w", err)
	}
	forkSHA, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch)
	if err != nil {
		if status, ok := githubStatus(err); ok && status == http.StatusNotFound {
			if createErr := c.createRef(ctx, token, fork.Owner, fork.Name, fork.Branch, upstreamSHA); createErr == nil {
				return nil
			} else {
				return fmt.Errorf("read AstroBox fork branch after synchronization: %w; create missing ref: %v", err, createErr)
			}
		}
		return fmt.Errorf("read AstroBox fork branch after synchronization: %w", err)
	}
	if forkSHA == upstreamSHA {
		return nil
	}

	if err := c.forceAlignForkRef(ctx, token, fork, upstreamSHA); err != nil {
		if mergeErr != nil {
			return fmt.Errorf("force-align AstroBox catalog fork after merge-upstream failed: %w", err)
		}
		return fmt.Errorf("force-align AstroBox catalog fork: %w", err)
	}
	alignedSHA, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch)
	if err != nil {
		return fmt.Errorf("verify AstroBox fork alignment: %w", err)
	}
	if alignedSHA != upstreamSHA {
		return fmt.Errorf("AstroBox catalog fork remains stale after force alignment")
	}
	return nil
}

// forceAlignForkRef updates the fork's default branch to the upstream commit.
// GitHub may briefly return 404 while a fork synchronization is being
// materialized, and an old fork may genuinely be missing the configured ref.
// Re-read the ref once before creating it so a transient response does not
// create a duplicate branch, then use the Git refs API as the missing-ref
// recovery path.
func (c *Client) forceAlignForkRef(ctx context.Context, token string, fork repo, sha string) error {
	updateErr := c.updateRef(ctx, token, fork.Owner, fork.Name, fork.Branch, sha)
	if updateErr == nil {
		return nil
	}
	status, ok := githubStatus(updateErr)
	if !ok || status != http.StatusNotFound {
		return updateErr
	}

	if currentSHA, err := c.refSHA(ctx, token, fork.Owner, fork.Name, fork.Branch); err == nil {
		if currentSHA == sha {
			return nil
		}
		if retryErr := c.updateRef(ctx, token, fork.Owner, fork.Name, fork.Branch, sha); retryErr == nil {
			return nil
		} else {
			updateErr = fmt.Errorf("initial update failed: %w; retry failed: %v", updateErr, retryErr)
		}
	} else {
		refStatus, refKnown := githubStatus(err)
		if !refKnown || refStatus != http.StatusNotFound {
			return fmt.Errorf("update fork ref: %w; re-read ref: %v", updateErr, err)
		}
	}

	if err := c.createRef(ctx, token, fork.Owner, fork.Name, fork.Branch, sha); err != nil {
		return fmt.Errorf("update fork ref: %w; create missing ref: %v", updateErr, err)
	}
	return nil
}

func (c *Client) refSHA(ctx context.Context, token, owner, repoName, branch string) (string, error) {
	var response struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	_, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", c.api, owner, repoName, url.PathEscape(branch)), nil, &response)
	return response.Object.SHA, err
}

func (c *Client) createRef(ctx context.Context, token, owner, repoName, branch, sha string) error {
	_, err := c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/git/refs", c.api, owner, repoName), map[string]any{"ref": "refs/heads/" + branch, "sha": sha}, nil)
	return err
}

func sanitize(value string) string {
	value = strings.ToLower(value)
	var output strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			output.WriteRune(r)
		} else {
			output.WriteByte('-')
		}
	}
	return strings.Trim(output.String(), "-")
}

func validateRepoName(value string) error {
	if len(value) > 100 {
		return fmt.Errorf("GitHub repository name cannot exceed 100 characters")
	}
	for index, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if !valid || index == 0 && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return fmt.Errorf("invalid GitHub repository name %q", value)
		}
	}
	last := value[len(value)-1]
	if !(last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z' || last >= '0' && last <= '9') {
		return fmt.Errorf("invalid GitHub repository name %q", value)
	}
	return nil
}
