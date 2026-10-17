package gitea

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Error struct{ Status int }

func (e Error) Error() string { return fmt.Sprintf("Gitea HTTP %d", e.Status) }

type Client struct {
	Config config.DeviceCatalogConfig
	HTTP   *http.Client
}

func (c Client) Request(ctx context.Context, method, suffix string, in, out any) error {
	parts := strings.Split(c.Config.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid Gitea content repository")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if in == nil {
		raw = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Config.BaseURL, "/")+"/api/v1/repos/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1])+"/"+suffix, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+c.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Gitea content unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Error{res.StatusCode}
	}
	if out == nil {
		return nil
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, (16<<20)+1))
	if err != nil || len(raw) > 16<<20 {
		return fmt.Errorf("Gitea response too large")
	}
	return json.Unmarshal(raw, out)
}
func ValidPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.ContainsAny(p, "\\?#%\x00") && !strings.HasPrefix(p, "../")
}
func (c Client) Head(ctx context.Context) (string, error) {
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := c.Request(ctx, "GET", "branches/"+url.PathEscape(c.Config.Ref), nil, &branch)
	if err == nil && branch.Commit.ID == "" {
		err = fmt.Errorf("empty Gitea branch commit")
	}
	return branch.Commit.ID, err
}
func (c Client) File(ctx context.Context, p, ref string) ([]byte, string, error) {
	if !ValidPath(p) {
		return nil, "", fmt.Errorf("invalid content path")
	}
	var f struct{ SHA, Content, Type string }
	err := c.Request(ctx, "GET", "contents/"+p+"?ref="+url.QueryEscape(ref), nil, &f)
	if err != nil {
		return nil, "", err
	}
	if f.SHA == "" || f.Type != "file" {
		return nil, "", fmt.Errorf("invalid Gitea file")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(f.Content, "\n", ""))
	return raw, f.SHA, err
}

type Change struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	SHA       string `json:"sha,omitempty"`
	Content   string `json:"content,omitempty"`
}

func FileChange(p, sha string, data []byte) Change {
	op := "update"
	if sha == "" {
		op = "create"
	}
	return Change{Operation: op, Path: p, SHA: sha, Content: base64.StdEncoding.EncodeToString(data)}
}
func (c Client) Commit(ctx context.Context, message string, files []Change) error {
	for _, f := range files {
		if !ValidPath(f.Path) {
			return fmt.Errorf("invalid content path")
		}
	}
	return c.Request(ctx, "POST", "contents", map[string]any{"branch": c.Config.Ref, "message": message, "files": files}, nil)
}
