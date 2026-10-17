package astrobox

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Remove submits a pull request that drops itemID from the catalog index.
// The resource repository itself is left untouched. An item that is not
// listed is a no-op and returns an empty Result.
func (c *Client) Remove(ctx context.Context, token, itemID, name string) (Result, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Result{}, fmt.Errorf("AstroBox item_id is required")
	}
	snapshot, err := c.prepareCatalogSnapshot(ctx, token)
	if err != nil {
		return Result{}, err
	}
	fork, catalog, sha, _, forkBaseSHA := snapshot.Fork, snapshot.Catalog, snapshot.FileSHA, snapshot.Commit, snapshot.ForkBase
	rows, err := csv.NewReader(bytes.NewReader(catalog)).ReadAll()
	if err != nil {
		return Result{}, fmt.Errorf("parse AstroBox catalog: %w", err)
	}
	kept := make([][]string, 0, len(rows))
	found := false
	for index, row := range rows {
		if index > 0 && len(row) > 0 && row[0] == itemID {
			found = true
			continue
		}
		kept = append(kept, row)
	}
	if !found {
		return Result{}, nil
	}
	branch := "oronbox-remove-" + sanitize(itemID)
	var pending []struct {
		URL    string `json:"html_url"`
		Number int    `json:"number"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&head=%s&per_page=100", c.api, c.cfg.RepoOwner, c.cfg.RepoName, url.QueryEscape(fork.Owner+":"+branch))
	if _, err := c.request(ctx, token, http.MethodGet, endpoint, nil, &pending); err != nil {
		return Result{}, err
	}
	if len(pending) > 0 {
		return Result{PullRequest: pending[0].URL, PullRequestNumber: pending[0].Number}, nil
	}
	if err := c.createRef(ctx, token, fork.Owner, fork.Name, branch, forkBaseSHA); err != nil {
		if _, readErr := c.refSHA(ctx, token, fork.Owner, fork.Name, branch); readErr != nil {
			return Result{}, fmt.Errorf("create AstroBox removal branch: %w", err)
		}
	}
	var encodedCatalog bytes.Buffer
	if err := csv.NewWriter(&encodedCatalog).WriteAll(kept); err != nil {
		return Result{}, err
	}
	if _, actualSHA, readErr := c.getContent(ctx, token, fork.Owner, fork.Name, c.cfg.CatalogPath, branch); readErr == nil {
		sha = actualSHA
	} else {
		return Result{}, readErr
	}
	if err := c.putContentWithSHA(ctx, token, fork.Owner, fork.Name, c.cfg.CatalogPath, branch, "Remove "+itemID, encodedCatalog.Bytes(), sha); err != nil {
		return Result{}, err
	}
	title := "[OBCC] Remove " + itemID
	if strings.TrimSpace(name) != "" {
		title = "[OBCC] Remove " + strings.TrimSpace(name)
	}
	body := fmt.Sprintf("## 删除资源\n\n- 资源名称：%s\n- 资源 ID：`%s`\n\n---\n\n此 PR 由 [OronBox 创作者中心](https://oronbox.zxor.org) 提交。Submitted through OronBox Creator Center.\n", name, itemID)
	pr, err := c.createPR(ctx, token, title, fork.Owner+":"+branch, c.cfg.RepoBranch, body)
	if err != nil {
		return Result{}, fmt.Errorf("create AstroBox removal pull request: %w", err)
	}
	return Result{PullRequest: pr.URL, PullRequestNumber: pr.Number}, nil
}

func (c *Client) ClosePullRequest(ctx context.Context, token string, number int) error {
	status, err := c.PullRequest(ctx, token, number)
	if err != nil {
		return err
	}
	if status.State != "open" {
		return nil
	}
	_, err = c.request(ctx, token, http.MethodPatch, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.api, c.cfg.RepoOwner, c.cfg.RepoName, number), map[string]string{"state": "closed"}, nil)
	return err
}
