package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

type Templates struct {
	t *template.Template
}

type TransitionPageData struct {
	Title       string
	Heading     string
	Description string
	ButtonLabel string
	Target      template.URL
	Auto        bool
	Tone        string
}

func NewTemplates() *Templates {
	return &Templates{
		t: template.Must(template.New("root").Funcs(template.FuncMap{
			"join":           strings.Join,
			"eqs":            func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
			"statusClass":    statusClass,
			"statusLabel":    statusLabel,
			"kindLabel":      kindLabel,
			"reportKind":     reportKind,
			"platformLabel":  platformLabel,
			"targetLabel":    targetLabel,
			"mediaRoleLabel": mediaRoleLabel,
			"dateTime":       dateTime,
			"sub1":           func(value int) int { return value - 1 },
			"add1":           func(value int) int { return value + 1 },
			"containsString": func(values []string, value string) bool {
				for _, item := range values {
					if item == value {
						return true
					}
				}
				return false
			},
		}).Parse(templates)),
	}
}

func (t *Templates) Render(w http.ResponseWriter, name string, data any) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.t.ExecuteTemplate(w, name, data)
}

func statusClass(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "active", "approved", "published", "success", "ok", "closed", "committed", "resolved", "visible", "listed":
		return "success"
	case "pending", "submitted", "reviewing", "receiving", "verifying", "open", "investigating":
		return "warning"
	case "replied", "running":
		return "info"
	case "archived", "superseded", "cancelled", "expired", "used", "dismissed":
		return "neutral"
	case "rejected", "failed", "failure", "aborted", "error", "suspended", "frozen", "delisted":
		return "danger"
	default:
		return "neutral"
	}
}

func statusLabel(value any) string {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	labels := map[string]string{
		"active": "活跃", "archived": "已归档", "pending": "待处理",
		"submitted": "待审核", "approved": "已通过", "rejected": "已拒绝",
		"superseded": "已取代", "running": "处理中", "published": "已发布",
		"reviewing": "外部审核", "failed": "失败", "cancelled": "已取消",
		"open": "待处理", "investigating": "处理中", "replied": "已回复",
		"resolved": "已解决", "dismissed": "已驳回", "closed": "已关闭",
		"visible": "正常", "suspended": "已下架", "frozen": "已冻结",
		"listed": "已上架", "delisted": "已下架",
		"success": "成功", "failure": "失败", "ok": "正常",
		"receiving": "接收中", "verifying": "校验中", "committed": "已提交",
		"expired": "已过期", "used": "已使用", "aborted": "已中止",
	}
	if label, ok := labels[s]; ok {
		return label
	}
	if s == "" {
		return "未知"
	}
	return s
}

func dateTime(value any) string {
	var timestamp time.Time
	switch typed := value.(type) {
	case time.Time:
		timestamp = typed
	case *time.Time:
		if typed != nil {
			timestamp = *typed
		}
	default:
		return fmt.Sprint(value)
	}
	if timestamp.IsZero() {
		return "—"
	}
	return timestamp.Local().Format("2006-01-02 15:04")
}

func kindLabel(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "quickapp":
		return "快应用"
	case "watchface":
		return "表盘"
	case "report", "resource_report":
		return "资源举报"
	case "comment_report":
		return "评论举报"
	case "feedback":
		return "意见反馈"
	default:
		if value == nil || fmt.Sprint(value) == "" {
			return "—"
		}
		return fmt.Sprint(value)
	}
}

func reportKind(value any) bool {
	kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	return kind == "report" || kind == "resource_report" || kind == "comment_report"
}

func platformLabel(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "velaos", "vela_os", "vela":
		return "VelaOS"
	case "zeppos", "zepp_os", "zepp":
		return "Zepp OS"
	default:
		return fallbackLabel(value)
	}
}

func targetLabel(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "oronbox":
		return "OronBox"
	case "bandbbs":
		return "米坛"
	case "astrobox":
		return "AstroBox"
	default:
		return fallbackLabel(value)
	}
}

func mediaRoleLabel(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "preview":
		return "预览图"
	case "icon":
		return "图标"
	case "cover":
		return "封面"
	default:
		return fallbackLabel(value)
	}
}

func fallbackLabel(value any) string {
	if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return "—"
	}
	return fmt.Sprint(value)
}

const templates = `
{{define "head"}}
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <title>{{.Title}} · OronBox</title>
  <link rel="preconnect" href="https://fonts.loli.net">
  <link rel="preconnect" href="https://gstatic.loli.net" crossorigin>
  <link href="https://fonts.loli.net/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200&amp;family=Roboto+Mono&amp;family=Roboto:wght@400;500;700&amp;display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/assets/app.css">
  <script>
  (() => {
    const savedTheme = localStorage.getItem('oronbox_server_theme');
    if (savedTheme === 'dark' || savedTheme === 'light') {
      document.documentElement.classList.add(savedTheme + '-theme');
    }
  })();
  </script>
</head>
<body>
{{end}}

{{define "tail"}}
<script>
(() => {
  const root = document.documentElement;
  const path = location.pathname.replace(/\/$/, '') || '/';
  const drawer = document.querySelector('.nav');
  const overlay = document.querySelector('.drawer-overlay');
  const drawerButton = document.querySelector('[data-drawer-toggle]');
  const desktopDrawer = matchMedia('(min-width: 901px)');
  const setDrawer = (open) => {
    drawer?.classList.toggle('open', open);
    overlay?.classList.toggle('open', open);
    document.body.classList.toggle('drawer-open', open);
    drawerButton?.setAttribute('aria-expanded', String(open));
  };
  drawerButton?.addEventListener('click', () => {
    if (desktopDrawer.matches) {
      document.body.classList.toggle('drawer-collapsed');
      drawerButton.setAttribute('aria-expanded', String(!document.body.classList.contains('drawer-collapsed')));
      return;
    }
    setDrawer(!drawer?.classList.contains('open'));
  });
  overlay?.addEventListener('click', () => setDrawer(false));
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') setDrawer(false);
  });

  const themeButton = document.querySelector('[data-theme-toggle]');
  const prefersDark = matchMedia('(prefers-color-scheme: dark)');
  const isDark = () => root.classList.contains('dark-theme') ||
    (!root.classList.contains('light-theme') && prefersDark.matches);
  const syncThemeIcon = () => {
    const dark = isDark();
    const icon = themeButton?.querySelector('.material-symbols-outlined');
    if (icon) icon.textContent = dark ? 'light_mode' : 'dark_mode';
    themeButton?.setAttribute('aria-label', dark ? '切换到浅色模式' : '切换到深色模式');
  };
  themeButton?.addEventListener('click', () => {
    const next = isDark() ? 'light' : 'dark';
    root.classList.remove('dark-theme', 'light-theme');
    root.classList.add(next + '-theme');
    localStorage.setItem('oronbox_server_theme', next);
    syncThemeIcon();
  });
  syncThemeIcon();

  document.querySelectorAll('[data-nav-path]').forEach((link) => {
    const target = link.dataset.navPath;
    const exact = target === '/admin';
    const aliases = (link.dataset.navAlias || '').split(',').filter(Boolean);
    if ((exact && path === target) || (!exact && path.startsWith(target)) || aliases.some((alias) => path.startsWith(alias))) {
      link.classList.add('active');
      link.setAttribute('aria-current', 'page');
    }
  });

  document.querySelectorAll('[data-copy]').forEach((node) => {
    node.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(node.dataset.copy || node.textContent.trim());
        node.classList.add('copied');
        window.setTimeout(() => node.classList.remove('copied'), 1200);
      } catch (_) {}
    });
  });

  document.querySelectorAll('form').forEach((form) => {
    form.addEventListener('submit', (event) => {
      const submitter = event.submitter;
      if (!submitter) return;
      window.setTimeout(() => {
        submitter.classList.add('submitting');
        submitter.setAttribute('aria-busy', 'true');
      });
    });
  });

  document.querySelectorAll('[data-toast]').forEach((toast) => {
    window.setTimeout(() => {
      toast.classList.add('leaving');
      window.setTimeout(() => toast.remove(), 220);
    }, 3000);
  });

  const confirmDialog = document.querySelector('#confirm-dialog');
  const confirmText = confirmDialog?.querySelector('[data-confirm-text]');
  const confirmAction = confirmDialog?.querySelector('[data-confirm-action]');
  let pendingAction = null;
  document.addEventListener('click', (event) => {
    const action = event.target.closest('[data-confirm]');
    if (!action || action.dataset.confirmed === 'true' || !confirmDialog) return;
    event.preventDefault();
    pendingAction = action;
    if (confirmText) confirmText.textContent = action.dataset.confirm;
    confirmDialog.showModal();
  });
  confirmAction?.addEventListener('click', () => {
    if (!pendingAction) return;
    pendingAction.dataset.confirmed = 'true';
    confirmDialog.close();
    pendingAction.form?.requestSubmit(pendingAction);
    pendingAction = null;
  });
})();
</script>
</body></html>
{{end}}

{{define "admin_nav"}}
<aside class="nav" id="admin-drawer" aria-label="管理后台导航">
  <nav class="nav-content">
    <a class="nav-link" data-nav-path="/admin" href="/admin"><span class="material-symbols-outlined">dashboard</span><span>概览</span></a>
    <a class="nav-link" data-nav-path="/admin/review" href="/admin/review"><span class="material-symbols-outlined">fact_check</span><span>待审核</span></a>
    <a class="nav-link" data-nav-path="/admin/resources" href="/admin/resources"><span class="material-symbols-outlined">inventory_2</span><span>全部资源</span></a>
    <a class="nav-link" data-nav-path="/admin/collections" href="/admin/collections"><span class="material-symbols-outlined">collections_bookmark</span><span>合集审核</span></a>
    <a class="nav-link" data-nav-path="/admin/plugins" href="/admin/plugins"><span class="material-symbols-outlined">extension</span><span>插件管理</span></a>
    <a class="nav-link" data-nav-path="/admin/coins" href="/admin/coins"><span class="material-symbols-outlined">toll</span><span>硬币管理</span></a>
    <a class="nav-link" data-nav-path="/admin/users" href="/admin/users"><span class="material-symbols-outlined">group</span><span>用户</span></a>
    <a class="nav-link" data-nav-path="/admin/comments" href="/admin/comments"><span class="material-symbols-outlined">forum</span><span>评论审核</span></a>
    <a class="nav-link" data-nav-path="/admin/reports" data-nav-alias="/admin/feedback" href="/admin/reports"><span class="material-symbols-outlined">report</span><span>举报与反馈</span></a>
    <a class="nav-link" data-nav-path="/admin/oauth/events" href="/admin/oauth/events"><span class="material-symbols-outlined">timeline</span><span>OAuth 事件</span></a>
    <a class="nav-link" data-nav-path="/admin/oauth/states" href="/admin/oauth/states"><span class="material-symbols-outlined">key</span><span>OAuth States</span></a>
    <a class="nav-link" data-nav-path="/admin/oauth/tickets" href="/admin/oauth/tickets"><span class="material-symbols-outlined">confirmation_number</span><span>登录 Tickets</span></a>
    <a class="nav-link" data-nav-path="/admin/clients" href="/admin/clients"><span class="material-symbols-outlined">devices</span><span>客户端</span></a>
    <a class="nav-link" data-nav-path="/admin/releases" href="/admin/releases"><span class="material-symbols-outlined">new_releases</span><span>客户端版本</span></a>
    <a class="nav-link" data-nav-path="/admin/health" href="/admin/health"><span class="material-symbols-outlined">monitoring</span><span>运行状态</span></a>
    <a class="nav-link" data-nav-path="/admin/audit" href="/admin/audit"><span class="material-symbols-outlined">history</span><span>审计日志</span></a>
    <a class="nav-link" data-nav-path="/admin/settings" href="/admin/settings"><span class="material-symbols-outlined">tune</span><span>设置</span></a>
  </nav>
</aside>
{{end}}

{{define "admin_open"}}
{{template "head" .}}
<a class="skip-link" href="#admin-content">跳到主要内容</a>
<header class="app-header">
  <div class="header-section">
    <button class="icon-button menu-button" type="button" data-drawer-toggle aria-label="切换导航" aria-controls="admin-drawer" aria-expanded="true"><span class="material-symbols-outlined">menu</span></button>
    <a class="header-brand" href="/admin"><span>OronBox</span><span class="brand-badge">SERVER</span></a>
  </div>
  <div class="header-section">
    <button class="icon-button" type="button" data-theme-toggle aria-label="切换主题"><span class="material-symbols-outlined">dark_mode</span></button>
    <form class="header-logout" method="post" action="/admin/logout"><button class="icon-button" type="submit" aria-label="退出登录"><span class="material-symbols-outlined">logout</span></button></form>
  </div>
</header>
<div class="drawer-overlay" aria-hidden="true"></div>
<div class="admin-layout">
  {{template "admin_nav" .}}
  <main class="admin-main" id="admin-content" tabindex="-1">
{{end}}

{{define "admin_close"}}
  </main>
</div>
<dialog class="confirm-dialog" id="confirm-dialog">
  <form method="dialog">
    <div class="dialog-icon"><span class="material-symbols-outlined">warning</span></div>
    <h2>确认操作</h2>
    <p data-confirm-text></p>
    <div class="dialog-actions">
      <button class="outlined-button" value="cancel">取消</button>
      <button class="filled-button" type="button" data-confirm-action>确认</button>
    </div>
  </form>
</dialog>
{{template "tail" .}}
{{end}}

{{define "page_header"}}
<header class="page-header">
  <div><h1 class="page-title">{{.Title}}</h1>{{if .Description}}<p>{{.Description}}</p>{{end}}</div>
</header>
{{end}}

{{define "empty_state"}}
<section class="empty-state">
  <div class="empty-mark">Z</div>
  <h2>{{.Title}}</h2>
  {{if .Description}}<p>{{.Description}}</p>{{end}}
</section>
{{end}}

{{define "admin_login"}}
{{template "head" .}}
<main class="standalone-page">
  <section class="standalone-card login-card">
    <div class="standalone-icon"><span class="material-symbols-outlined">admin_panel_settings</span></div>
    <h1>OronBox 管理后台</h1>
    <p>使用已授权的米坛账号验证管理员身份</p>
    {{if .Error}}<div class="notice danger">登录失败：{{.Error}}</div>{{end}}
    <div class="standalone-actions"><a class="filled-button full-button" href="{{.AuthorizeURL}}"><span>使用米坛账号登录</span><span class="material-symbols-outlined">arrow_forward</span></a></div>
  </section>
</main>
{{template "tail" .}}
{{end}}

{{define "transition_page"}}
{{template "head" .}}
<main class="standalone-page">
  <section class="standalone-card transition-card {{.Tone}}">
    <div class="standalone-icon"><span class="material-symbols-outlined">{{if eqs .Tone "danger"}}error{{else if eqs .Tone "info"}}open_in_new{{else}}check_circle{{end}}</span></div>
    <h1>{{.Heading}}</h1>
    <p>{{.Description}}</p>
    {{if and .Target .ButtonLabel}}<div class="standalone-actions"><a class="filled-button full-button" id="continue-action" href="{{.Target}}">{{.ButtonLabel}}</a></div>{{end}}
    {{if and .Target (not .ButtonLabel)}}<a class="transition-retry" id="continue-action" href="{{.Target}}">没有自动打开？<span>点此重试</span></a>{{end}}
    {{if and .Auto .Target}}<script>window.setTimeout(() => { location.replace({{.Target}}); }, 900);</script>{{end}}
  </section>
</main>
{{template "tail" .}}
{{end}}

{{define "server_home"}}
{{template "head" .}}
<main class="standalone-page">
  <section class="standalone-card server-card">
    <div class="standalone-icon"><span class="material-symbols-outlined">dns</span></div>
    <h1>OronBox Server</h1>
    <p>为 OronBox 提供账号授权、资源服务和创作者工作流</p>
    <div class="standalone-actions"><a class="filled-button full-button" href="/admin">进入管理后台</a></div>
  </section>
</main>
{{template "tail" .}}
{{end}}

{{define "admin_dashboard"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="metrics" aria-label="今日服务指标">
  <article><span>全部资源</span><strong>{{.Stats.ResourcesTotal}}</strong><small>资源工作区</small></article>
  <article><span>已发布</span><strong>{{.Stats.PublishedResources}}</strong><small>OronBox 源可见</small></article>
  <article><span>待审核</span><strong>{{.Stats.PendingReviews}}</strong><small><a href="/admin/review">打开审核队列</a></small></article>
  <article><span>待处理举报</span><strong>{{.Stats.OpenReports}}</strong><small><a href="/admin/reports">进入举报中心</a></small></article>
  <article><span>发布失败</span><strong>{{.Stats.FailedPublications}}</strong><small>外部发布任务</small></article>
  <article><span>OAuth 回调</span><strong>{{.Stats.CallbackOKToday}}</strong><small>今日 {{.Stats.CallbackFailToday}} 次失败</small></article>
</section>
<div class="content-grid dashboard-grid">
  <section class="panel">
    <div class="section-header"><div><h2>最近 OAuth 事件</h2><p>最新的授权、回调和令牌交换结果</p></div><a class="text-link" href="/admin/oauth/events">查看全部</a></div>
    {{template "events_table" .Events}}
  </section>
  <section class="panel">
    <div class="section-header"><div><h2>活跃客户端</h2><p>按版本和平台聚合</p></div><a class="text-link" href="/admin/clients">查看全部</a></div>
    {{template "clients_table" .Clients}}
  </section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_events"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="panel">
  <div class="section-header"><div><h2>最近 200 条事件</h2><p>失败原因和请求耗时可直接在表格中查看</p></div></div>
  {{template "events_table" .Events}}
</section>
{{template "admin_close" .}}
{{end}}

{{define "events_table"}}
<div class="table-wrap"><table>
<thead><tr><th>时间</th><th>Provider</th><th>事件</th><th>结果</th><th>客户端</th><th>平台</th><th>用户 ID</th><th>耗时</th><th>错误</th></tr></thead>
<tbody>{{range .}}<tr>
  <td class="secondary nowrap">{{.CreatedAt}}</td><td>{{.Provider}}</td><td><code>{{.EventType}}</code></td>
  <td><span class="status {{statusClass .Result}}">{{statusLabel .Result}}</span></td>
  <td>{{.AppID}}<span class="cell-note">{{.AppVersion}}</span></td><td>{{.Platform}}</td>
  <td><code>{{if .ProviderUserID}}{{.ProviderUserID}}{{else}}—{{end}}</code></td><td>{{.LatencyMS}} ms</td>
  <td class="error-cell">{{if .ErrorCode}}{{.ErrorCode}}{{else}}—{{end}}{{if .ErrorMessage}}<span class="cell-note">{{.ErrorMessage}}</span>{{end}}</td>
</tr>{{else}}<tr><td class="table-empty" colspan="9">暂无 OAuth 事件</td></tr>{{end}}</tbody>
</table></div>
{{end}}

{{define "admin_states"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="panel">
<div class="section-header"><div><h2>最近 200 条 State</h2><p>授权流程状态和使用情况</p></div></div>
<div class="table-wrap"><table>
<thead><tr><th>State</th><th>Provider</th><th>用途</th><th>状态</th><th>创建</th><th>过期</th><th>客户端</th><th>IP</th></tr></thead>
<tbody>{{range .States}}<tr><td><code class="truncate-code">{{.ID}}</code></td><td>{{.Provider}}</td><td>{{.Purpose}}</td><td>{{if .UsedAt}}<span class="status neutral">已使用</span>{{else}}<span class="status success">有效</span>{{end}}</td><td class="secondary">{{.CreatedAt}}</td><td class="secondary">{{.ExpiresAt}}</td><td>{{.AppID}}<span class="cell-note">{{.Platform}} · {{.AppVersion}}</span></td><td><code>{{.IP}}</code></td></tr>{{else}}<tr><td class="table-empty" colspan="8">暂无 OAuth State</td></tr>{{end}}</tbody>
</table></div></section>
{{template "admin_close" .}}
{{end}}

{{define "admin_tickets"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="panel">
<div class="section-header"><div><h2>最近 200 个 Ticket</h2><p>登录票据生命周期和令牌携带状态</p></div></div>
<div class="table-wrap"><table>
<thead><tr><th>Ticket</th><th>用户</th><th>状态</th><th>携带令牌</th><th>创建</th><th>过期</th><th>客户端</th></tr></thead>
<tbody>{{range .Tickets}}<tr><td><code class="truncate-code">{{.ID}}</code></td><td>{{.UserLabel}}</td><td>{{if .UsedAt}}<span class="status neutral">已使用</span>{{else}}<span class="status success">有效</span>{{end}}</td><td>{{if .HasToken}}是{{else}}否{{end}}</td><td class="secondary">{{.CreatedAt}}</td><td class="secondary">{{.ExpiresAt}}</td><td>{{.AppID}}<span class="cell-note">{{.Platform}}</span></td></tr>{{else}}<tr><td class="table-empty" colspan="7">暂无登录 Ticket</td></tr>{{end}}</tbody>
</table></div></section>
{{template "admin_close" .}}
{{end}}

{{define "admin_clients"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="panel">
  <div class="section-header"><div><h2>客户端版本分布</h2><p>数据来自 OAuth 服务请求</p></div></div>
  {{template "clients_table" .Clients}}
</section>
{{template "admin_close" .}}
{{end}}

{{define "clients_table"}}
<div class="table-wrap"><table>
<thead><tr><th>App ID</th><th>平台</th><th>版本</th><th>构建</th><th>请求</th><th>成功</th><th>失败</th><th>最后出现</th></tr></thead>
<tbody>{{range .}}<tr><td><code>{{.AppID}}</code></td><td>{{.Platform}}</td><td>{{.AppVersion}}</td><td>{{.AppBuild}}</td><td>{{.RequestCount}}</td><td class="positive">{{.SuccessCount}}</td><td class="negative">{{.FailureCount}}</td><td class="secondary nowrap">{{.LastSeen}}</td></tr>{{else}}<tr><td class="table-empty" colspan="8">暂无客户端数据</td></tr>{{end}}</tbody>
</table></div>
{{end}}

{{define "admin_settings"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<div class="content-grid settings-grid">
  <section class="panel"><div class="section-header"><div><h2>BandBBS OAuth</h2><p>用户身份和米坛发布授权</p></div></div><dl class="settings">
    <dt>Client ID</dt><dd><code>{{.Config.BandBBS.ClientID}}</code></dd>
    <dt>Client Secret</dt><dd><span class="status {{if eqs .BandBBSSecretState "已配置"}}success{{else}}danger{{end}}">{{.BandBBSSecretState}}</span></dd>
    <dt>Redirect URI</dt><dd><code>{{.Config.BandBBS.RedirectURI}}</code></dd>
    <dt>登录 Scopes</dt><dd>{{join .Config.BandBBS.Scopes " "}}</dd>
    <dt>发布 Scopes</dt><dd>{{join .Config.BandBBS.PublishScopes " "}}</dd>
  </dl></section>
  <section class="panel"><div class="section-header"><div><h2>GitHub OAuth</h2><p>AstroBox 发布授权</p></div></div><dl class="settings">
    <dt>Client ID</dt><dd><code>{{.Config.GitHub.ClientID}}</code></dd>
    <dt>Client Secret</dt><dd><span class="status {{if eqs .GitHubSecretState "已配置"}}success{{else}}danger{{end}}">{{.GitHubSecretState}}</span></dd>
    <dt>Redirect URI</dt><dd><code>{{.Config.GitHub.RedirectURI}}</code></dd>
    <dt>Scopes</dt><dd>{{join .Config.GitHub.Scopes " "}}</dd>
  </dl></section>
  <section class="panel span-2"><div class="section-header"><div><h2>服务信息</h2></div></div><dl class="settings">
    <dt>Public URL</dt><dd><code>{{.Config.PublicURL}}</code></dd>
    <dt>服务版本</dt><dd>{{.Config.Version}} {{.Config.Commit}}</dd>
  </dl></section>
  <section class="panel span-2"><div class="section-header"><div><h2>资源内容标签</h2><p>作者可多选，审核员确认；系数 1.00 不改变推荐权重</p></div></div>
    <div class="stack-list">{{range .ResourceAttributes}}<article class="stack-row"><form method="post" action="/admin/resource-attributes" class="filter-bar embedded"><input type="hidden" name="id" value="{{.ID}}"><label><span>中文名称</span><input name="name_zh" value="{{.NameZH}}" required></label><label><span>英文名称</span><input name="name_en" value="{{.NameEN}}"></label><label><span>系数</span><input name="coefficient" type="number" min="0.0001" max="10" step="0.01" value="{{.Coefficient}}" required></label><label><span>顺序</span><input name="position" type="number" value="{{.Position}}"></label><label><span>启用</span><input name="enabled" type="checkbox" {{if .Enabled}}checked{{end}}></label><button class="outlined-button" type="submit">保存</button></form><form method="post" action="/admin/resource-attributes/{{.ID}}/delete"><button class="outlined-button danger" type="submit" data-confirm="确定停用这个标签吗？历史资源上的标签会保留">删除</button></form></article>{{end}}</div>
    <form method="post" action="/admin/resource-attributes"><div class="section-header"><div><h3>新建标签</h3></div></div><div class="filter-bar embedded"><label><span>标识</span><input name="id" pattern="[a-z0-9][a-z0-9_-]{0,63}" required placeholder="custom_tag"></label><label><span>中文名称</span><input name="name_zh" required></label><label><span>英文名称</span><input name="name_en"></label><label><span>系数</span><input name="coefficient" type="number" min="0.0001" max="10" step="0.01" value="1.00" required></label><label><span>顺序</span><input name="position" type="number" value="100"></label><input type="hidden" name="enabled" value="on"><button class="filled-button" type="submit">新建标签</button></div></form>
  </section>
  <section class="panel span-2"><div class="section-header"><div><h2>发布公告</h2><p>公告会在用户进入应用时展示</p></div></div><form method="post" action="/admin/announcements"><label><span>标题</span><input name="title" required maxlength="200"></label><label><span>正文</span><textarea name="body" rows="6" required></textarea></label><div class="actions"><button class="filled-button" type="submit">发布公告</button></div></form></section>
  <section class="panel span-2"><div class="section-header"><div><h2>公告管理</h2><p>公告原始记录不会自动过期，只能在这里手动删除</p></div></div><div class="stack-list">{{range .Announcements}}<article class="stack-row"><div><strong>{{.Title}}</strong><span class="cell-note">{{dateTime .PublishedAt}}</span><p>{{.Body}}</p></div><form method="post" action="/admin/announcements/{{.ID}}/delete"><button class="outlined-button danger" type="submit" data-confirm="确定删除这条公告吗？相关用户通知也会一并删除">删除</button></form></article>{{else}}<p class="muted">尚未发布公告</p>{{end}}</div></section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_releases"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>客户端版本</h1><p>发布版本并维护各平台、架构和渠道的更新信息</p></div></header>
<div class="content-grid">
<section class="panel"><div class="section-header"><div><h2>发布新版本</h2></div></div><form method="post" action="/admin/releases">
<label><span>版本号</span><input name="version" required placeholder="1.2.3"></label><label><span>渠道</span><select name="channel"><option value="stable">stable</option><option value="beta">beta</option><option value="nightly">nightly</option></select></label>
<label><span>平台</span><select name="platform"><option value="all">全部</option><option value="android">Android</option><option value="linux">Linux</option><option value="windows">Windows</option><option value="macos">macOS</option><option value="web">Web</option></select></label><label><span>架构</span><input name="arch" value="all" placeholder="all / x64 / arm64"></label>
<label><span>最低可用版本</span><input name="minimum_version" placeholder="留空表示不强制更新"></label><label><span>下载地址模板</span><input name="download_url" required placeholder="支持 {version} {platform} {arch} {channel}"></label>
<label><span>中文更新说明</span><textarea name="notes_zh" rows="5"></textarea></label><label><span>英文更新说明</span><textarea name="notes_en" rows="5"></textarea></label><div class="actions"><button class="filled-button" type="submit">发布版本</button></div></form></section>
<section class="panel"><div class="section-header"><div><h2>发布历史</h2></div></div><div class="table-wrap"><table><thead><tr><th>版本</th><th>渠道</th><th>目标</th><th>最低版本</th><th>发布时间</th></tr></thead><tbody>{{range .Items}}<tr><td><strong>{{.Version}}</strong></td><td>{{.Channel}}</td><td>{{.Platform}} / {{.Arch}}</td><td>{{if .MinimumVersion}}{{.MinimumVersion}}{{else}}—{{end}}</td><td>{{dateTime .PublishedAt}}</td></tr>{{else}}<tr><td colspan="5" class="table-empty">尚未发布客户端版本</td></tr>{{end}}</tbody></table></div></section>
</div>{{template "admin_close" .}}
{{end}}

{{define "admin_health"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="metrics system-metrics" aria-label="系统运行指标">
  <article><span>有效 OAuth State</span><strong>{{.Stats.ActiveStates}}</strong><small>等待授权回调</small></article>
  <article><span>有效登录 Ticket</span><strong>{{.Stats.ActiveTickets}}</strong><small>等待客户端交换</small></article>
  <article><span>今日刷新成功</span><strong>{{.Stats.RefreshOKToday}}</strong><small>{{.Stats.RefreshFailToday}} 次失败</small></article>
  <article><span>Scope 异常</span><strong>{{.Stats.ScopeMismatchToday}}</strong><small>今日检测结果</small></article>
</section>
<div class="content-grid settings-grid">
  <section class="panel"><div class="section-header"><div><h2>数据库</h2></div><span class="status {{if eqs .DBStatus "ok"}}success{{else}}danger{{end}}">{{if eqs .DBStatus "ok"}}正常{{else}}异常{{end}}</span></div><p class="diagnostic">{{.DBStatus}}</p></section>
  <section class="panel"><div class="section-header"><div><h2>进程</h2></div><span class="status success">运行中</span></div><dl class="settings compact"><dt>启动时间</dt><dd>{{.Stats.StartedAt}}</dd></dl></section>
  <section class="panel span-2"><div class="section-header"><div><h2>维护</h2><p>在单个事务中清理过期 OAuth State、登录 Ticket、后台会话和系统消息；公告原始记录不会被清理</p></div></div><form method="post" action="/admin/cleanup"><button class="outlined-button" type="submit">立即清理过期记录</button></form></section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_audit"}}
{{template "admin_open" .}}
{{template "page_header" .}}
<section class="panel"><div class="section-header"><div><h2>最近 200 条审计记录</h2><p>管理员操作和服务端处置结果</p></div></div>
<div class="table-wrap"><table>
<thead><tr><th>时间</th><th>账号</th><th>动作</th><th>结果</th><th>IP</th><th>消息</th></tr></thead>
<tbody>{{range .Logs}}<tr><td class="secondary nowrap">{{.CreatedAt}}</td><td>{{.Username}}</td><td><code>{{.Action}}</code></td><td><span class="status {{statusClass .Result}}">{{statusLabel .Result}}</span></td><td><code>{{.IP}}</code></td><td class="wrap-cell">{{.Message}}</td></tr>{{else}}<tr><td class="table-empty" colspan="6">暂无审计记录</td></tr>{{end}}</tbody>
</table></div></section>
{{template "admin_close" .}}
{{end}}

{{define "admin_review"}}
{{template "admin_open" .}}
{{$attributeDefinitions := .Attributes}}
<header class="page-header"><div><h1>待审核</h1><p>审核提交到 OronBox 源的新资源和修订</p></div><span class="count-badge">{{len .Items}} 项</span></header>
{{if .Decided}}<div class="notice success toast-notice" data-toast>审核决定已保存</div>{{end}}
{{range .Items}}
{{$reviewItem := .}}
<article class="review-card">
  <header class="review-summary">
    <div><div class="title-line"><h2>{{.Name}}</h2><span class="status warning">待审核</span></div><p>{{.Summary}}</p><a class="row-action" href="/admin/resources/{{.ResourceID}}">打开完整资源详情</a></div>
    <dl class="review-meta"><div><dt>创作者</dt><dd>{{.Owner}}</dd></div><div><dt>修订</dt><dd>#{{.RevisionNo}}</dd></div><div><dt>提交时间</dt><dd>{{.SubmittedAt}}</dd></div><div><dt>发布目标</dt><dd>{{if .Targets}}{{join .Targets " · "}}{{else}}OronBox{{end}}</dd></div></dl>
  </header>
  {{if .Media}}<div class="media-gallery review-media">{{range .Media}}<a class="media-preview" href="/admin/blobs/{{.SHA256}}" target="_blank" rel="noopener"><img src="/admin/blobs/{{.SHA256}}" alt="{{mediaRoleLabel .Role}} #{{.Position}}" loading="lazy"><span>{{mediaRoleLabel .Role}} · {{.Width}} × {{.Height}}</span></a>{{end}}</div>{{end}}
  {{if .Artifacts}}<div class="review-downloads">{{range .Artifacts}}<a class="outlined-button" href="/admin/blobs/{{.SHA256}}?download=1&amp;name={{urlquery .OriginalName}}"><span class="material-symbols-outlined">download</span>{{.OriginalName}}</a>{{end}}</div>{{end}}
  <details class="snapshot"><summary>查看完整提交快照</summary><pre>{{.Snapshot}}</pre></details>
  <form method="post" action="/admin/review/{{.RevisionID}}" class="decision-form">
    <fieldset><legend>内容标签</legend><div class="chip-row">{{range $attributeDefinitions}}<label><input type="checkbox" name="attributes" value="{{.ID}}" {{if containsString $reviewItem.Attributes .ID}}checked{{end}}> {{.NameZH}}</label>{{end}}</div></fieldset>
    <label>审核评价<select name="curation_grade"><option value="standard">普通 · 1.00</option><option value="featured">精选 · 1.50</option></select></label>
    <label>审核意见<textarea name="note" rows="4" placeholder="审核结论将完整显示给创作者；退回时请直接说明需要修改的内容"></textarea></label>
    <div class="actions"><button class="filled-button" type="submit" name="decision" value="approve">通过审核</button><button class="outlined-button danger" type="submit" name="decision" value="reject">退回修改</button></div>
  </form>
</article>
{{else}}
<section class="empty-state"><div class="empty-mark">✓</div><h2>审核队列为空</h2><p>当前没有等待处理的资源修订</p></section>
{{end}}
{{template "admin_close" .}}
{{end}}

{{define "admin_resources"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>全部资源</h1><p>查看、筛选和管理 OronBox 源中的资源</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>资源操作已完成</div>{{end}}
<section class="panel list-panel">
<form class="filter-bar resource-filters embedded" method="get" action="/admin/resources">
  <label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="名称、Slug、简介或资源 ID"></label>
  <label><span>创作者</span><input name="owner" value="{{.Query.Owner}}" placeholder="用户名或用户 ID"></label>
  <label><span>类型</span><select name="kind"><option value="">全部</option><option value="quickapp" {{if eqs .Query.Kind "quickapp"}}selected{{end}}>快应用</option><option value="watchface" {{if eqs .Query.Kind "watchface"}}selected{{end}}>表盘</option></select></label>
  <details class="filter-more" {{if or .Query.Moderation .Query.RevisionState .Query.ReviewState .Query.PublicationTarget .Query.PublicationState}}open{{end}}>
    <summary><span class="material-symbols-outlined">tune</span>更多筛选</summary>
    <div class="filter-more-grid">
      <label><span>管理状态</span><select name="moderation"><option value="">全部</option><option value="visible" {{if eqs .Query.Moderation "visible"}}selected{{end}}>正常</option><option value="suspended" {{if eqs .Query.Moderation "suspended"}}selected{{end}}>已下架</option><option value="frozen" {{if eqs .Query.Moderation "frozen"}}selected{{end}}>已冻结</option></select></label>
      <label><span>修订状态</span><select name="revision_state"><option value="">全部</option><option value="submitted" {{if eqs .Query.RevisionState "submitted"}}selected{{end}}>待审核</option><option value="approved" {{if eqs .Query.RevisionState "approved"}}selected{{end}}>已通过</option><option value="rejected" {{if eqs .Query.RevisionState "rejected"}}selected{{end}}>已拒绝</option><option value="superseded" {{if eqs .Query.RevisionState "superseded"}}selected{{end}}>已取代</option></select></label>
      <label><span>审核状态</span><select name="review_state"><option value="">全部</option><option value="pending" {{if eqs .Query.ReviewState "pending"}}selected{{end}}>待处理</option><option value="approved" {{if eqs .Query.ReviewState "approved"}}selected{{end}}>已通过</option><option value="rejected" {{if eqs .Query.ReviewState "rejected"}}selected{{end}}>已拒绝</option><option value="superseded" {{if eqs .Query.ReviewState "superseded"}}selected{{end}}>已取代</option></select></label>
      <label><span>发布目标</span><select name="target"><option value="">全部</option><option value="oronbox" {{if eqs .Query.PublicationTarget "oronbox"}}selected{{end}}>OronBox</option><option value="bandbbs" {{if eqs .Query.PublicationTarget "bandbbs"}}selected{{end}}>米坛</option><option value="astrobox" {{if eqs .Query.PublicationTarget "astrobox"}}selected{{end}}>AstroBox</option></select></label>
      <label><span>发布状态</span><select name="publication_state"><option value="">全部</option><option value="pending" {{if eqs .Query.PublicationState "pending"}}selected{{end}}>等待</option><option value="running" {{if eqs .Query.PublicationState "running"}}selected{{end}}>处理中</option><option value="reviewing" {{if eqs .Query.PublicationState "reviewing"}}selected{{end}}>外部审核</option><option value="published" {{if eqs .Query.PublicationState "published"}}selected{{end}}>已发布</option><option value="failed" {{if eqs .Query.PublicationState "failed"}}selected{{end}}>失败</option><option value="cancelled" {{if eqs .Query.PublicationState "cancelled"}}selected{{end}}>已取消</option></select></label>
      <label><span>排序</span><select name="sort"><option value="updated_desc" {{if eqs .Query.Sort "updated_desc"}}selected{{end}}>最近更新</option><option value="created_desc" {{if eqs .Query.Sort "created_desc"}}selected{{end}}>最近创建</option><option value="updated_asc" {{if eqs .Query.Sort "updated_asc"}}selected{{end}}>最早更新</option><option value="name" {{if eqs .Query.Sort "name"}}selected{{end}}>名称</option><option value="owner" {{if eqs .Query.Sort "owner"}}selected{{end}}>创作者</option></select></label>
    </div>
  </details>
  <div class="filter-actions"><button class="filled-button" type="submit">应用筛选</button><a class="text-link filter-reset" href="/admin/resources">清除</a></div>
</form>
<div class="table-panel"><div class="table-wrap"><table>
<thead><tr><th>资源</th><th>创作者</th><th>类型</th><th>状态</th><th>最新修订</th><th>发布目标</th><th>更新时间</th><th></th></tr></thead>
<tbody>{{range .Items}}<tr>
  <td><a class="resource-name" href="/admin/resources/{{.ID}}">{{.Name}}</a><span class="cell-note"><code>{{.Slug}}</code> · {{.ID}}</span></td>
  <td>{{.Owner}}</td><td>{{kindLabel .Kind}}</td><td><span class="status {{statusClass .ModerationState}}">{{statusLabel .ModerationState}}</span>{{if .ModerationBy}}<span class="cell-note">{{if eqs .ModerationBy "owner"}}创作者下架{{else}}管理员操作{{end}}</span>{{end}}</td>
  <td>{{if .RevisionNo}}#{{.RevisionNo}} · {{statusLabel .RevisionState}}{{else}}未发布{{end}}{{if .ReviewState}}<span class="cell-note">审核：{{statusLabel .ReviewState}}</span>{{end}}</td>
  <td><div class="tag-stack">{{range .Publications}}<span class="status {{statusClass .State}}" title="{{targetLabel .Target}} · {{statusLabel .State}}">{{targetLabel .Target}}</span>{{else}}—{{end}}</div></td><td class="secondary nowrap">{{dateTime .UpdatedAt}}</td>
  <td><a class="row-action" href="/admin/resources/{{.ID}}">查看</a></td>
</tr>{{else}}<tr><td class="table-empty" colspan="8">没有符合筛选条件的资源</td></tr>{{end}}</tbody>
</table></div>
{{if gt .Page.TotalPages 1}}<nav class="pagination" aria-label="资源分页"><span>第 {{.Page.Page}} / {{.Page.TotalPages}} 页</span><div>{{if gt .Page.Page 1}}<a class="outlined-button" href="?page={{sub1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}&owner={{urlquery .Query.Owner}}&kind={{urlquery .Query.Kind}}&moderation={{urlquery .Query.Moderation}}&revision_state={{urlquery .Query.RevisionState}}&review_state={{urlquery .Query.ReviewState}}&target={{urlquery .Query.PublicationTarget}}&publication_state={{urlquery .Query.PublicationState}}&sort={{urlquery .Query.Sort}}">上一页</a>{{end}}{{if lt .Page.Page .Page.TotalPages}}<a class="outlined-button" href="?page={{add1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}&owner={{urlquery .Query.Owner}}&kind={{urlquery .Query.Kind}}&moderation={{urlquery .Query.Moderation}}&revision_state={{urlquery .Query.RevisionState}}&review_state={{urlquery .Query.ReviewState}}&target={{urlquery .Query.PublicationTarget}}&publication_state={{urlquery .Query.PublicationState}}&sort={{urlquery .Query.Sort}}">下一页</a>{{end}}</div></nav>{{end}}
</div>
</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_resource_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/resources">← 全部资源</a><div class="title-line"><h1>{{.Item.Name}}</h1><span class="status {{statusClass .Item.ModerationState}}">{{statusLabel .Item.ModerationState}}</span></div><p>{{.Item.Owner}} · <code>{{.Item.Slug}}</code></p></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>资源状态已更新</div>{{end}}
<div class="content-grid detail-grid">
  <section class="panel"><div class="section-header"><div><h2>资源信息</h2></div></div><dl class="settings">
    <dt>资源 ID</dt><dd><code>{{.Item.ID}}</code></dd><dt>Slug</dt><dd><code>{{.Item.Slug}}</code></dd><dt>创作者</dt><dd>{{.Item.Owner}}<span class="cell-note"><code>{{.Item.OwnerID}}</code></span></dd><dt>平台</dt><dd>{{platformLabel .Item.Platform}}</dd><dt>类型</dt><dd>{{kindLabel .Item.Kind}}</dd><dt>管理状态</dt><dd><span class="status {{statusClass .Item.ModerationState}}">{{statusLabel .Item.ModerationState}}</span>{{if .Item.ModerationBy}}<span class="cell-note">{{if eqs .Item.ModerationBy "owner"}}创作者下架{{else}}管理员操作{{end}}{{if .Item.ModerationAt}} · {{dateTime .Item.ModerationAt}}{{end}}</span>{{end}}</dd>{{if .Item.ModerationReason}}<dt>管理原因</dt><dd>{{.Item.ModerationReason}}</dd>{{end}}<dt>最新修订</dt><dd>{{if .Item.RevisionNo}}#{{.Item.RevisionNo}} · {{statusLabel .Item.RevisionState}}{{else}}未发布{{end}}</dd><dt>创建时间</dt><dd>{{dateTime .Item.CreatedAt}}</dd><dt>更新时间</dt><dd>{{dateTime .Item.UpdatedAt}}</dd>
  </dl></section>
  <section class="panel"><div class="section-header"><div><h2>管理</h2><p>资源操作会写入审计日志</p></div></div><div class="management-actions">
    {{if eqs .Item.ModerationState "frozen"}}
    <form method="post" action="/admin/resources/{{.Item.ID}}/state"><button class="outlined-button" name="action" value="unfreeze">解除冻结</button></form>
    {{else if eqs .Item.ModerationState "suspended"}}
    <form method="post" action="/admin/resources/{{.Item.ID}}/state"><button class="outlined-button" name="action" value="restore">恢复公开</button></form>
    <form method="post" action="/admin/resources/{{.Item.ID}}/state"><input name="reason" placeholder="冻结原因（可选）"><button class="outlined-button danger" name="action" value="freeze" data-confirm="确定冻结这个资源吗？创作者将无法再修改它">冻结资源</button></form>
    {{else}}
    <form method="post" action="/admin/resources/{{.Item.ID}}/state"><input name="reason" placeholder="下架原因（可选）"><button class="outlined-button danger" name="action" value="suspend" data-confirm="确定下架这个资源吗？它将从公开资源列表中隐藏">下架资源</button></form>
    <form method="post" action="/admin/resources/{{.Item.ID}}/state"><input name="reason" placeholder="冻结原因（可选）"><button class="outlined-button danger" name="action" value="freeze" data-confirm="确定冻结这个资源吗？创作者将无法再修改它">冻结资源</button></form>
    {{end}}
    {{if eq .Item.RevisionNo 0}}<form method="post" action="/admin/resources/{{.Item.ID}}/state"><button class="outlined-button danger" name="action" value="delete" data-confirm="确定永久删除这个从未发布的资源吗？">永久删除</button></form>{{end}}
  </div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>发布状态</h2></div></div><div class="stack-list">{{range .Publications}}<article class="stack-row"><div><strong>{{targetLabel .Target}}</strong><span class="cell-note">尝试 {{.Attempts}} 次 · {{dateTime .UpdatedAt}}</span>{{if .ExternalURL}}<a class="cell-note" href="{{.ExternalURL}}" target="_blank" rel="noopener noreferrer">打开外部页面 ↗</a>{{end}}</div><span class="status {{statusClass .State}}">{{statusLabel .State}}</span>{{if .ErrorMessage}}<p class="row-error">{{.ErrorMessage}}</p>{{end}}</article>{{else}}<p class="muted">没有发布记录</p>{{end}}</div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>资源文件</h2><p>下载并实际检查创作者提交的安装包</p></div></div><div class="table-wrap"><table><thead><tr><th>文件</th><th>格式</th><th>Package ID</th><th>版本</th><th>设备</th><th></th></tr></thead><tbody>{{range .Artifacts}}<tr><td>{{.OriginalName}}</td><td><code>{{.PackageFormat}}</code></td><td><code>{{.PackageID}}</code></td><td>{{.Version}}</td><td>{{join .Devices " · "}}</td><td><a class="row-action" href="/admin/blobs/{{.SHA256}}?download=1&amp;name={{urlquery .OriginalName}}">下载</a></td></tr>{{else}}<tr><td class="table-empty" colspan="6">没有资源文件</td></tr>{{end}}</tbody></table></div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>预览与媒体</h2><p>点击图片可查看原图</p></div></div>{{if .Media}}<div class="media-gallery">{{range .Media}}<a class="media-preview" href="/admin/blobs/{{.SHA256}}" target="_blank" rel="noopener"><img src="/admin/blobs/{{.SHA256}}" alt="{{mediaRoleLabel .Role}} #{{.Position}}" loading="lazy"><span>{{mediaRoleLabel .Role}} · {{.Width}} × {{.Height}}</span></a>{{end}}</div>{{else}}<p class="muted">没有媒体文件</p>{{end}}</section>
  <section class="panel"><div class="section-header"><div><h2>外部绑定</h2></div></div><div class="stack-list">{{range .Detail.Bindings}}<article class="stack-row"><div><strong>{{targetLabel .Provider}}</strong><span class="cell-note"><code>{{.ExternalID}}</code> · {{.Origin}}</span>{{if .ExternalURL}}<a class="cell-note" href="{{.ExternalURL}}" target="_blank" rel="noopener noreferrer">打开外部页面 ↗</a>{{end}}</div></article>{{else}}<p class="muted">没有外部绑定</p>{{end}}</div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>修订历史</h2></div></div><div class="table-wrap"><table><thead><tr><th>修订</th><th>名称</th><th>状态</th><th>审核</th><th>内容</th><th>时间</th></tr></thead><tbody>{{range .Detail.Revisions}}<tr><td>#{{.Number}}</td><td>{{.Name}}</td><td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span></td><td>{{if .ReviewState}}<span class="status {{statusClass .ReviewState}}">{{statusLabel .ReviewState}}</span>{{else}}—{{end}}{{if .ReviewNote}}<span class="cell-note">{{.ReviewNote}}</span>{{end}}</td><td>{{.ArtifactCount}} 文件 · {{.MediaCount}} 图片</td><td class="secondary nowrap">{{dateTime .CreatedAt}}</td></tr>{{else}}<tr><td class="table-empty" colspan="6">尚未提交修订</td></tr>{{end}}</tbody></table></div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>资源事件</h2><p>资源生命周期和管理操作记录</p></div></div><div class="table-wrap"><table><thead><tr><th>时间</th><th>事件</th><th>操作者</th><th>ID</th></tr></thead><tbody>{{range .Detail.Events}}<tr><td class="secondary nowrap">{{dateTime .CreatedAt}}</td><td><code>{{.EventType}}</code></td><td>{{if .Actor}}{{.Actor}}{{else}}系统{{end}}</td><td>{{.ID}}</td></tr>{{else}}<tr><td class="table-empty" colspan="4">没有资源事件</td></tr>{{end}}</tbody></table></div></section>
  <section class="panel span-2"><details class="snapshot"><summary>查看完整工作区快照</summary><pre>{{.Snapshot}}</pre></details></section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_users"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>用户</h1><p>管理账号、角色与治理能力，操作会写入审计日志</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>{{if eqs .Action "message_sent"}}管理员消息已发送{{else}}用户操作已完成{{end}}</div>{{end}}
<section class="panel list-panel">
<form class="filter-bar" method="get" action="/admin/users">
  <label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="用户名或米坛用户 ID"></label>
  <div class="filter-actions"><button class="filled-button" type="submit">筛选</button><a class="text-link filter-reset" href="/admin/users">清除</a></div>
</form>
<form id="admin-message-form" method="post" action="/admin/users/messages" class="filter-bar">
  <label class="search-field"><span>消息标题</span><input name="title" required maxlength="200" placeholder="发送给勾选用户"></label>
  <label class="search-field"><span>消息正文</span><input name="body" required maxlength="2000" placeholder="管理员消息内容"></label>
  <div class="filter-actions"><button class="filled-button" type="submit">发送消息</button></div>
</form>
<div class="table-panel"><div class="table-wrap"><table>
<thead><tr><th>选择</th><th>用户</th><th>米坛 ID</th><th>角色</th><th>状态</th><th>资源 / 工单</th><th>注册时间</th><th>管理</th></tr></thead>
<tbody>{{range .Items}}<tr>
  <td><input type="checkbox" name="user" value="{{.ID}}" form="admin-message-form" aria-label="选择 {{.Username}}"></td>
  <td>{{.Username}}<span class="cell-note"><code>{{.ID}}</code></span></td>
  <td><code>{{.BandBBSUserID}}</code></td>
  <td><form method="post" action="/admin/users/{{.ID}}/state"><input type="hidden" name="action" value="set_role"><select name="role" onchange="this.form.submit()"><option value="user" {{if eqs .Role "user"}}selected{{end}}>用户</option><option value="reviewer" {{if eqs .Role "reviewer"}}selected{{end}}>审核员</option><option value="admin" {{if eqs .Role "admin"}}selected{{end}}>管理员</option></select></form></td>
  <td>{{if .BannedAt}}<span class="status danger">已封禁</span>{{if .BanReason}}<span class="cell-note">{{.BanReason}}</span>{{end}}{{else}}<span class="status success">正常</span>{{end}}{{if .CreatorFrozenAt}}<span class="cell-note">创作者已冻结</span>{{end}}</td>
  <td>{{.ResourceCount}} / {{.TicketCount}}</td>
  <td class="secondary nowrap">{{dateTime .CreatedAt}}</td>
  <td><div class="tag-stack">
    {{if .BannedAt}}
    <form method="post" action="/admin/users/{{.ID}}/state"><button class="row-action" name="action" value="unban">解封</button></form>
    {{else}}
    <form method="post" action="/admin/users/{{.ID}}/state"><input name="reason" placeholder="封禁原因（可选）"><button class="outlined-button danger" name="action" value="ban" data-confirm="确定封禁该用户吗？将吊销其全部客户端会话">封禁</button></form>
    {{end}}
    {{if .CreatorFrozenAt}}
    <form method="post" action="/admin/users/{{.ID}}/state"><button class="row-action" name="action" value="unfreeze_creator">解冻创作者</button></form>
    {{else}}
    <form method="post" action="/admin/users/{{.ID}}/state"><button class="row-action" name="action" value="freeze_creator" data-confirm="确定冻结该用户的创作者功能吗？其将无法提交或管理资源">冻结创作者</button></form>
    {{end}}
  </div></td>
</tr>{{else}}<tr><td class="table-empty" colspan="8">没有匹配的用户</td></tr>{{end}}</tbody>
</table></div>
{{if gt .Page.TotalPages 1}}<nav class="pagination" aria-label="用户分页"><span>第 {{.Page.Page}} / {{.Page.TotalPages}} 页</span><div>{{if gt .Page.Page 1}}<a class="outlined-button" href="?page={{sub1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}">上一页</a>{{end}}{{if lt .Page.Page .Page.TotalPages}}<a class="outlined-button" href="?page={{add1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}">下一页</a>{{end}}</div></nav>{{end}}
</div>
</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_reports"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>举报</h1><p>受理资源与评论举报并同步处理结果</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<nav class="subtabs"><a class="active" href="/admin/reports">举报</a><a href="/admin/feedback">全部反馈</a></nav>
{{if .Action}}<div class="notice success toast-notice" data-toast>举报处理状态已更新</div>{{end}}
<form class="filter-bar" method="get" action="/admin/reports">
  <label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="主题、用户名、内容或目标资源"></label>
  <label><span>来源</span><input name="source" value="{{.Query.TargetSource}}" placeholder="oronbox"></label>
  <label><span>状态</span><select name="status"><option value="">全部</option><option value="open" {{if eqs .Query.Status "open"}}selected{{end}}>待处理</option><option value="investigating" {{if eqs .Query.Status "investigating"}}selected{{end}}>处理中</option><option value="replied" {{if eqs .Query.Status "replied"}}selected{{end}}>已回复</option><option value="resolved" {{if eqs .Query.Status "resolved"}}selected{{end}}>已解决</option><option value="dismissed" {{if eqs .Query.Status "dismissed"}}selected{{end}}>已驳回</option><option value="closed" {{if eqs .Query.Status "closed"}}selected{{end}}>已关闭</option></select></label>
  <button class="filled-button" type="submit">筛选</button><a class="text-link filter-reset" href="/admin/reports">清除</a>
</form>
<div class="ticket-list">
{{range .Items}}
<article class="ticket-card">
  <header><div><div class="title-line"><span class="status danger">{{kindLabel .Kind}}</span><h2><a class="resource-name" href="/admin/reports/{{.ID}}">{{.Subject}}</a></h2></div><p class="ticket-meta">{{.Username}} · {{dateTime .CreatedAt}} · <code>{{.ID}}</code></p></div><span class="status {{statusClass .Status}}">{{statusLabel .Status}}</span></header>
  <p class="ticket-excerpt">{{.Message}}</p>
  <div class="ticket-target">{{if .TargetSource}}{{targetLabel .TargetSource}} · {{end}}{{if .TargetID}}<code>{{.TargetID}}</code>{{else}}未关联资源{{end}}<a class="row-action" href="/admin/reports/{{.ID}}">查看并处理</a></div>
</article>
{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>没有举报</h2><p>当前筛选条件下没有举报记录</p></section>{{end}}
</div>
{{if gt .Page.TotalPages 1}}<nav class="pagination" aria-label="举报分页"><span>第 {{.Page.Page}} / {{.Page.TotalPages}} 页</span><div>{{if gt .Page.Page 1}}<a class="outlined-button" href="?page={{sub1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}&source={{urlquery .Query.TargetSource}}&status={{urlquery .Query.Status}}">上一页</a>{{end}}{{if lt .Page.Page .Page.TotalPages}}<a class="outlined-button" href="?page={{add1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}&source={{urlquery .Query.TargetSource}}&status={{urlquery .Query.Status}}">下一页</a>{{end}}</div></nav>{{end}}
{{template "admin_close" .}}
{{end}}

{{define "admin_report_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/reports">← 举报</a><div class="title-line"><span class="status danger">{{kindLabel .Ticket.Kind}}</span><h1>{{.Ticket.Subject}}</h1></div><p>{{.Ticket.Username}} · {{dateTime .Ticket.CreatedAt}} · <code>{{.Ticket.ID}}</code></p></div><span class="status {{statusClass .Ticket.Status}}">{{statusLabel .Ticket.Status}}</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>处理结果已保存</div>{{end}}
<div class="content-grid detail-grid">
  <section class="panel span-2"><div class="section-header"><div><h2>举报内容</h2></div></div><div class="ticket-message">{{.Ticket.Message}}</div><dl class="target-box">{{if .Ticket.TargetSource}}<div><dt>来源</dt><dd>{{targetLabel .Ticket.TargetSource}}</dd></div>{{end}}{{if .Ticket.TargetID}}<div><dt>目标 ID</dt><dd><code>{{.Ticket.TargetID}}</code></dd></div>{{end}}{{if .Ticket.TargetURL}}<div><dt>目标链接</dt><dd><a href="{{.Ticket.TargetURL}}" target="_blank" rel="noopener noreferrer">{{.Ticket.TargetURL}} ↗</a></dd></div>{{end}}</dl></section>
  {{if .Resource}}<section class="panel"><div class="section-header"><div><h2>关联资源</h2></div><a class="row-action" href="/admin/resources/{{.Resource.Resource.ID}}">打开资源</a></div><dl class="settings compact"><dt>名称</dt><dd>{{.Resource.Resource.Name}}</dd><dt>创作者</dt><dd>{{.Resource.Resource.Owner}}</dd><dt>状态</dt><dd><span class="status {{statusClass .Resource.Resource.ModerationState}}">{{statusLabel .Resource.Resource.ModerationState}}</span></dd></dl></section>{{end}}
  <section class="panel"><div class="section-header"><div><h2>答复记录</h2></div></div><div class="reply-thread">{{range .Ticket.Replies}}<article><div><strong>{{.Author}}</strong><time>{{dateTime .CreatedAt}}</time></div><p>{{.Message}}</p></article>{{else}}<p class="muted">尚未答复</p>{{end}}</div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>处理举报</h2><p>回复会显示在用户客户端；状态会同步更新</p></div></div><form method="post" action="/admin/reports/{{.Ticket.ID}}" class="report-form"><label><span>处理状态</span><select name="status"><option value="open" {{if eqs .Ticket.Status "open"}}selected{{end}}>待处理</option><option value="investigating" {{if eqs .Ticket.Status "investigating"}}selected{{end}}>处理中</option><option value="replied" {{if eqs .Ticket.Status "replied"}}selected{{end}}>已回复</option><option value="resolved" {{if eqs .Ticket.Status "resolved"}}selected{{end}}>已解决</option><option value="dismissed" {{if eqs .Ticket.Status "dismissed"}}selected{{end}}>已驳回</option><option value="closed" {{if eqs .Ticket.Status "closed"}}selected{{end}}>已关闭</option></select></label><label><span>回复与处理说明</span><textarea name="message" rows="4" placeholder="填写后会作为一条回复显示在用户客户端"></textarea></label><div class="actions span-2"><button class="filled-button" type="submit">保存处理结果</button></div></form></section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_feedback"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>举报与反馈</h1><p>查看全部资源举报、评论举报和用户意见</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<nav class="subtabs"><a href="/admin/reports">举报</a><a class="active" href="/admin/feedback">全部反馈</a></nav>
{{if .Replied}}<div class="notice success toast-notice" data-toast>答复已发送</div>{{end}}
<form class="filter-bar" method="get" action="/admin/feedback"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="主题、用户名、内容或目标"></label><label><span>类型</span><select name="kind"><option value="">全部</option><option value="reports" {{if eqs .Query.Kind "reports"}}selected{{end}}>全部举报</option><option value="resource_report" {{if eqs .Query.Kind "resource_report"}}selected{{end}}>资源举报</option><option value="comment_report" {{if eqs .Query.Kind "comment_report"}}selected{{end}}>评论举报</option><option value="feedback" {{if eqs .Query.Kind "feedback"}}selected{{end}}>意见反馈</option></select></label><label><span>来源</span><input name="source" value="{{.Query.TargetSource}}" placeholder="oronbox"></label><label><span>状态</span><select name="status"><option value="">全部</option><option value="open" {{if eqs .Query.Status "open"}}selected{{end}}>待处理</option><option value="investigating" {{if eqs .Query.Status "investigating"}}selected{{end}}>处理中</option><option value="replied" {{if eqs .Query.Status "replied"}}selected{{end}}>已回复</option><option value="resolved" {{if eqs .Query.Status "resolved"}}selected{{end}}>已解决</option><option value="dismissed" {{if eqs .Query.Status "dismissed"}}selected{{end}}>已驳回</option><option value="closed" {{if eqs .Query.Status "closed"}}selected{{end}}>已关闭</option></select></label><button class="filled-button" type="submit">筛选</button><a class="text-link filter-reset" href="/admin/feedback">清除</a></form>
<div class="ticket-list">{{range .Items}}<article class="ticket-card"><header><div><div class="title-line"><span class="status {{if reportKind .Kind}}danger{{else}}info{{end}}">{{kindLabel .Kind}}</span><h2>{{if reportKind .Kind}}<a class="resource-name" href="/admin/reports/{{.ID}}">{{.Subject}}</a>{{else}}{{.Subject}}{{end}}</h2></div><p class="ticket-meta">{{.Username}} · {{dateTime .CreatedAt}} · <code>{{.ID}}</code></p></div><span class="status {{statusClass .Status}}">{{statusLabel .Status}}</span></header><div class="ticket-message">{{.Message}}</div>{{if and (not (reportKind .Kind)) (ne .Status "closed")}}<form method="post" action="/admin/feedback/{{.ID}}" class="reply-form"><label>答复<textarea name="message" rows="3" required placeholder="答复将显示在用户客户端"></textarea></label><div class="actions"><button class="filled-button" type="submit" name="close" value="no">发送答复</button><button class="outlined-button" type="submit" name="close" value="yes">答复并关闭</button></div></form>{{end}}</article>{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>没有反馈</h2><p>当前筛选条件下没有记录</p></section>{{end}}</div>
{{if gt .Page.TotalPages 1}}<nav class="pagination" aria-label="反馈分页"><span>第 {{.Page.Page}} / {{.Page.TotalPages}} 页</span><div>{{if gt .Page.Page 1}}<a class="outlined-button" href="?page={{sub1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}&kind={{urlquery .Query.Kind}}&source={{urlquery .Query.TargetSource}}&status={{urlquery .Query.Status}}">上一页</a>{{end}}{{if lt .Page.Page .Page.TotalPages}}<a class="outlined-button" href="?page={{add1 .Page.Page}}&per_page={{.Page.PerPage}}&q={{urlquery .Query.Search}}&kind={{urlquery .Query.Kind}}&source={{urlquery .Query.TargetSource}}&status={{urlquery .Query.Status}}">下一页</a>{{end}}</div></nav>{{end}}
{{template "admin_close" .}}
{{end}}

{{define "admin_comments"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>评论审核</h1><p>处理 AI 标记的评论并配置审核提示词</p></div><span class="count-badge">{{.Total}} 项</span></header>
<div class="content-grid">
  <section class="panel span-2"><div class="section-header"><div><h2>待人工复审</h2></div></div>
    <div class="ticket-list">{{range .Items}}<article class="ticket-card"><header><div><strong>{{.Username}}</strong><p class="ticket-meta">米坛 ID {{.BandBBSUserID}} · {{dateTime .CreatedAt}}</p></div><span class="status danger">{{.ModerationAction}}</span></header><div class="ticket-message">{{.Body}}</div><p class="muted">{{.ModerationModel}} · {{.ModerationReason}}</p><form method="post" action="/admin/comments/{{.ID}}" class="actions"><button class="filled-button" name="action" value="approve">通过</button><button class="outlined-button danger" name="action" value="hide">隐藏</button></form></article>{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>没有待复审评论</h2></section>{{end}}</div>
  </section>
  <section class="panel"><div class="section-header"><div><h2>审核提示词</h2></div></div><form method="post" action="/admin/comments/prompt"><label><span>提示词</span><textarea name="prompt" rows="12" required>{{.Prompt}}</textarea></label><div class="actions"><button class="filled-button" type="submit">保存</button></div></form></section>
  <section class="panel"><div class="section-header"><div><h2>测试台</h2><p>用当前提示词测试任意文本</p></div></div><form method="post" action="/admin/comments/test"><label><span>测试内容</span><textarea name="text" rows="8" required>{{.TestText}}</textarea></label><div class="actions"><button class="filled-button" type="submit">运行审核</button></div></form>{{if .TestResult}}<h3>原始 JSON</h3><pre class="diagnostic">{{.TestResult}}</pre>{{end}}</section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_collections"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>合集审核</h1><p>审核合集名称和简介；资源仍按各自审核状态发布</p></div><span class="count-badge">{{len .Items}} 项</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>合集审核结果已保存</div>{{end}}
{{range .Items}}<article class="review-card"><header class="review-summary"><div><div class="title-line"><h2>{{.PendingRevision.Name}}</h2><span class="status warning">待审核</span></div><p>{{.PendingRevision.Summary}}</p></div><dl class="review-meta"><div><dt>创作者</dt><dd><code>{{.OwnerID}}</code></dd></div><div><dt>类型</dt><dd>{{kindLabel .Kind}}</dd></div><div><dt>资源</dt><dd>{{.ResourceCount}} 项</dd></div><div><dt>Slug</dt><dd><code>{{.Slug}}</code></dd></div></dl></header><form method="post" action="/admin/collections/{{.PendingRevision.ID}}" class="decision-form"><label>审核意见<textarea name="note" rows="4" placeholder="退回时请直接说明需要修改的内容"></textarea></label><div class="actions"><button class="filled-button" name="decision" value="approve">通过审核</button><button class="outlined-button danger" name="decision" value="reject">退回修改</button></div></form></article>{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>合集审核队列为空</h2><p>当前没有等待处理的合集元数据</p></section>{{end}}
{{template "admin_close" .}}
{{end}}

{{define "admin_plugins"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>插件管理</h1><p>审核新上传的插件，管理已上架插件</p></div><span class="count-badge">{{len .Plugins}} 项</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>插件操作已完成</div>{{end}}
<section class="panel"><div class="section-header"><div><h2>待审核</h2><p>新上传和更新的插件包，通过后才会上架</p></div><span class="count-badge">{{len .Pending}} 项</span></div>
{{range .Pending}}<article class="review-card"><header class="review-summary"><div><div class="title-line"><h2>{{.Name}}</h2><span class="status warning">待审核</span></div><p>{{.Description}}</p></div><dl class="review-meta"><div><dt>插件 ID</dt><dd><code>{{.ID}}</code></dd></div><div><dt>上传者</dt><dd>{{.UploaderName}} <code>{{.UploaderID}}</code></dd></div><div><dt>版本</dt><dd>{{.Version}}</dd></div><div><dt>运行时</dt><dd><code>{{.Runtime}}</code></dd></div><div><dt>权限</dt><dd>{{range .Permissions}}<code>{{.}}</code> {{else}}无{{end}}</dd></div><div><dt>包体</dt><dd><a href="/admin/blobs/{{.PackageSHA256}}">下载检查</a> · {{.PackageSize}} B</dd></div><div><dt>上传时间</dt><dd>{{dateTime .UpdatedAt}}</dd></div></dl></header><form method="post" action="/admin/plugins/{{.ID}}/review" class="decision-form"><label>审核意见<textarea name="note" rows="3" placeholder="拒绝时必须说明原因"></textarea></label><div class="actions"><button class="filled-button" name="decision" value="approve">通过上架</button><button class="outlined-button danger" name="decision" value="reject">拒绝</button></div></form></article>{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>审核队列为空</h2><p>当前没有等待审核的插件</p></section>{{end}}
</section>
<section class="panel"><div class="section-header"><div><h2>全部插件</h2><p>已上架插件可强制下架，已下架可恢复上架；重新上传的版本会重新进入审核</p></div></div>
<div class="table-wrap"><table><thead><tr><th>插件</th><th>上传者</th><th>版本</th><th>运行时</th><th>大小</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{{range .Plugins}}<tr>
<td>{{.Name}}<span class="cell-note"><code>{{.ID}}</code></span></td>
<td>{{.UploaderName}}<span class="cell-note"><code>{{.UploaderID}}</code></span></td>
<td>{{.Version}}</td><td><code>{{.Runtime}}</code></td><td class="nowrap">{{.PackageSize}} B</td>
<td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span>{{if .ModerationReason}}<span class="cell-note">{{.ModerationReason}}</span>{{end}}</td>
<td class="secondary nowrap">{{dateTime .UpdatedAt}}</td>
<td>{{if eqs .State "listed"}}<form method="post" action="/admin/plugins/{{.ID}}/state" class="reply-form"><label><span>下架原因</span><input name="reason" required></label><div class="actions"><button class="outlined-button danger" name="action" value="delist">强制下架</button></div></form>{{else if eqs .State "delisted"}}<form method="post" action="/admin/plugins/{{.ID}}/state"><div class="actions"><button class="outlined-button" name="action" value="restore">恢复上架</button></div></form>{{else}}—{{end}}</td>
</tr>{{else}}<tr><td class="table-empty" colspan="8">暂无插件</td></tr>{{end}}</tbody></table></div></section>
{{template "admin_close" .}}
{{end}}

{{define "admin_coins"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>硬币管理</h1><p>查看发行与流转、冻结异常账号、调账并作废异常投币</p></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>硬币操作已完成</div>{{end}}
<div class="metric-grid"><article><span>已发行</span><strong>{{.Stats.IssuedUnits}}</strong><small>0.1 枚 / 单位</small></article><article><span>资源投币</span><strong>{{.Stats.SpentUnits}}</strong><small>支出单位</small></article><article><span>创作者奖励</span><strong>{{.Stats.RewardedUnits}}</strong><small>奖励单位</small></article><article><span>冻结账号</span><strong>{{.Stats.FrozenVoters}}</strong><small>活跃投币用户 {{.Stats.ActiveVoters}}</small></article></div>
<div class="content-grid"><section class="panel"><div class="section-header"><div><h2>账号操作</h2><p>余额使用 0.1 枚为一个单位</p></div></div><form method="post" action="/admin/coins/users" class="reply-form"><label><span>用户 UUID</span><input name="user_id" required></label><label><span>动作</span><select name="action"><option value="adjust">调账</option><option value="freeze">冻结投币</option><option value="unfreeze">解除冻结</option></select></label><label><span>余额变动单位</span><input name="delta_units" type="number" value="0"></label><label><span>原因</span><textarea name="reason" required rows="3"></textarea></label><div class="actions"><button class="filled-button">执行</button></div></form></section><section class="panel"><div class="section-header"><div><h2>作废投币</h2><p>退回投币并回滚可追回的创作者奖励</p></div></div><form method="post" action="/admin/coins/invalidate" class="reply-form"><label><span>资源 UUID</span><input name="resource_id" required></label><label><span>投币用户 UUID</span><input name="user_id" required></label><label><span>原因</span><textarea name="reason" required rows="3"></textarea></label><div class="actions"><button class="outlined-button danger">作废投币</button></div></form></section></div>
<section class="panel"><div class="section-header"><div><h2>最近 200 条账本记录</h2><p>原始流水不可删除</p></div></div><div class="table-wrap"><table><thead><tr><th>时间</th><th>用户</th><th>类型</th><th>变动</th><th>关联</th><th>原因</th></tr></thead><tbody>{{range .Ledger}}<tr><td class="secondary nowrap">{{.CreatedAt}}</td><td>{{.Username}}<span class="cell-note"><code>{{.UserID}}</code></span></td><td><code>{{.Kind}}</code></td><td>{{.DeltaUnits}}</td><td>{{.ReferenceType}} <code>{{.ReferenceID}}</code></td><td>{{.Note}}</td></tr>{{else}}<tr><td class="table-empty" colspan="6">暂无硬币流水</td></tr>{{end}}</tbody></table></div></section>
{{template "admin_close" .}}
{{end}}
`
