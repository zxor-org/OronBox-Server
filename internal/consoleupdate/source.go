package consoleupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/gitea"
)

const consolePackageName = "console.tar.gz"

var consoleCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func (m *Manager) consoleHTTPClient() *http.Client {
	client := http.Client{Timeout: 60 * time.Second}
	if m.state.http != nil {
		client = *m.state.http
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !m.validConsoleURL(req.URL.String()) {
			return fmt.Errorf("Console download redirect outside configured Gitea")
		}
		return nil
	}
	return &client
}

func (m *Manager) validConsoleURL(raw string) bool {
	base, err := url.Parse(m.source.BaseURL)
	if err != nil {
		return false
	}
	target, err := url.Parse(raw)
	return err == nil && target.Scheme == "https" && strings.EqualFold(target.Host, base.Host) && target.Host != "" && target.User == nil && target.Fragment == ""
}

func (m *Manager) consoleAsset(ctx context.Context, endpoint string) (*http.Response, error) {
	if !m.validConsoleURL(endpoint) {
		return nil, fmt.Errorf("invalid Console asset source")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+m.source.Token)
	res, err := m.consoleHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Console asset download unavailable")
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("Console asset returned HTTP %d", res.StatusCode)
	}
	return res, nil
}

func (m *Manager) checkConsole(ctx context.Context, force bool) (result error) {
	m.state.check.Lock()
	defer m.state.check.Unlock()
	m.state.mu.RLock()
	fresh := time.Since(m.state.lastCheck) < 5*time.Minute
	lastError := m.state.checkError
	m.state.mu.RUnlock()
	if fresh && !force {
		if lastError != "" {
			return errors.New(lastError)
		}
		return nil
	}
	defer func() {
		m.state.mu.Lock()
		m.state.lastCheck = time.Now()
		m.state.checkError = ""
		if result != nil {
			m.state.checkError = result.Error()
		}
		m.state.mu.Unlock()
	}()
	cfg := m.source
	cfg.Repository = m.repository
	if cfg.Repository == "" {
		cfg.Repository = "OronBoxCommunity/OronBox-Server-Console"
	}
	if !m.validConsoleURL(strings.TrimRight(cfg.BaseURL, "/") + "/api/v1") {
		return fmt.Errorf("Console requires a configured HTTPS Gitea source")
	}
	client := gitea.Client{Config: cfg, HTTP: m.consoleHTTPClient()}
	var release struct {
		ID         int64  `json:"id"`
		Tag        string `json:"tag_name"`
		Target     string `json:"target_commitish"`
		Published  string `json:"published_at"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := client.Request(ctx, "GET", "releases/latest", nil, &release); err != nil {
		return err
	}
	if release.ID <= 0 || release.Draft {
		return fmt.Errorf("Console release not found or in draft state")
	}
	var packageURL, checksumURL string
	var assetID, size int64
	for _, asset := range release.Assets {
		if asset.Name != consolePackageName && asset.Name != consolePackageName+".sha256" {
			continue
		}
		if asset.ID <= 0 || !m.validConsoleURL(asset.URL) {
			return fmt.Errorf("invalid Console release attachment")
		}
		if asset.Name == consolePackageName {
			if packageURL != "" || asset.Size <= 0 || asset.Size > 50<<20 {
				return fmt.Errorf("invalid or duplicate Console package")
			}
			packageURL, assetID, size = asset.URL, asset.ID, asset.Size
		} else {
			if checksumURL != "" || asset.Size <= 0 || asset.Size > 1024 {
				return fmt.Errorf("invalid or duplicate Console checksum")
			}
			checksumURL = asset.URL
		}
	}
	if packageURL == "" {
		return fmt.Errorf("Console release lacks package attachment")
	}
	checksum := ""
	if checksumURL != "" {
		res, err := m.consoleAsset(ctx, checksumURL)
		if err == nil {
			defer res.Body.Close()
			raw, _ := io.ReadAll(io.LimitReader(res.Body, 1025))
			fields := strings.Fields(string(raw))
			if len(fields) >= 1 && len(fields[0]) == 64 {
				checksum = strings.ToLower(fields[0])
			}
		}
	}
	commit := release.Target
	if commit == "" {
		commit = release.Tag
	}
	m.state.mu.Lock()
	m.state.latest = &Metadata{Commit: commit, Tag: release.Tag, ReleaseID: release.ID, AssetID: assetID, Size: size, PublishedAt: release.Published, SHA256: checksum}
	m.state.download = packageURL
	m.state.mu.Unlock()
	return nil
}
