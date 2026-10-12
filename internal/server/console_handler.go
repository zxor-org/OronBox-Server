package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

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
		httputil.JSON(w, http.StatusOK, consoleSyncResult{
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
		httputil.Error(w, http.StatusBadGateway, "console_sync_failed", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, result)
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

	httputil.JSON(w, http.StatusOK, map[string]any{
		"installed":  installed,
		"syncing":    syncing,
		"has_update": hasUpdate,
		"current":    current,
		"latest":     latest,
		"last_error": lastErr,
	})
}
