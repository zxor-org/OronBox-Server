package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

)

func (a *application) queryGiteaLatestRelease(ctx context.Context) (*consoleMetadata, string, error) {
	baseURL := strings.TrimRight(a.cfg.Gitea.APIURL, "/")
	repo := a.cfg.Gitea.ConsoleRepo
	if repo == "" {
		repo = "OronBoxCommunity/OronBox-Server-Console"
	}

	relURL := fmt.Sprintf("%s/api/v1/repos/%s/releases/latest", baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", relURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create release request: %w", err)
	}
	if a.cfg.Gitea.ClientSecret != "" {
		req.Header.Set("Authorization", "token "+a.cfg.Gitea.ClientSecret)
	}

	resp, err := giteaSyncClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("query release failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		tagURL := fmt.Sprintf("%s/api/v1/repos/%s/releases/tags/latest", baseURL, repo)
		tagReq, _ := http.NewRequestWithContext(ctx, "GET", tagURL, nil)
		if a.cfg.Gitea.ClientSecret != "" {
			tagReq.Header.Set("Authorization", "token "+a.cfg.Gitea.ClientSecret)
		}
		if tagResp, tagErr := giteaSyncClient.Do(tagReq); tagErr == nil {
			defer tagResp.Body.Close()
			if tagResp.StatusCode >= 200 && tagResp.StatusCode < 300 {
				resp = tagResp
			}
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("gitea release endpoint returned HTTP %d", resp.StatusCode)
	}

	var release struct {
		ID              int64  `json:"id"`
		TagName         string `json:"tag_name"`
		TargetCommitish string `json:"target_commitish"`
		PublishedAt     string `json:"published_at"`
		Assets          []struct {
			ID                 int64  `json:"id"`
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, "", fmt.Errorf("decode release json: %w", err)
	}

	var downloadURL string
	var targetAssetID int64
	var targetAssetSize int64
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".tar.gz") || strings.HasSuffix(asset.Name, ".tgz") {
			downloadURL = asset.BrowserDownloadURL
			targetAssetID = asset.ID
			targetAssetSize = asset.Size
			break
		}
	}
	if downloadURL == "" {
		return nil, "", fmt.Errorf("no .tar.gz asset found in release")
	}

	if parsedBase, err := url.Parse(baseURL); err == nil && parsedBase.Host != "" {
		if parsedDL, err := url.Parse(downloadURL); err == nil {
			if parsedDL.Host != parsedBase.Host {
				parsedDL.Scheme = parsedBase.Scheme
				parsedDL.Host = parsedBase.Host
				downloadURL = parsedDL.String()
			}
		}
	}

	meta := &consoleMetadata{
		Commit:      release.TargetCommitish,
		AssetID:     targetAssetID,
		Size:        targetAssetSize,
		PublishedAt: release.PublishedAt,
	}
	return meta, downloadURL, nil
}

func (a *application) syncConsole(ctx context.Context) (res *consoleSyncResult, err error) {
	consoleSyncMu.Lock()
	defer consoleSyncMu.Unlock()

	globalConsoleState.mu.Lock()
	globalConsoleState.Syncing = true
	globalConsoleState.SyncStart = time.Now()
	globalConsoleState.LastError = ""
	globalConsoleState.ErrorDismissed = false
	globalConsoleState.mu.Unlock()

	defer func() {
		globalConsoleState.mu.Lock()
		globalConsoleState.Syncing = false
		if err != nil {
			globalConsoleState.LastError = err.Error()
			globalConsoleState.ErrorDismissed = false
		} else {
			globalConsoleState.LastError = ""
			globalConsoleState.ErrorDismissed = false
		}
		globalConsoleState.mu.Unlock()
	}()

	meta, downloadURL, qErr := a.queryGiteaLatestRelease(ctx)
	if qErr != nil {
		return nil, qErr
	}

	dlReq, reqErr := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if reqErr != nil {
		return nil, fmt.Errorf("create download request: %w", reqErr)
	}
	if a.cfg.Gitea.ClientSecret != "" {
		dlReq.Header.Set("Authorization", "token "+a.cfg.Gitea.ClientSecret)
	}

	dlResp, dlErr := giteaSyncClient.Do(dlReq)
	if dlErr != nil {
		return nil, fmt.Errorf("download asset failed: %w", dlErr)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode < 200 || dlResp.StatusCode >= 300 {
		return nil, fmt.Errorf("asset download returned HTTP %d", dlResp.StatusCode)
	}

	consoleDir := a.cfg.ConsoleDir
	if consoleDir == "" {
		consoleDir = "console"
	}
	absConsoleDir, pErr := filepath.Abs(consoleDir)
	if pErr != nil {
		return nil, pErr
	}
	parentDir := filepath.Dir(absConsoleDir)
	if mErr := os.MkdirAll(parentDir, 0755); mErr != nil {
		return nil, fmt.Errorf("create parent dir: %w", mErr)
	}

	tmpDir, tErr := os.MkdirTemp(parentDir, "console_tmp_*")
	if tErr != nil {
		return nil, fmt.Errorf("create temp dir: %w", tErr)
	}
	defer os.RemoveAll(tmpDir)

	if uErr := untarArchive(dlResp.Body, tmpDir); uErr != nil {
		return nil, fmt.Errorf("untar archive failed: %w", uErr)
	}

	// 若打包在 dist 子目录下，自动上移提升
	if _, err := os.Stat(filepath.Join(tmpDir, "index.html")); os.IsNotExist(err) {
		distIndex := filepath.Join(tmpDir, "dist", "index.html")
		if _, distErr := os.Stat(distIndex); distErr == nil {
			distDir := filepath.Join(tmpDir, "dist")
			entries, _ := os.ReadDir(distDir)
			for _, entry := range entries {
				_ = os.Rename(filepath.Join(distDir, entry.Name()), filepath.Join(tmpDir, entry.Name()))
			}
			_ = os.Remove(distDir)
		}
	}

	// 验证 index.html
	if _, iErr := os.Stat(filepath.Join(tmpDir, "index.html")); os.IsNotExist(iErr) {
		return nil, fmt.Errorf("index.html not found in package")
	}

	meta.SyncedAt = time.Now().UTC()
	metaBytes, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(tmpDir, ".console_info"), metaBytes, 0644)

	// 原子替换覆盖目录，并将上一可用版本保留在 console_prev
	prevDir := absConsoleDir + "_prev"
	_ = os.RemoveAll(prevDir)
	if _, err := os.Stat(absConsoleDir); err == nil {
		if err := os.Rename(absConsoleDir, prevDir); err != nil {
			return nil, fmt.Errorf("backup existing console dir: %w", err)
		}
	}

	if err := os.Rename(tmpDir, absConsoleDir); err != nil {
		if _, bErr := os.Stat(prevDir); bErr == nil {
			_ = os.Rename(prevDir, absConsoleDir)
		}
		return nil, fmt.Errorf("activate new console dir: %w", err)
	}

	// 更新内存状态
	globalConsoleState.mu.Lock()
	globalConsoleState.Current = meta
	globalConsoleState.LastError = ""
	globalConsoleState.mu.Unlock()

	return &consoleSyncResult{
		Synced:   true,
		Message:  "console synced successfully",
		Metadata: meta,
	}, nil
}
