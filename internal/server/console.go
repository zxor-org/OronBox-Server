package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

var consoleSyncMu sync.Mutex

type consoleMetadata struct {
	Commit      string    `json:"commit,omitempty"`
	AssetID     int64     `json:"asset_id"`
	Size        int64     `json:"size,omitempty"`
	PublishedAt string    `json:"published_at,omitempty"`
	SyncedAt    time.Time `json:"synced_at,omitempty"`
}

type consoleState struct {
	mu        sync.RWMutex
	Syncing   bool             `json:"syncing"`
	SyncStart time.Time        `json:"sync_start,omitempty"`
	LastError      string           `json:"last_error,omitempty"`
	ErrorDismissed bool             `json:"error_dismissed,omitempty"`
	Current        *consoleMetadata `json:"current,omitempty"`
	Latest         *consoleMetadata `json:"latest,omitempty"`
}

var globalConsoleState consoleState

var giteaSyncClient = &http.Client{
	Timeout: 60 * time.Second,
}

type consoleSyncResult struct {
	Synced   bool             `json:"synced"`
	Syncing  bool             `json:"syncing,omitempty"`
	Message  string           `json:"message,omitempty"`
	Metadata *consoleMetadata `json:"metadata,omitempty"`
}

func (a *application) consoleRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/console/", http.StatusMovedPermanently)
}

func (a *application) consoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/console" {
		http.Redirect(w, r, "/console/", http.StatusMovedPermanently)
		return
	}

	consoleDir := a.cfg.ConsoleDir
	if consoleDir == "" {
		consoleDir = "console"
	}

	// 静态资源文件（/console/assets/...、favicon 等）优先托管，避免前端模块脚本因凭据剥离被拦截
	rel := strings.TrimPrefix(r.URL.Path, "/console/")
	if strings.HasPrefix(rel, "assets/") || rel == "favicon.svg" {
		cleanRel := filepath.Clean("/" + rel)
		targetFile := filepath.Join(consoleDir, cleanRel)
		absConsole, absErr := filepath.Abs(consoleDir)
		absTarget, tgtErr := filepath.Abs(targetFile)
		if absErr == nil && tgtErr == nil && (strings.HasPrefix(absTarget, absConsole+string(filepath.Separator)) || absTarget == absConsole) {
			if fi, err := os.Stat(targetFile); err == nil && !fi.IsDir() {
				if strings.HasPrefix(rel, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeFile(w, r, targetFile)
				return
			}
		}
	}

	// 1. 门禁鉴权：未登录管理员直接展示内置登录卡片（对齐 legacy UI/UX）
	if !a.isAdminAuthenticated(r) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		a.renderAdminLogin(w, r)
		return
	}

	// 处理显式操作指令
	if r.URL.Query().Get("dismiss") == "1" {
		globalConsoleState.mu.Lock()
		globalConsoleState.ErrorDismissed = true
		globalConsoleState.mu.Unlock()
	}
	if r.URL.Query().Get("sync") == "1" {
		globalConsoleState.mu.Lock()
		if !globalConsoleState.Syncing {
			globalConsoleState.Syncing = true
			globalConsoleState.SyncStart = time.Now()
			globalConsoleState.LastError = ""
			globalConsoleState.ErrorDismissed = false
			go func() {
				_, _ = a.syncConsole(context.Background())
			}()
		}
		globalConsoleState.mu.Unlock()
		a.renderConsoleSyncing(w, r)
		return
	}

	// 2. 状态检查：如果正在同步前端，展示“正在同步前端”过渡页
	globalConsoleState.mu.RLock()
	syncing := globalConsoleState.Syncing
	lastErr := globalConsoleState.LastError
	dismissed := globalConsoleState.ErrorDismissed
	globalConsoleState.mu.RUnlock()

	if syncing {
		a.renderConsoleSyncing(w, r)
		return
	}

	// 3. 检查本地前端目录及回退版本
	indexPath := filepath.Join(consoleDir, "index.html")

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		// 检查是否有上一版本备份可供自动回退
		prevDir := consoleDir + "_prev"
		if _, pErr := os.Stat(filepath.Join(prevDir, "index.html")); pErr == nil {
			_ = os.RemoveAll(consoleDir)
			_ = os.Rename(prevDir, consoleDir)
		}
	}

	hasInstalled := false
	if _, err := os.Stat(indexPath); err == nil {
		hasInstalled = true
	}

	// 若同步失败且管理员尚未点击放行：展示失败卡片
	if lastErr != "" && !dismissed {
		if hasInstalled {
			a.renderConsoleError(w, r, lastErr, "返回管理后台", "/console/?dismiss=1")
		} else {
			a.renderConsoleError(w, r, lastErr, "重新尝试同步", "/console/?sync=1")
		}
		return
	}

	if !hasInstalled {
		// 本地完全无历史版本（冷启动阶段）：若配置了 Gitea Token 则自动触发后台同步，并先输出过渡页
		if a.cfg.Gitea.ClientSecret != "" {
			globalConsoleState.mu.Lock()
			if !globalConsoleState.Syncing {
				globalConsoleState.Syncing = true
				globalConsoleState.SyncStart = time.Now()
				globalConsoleState.LastError = ""
				globalConsoleState.ErrorDismissed = false
				go func() {
					_, _ = a.syncConsole(context.Background())
				}()
			}
			globalConsoleState.mu.Unlock()

			a.renderConsoleSyncing(w, r)
			return
		}

		// 未配置 Token 且无缓存：返回 404
		http.NotFound(w, r)
		return
	}

	// 4. 静态托管与 SPA History 回退
	rel = strings.TrimPrefix(r.URL.Path, "/console/")
	if rel == "" || rel == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, indexPath)
		return
	}

	cleanRel := filepath.Clean("/" + rel)
	targetFile := filepath.Join(consoleDir, cleanRel)

	absConsole, absErr := filepath.Abs(consoleDir)
	absTarget, tgtErr := filepath.Abs(targetFile)
	if absErr != nil || tgtErr != nil || (!strings.HasPrefix(absTarget, absConsole+string(filepath.Separator)) && absTarget != absConsole) {
		// 严禁路径穿越超出 console 根目录
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, indexPath)
		return
	}

	// 严禁对外提供任何点开头的内部或隐藏文件（如 .console_info）
	for _, part := range strings.Split(filepath.ToSlash(cleanRel), "/") {
		if strings.HasPrefix(part, ".") {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	if fi, err := os.Stat(targetFile); err == nil && !fi.IsDir() {
		if strings.HasPrefix(rel, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, targetFile)
		return
	}

	// SPA 路由回退
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, indexPath)
}

func (a *application) renderAdminLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	_ = defaultTemplates.Render(w, "admin_login", web.AdminLoginPageData{
		Title:        "OronBox 管理后台",
		AuthorizeURL: "/admin/api/auth/bandbbs/authorize",
	})
}

func (a *application) renderConsoleSyncing(w http.ResponseWriter, r *http.Request) {
	_ = defaultTemplates.Render(w, "console_syncing", web.ConsoleSyncingPageData{
		Title: "正在同步前端",
	})
}

func (a *application) renderConsoleError(w http.ResponseWriter, r *http.Request, errMsg, buttonLabel, target string) {
	_ = defaultTemplates.Render(w, "console_error", web.ConsoleErrorPageData{
		Title:       "前端同步失败",
		Error:       errMsg,
		ButtonLabel: buttonLabel,
		Target:      target,
	})
}

func (a *application) consoleSyncPost(w http.ResponseWriter, r *http.Request) {
	globalConsoleState.mu.RLock()
	if globalConsoleState.Syncing {
		globalConsoleState.mu.RUnlock()
		jsonResponse(w, http.StatusOK, consoleSyncResult{
			Synced:  false,
			Syncing: true,
			Message: "sync_already_in_progress",
		})
		return
	}
	globalConsoleState.mu.RUnlock()

	// 脱离前端客户端请求生命周期，采用独立的后台上下文和 2 分钟超时，防止客户端中断连带导致服务端同步腰斩
	syncCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := a.syncConsole(syncCtx)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "console_sync_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (a *application) consoleStatusGet(w http.ResponseWriter, r *http.Request) {
	consoleDir := a.cfg.ConsoleDir
	if consoleDir == "" {
		consoleDir = "console"
	}
	indexPath := filepath.Join(consoleDir, "index.html")
	installed := false
	if _, err := os.Stat(indexPath); err == nil {
		installed = true
	}

	globalConsoleState.mu.Lock()
	// 加载当前本地版本元数据（若内存中尚无）
	if globalConsoleState.Current == nil {
		infoPath := filepath.Join(consoleDir, ".console_info")
		if data, err := os.ReadFile(infoPath); err == nil {
			var m consoleMetadata
			if json.Unmarshal(data, &m) == nil {
				globalConsoleState.Current = &m
			}
		}
	}

	// 查更新：同机直连，无需任何缓存，立即向 Gitea 实时查询最新构建信息
	if a.cfg.Gitea.ClientSecret != "" {
		if meta, _, err := a.queryGiteaLatestRelease(r.Context()); err == nil {
			globalConsoleState.Latest = meta
		}
	}

	syncing := globalConsoleState.Syncing
	lastErr := globalConsoleState.LastError
	current := globalConsoleState.Current
	latest := globalConsoleState.Latest
	globalConsoleState.mu.Unlock()

	// 比较是否有更新：通过 commit、published_at 或 asset_id 精确感知，不依赖无意义的 tag
	hasUpdate := false
	if latest != nil {
		if current == nil {
			hasUpdate = true
		} else if latest.Commit != "" && current.Commit != "" && latest.Commit != current.Commit {
			hasUpdate = true
		} else if latest.PublishedAt != "" && current.PublishedAt != "" && latest.PublishedAt != current.PublishedAt {
			hasUpdate = true
		} else if latest.AssetID != 0 && current.AssetID != 0 && latest.AssetID != current.AssetID {
			hasUpdate = true
		}
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"installed":  installed,
		"syncing":    syncing,
		"has_update": hasUpdate,
		"current":    current,
		"latest":     latest,
		"last_error": lastErr,
	})
}

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

const (
	maxTarFileSize   int64 = 50 * 1024 * 1024  // 单个文件最大 50MB
	maxTarTotalBytes int64 = 250 * 1024 * 1024 // 解压总大小最大 250MB
	maxTarFileCount  int   = 5000              // 解压总文件数最大 5000
)

func untarArchive(src io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	cleanDest := filepath.Clean(destDir)
	tr := tar.NewReader(gzr)
	var totalWritten int64
	var fileCount int

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		fileCount++
		if fileCount > maxTarFileCount {
			return fmt.Errorf("archive exceeds maximum file count limit (%d)", maxTarFileCount)
		}

		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			continue
		}
		target := filepath.Join(cleanDest, cleanName)
		if !strings.HasPrefix(target, cleanDest+string(filepath.Separator)) && target != cleanDest {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size > maxTarFileSize {
				return fmt.Errorf("file %q exceeds max size %d bytes", header.Name, maxTarFileSize)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			limitedReader := io.LimitReader(tr, maxTarFileSize+1)
			written, err := io.Copy(f, limitedReader)
			f.Close()
			if err != nil {
				return err
			}
			if written > maxTarFileSize {
				return fmt.Errorf("file %q exceeds max size %d bytes", header.Name, maxTarFileSize)
			}
			totalWritten += written
			if totalWritten > maxTarTotalBytes {
				return fmt.Errorf("archive total uncompressed size exceeds limit of %d bytes", maxTarTotalBytes)
			}
		}
	}
	return nil
}
