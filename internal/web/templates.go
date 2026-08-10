package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
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
			"oauthLifecycle": oauthLifecycle,
			"kindLabel":      kindLabel,
			"reportKind":     reportKind,
			"platformLabel":  platformLabel,
			"targetLabel":    targetLabel,
			"mediaRoleLabel": mediaRoleLabel,
			"paidTypeLabel":  paidTypeLabel,
			"dateTime":       dateTime,
			"humanBytes": func(value int64) string {
				const unit = int64(1024)
				if value < unit {
					return fmt.Sprintf("%d B", value)
				}
				divisor, exponent := unit, 0
				for quotient := value / unit; quotient >= unit && exponent < 5; quotient /= unit {
					divisor *= unit
					exponent++
				}
				return fmt.Sprintf("%.1f %ciB", float64(value)/float64(divisor), "KMGTPE"[exponent])
			},
			"rawJSON": func(value []byte) string {
				return string(value)
			},
			"prettyJSON": func(value any) string {
				raw, err := json.MarshalIndent(value, "", "  ")
				if err != nil {
					return "{}"
				}
				return string(raw)
			},
			"publicationEventLabel": publicationEventLabel,
			"publicationPhaseLabel": publicationPhaseLabel,
			"splitStates": func() []string {
				return []string{"pending", "running", "reviewing", "published", "failed", "cancelled"}
			},
			"splitMessageKinds": func() []string {
				return []string{"review_result", "moderation", "comment_reply", "report_result", "admin_message", "account", "announcement"}
			},
			"sub1": func(value int) int { return value - 1 },
			"add1": func(value int) int { return value + 1 },
			"containsString": func(values []string, value string) bool {
				for _, item := range values {
					if item == value {
						return true
					}
				}
				return false
			},
			"containsDevice": func(values []store.AdminArtifactDevice, value string) bool {
				for _, item := range values {
					if item.ID == value {
						return true
					}
				}
				return false
			},
		}).Parse(templates)),
	}
}

func oauthLifecycle(usedAt, expiresAt string) string {
	if strings.TrimSpace(usedAt) != "" {
		return "used"
	}
	expires, err := time.ParseInLocation("2006-01-02 15:04:05", expiresAt, time.Local)
	if err == nil && !expires.After(time.Now()) {
		return "expired"
	}
	return "active"
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
		"draft": "草稿", "submitted": "待审核", "approved": "已通过", "rejected": "已拒绝",
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

func publicationPhaseLabel(value string) string {
	switch value {
	case "execute":
		return "执行"
	case "poll":
		return "外部轮询"
	case "admin":
		return "管理员"
	default:
		return value
	}
}

func publicationEventLabel(value string) string {
	switch value {
	case "execution_started":
		return "开始执行"
	case "retry_scheduled":
		return "失败，等待重试"
	case "execution_failed":
		return "执行失败"
	case "submitted_for_review":
		return "已提交外部审核"
	case "poll_started":
		return "检查外部状态"
	case "review_pending":
		return "外部审核中"
	case "poll_failed":
		return "状态检查失败"
	case "external_review_rejected":
		return "外部审核拒绝"
	case "published":
		return "发布成功"
	case "requeued":
		return "管理员重新入队"
	case "cancelled":
		return "管理员取消"
	case "history_imported":
		return "迁移前状态快照"
	default:
		return value
	}
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

func paidTypeLabel(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "free":
		return "免费"
	case "paid":
		return "付费"
	case "force_paid":
		return "强制付费"
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
<div class="sr-only" id="admin-live-region" role="status" aria-live="polite" aria-atomic="true"></div>
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

  const liveRegion = document.querySelector('#admin-live-region');
  const announce = (message) => {
    if (!liveRegion) return;
    liveRegion.textContent = '';
    window.setTimeout(() => { liveRegion.textContent = message; }, 20);
  };

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
        announce('已复制到剪贴板');
        window.setTimeout(() => node.classList.remove('copied'), 1200);
      } catch (_) { announce('复制失败，请手动复制'); }
    });
  });

  document.querySelectorAll('button:not([type])').forEach((button) => {
    button.type = button.closest('form') ? 'submit' : 'button';
  });
  const iconLabels = { delete: '删除', close: '关闭', arrow_upward: '上移', arrow_downward: '下移', edit: '编辑', content_copy: '复制', download: '下载', refresh: '刷新', first_page: '第一页', last_page: '最后一页' };
  document.querySelectorAll('button, a.icon-button').forEach((control) => {
    const icon = control.querySelector('.material-symbols-outlined');
    const iconName = icon?.textContent.trim();
    icon?.setAttribute('aria-hidden', 'true');
    const visibleText = Array.from(control.childNodes).filter((node) => node.nodeType === Node.TEXT_NODE).map((node) => node.textContent).join('').trim();
    if (control.getAttribute('aria-label') || visibleText) return;
    const label = control.getAttribute('title') || iconLabels[iconName] || iconName;
    if (label) control.setAttribute('aria-label', label);
  });
  document.querySelectorAll('input, select, textarea').forEach((field, index) => {
    if (field.type === 'hidden' || field.labels?.length || field.getAttribute('aria-label') || field.getAttribute('aria-labelledby')) return;
    const label = field.closest('label')?.textContent.replace(field.value || '', '').trim() || field.placeholder || field.name;
    if (label) field.setAttribute('aria-label', label);
    if (!field.id) field.id = 'admin-field-' + index;
  });

  document.querySelectorAll('.table-wrap table').forEach((table) => {
    const labels = Array.from(table.querySelectorAll('thead th')).map((cell) => cell.textContent.trim());
    table.querySelectorAll('tbody tr').forEach((row) => {
      Array.from(row.cells).forEach((cell, index) => {
        if (cell.colSpan === 1 && labels[index]) cell.dataset.label = labels[index];
      });
    });
  });

  document.querySelectorAll('form').forEach((form) => {
    const errorSummary = document.createElement('div');
    errorSummary.className = 'form-error-summary';
    errorSummary.setAttribute('role', 'alert');
    errorSummary.setAttribute('tabindex', '-1');
    errorSummary.hidden = true;
    form.prepend(errorSummary);
    form.addEventListener('invalid', (event) => {
      event.target.setAttribute('aria-invalid', 'true');
      errorSummary.textContent = '请检查标出的字段：' + (event.target.labels?.[0]?.textContent.trim() || event.target.getAttribute('aria-label') || '表单字段');
      errorSummary.hidden = false;
      window.setTimeout(() => errorSummary.focus(), 0);
    }, true);
    form.addEventListener('input', (event) => {
      if (event.target.checkValidity()) event.target.removeAttribute('aria-invalid');
      if (form.checkValidity()) errorSummary.hidden = true;
    });
    form.addEventListener('submit', (event) => {
      const submitter = event.submitter;
      if (!submitter) return;
      if (form.dataset.submitting === 'true') {
        event.preventDefault();
        return;
      }
      form.dataset.submitting = 'true';
      submitter.classList.add('submitting');
      submitter.setAttribute('aria-busy', 'true');
      submitter.setAttribute('aria-disabled', 'true');
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
  let dialogTrigger = null;
  document.addEventListener('click', (event) => {
    const action = event.target.closest('[data-confirm]');
    if (!action || action.dataset.confirmed === 'true' || !confirmDialog) return;
    event.preventDefault();
    pendingAction = action;
    dialogTrigger = action;
    if (confirmText) confirmText.textContent = action.dataset.confirm;
    confirmDialog.showModal();
    window.setTimeout(() => confirmAction?.focus(), 0);
  });
  confirmAction?.addEventListener('click', () => {
    if (!pendingAction) return;
    pendingAction.dataset.confirmed = 'true';
    confirmDialog.close();
    pendingAction.form?.requestSubmit(pendingAction);
    pendingAction = null;
  });
  confirmDialog?.addEventListener('cancel', () => { pendingAction = null; });
  confirmDialog?.addEventListener('close', () => {
    pendingAction = null;
    dialogTrigger?.focus();
    dialogTrigger = null;
  });
})();
</script>
</body></html>
{{end}}

{{define "admin_nav"}}
<aside class="nav" id="admin-drawer" aria-label="管理后台导航">
  <nav class="nav-content">
    <div class="nav-group-label">内容与发布</div>
    <a class="nav-link" data-nav-path="/admin" href="/admin"><span class="material-symbols-outlined">dashboard</span><span>概览</span></a>
    <a class="nav-link" data-nav-path="/admin/review" href="/admin/review"><span class="material-symbols-outlined">fact_check</span><span>待审核</span></a>
    <a class="nav-link" data-nav-path="/admin/resources" href="/admin/resources"><span class="material-symbols-outlined">inventory_2</span><span>全部资源</span></a>
    <a class="nav-link" data-nav-path="/admin/publications" href="/admin/publications"><span class="material-symbols-outlined">cloud_upload</span><span>发布任务</span></a>
    <a class="nav-link" data-nav-path="/admin/devices" href="/admin/devices"><span class="material-symbols-outlined">watch</span><span>设备目录</span></a>
    <a class="nav-link" data-nav-path="/admin/collections" href="/admin/collections"><span class="material-symbols-outlined">collections_bookmark</span><span>合集审核</span></a>
    <a class="nav-link" data-nav-path="/admin/plugins" href="/admin/plugins"><span class="material-symbols-outlined">extension</span><span>插件管理</span></a>
    <div class="nav-group-label">社区与用户</div>
    <a class="nav-link" data-nav-path="/admin/coins" href="/admin/coins"><span class="material-symbols-outlined">toll</span><span>硬币管理</span></a>
    <a class="nav-link" data-nav-path="/admin/users" href="/admin/users"><span class="material-symbols-outlined">group</span><span>用户</span></a>
    <a class="nav-link" data-nav-path="/admin/messages" href="/admin/messages"><span class="material-symbols-outlined">notifications</span><span>系统消息</span></a>
    <a class="nav-link" data-nav-path="/admin/comments" href="/admin/comments"><span class="material-symbols-outlined">forum</span><span>评论审核</span></a>
    <a class="nav-link" data-nav-path="/admin/reports" data-nav-alias="/admin/feedback" href="/admin/reports"><span class="material-symbols-outlined">report</span><span>举报与反馈</span></a>
    <div class="nav-group-label">内容运营</div>
    <a class="nav-link" data-nav-path="/admin/home" href="/admin/home"><span class="material-symbols-outlined">home</span><span>首页编排</span></a>
    <a class="nav-link" data-nav-path="/admin/blog" href="/admin/blog"><span class="material-symbols-outlined">article</span><span>Blog 管理</span></a>
    <a class="nav-link" data-nav-path="/admin/announcements" href="/admin/announcements"><span class="material-symbols-outlined">campaign</span><span>公告</span></a>
    <a class="nav-link" data-nav-path="/admin/releases" href="/admin/releases"><span class="material-symbols-outlined">new_releases</span><span>客户端版本</span></a>
    <div class="nav-group-label">系统与诊断</div>
    <a class="nav-link" data-nav-path="/admin/oauth/events" href="/admin/oauth/events"><span class="material-symbols-outlined">timeline</span><span>OAuth 事件</span></a>
    <a class="nav-link" data-nav-path="/admin/oauth/states" href="/admin/oauth/states"><span class="material-symbols-outlined">key</span><span>OAuth States</span></a>
    <a class="nav-link" data-nav-path="/admin/oauth/tickets" href="/admin/oauth/tickets"><span class="material-symbols-outlined">confirmation_number</span><span>登录 Tickets</span></a>
    <a class="nav-link" data-nav-path="/admin/clients" href="/admin/clients"><span class="material-symbols-outlined">devices</span><span>客户端统计</span></a>
    <a class="nav-link" data-nav-path="/admin/storage/blobs" href="/admin/storage/blobs"><span class="material-symbols-outlined">database</span><span>Blob 与副本</span></a>
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
<dialog class="confirm-dialog" id="confirm-dialog" aria-labelledby="confirm-dialog-title" aria-describedby="confirm-dialog-description">
  <form method="dialog">
    <div class="dialog-icon"><span class="material-symbols-outlined">warning</span></div>
    <h2 id="confirm-dialog-title">确认操作</h2>
    <p id="confirm-dialog-description" data-confirm-text></p>
    <div class="dialog-actions">
      <button class="outlined-button" type="submit" value="cancel">取消</button>
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

{{define "pagination"}}
{{with $pager := .Pager}}{{if gt $pager.Total 0}}
<nav class="pagination" aria-label="分页">
  <div class="pagination-summary"><span>第 {{$pager.From}}–{{$pager.To}} 条，共 {{$pager.Total}} 条</span><span>每页</span>{{range $size := $.PageSizes}}<a class="page-size {{if eq $pager.PerPage $size}}active{{end}}" href="{{$pager.PerPageURL $size}}">{{$size}}</a>{{end}}</div>
  {{if gt $pager.TotalPages 1}}<div class="pagination-pages">
    <a class="icon-button pagination-edge {{if eq $pager.Page 1}}disabled{{end}}" href="{{$pager.URL 1}}" aria-label="第一页"><span class="material-symbols-outlined">first_page</span></a>
    <a class="outlined-button {{if eq $pager.Page 1}}disabled{{end}}" href="{{$pager.URL (sub1 $pager.Page)}}">上一页</a>
    {{range $page := $pager.Pages}}<a class="page-number {{if eq $pager.Page $page}}active{{end}}" href="{{$pager.URL $page}}" {{if eq $pager.Page $page}}aria-current="page"{{end}}>{{$page}}</a>{{end}}
    <a class="outlined-button {{if eq $pager.Page $pager.TotalPages}}disabled{{end}}" href="{{$pager.URL (add1 $pager.Page)}}">下一页</a>
    <a class="icon-button pagination-edge {{if eq $pager.Page $pager.TotalPages}}disabled{{end}}" href="{{$pager.URL $pager.TotalPages}}" aria-label="最后一页"><span class="material-symbols-outlined">last_page</span></a>
  </div>{{end}}
</nav>
{{end}}{{end}}
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
<header class="page-header"><div><h1>OAuth 事件</h1><p>检索授权、回调和令牌交换事件</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<section class="panel list-panel">
  <form class="filter-bar embedded" method="get" action="/admin/oauth/events"><label class="search-field"><span>关键词</span><input name="q" value="{{.Query.Search}}" placeholder="事件、用户或错误"></label><label><span>App ID</span><input name="app" value="{{.Query.App}}" placeholder="oronbox"></label><label><span>结果</span><select name="result"><option value="">全部</option><option value="success" {{if eqs .Query.Result "success"}}selected{{end}}>成功</option><option value="failure" {{if eqs .Query.Result "failure"}}selected{{end}}>失败</option></select></label><label><span>平台</span><input name="platform" value="{{.Query.Platform}}" placeholder="android"></label><label><span>开始日期</span><input name="from" type="date" value="{{.From}}"></label><label><span>结束日期</span><input name="to" type="date" value="{{.To}}"></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/oauth/events">清除</a></div></form>
  {{template "events_table" .Events}}
  {{template "pagination" .}}
</section>
{{template "admin_close" .}}
{{end}}

{{define "events_table"}}
<div class="table-wrap"><table>
<thead><tr><th>时间</th><th>Provider</th><th>事件</th><th>结果</th><th>客户端</th><th>平台</th><th>用户 ID</th><th>耗时</th><th>错误</th></tr></thead>
<tbody>{{range .}}<tr>
  <td class="secondary nowrap"><a href="/admin/oauth/events/{{.ID}}">{{.CreatedAt}}</a></td><td>{{.Provider}}</td><td><code>{{.EventType}}</code></td>
  <td><span class="status {{statusClass .Result}}">{{statusLabel .Result}}</span></td>
  <td>{{.AppID}}<span class="cell-note">{{.AppVersion}}</span></td><td>{{.Platform}}</td>
  <td><code>{{if .ProviderUserID}}{{.ProviderUserID}}{{else}}—{{end}}</code></td><td>{{.LatencyMS}} ms</td>
  <td class="error-cell">{{if .ErrorCode}}<details><summary>{{.ErrorCode}}</summary>{{if .ErrorMessage}}<pre class="diagnostic">{{.ErrorMessage}}</pre>{{end}}</details>{{else}}—{{end}}</td>
</tr>{{else}}<tr><td class="table-empty" colspan="9">暂无 OAuth 事件</td></tr>{{end}}</tbody>
</table></div>
{{end}}

{{define "admin_states"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>OAuth States</h1><p>检索授权流程状态和使用情况</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<section class="panel list-panel">
<form class="filter-bar embedded" method="get" action="/admin/oauth/states"><label class="search-field"><span>关键词</span><input name="q" value="{{.Query.Search}}" placeholder="State 或用途"></label><label><span>App ID</span><input name="app" value="{{.Query.App}}"></label><label><span>状态</span><select name="status"><option value="">全部</option><option value="active" {{if eqs .Query.Status "active"}}selected{{end}}>有效</option><option value="used" {{if eqs .Query.Status "used"}}selected{{end}}>已使用</option><option value="expired" {{if eqs .Query.Status "expired"}}selected{{end}}>已过期</option></select></label><label><span>平台</span><input name="platform" value="{{.Query.Platform}}"></label><label><span>开始日期</span><input name="from" type="date" value="{{.From}}"></label><label><span>结束日期</span><input name="to" type="date" value="{{.To}}"></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/oauth/states">清除</a></div></form>
<div class="table-wrap"><table>
<thead><tr><th>State</th><th>Provider</th><th>用途</th><th>状态</th><th>创建</th><th>过期</th><th>客户端</th><th>IP</th></tr></thead>
<tbody>{{range .States}}{{$stateStatus:=oauthLifecycle .UsedAt .ExpiresAt}}<tr><td><a href="/admin/oauth/states/{{.ID}}"><code class="truncate-code">{{.ID}}</code></a></td><td>{{.Provider}}</td><td>{{.Purpose}}</td><td><span class="status {{statusClass $stateStatus}}">{{statusLabel $stateStatus}}</span></td><td class="secondary">{{.CreatedAt}}</td><td class="secondary">{{.ExpiresAt}}</td><td>{{.AppID}}<span class="cell-note">{{.Platform}} · {{.AppVersion}}</span></td><td><code>{{.IP}}</code></td></tr>{{else}}<tr><td class="table-empty" colspan="8">暂无 OAuth State</td></tr>{{end}}</tbody>
</table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_tickets"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>登录 Tickets</h1><p>检索登录票据生命周期和令牌携带状态</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<section class="panel list-panel">
<form class="filter-bar embedded" method="get" action="/admin/oauth/tickets"><label class="search-field"><span>关键词</span><input name="q" value="{{.Query.Search}}" placeholder="Ticket 或用户"></label><label><span>App ID</span><input name="app" value="{{.Query.App}}"></label><label><span>状态</span><select name="status"><option value="">全部</option><option value="active" {{if eqs .Query.Status "active"}}selected{{end}}>有效</option><option value="used" {{if eqs .Query.Status "used"}}selected{{end}}>已使用</option><option value="expired" {{if eqs .Query.Status "expired"}}selected{{end}}>已过期</option></select></label><label><span>平台</span><input name="platform" value="{{.Query.Platform}}"></label><label><span>开始日期</span><input name="from" type="date" value="{{.From}}"></label><label><span>结束日期</span><input name="to" type="date" value="{{.To}}"></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/oauth/tickets">清除</a></div></form>
<div class="table-wrap"><table>
<thead><tr><th>Ticket</th><th>用户</th><th>状态</th><th>携带令牌</th><th>创建</th><th>过期</th><th>客户端</th></tr></thead>
<tbody>{{range .Tickets}}{{$ticketStatus:=oauthLifecycle .UsedAt .ExpiresAt}}<tr><td><a href="/admin/oauth/tickets/{{.ID}}"><code class="truncate-code">{{.ID}}</code></a></td><td>{{.UserLabel}}</td><td><span class="status {{statusClass $ticketStatus}}">{{statusLabel $ticketStatus}}</span></td><td>{{if .HasToken}}是{{else}}否{{end}}</td><td class="secondary">{{.CreatedAt}}</td><td class="secondary">{{.ExpiresAt}}</td><td>{{.AppID}}<span class="cell-note">{{.Platform}}</span></td></tr>{{else}}<tr><td class="table-empty" colspan="7">暂无登录 Ticket</td></tr>{{end}}</tbody>
</table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_clients"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>客户端统计</h1><p>按应用、版本、构建和平台聚合 OAuth 请求</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<section class="panel list-panel">
  <form class="filter-bar embedded" method="get" action="/admin/clients"><label class="search-field"><span>关键词</span><input name="q" value="{{.Query.Search}}" placeholder="版本或构建"></label><label><span>App ID</span><input name="app" value="{{.Query.App}}"></label><label><span>结果</span><select name="result"><option value="">全部</option><option value="success" {{if eqs .Query.Result "success"}}selected{{end}}>成功</option><option value="failure" {{if eqs .Query.Result "failure"}}selected{{end}}>失败</option></select></label><label><span>平台</span><input name="platform" value="{{.Query.Platform}}"></label><label><span>开始日期</span><input name="from" type="date" value="{{.From}}"></label><label><span>结束日期</span><input name="to" type="date" value="{{.To}}"></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/clients">清除</a></div></form>
  {{template "clients_table" .Clients}}
  {{template "pagination" .}}
</section>
{{template "admin_close" .}}
{{end}}

{{define "clients_table"}}
<div class="table-wrap"><table>
<thead><tr><th>App ID</th><th>平台</th><th>版本</th><th>构建</th><th>请求</th><th>成功</th><th>失败</th><th>最后出现</th></tr></thead>
<tbody>{{range .}}<tr><td><a href="/admin/clients/detail?app={{.AppID}}&version={{.AppVersion}}&build={{.AppBuild}}&platform={{.Platform}}"><code>{{.AppID}}</code></a></td><td>{{.Platform}}</td><td>{{.AppVersion}}</td><td>{{.AppBuild}}</td><td>{{.RequestCount}}</td><td class="positive">{{.SuccessCount}}</td><td class="negative">{{.FailureCount}}</td><td class="secondary nowrap">{{.LastSeen}}</td></tr>{{else}}<tr><td class="table-empty" colspan="8">暂无客户端数据</td></tr>{{end}}</tbody>
</table></div>
{{end}}

{{define "admin_event_detail"}}{{template "admin_open" .}}{{$event:=.Detail.Event}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/oauth/events">← OAuth 事件</a><div class="title-line"><h1>事件 #{{$event.ID}}</h1><span class="status {{statusClass $event.Result}}">{{statusLabel $event.Result}}</span></div><p>{{$event.Provider}} · <code>{{$event.EventType}}</code></p></div></header><div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>请求</h2></div></div><dl class="settings"><dt>时间</dt><dd>{{$event.CreatedAt}}</dd><dt>App</dt><dd>{{$event.AppID}} · {{$event.AppVersion}} ({{$event.AppBuild}})</dd><dt>平台</dt><dd>{{$event.Platform}}</dd><dt>来源</dt><dd><code>{{$event.IP}}</code></dd><dt>User-Agent</dt><dd><code>{{$event.UserAgent}}</code></dd><dt>耗时</dt><dd>{{$event.LatencyMS}} ms</dd></dl></section><section class="panel"><div class="section-header"><div><h2>OAuth 结果</h2></div></div><dl class="settings"><dt>Provider 用户</dt><dd>{{$event.ProviderUserID}}</dd><dt>预期 Scopes</dt><dd>{{$event.ExpectedScopes}}</dd><dt>实际 Scopes</dt><dd>{{$event.ActualScopes}}</dd></dl>{{if or $event.ErrorCode $event.ErrorMessage}}<details><summary>安全展开错误详情</summary><dl class="settings"><dt>错误码</dt><dd><code>{{$event.ErrorCode}}</code></dd><dt>错误信息</dt><dd><pre class="diagnostic">{{$event.ErrorMessage}}</pre></dd></dl></details>{{end}}</section><section class="panel span-2"><div class="section-header"><div><h2>关联链路</h2></div></div><dl class="settings"><dt>State</dt><dd>{{if .Detail.State}}<a href="/admin/oauth/states/{{.Detail.State.ID}}"><code>{{.Detail.State.ID}}</code></a>{{else if $event.StateID}}<code>{{$event.StateID}}</code>（记录已清理）{{else}}—{{end}}</dd><dt>Ticket</dt><dd>{{if .Detail.Ticket}}<a href="/admin/oauth/tickets/{{.Detail.Ticket.ID}}"><code>{{.Detail.Ticket.ID}}</code></a>{{else if $event.TicketID}}<code>{{$event.TicketID}}</code>（记录已清理）{{else}}—{{end}}</dd></dl></section></div>{{template "admin_close" .}}{{end}}

{{define "admin_state_detail"}}{{template "admin_open" .}}{{$state:=.Detail.State}}{{$stateStatus:=oauthLifecycle $state.UsedAt $state.ExpiresAt}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/oauth/states">← OAuth States</a><h1>State 详情</h1><p><code data-copy="{{$state.ID}}">{{$state.ID}}</code></p></div></header><div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>生命周期</h2></div></div><dl class="settings"><dt>状态</dt><dd><span class="status {{statusClass $stateStatus}}">{{statusLabel $stateStatus}}</span></dd><dt>创建</dt><dd>{{$state.CreatedAt}}</dd><dt>过期</dt><dd>{{$state.ExpiresAt}}</dd><dt>使用</dt><dd>{{if $state.UsedAt}}{{$state.UsedAt}}{{else}}—{{end}}</dd><dt>Provider / 用途</dt><dd>{{$state.Provider}} / {{$state.Purpose}}</dd></dl></section><section class="panel"><div class="section-header"><div><h2>客户端</h2></div></div><dl class="settings"><dt>App</dt><dd>{{$state.AppID}} · {{$state.AppVersion}} ({{$state.AppBuild}})</dd><dt>平台</dt><dd>{{$state.Platform}}</dd><dt>Return URI</dt><dd><code>{{$state.ReturnURI}}</code></dd><dt>IP</dt><dd><code>{{$state.IP}}</code></dd><dt>User-Agent</dt><dd><code>{{$state.UserAgent}}</code></dd></dl></section><section class="panel span-2"><div class="section-header"><div><h2>关联事件</h2></div></div>{{template "events_table" .Detail.Events}}</section><section class="panel span-2"><div class="section-header"><div><h2>关联 Tickets</h2></div></div><div class="tag-stack">{{range .Detail.Tickets}}<a class="status neutral" href="/admin/oauth/tickets/{{.ID}}">{{.ID}}</a>{{else}}—{{end}}</div></section></div>{{template "admin_close" .}}{{end}}

{{define "admin_ticket_detail"}}{{template "admin_open" .}}{{$ticket:=.Detail.Ticket}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/oauth/tickets">← 登录 Tickets</a><h1>Ticket 详情</h1><p><code data-copy="{{$ticket.ID}}">{{$ticket.ID}}</code></p></div></header><div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>生命周期</h2></div></div><dl class="settings"><dt>用户</dt><dd><a href="/admin/users/{{.Detail.UserID}}">{{$ticket.UserLabel}}</a></dd><dt>创建</dt><dd>{{$ticket.CreatedAt}}</dd><dt>过期</dt><dd>{{$ticket.ExpiresAt}}</dd><dt>使用</dt><dd>{{if $ticket.UsedAt}}{{$ticket.UsedAt}}{{else}}—{{end}}</dd><dt>携带 Provider Token</dt><dd>{{if $ticket.HasToken}}是（密文不展示）{{else}}否{{end}}</dd></dl></section><section class="panel"><div class="section-header"><div><h2>客户端</h2></div></div><dl class="settings"><dt>App</dt><dd>{{$ticket.AppID}}</dd><dt>平台</dt><dd>{{$ticket.Platform}}</dd><dt>Return URI</dt><dd><code>{{$ticket.ReturnURI}}</code></dd></dl></section><section class="panel span-2"><div class="section-header"><div><h2>关联事件</h2></div></div>{{template "events_table" .Detail.Events}}</section><section class="panel span-2"><div class="section-header"><div><h2>关联 States</h2></div></div><div class="tag-stack">{{range .Detail.States}}<a class="status neutral" href="/admin/oauth/states/{{.ID}}">{{.ID}}</a>{{else}}—{{end}}</div></section></div>{{template "admin_close" .}}{{end}}

{{define "admin_client_detail"}}{{template "admin_open" .}}{{$client:=.Detail.Stats}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/clients">← 客户端统计</a><h1>{{$client.AppID}}</h1><p>{{$client.Platform}} · {{$client.AppVersion}} ({{$client.AppBuild}})</p></div></header><div class="metrics"><article><span>请求</span><strong>{{$client.RequestCount}}</strong></article><article><span>成功</span><strong>{{$client.SuccessCount}}</strong></article><article><span>失败</span><strong>{{$client.FailureCount}}</strong></article><article><span>最后出现</span><strong>{{$client.LastSeen}}</strong></article></div><section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/clients/detail"><input type="hidden" name="app" value="{{$client.AppID}}"><input type="hidden" name="version" value="{{$client.AppVersion}}"><input type="hidden" name="build" value="{{$client.AppBuild}}"><input type="hidden" name="platform" value="{{$client.Platform}}"><label><span>结果</span><select name="result"><option value="">全部</option><option value="success" {{if eqs .Detail.Events.Query.Result "success"}}selected{{end}}>成功</option><option value="failure" {{if eqs .Detail.Events.Query.Result "failure"}}selected{{end}}>失败</option></select></label><label><span>开始日期</span><input name="from" type="date" value="{{.From}}"></label><label><span>结束日期</span><input name="to" type="date" value="{{.To}}"></label><button class="filled-button">筛选事件</button></form>{{template "events_table" .Detail.Events.Items}}{{template "pagination" .}}</section>{{template "admin_close" .}}{{end}}

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

{{define "admin_announcements"}}{{template "admin_open" .}}<header class="page-header"><div><h1>公告</h1><p>发布和检索客户端系统公告</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>{{if .Action}}<div class="notice success toast-notice" data-toast>公告操作已完成</div>{{end}}<div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>发布公告</h2></div></div><form method="post" action="/admin/announcements"><label><span>标题</span><input name="title" maxlength="200" required></label><label><span>正文</span><textarea name="body" rows="8" required></textarea></label><button class="filled-button">发布</button></form></section><section class="panel"><div class="section-header"><div><h2>筛选</h2></div></div><form method="get" action="/admin/announcements"><label><span>搜索</span><input name="q" value="{{.Query.Search}}"></label><label><span>开始</span><input type="date" name="from" value="{{.From}}"></label><label><span>结束</span><input type="date" name="to" value="{{.To}}"></label><button class="outlined-button">筛选</button></form></section><section class="panel span-2"><div class="stack-list">{{range .Items}}<article class="stack-row"><div><strong>{{.Title}}</strong><span class="cell-note">{{.Creator}} · {{dateTime .PublishedAt}}</span><p>{{.Body}}</p></div><form method="post" action="/admin/announcements/{{.ID}}/delete"><button class="outlined-button danger" data-confirm="确定删除这条公告吗？">删除</button></form></article>{{else}}<p class="muted">暂无公告</p>{{end}}</div>{{template "pagination" .}}</section></div>{{template "admin_close" .}}{{end}}

{{define "admin_releases"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>客户端版本</h1><p>发布版本并维护各平台、架构和渠道的更新信息</p></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>客户端版本已更新</div>{{end}}
<div class="content-grid">
<section class="panel"><div class="section-header"><div><h2>发布新版本</h2></div></div><form method="post" action="/admin/releases">
<label><span>版本号</span><input name="version" required placeholder="1.2.3"></label><label><span>渠道</span><select name="channel"><option value="stable">stable</option><option value="beta">beta</option><option value="nightly">nightly</option></select></label>
<label><span>平台</span><select name="platform"><option value="all">全部</option><option value="android">Android</option><option value="linux">Linux</option><option value="windows">Windows</option><option value="macos">macOS</option><option value="web">Web</option></select></label><label><span>架构</span><input name="arch" value="all" placeholder="all / x64 / arm64"></label>
<label><span>最低可用版本</span><input name="minimum_version" placeholder="留空表示不强制更新"></label><label><span>下载地址模板</span><input name="download_url" required placeholder="支持 {version} {platform} {arch} {channel}"></label>
<label><span>中文更新说明</span><textarea name="notes_zh" rows="5"></textarea></label><label><span>英文更新说明</span><textarea name="notes_en" rows="5"></textarea></label><div class="actions"><button class="filled-button" type="submit">发布版本</button></div></form></section>
<section class="panel"><div class="section-header"><div><h2>发布历史</h2></div></div><form class="filter-bar embedded" method="get" action="/admin/releases"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}"></label><label><span>渠道</span><input name="channel" value="{{.Query.Channel}}"></label><label><span>平台</span><input name="platform" value="{{.Query.Platform}}"></label><label><span>架构</span><input name="arch" value="{{.Query.Arch}}"></label><label><span>状态</span><select name="state"><option value="">全部</option><option value="enabled">启用</option><option value="disabled">停用</option><option value="revoked">已撤回</option></select></label><button class="filled-button">筛选</button></form><div class="table-wrap"><table><thead><tr><th>版本</th><th>渠道</th><th>目标</th><th>状态</th><th>发布时间</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/releases/{{.ID}}">{{.Version}}</a></td><td>{{.Channel}}</td><td>{{.Platform}} / {{.Arch}}</td><td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span></td><td>{{dateTime .PublishedAt}}</td><td><a class="row-action" href="/admin/releases/{{.ID}}">详情</a></td></tr>{{else}}<tr><td colspan="6" class="table-empty">尚未发布客户端版本</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>
</div>{{template "admin_close" .}}
{{end}}

{{define "admin_release_detail"}}{{template "admin_open" .}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/releases">← 客户端版本</a><div class="title-line"><h1>{{.Item.Version}}</h1><span class="status {{statusClass .Item.State}}">{{statusLabel .Item.State}}</span></div><p>{{.Item.Channel}} · {{.Item.Platform}} / {{.Item.Arch}}</p></div></header>{{if .Action}}<div class="notice success toast-notice" data-toast>版本已更新</div>{{end}}<div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>发布说明</h2></div></div><form method="post" action="/admin/releases/{{.Item.ID}}/notes"><label><span>最低版本</span><input name="minimum_version" value="{{.Item.MinimumVersion}}"></label><label><span>中文说明</span><textarea name="notes_zh" rows="8">{{.Item.NotesZH}}</textarea></label><label><span>英文说明</span><textarea name="notes_en" rows="8">{{.Item.NotesEN}}</textarea></label><button class="filled-button">保存说明</button></form></section><section class="panel"><div class="section-header"><div><h2>生命周期</h2></div></div><dl class="settings"><dt>下载地址</dt><dd><a href="{{.Item.DownloadURL}}">{{.Item.DownloadURL}}</a></dd><dt>发布时间</dt><dd>{{dateTime .Item.PublishedAt}}</dd><dt>更新时间</dt><dd>{{dateTime .Item.UpdatedAt}}</dd></dl><div class="management-actions">{{if eqs .Item.State "enabled"}}<form method="post" action="/admin/releases/{{.Item.ID}}/state"><button class="outlined-button" name="action" value="disable">停用</button></form>{{else if eqs .Item.State "disabled"}}<form method="post" action="/admin/releases/{{.Item.ID}}/state"><button class="filled-button" name="action" value="enable">启用</button></form>{{end}}{{if not .Item.RevokedAt}}<form method="post" action="/admin/releases/{{.Item.ID}}/state"><button class="outlined-button danger" name="action" value="revoke" data-confirm="永久撤回后不能恢复，确定继续吗？">永久撤回</button></form>{{end}}</div></section></div>{{template "admin_close" .}}{{end}}

{{define "admin_health"}}
{{template "admin_open" .}}
{{template "page_header" .}}
{{if .Action}}<div class="notice success toast-notice" data-toast>过期记录清理已完成，完整结果已写入审计日志</div>{{end}}
{{if .Error}}<div class="notice danger" role="alert">{{.Error}}</div>{{end}}
<section class="metrics system-metrics" aria-label="系统运行指标">
  <article><span>服务运行时间</span><strong>{{.Uptime}}</strong><small>启动于 {{dateTime .Stats.StartedAt}}</small></article>
  <article><span>发布队列就绪</span><strong>{{.Diagnostics.Publications.Ready}}</strong><small>{{.Diagnostics.Publications.Delayed}} 个延迟任务</small></article>
  <article><span>失败任务</span><strong>{{.Diagnostics.Publications.Failed}}</strong><small>{{.Diagnostics.Blobs.ReplicaFailed}} 个副本失败</small></article>
  <article><span>OAuth 错误率</span><strong>{{printf "%.2f%%" .Diagnostics.OAuth.FailureRate}}</strong><small>近 24 小时 {{.Diagnostics.OAuth.Failures24Hours}} / {{.Diagnostics.OAuth.Events24Hours}}</small></article>
</section>
<div class="content-grid settings-grid">
  <section class="panel"><div class="section-header"><div><h2>数据库</h2><p>连接、响应时间与当前数据库占用</p></div><span class="status {{if eqs .DBStatus "ok"}}success{{else}}danger{{end}}">{{if eqs .DBStatus "ok"}}正常{{else}}异常{{end}}</span></div><dl class="settings compact"><dt>Ping</dt><dd>{{.DBLatency}}</dd><dt>数据库大小</dt><dd>{{humanBytes .Diagnostics.DatabaseSizeBytes}}</dd><dt>活动连接</dt><dd>{{.Diagnostics.DatabaseSessions}}</dd></dl>{{if not (eqs .DBStatus "ok")}}<p class="diagnostic row-error">{{.DBStatus}}</p>{{end}}</section>
  <section class="panel"><div class="section-header"><div><h2>发布队列</h2><p>执行就绪、延迟和疑似卡死任务</p></div></div><dl class="settings compact"><dt>等待 / 运行 / 审核</dt><dd>{{.Diagnostics.Publications.Pending}} / {{.Diagnostics.Publications.Running}} / {{.Diagnostics.Publications.Reviewing}}</dd><dt>已发布</dt><dd>{{.Diagnostics.Publications.Published}}</dd><dt>失败 / 取消</dt><dd>{{.Diagnostics.Publications.Failed}} / {{.Diagnostics.Publications.Cancelled}}</dd><dt>运行超过 15 分钟</dt><dd>{{.Diagnostics.Publications.StaleRunning}}</dd></dl><a class="text-link" href="/admin/publications?state=failed">查看失败发布任务</a></section>
  <section class="panel"><div class="section-header"><div><h2>Blob 与副本</h2><p>目录存储量及 R2 副本状态</p></div></div><dl class="settings compact"><dt>Blob</dt><dd>{{.Diagnostics.Blobs.Count}} 个 · {{humanBytes .Diagnostics.Blobs.SizeBytes}}</dd><dt>缺少副本记录</dt><dd>{{.Diagnostics.Blobs.ReplicaMissing}}</dd><dt>等待 / 上传中</dt><dd>{{.Diagnostics.Blobs.ReplicaPending}} / {{.Diagnostics.Blobs.ReplicaUploading}}</dd><dt>就绪 / 失败</dt><dd>{{.Diagnostics.Blobs.ReplicaReady}} / {{.Diagnostics.Blobs.ReplicaFailed}}</dd><dt>可立即重试失败</dt><dd>{{.Diagnostics.Blobs.ReplicaRetryReady}}</dd></dl><a class="text-link" href="/admin/storage/blobs?replica_state=failed">检查失败副本</a></section>
  <section class="panel"><div class="section-header"><div><h2>OAuth</h2><p>近 24 小时服务端事件结果</p></div></div><dl class="settings compact"><dt>事件</dt><dd>{{.Diagnostics.OAuth.Events24Hours}}</dd><dt>失败</dt><dd>{{.Diagnostics.OAuth.Failures24Hours}}</dd><dt>失败率</dt><dd>{{printf "%.2f%%" .Diagnostics.OAuth.FailureRate}}</dd><dt>有效 State / Ticket</dt><dd>{{.Stats.ActiveStates}} / {{.Stats.ActiveTickets}}</dd><dt>今日 Scope 异常</dt><dd>{{.Stats.ScopeMismatchToday}}</dd></dl><a class="text-link" href="/admin/oauth/events?result=failure">查看失败事件</a></section>
  <section class="panel span-2"><div class="section-header"><div><h2>手动清理</h2><p>仅清理过期 OAuth State、登录 Ticket、后台会话和系统消息。必须先生成快照预览，再输入危险确认短语执行；两步均会审计。</p></div><span class="status warning">Admin only</span></div>
    {{if .CleanupPreview}}
    <div class="metrics compact-metrics" aria-label="清理预览"><article><span>OAuth State</span><strong>{{.CleanupPreview.OAuthStates}}</strong></article><article><span>登录 Ticket</span><strong>{{.CleanupPreview.LoginTickets}}</strong></article><article><span>后台会话</span><strong>{{.CleanupPreview.AdminSessions}}</strong></article><article><span>系统消息</span><strong>{{.CleanupPreview.UserMessages}}</strong></article></div>
    <div class="notice warning"><strong>预览截止时间：</strong>{{dateTime .CleanupPreview.Cutoff}}。执行不会删除此时间之后新增或过期的记录。预览 10 分钟后失效。</div>
    <form method="post" action="/admin/cleanup/execute" class="decision-form"><input type="hidden" name="preview_token" value="{{.CleanupToken}}"><label><span>请输入危险确认短语：<code>{{.CleanupConfirmation}}</code></span><input name="confirmation" autocomplete="off" required></label><button class="outlined-button danger" type="submit" data-confirm="这是不可撤销的数据库删除操作。确认执行刚才预览的清理吗？">执行清理</button></form>
    {{else}}
    <form method="post" action="/admin/cleanup/preview"><button class="outlined-button" type="submit">预览可清理记录</button></form>
    {{end}}
  </section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_blobs"}}{{template "admin_open" .}}<header class="page-header"><div><h1>Blob 与副本</h1><p>检查本地对象、R2 副本和内容引用</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>{{if .Action}}<div class="notice success toast-notice" data-toast>副本任务已重新入队</div>{{end}}<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/storage/blobs"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="SHA、媒体类型、对象 Key 或错误"></label><label><span>媒体类型</span><input name="media_type" value="{{.Query.MediaType}}"></label><label><span>副本状态</span><select name="replica_state"><option value="">全部</option><option value="missing">缺失</option><option value="pending">等待</option><option value="uploading">上传中</option><option value="ready">就绪</option><option value="failed">失败</option></select></label><label><span>引用</span><select name="referenced"><option value="">全部</option><option value="referenced">已引用</option><option value="unreferenced">未引用</option></select></label><button class="filled-button">筛选</button></form><div class="table-wrap"><table><thead><tr><th>SHA-256</th><th>类型</th><th>大小</th><th>本地</th><th>R2</th><th>引用</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/storage/blobs/{{.SHA256}}"><code>{{.SHA256}}</code></a></td><td>{{.MediaType}}</td><td>{{.SizeBytes}} B</td><td>{{if .LocalAvailable}}可用{{else}}缺失{{end}}</td><td><span class="status {{statusClass .R2State}}">{{statusLabel .R2State}}</span>{{if .R2ErrorMessage}}<span class="cell-note row-error">{{.R2ErrorMessage}}</span>{{end}}</td><td>{{.ReferenceCount}}</td><td><a class="row-action" href="/admin/storage/blobs/{{.SHA256}}">详情</a></td></tr>{{else}}<tr><td colspan="7" class="table-empty">没有符合筛选条件的 Blob</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>{{template "admin_close" .}}{{end}}

{{define "admin_blob_detail"}}{{template "admin_open" .}}{{$blob:=.Detail.Blob}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/storage/blobs">← Blob 与副本</a><h1>Blob 详情</h1><p><code data-copy="{{$blob.SHA256}}">{{$blob.SHA256}}</code></p></div><div class="header-actions"><a class="outlined-button" href="/admin/blobs/{{$blob.SHA256}}?download=1">下载</a></div></header>{{if .Action}}<div class="notice success toast-notice" data-toast>副本任务已重新入队</div>{{end}}<div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>存储</h2></div></div><dl class="settings"><dt>媒体类型</dt><dd>{{$blob.MediaType}}</dd><dt>大小</dt><dd>{{$blob.SizeBytes}} B</dd><dt>本地 Key</dt><dd><code>{{$blob.LocalKey}}</code></dd><dt>R2 状态</dt><dd><span class="status {{statusClass $blob.R2State}}">{{statusLabel $blob.R2State}}</span></dd><dt>R2 Key</dt><dd><code>{{$blob.R2ObjectKey}}</code></dd><dt>尝试</dt><dd>{{$blob.R2Attempts}}</dd></dl>{{if eqs $blob.R2State "failed"}}<form method="post" action="/admin/storage/blobs/{{$blob.SHA256}}/requeue"><button class="filled-button">重新入队</button></form>{{end}}{{if $blob.R2ErrorMessage}}<p class="row-error">{{$blob.R2ErrorMessage}}</p>{{end}}</section><section class="panel"><div class="section-header"><div><h2>引用汇总</h2></div></div><div class="metrics compact-metrics"><article><span>资源</span><strong>{{len .Detail.Resources}}</strong></article><article><span>修订</span><strong>{{len .Detail.Revisions}}</strong></article><article><span>Blog</span><strong>{{len .Detail.Blogs}}</strong></article><article><span>Banner</span><strong>{{len .Detail.Banners}}</strong></article></div></section><section class="panel span-2"><div class="section-header"><div><h2>资源修订引用</h2></div></div><div class="table-wrap"><table><thead><tr><th>资源</th><th>修订</th><th>状态</th><th>用途</th></tr></thead><tbody>{{range .Detail.Revisions}}<tr><td><a href="/admin/resources/{{.ResourceID}}">{{.ResourceSlug}}</a></td><td><a href="/admin/resources/{{.ResourceID}}/revisions/{{.ID}}">#{{.RevisionNumber}} · {{.Name}}</a></td><td>{{statusLabel .State}}</td><td>{{.Usages}}</td></tr>{{else}}<tr><td colspan="4" class="table-empty">无资源引用</td></tr>{{end}}</tbody></table></div></section></div>{{template "admin_close" .}}{{end}}

{{define "admin_audit"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>审计日志</h1><p>检索管理员操作和服务端处置结果</p></div><div class="header-actions"><a class="outlined-button" href="{{.ExportURL}}"><span class="material-symbols-outlined">download</span>导出 CSV</a><span class="count-badge">{{.Page.Total}} 项</span></div></header>
<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/audit"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="账号、动作、IP 或消息"></label><label><span>结果</span><select name="result"><option value="">全部</option><option value="success" {{if eqs .Query.Result "success"}}selected{{end}}>成功</option><option value="failure" {{if eqs .Query.Result "failure"}}selected{{end}}>失败</option></select></label><label><span>目标类型</span><select name="target_type"><option value="">全部</option><option value="resource" {{if eqs .Query.TargetType "resource"}}selected{{end}}>资源</option><option value="user" {{if eqs .Query.TargetType "user"}}selected{{end}}>用户</option><option value="ticket" {{if eqs .Query.TargetType "ticket"}}selected{{end}}>举报工单</option><option value="feedback" {{if eqs .Query.TargetType "feedback"}}selected{{end}}>反馈工单</option><option value="blob" {{if eqs .Query.TargetType "blob"}}selected{{end}}>Blob</option></select></label><label><span>目标 ID</span><input name="target_id" value="{{.Query.TargetID}}"></label><label><span>操作者 ID</span><input name="actor_user_id" value="{{.Query.ActorUserID}}"></label><label><span>开始日期</span><input name="from" type="date" value="{{.From}}"></label><label><span>结束日期</span><input name="to" type="date" value="{{.To}}"></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/audit">清除</a></div></form>
<div class="table-wrap"><table>
<thead><tr><th>时间</th><th>账号</th><th>动作</th><th>结果</th><th>IP</th><th>消息</th></tr></thead>
<tbody>{{range .Logs}}<tr><td class="secondary nowrap"><a href="/admin/audit/{{.ID}}">{{.CreatedAt}}</a></td><td>{{if .ActorUserID}}<a href="/admin/users/{{.ActorUserID}}">{{.Username}}</a>{{else}}{{.Username}}{{end}}</td><td><code>{{.Action}}</code>{{if .Target.ID}}<span class="cell-note">{{.Target.Type}} · {{.Target.ID}}</span>{{end}}</td><td><span class="status {{statusClass .Result}}">{{statusLabel .Result}}</span></td><td><code>{{.IP}}</code></td><td class="wrap-cell">{{.Message}}</td></tr>{{else}}<tr><td class="table-empty" colspan="6">暂无审计记录</td></tr>{{end}}</tbody>
</table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_audit_detail"}}
{{template "admin_open" .}}{{$item:=.Item}}{{$targetURL:=$item.Target.AdminURL}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/audit">← 审计日志</a><div class="title-line"><h1>审计 #{{$item.ID}}</h1><span class="status {{statusClass $item.Result}}">{{statusLabel $item.Result}}</span></div><p><code>{{$item.Action}}</code> · {{$item.CreatedAt}}</p></div>{{if $targetURL}}<div class="header-actions"><a class="filled-button" href="{{$targetURL}}">打开关联对象</a></div>{{end}}</header>
<div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>请求与操作者</h2></div></div><dl class="settings"><dt>操作者</dt><dd>{{if $item.ActorUserID}}<a href="/admin/users/{{$item.ActorUserID}}">{{$item.Username}}</a><span class="cell-note"><code>{{$item.ActorUserID}}</code></span><a class="text-link" href="/admin/audit?actor_user_id={{$item.ActorUserID}}">该用户的审计记录</a>{{else}}{{$item.Username}}（账号已删除或系统操作）{{end}}</dd><dt>IP</dt><dd><code>{{$item.IP}}</code></dd><dt>User-Agent</dt><dd class="wrap-cell">{{$item.UserAgent}}</dd><dt>消息</dt><dd class="wrap-cell">{{$item.Message}}</dd></dl></section><section class="panel"><div class="section-header"><div><h2>关联目标</h2></div></div>{{if $item.Target.ID}}<dl class="settings"><dt>类型</dt><dd>{{$item.Target.Type}}</dd><dt>ID</dt><dd><code>{{$item.Target.ID}}</code></dd><dt>标签</dt><dd>{{$item.Target.Label}}</dd></dl><div class="button-row">{{if $targetURL}}<a class="outlined-button" href="{{$targetURL}}">查看关联对象</a>{{end}}<a class="text-link" href="/admin/audit?target_type={{$item.Target.Type}}&amp;target_id={{$item.Target.ID}}">该对象的审计记录</a></div>{{else}}<p class="muted">该记录没有结构化关联目标；旧记录仍可通过消息和元数据追溯。</p>{{end}}</section><section class="panel"><div class="section-header"><div><h2>变更前</h2></div></div><pre class="code-block">{{prettyJSON $item.Before}}</pre></section><section class="panel"><div class="section-header"><div><h2>变更后</h2></div></div><pre class="code-block">{{prettyJSON $item.After}}</pre></section><section class="panel span-2"><div class="section-header"><div><h2>完整元数据</h2><p>保留旧 metadata 内容，便于兼容历史日志。</p></div></div><pre class="code-block">{{prettyJSON $item.Metadata}}</pre></section></div>
{{template "admin_close" .}}
{{end}}

{{define "admin_messages"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>系统消息</h1><p>只读查看发送给用户的站内消息与读取状态</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/messages"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="标题、正文、引用或消息 ID"></label><label><span>类型</span><select name="kind"><option value="">全部</option>{{range $kind := splitMessageKinds}}<option value="{{$kind}}" {{if eqs $.Query.Kind $kind}}selected{{end}}>{{kindLabel $kind}}</option>{{end}}</select></label><label><span>读取状态</span><select name="read"><option value="">全部</option><option value="unread" {{if eqs .Query.Read "unread"}}selected{{end}}>未读</option><option value="read" {{if eqs .Query.Read "read"}}selected{{end}}>已读</option></select></label><label><span>用户</span><input name="user" value="{{.Query.User}}" placeholder="用户名或 UUID"></label><label><span>开始日期</span><input type="date" name="from" value="{{.From}}"></label><label><span>结束日期</span><input type="date" name="to" value="{{.To}}"></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/messages">清除</a></div></form><div class="table-wrap"><table><thead><tr><th>消息</th><th>用户</th><th>类型</th><th>读取状态</th><th>发送时间</th><th>到期时间</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/messages/{{.ID}}">{{.Title}}</a><span class="cell-note">{{.Body}}</span></td><td><a href="/admin/users/{{.UserID}}">{{.Username}}</a></td><td>{{kindLabel .Kind}}</td><td>{{if .ReadAt}}<span class="status success">已读</span>{{else}}<span class="status neutral">未读</span>{{end}}</td><td>{{dateTime .CreatedAt}}</td><td>{{dateTime .ExpiresAt}}</td><td><a class="row-action" href="/admin/messages/{{.ID}}">详情</a></td></tr>{{else}}<tr><td class="table-empty" colspan="7">没有符合筛选条件的系统消息</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_message_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/messages">← 系统消息</a><div class="title-line"><h1>{{.Item.Title}}</h1>{{if .Item.ReadAt}}<span class="status success">已读</span>{{else}}<span class="status neutral">未读</span>{{end}}</div><p>{{kindLabel .Item.Kind}} · {{dateTime .Item.CreatedAt}}</p></div></header>
<div class="content-grid detail-grid"><section class="panel span-2"><div class="section-header"><div><h2>消息正文</h2></div></div><div class="ticket-message">{{.Item.Body}}</div></section><section class="panel"><div class="section-header"><div><h2>关联用户</h2></div></div><dl class="settings"><dt>用户名</dt><dd><a href="/admin/users/{{.Item.UserID}}">{{.Item.Username}}</a></dd><dt>用户 ID</dt><dd><code data-copy="{{.Item.UserID}}">{{.Item.UserID}}</code></dd></dl></section><section class="panel"><div class="section-header"><div><h2>消息信息</h2></div></div><dl class="settings"><dt>消息 ID</dt><dd><code data-copy="{{.Item.ID}}">{{.Item.ID}}</code></dd><dt>类型</dt><dd>{{kindLabel .Item.Kind}}</dd><dt>引用</dt><dd>{{if .Item.Ref}}<code>{{.Item.Ref}}</code>{{else}}—{{end}}</dd><dt>发送时间</dt><dd>{{dateTime .Item.CreatedAt}}</dd><dt>读取时间</dt><dd>{{if .Item.ReadAt}}{{dateTime .Item.ReadAt}}{{else}}未读{{end}}</dd><dt>到期时间</dt><dd>{{dateTime .Item.ExpiresAt}}</dd></dl></section></div>
{{template "admin_close" .}}
{{end}}

{{define "admin_publications"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>发布任务</h1><p>诊断并控制 OronBox、米坛和 AstroBox 发布队列</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>{{if eqs .Action "batch_retried"}}已按当前筛选重新入队 {{.Retried}} 个失败任务{{else}}发布任务已更新{{end}}</div>{{end}}
{{if eqs .Query.State "failed"}}<section class="panel"><div class="section-header"><div><h2>批量处理失败任务</h2><p>作用于当前筛选命中的全部失败任务，不限于当前页；未批准修订会安全跳过。</p></div></div><form method="post" action="/admin/publications/retry-failed" class="management-actions">{{if .Query.Search}}<input type="hidden" name="q" value="{{.Query.Search}}">{{end}}{{if .Query.Target}}<input type="hidden" name="target" value="{{.Query.Target}}">{{end}}{{if .Query.Resource}}<input type="hidden" name="resource" value="{{.Query.Resource}}">{{end}}{{if .Query.Owner}}<input type="hidden" name="owner" value="{{.Query.Owner}}">{{end}}<input type="hidden" name="sort" value="{{.Query.Sort}}"><input type="hidden" name="per_page" value="{{.Page.PerPage}}"><button class="filled-button danger" type="submit" data-confirm="确定重新入队当前筛选命中的全部失败任务吗？此操作不限于当前页。">按当前筛选批量重试（{{.Page.Total}}）</button></form></section>{{end}}
<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/publications"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="资源、创作者、任务 ID 或错误"></label><label><span>目标</span><select name="target"><option value="">全部</option><option value="oronbox" {{if eqs .Query.Target "oronbox"}}selected{{end}}>OronBox</option><option value="bandbbs" {{if eqs .Query.Target "bandbbs"}}selected{{end}}>米坛</option><option value="astrobox" {{if eqs .Query.Target "astrobox"}}selected{{end}}>AstroBox</option></select></label><label><span>状态</span><select name="state"><option value="">全部</option>{{range $state := splitStates}}<option value="{{$state}}" {{if eqs $.Query.State $state}}selected{{end}}>{{statusLabel $state}}</option>{{end}}</select></label><label><span>资源</span><input name="resource" value="{{.Query.Resource}}" placeholder="Slug 或 ID"></label><label><span>创作者</span><input name="owner" value="{{.Query.Owner}}" placeholder="用户名或 ID"></label><label><span>排序</span><select name="sort"><option value="updated_desc" {{if eqs .Query.Sort "updated_desc"}}selected{{end}}>最近更新</option><option value="created_desc" {{if eqs .Query.Sort "created_desc"}}selected{{end}}>最近创建</option><option value="attempts_desc" {{if eqs .Query.Sort "attempts_desc"}}selected{{end}}>尝试次数</option></select></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/publications">清除</a></div></form><div class="table-wrap"><table><thead><tr><th>资源</th><th>目标</th><th>状态</th><th>尝试</th><th>下次执行</th><th>更新时间</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/publications/{{.ID}}">{{.RevisionName}}</a><span class="cell-note">{{.Owner}} · #{{.RevisionNumber}}</span></td><td>{{targetLabel .Target}}</td><td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span>{{if .ErrorMessage}}<span class="cell-note row-error">{{.ErrorMessage}}</span>{{end}}</td><td>{{.Attempts}}</td><td>{{dateTime .NextAttemptAt}}</td><td>{{dateTime .UpdatedAt}}</td><td><a class="row-action" href="/admin/publications/{{.ID}}">详情</a></td></tr>{{else}}<tr><td class="table-empty" colspan="7">没有符合筛选条件的发布任务</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_publication_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/publications">← 发布任务</a><div class="title-line"><h1>{{.Item.RevisionName}}</h1><span class="status {{statusClass .Item.State}}">{{statusLabel .Item.State}}</span></div><p>{{targetLabel .Item.Target}} · 修订 #{{.Item.RevisionNumber}}</p></div></header>{{if .Action}}<div class="notice success toast-notice" data-toast>发布任务已更新</div>{{end}}
<div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>任务信息</h2></div></div><dl class="settings"><dt>任务 ID</dt><dd><code data-copy="{{.Item.ID}}">{{.Item.ID}}</code></dd><dt>资源</dt><dd><a href="/admin/resources/{{.Item.ResourceID}}">{{.Item.RevisionName}}</a></dd><dt>创作者</dt><dd>{{.Item.Owner}}</dd><dt>目标</dt><dd>{{targetLabel .Item.Target}}</dd><dt>尝试次数</dt><dd>{{.Item.Attempts}}</dd><dt>下次执行</dt><dd>{{dateTime .Item.NextAttemptAt}}</dd>{{if .Item.ExternalURL}}<dt>外部页面</dt><dd><a href="{{.Item.ExternalURL}}" target="_blank" rel="noopener">打开 ↗</a></dd>{{end}}</dl></section><section class="panel"><div class="section-header"><div><h2>队列操作</h2><p>运行中的任务不能取消，避免外部发布完成后覆盖状态</p></div></div><div class="management-actions">{{if or (eqs .Item.State "failed") (eqs .Item.State "cancelled")}}<form method="post" action="/admin/publications/{{.Item.ID}}"><button class="filled-button" name="action" value="requeue">重新入队</button></form>{{end}}{{if or (eqs .Item.State "pending") (eqs .Item.State "failed") (eqs .Item.State "reviewing")}}<form method="post" action="/admin/publications/{{.Item.ID}}"><button class="outlined-button danger" name="action" value="cancel" data-confirm="确定取消此发布任务吗？">取消任务</button></form>{{end}}</div></section><section class="panel"><div class="section-header"><div><h2>发布配置</h2></div></div><pre class="diagnostic">{{prettyJSON .Item.Config}}</pre></section><section class="panel"><div class="section-header"><div><h2>状态详情</h2></div></div><pre class="diagnostic">{{prettyJSON .Item.StatusDetail}}</pre>{{if .Item.ErrorMessage}}<p class="row-error">{{.Item.ErrorMessage}}</p>{{end}}</section><section class="panel span-2"><div class="section-header"><div><h2>执行历史</h2><p>每次执行、外部状态检查和管理员队列操作都会追加记录</p></div></div><div class="table-wrap"><table><thead><tr><th>时间</th><th>阶段</th><th>尝试</th><th>事件</th><th>状态变化</th><th>详情</th></tr></thead><tbody>{{range .Item.History}}<tr><td class="secondary nowrap">{{dateTime .CreatedAt}}</td><td>{{publicationPhaseLabel .Phase}}</td><td>#{{.AttemptNumber}}</td><td><strong>{{publicationEventLabel .Event}}</strong>{{if .ErrorMessage}}<span class="cell-note row-error">{{.ErrorMessage}}</span>{{end}}</td><td><span class="status {{statusClass .StateFrom}}">{{statusLabel .StateFrom}}</span> → <span class="status {{statusClass .StateTo}}">{{statusLabel .StateTo}}</span></td><td><details><summary>JSON</summary><pre class="diagnostic">{{prettyJSON .Detail}}</pre></details></td></tr>{{else}}<tr><td colspan="6" class="table-empty">尚无执行历史</td></tr>{{end}}</tbody></table></div></section><section class="panel span-2"><div class="section-header"><div><h2>关联设备</h2></div></div><div class="tag-stack">{{range .Item.Devices}}<span class="status neutral">{{.DisplayName}} · {{.Codename}}</span>{{else}}—{{end}}</div></section></div>
{{template "admin_close" .}}
{{end}}

{{define "admin_review_legacy"}}
{{template "admin_open" .}}<header class="page-header"><div><h1>审核中心</h1><p>按审核单查看提交、差异和处理历史</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>{{if .Decided}}<div class="notice success toast-notice" data-toast>审核决定已保存</div>{{end}}<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/review"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="资源、创作者、修订或审核 ID"></label><label><span>状态</span><select name="state"><option value="">全部</option><option value="pending" {{if eqs .Query.State "pending"}}selected{{end}}>待审核</option><option value="approved" {{if eqs .Query.State "approved"}}selected{{end}}>已通过</option><option value="rejected" {{if eqs .Query.State "rejected"}}selected{{end}}>已拒绝</option></select></label><label><span>类型</span><select name="kind"><option value="">全部</option><option value="quickapp">快应用</option><option value="watchface">表盘</option></select></label><label><span>目标</span><select name="target"><option value="">全部</option><option value="oronbox">OronBox</option><option value="bandbbs">米坛</option><option value="astrobox">AstroBox</option></select></label><label><span>创作者</span><input name="owner" value="{{.Query.Owner}}"></label><label><span>开始</span><input type="date" name="from" value="{{.From}}"></label><label><span>结束</span><input type="date" name="to" value="{{.To}}"></label><button class="filled-button">筛选</button></form><div class="table-wrap"><table><thead><tr><th>资源</th><th>创作者</th><th>类型</th><th>目标</th><th>状态</th><th>更新时间</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/review/{{.ID}}">{{.RevisionName}}</a><span class="cell-note">#{{.RevisionNumber}} · <code>{{.ResourceSlug}}</code></span></td><td>{{.Owner}}</td><td>{{kindLabel .ResourceKind}}</td><td>{{join .Targets " · "}}</td><td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span></td><td>{{dateTime .UpdatedAt}}</td><td><a class="row-action" href="/admin/review/{{.ID}}">审核</a></td></tr>{{else}}<tr><td class="table-empty" colspan="7">没有符合筛选条件的审核单</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>{{template "admin_close" .}}{{end}}

{{define "admin_review_detail_legacy"}}
{{template "admin_open" .}}{{$review := .Detail.Review}}{{$current := .Detail.Current}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/review">← 审核中心</a><div class="title-line"><h1>{{$current.Name}}</h1><span class="status {{statusClass $review.State}}">{{statusLabel $review.State}}</span></div><p>{{$review.Owner}} · 修订 #{{$current.Number}}</p></div><div class="header-actions"><a class="outlined-button" href="/admin/resources/{{$review.ResourceID}}/draft?base={{$current.ID}}">边审核边编辑</a></div></header><div class="metrics"><article><span>元数据</span><strong>{{if .Detail.Diff.MetadataChanged}}有变化{{else}}无变化{{end}}</strong><small>{{join .Detail.Diff.MetadataFields " · "}}</small></article><article><span>媒体变化</span><strong>+{{.Detail.Diff.Media.Added}} / -{{.Detail.Diff.Media.Removed}}</strong></article><article><span>文件变化</span><strong>+{{.Detail.Diff.Artifacts.Added}} / -{{.Detail.Diff.Artifacts.Removed}}</strong></article><article><span>设备变化</span><strong>+{{.Detail.Diff.Devices.Added}} / -{{.Detail.Diff.Devices.Removed}}</strong></article></div><div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>当前提交</h2></div></div><dl class="settings"><dt>名称</dt><dd>{{$current.Name}}</dd><dt>简介</dt><dd>{{$current.Summary}}</dd><dt>付费类型</dt><dd>{{paidTypeLabel $current.PaidType}}</dd><dt>发布计划</dt><dd><pre class="diagnostic">{{prettyJSON $current.PublicationPlan}}</pre></dd></dl></section><section class="panel"><div class="section-header"><div><h2>基础修订</h2></div></div>{{if .Detail.Diff.HasBase}}<dl class="settings"><dt>名称</dt><dd>{{.Detail.Base.Name}}</dd><dt>简介</dt><dd>{{.Detail.Base.Summary}}</dd><dt>付费类型</dt><dd>{{paidTypeLabel .Detail.Base.PaidType}}</dd></dl>{{else}}<p class="muted">首次提交，没有基础修订</p>{{end}}</section><section class="panel span-2"><div class="section-header"><div><h2>审核决定</h2></div></div>{{if eqs $review.State "pending"}}<form method="post" action="/admin/review/{{$current.ID}}" class="decision-form"><fieldset><legend>确认标签</legend><div class="choice-grid">{{range .Attributes}}<label class="choice-card"><input type="checkbox" name="attributes" value="{{.ID}}" {{if containsString $current.Attributes .ID}}checked{{end}}><span><strong>{{.NameZH}}</strong></span></label>{{end}}</div></fieldset><label>策展等级<select name="curation_grade"><option value="standard">普通</option><option value="featured">精选</option></select></label><label>审核意见<textarea name="note" rows="5"></textarea></label><div class="actions"><button class="filled-button" name="decision" value="approve">批准</button><button class="outlined-button danger" name="decision" value="reject">退回</button></div></form>{{else}}<dl class="settings"><dt>审核员</dt><dd>{{$review.Reviewer}}</dd><dt>意见</dt><dd>{{$review.Note}}</dd></dl>{{end}}</section></div>{{template "admin_close" .}}{{end}}

{{define "admin_devices"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>设备目录</h1><p>管理资源适配设备，并以产品名关联安装包和资源</p></div><div class="header-actions"><span class="count-badge">{{.Page.Total}} 项</span><a class="filled-button" href="/admin/devices/new">新增设备</a></div></header>
<section class="panel list-panel">
<form class="filter-bar embedded" method="get" action="/admin/devices">
  <label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="产品名、代号或 AstroBox ID"></label>
  <label><span>平台</span><select name="platform"><option value="">全部平台</option><option value="vela_os" {{if eqs .Query.Platform "vela_os"}}selected{{end}}>VelaOS</option><option value="zepp_os" {{if eqs .Query.Platform "zepp_os"}}selected{{end}}>Zepp OS</option></select></label>
  <label><span>厂商</span><input name="vendor" value="{{.Query.Vendor}}" placeholder="例如 xiaomi"></label>
  <label><span>状态</span><select name="state"><option value="">全部状态</option><option value="enabled" {{if eqs .Query.State "enabled"}}selected{{end}}>启用</option><option value="disabled" {{if eqs .Query.State "disabled"}}selected{{end}}>停用</option></select></label>
  <label><span>排序</span><select name="sort"><option value="name" {{if eqs .Query.Sort "name"}}selected{{end}}>产品名</option><option value="codename" {{if eqs .Query.Sort "codename"}}selected{{end}}>设备代号</option><option value="platform" {{if eqs .Query.Sort "platform"}}selected{{end}}>平台</option><option value="vendor" {{if eqs .Query.Sort "vendor"}}selected{{end}}>厂商</option><option value="resources_desc" {{if eqs .Query.Sort "resources_desc"}}selected{{end}}>关联资源数</option><option value="artifacts_desc" {{if eqs .Query.Sort "artifacts_desc"}}selected{{end}}>安装包数</option></select></label>
  <div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link filter-reset" href="/admin/devices">清除</a></div>
</form>
<div class="table-panel"><div class="table-wrap"><table><thead><tr><th>产品</th><th>平台</th><th>厂商</th><th>AstroBox ID</th><th>资源</th><th>安装包</th><th></th></tr></thead><tbody>
{{range .Items}}<tr><td><a class="resource-name" href="/admin/devices/{{.ID}}">{{.DisplayName}}</a><span class="cell-note"><code>{{.Codename}}</code> · {{if .Enabled}}启用{{else}}停用{{end}}</span></td><td>{{platformLabel .Platform}}</td><td>{{if .Vendor}}{{.Vendor}}{{else}}—{{end}}</td><td>{{if .AstroBoxID}}<code data-copy="{{.AstroBoxID}}">{{.AstroBoxID}}</code>{{else}}—{{end}}</td><td>{{.ResourceCount}}</td><td>{{.ArtifactCount}}</td><td><a class="row-action" href="/admin/devices/{{.ID}}">查看</a></td></tr>{{else}}<tr><td class="table-empty" colspan="7">没有符合筛选条件的设备</td></tr>{{end}}
</tbody></table></div>{{template "pagination" .}}</div>
</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_device_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/devices">← 设备目录</a><div class="title-line"><h1>{{if .New}}新增设备{{else}}{{.Item.DisplayName}}{{end}}</h1>{{if not .New}}<span class="status {{if .Item.Enabled}}success{{else}}neutral{{end}}">{{if .Item.Enabled}}启用{{else}}停用{{end}}</span>{{end}}</div>{{if not .New}}<p><code>{{.Item.Codename}}</code></p>{{end}}</div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>设备目录已更新</div>{{end}}
<div class="content-grid detail-grid">
  <section class="panel"><div class="section-header"><div><h2>设备信息</h2><p>代号与 AstroBox ID 在目录中不可重复</p></div></div><form class="editor-field-grid" method="post" action="/admin/devices/{{if .New}}new{{else}}{{.Item.ID}}{{end}}"><label><span>产品名</span><input name="display_name" value="{{.Item.DisplayName}}" maxlength="120" required></label><label><span>设备代号</span><input name="codename" value="{{.Item.Codename}}" pattern="[a-z0-9][a-z0-9-]{0,63}" required></label><label><span>平台</span><select name="platform"><option value="vela_os" {{if eqs .Item.Platform "vela_os"}}selected{{end}}>VelaOS</option><option value="zepp_os" {{if eqs .Item.Platform "zepp_os"}}selected{{end}}>Zepp OS</option></select></label><label><span>厂商</span><input name="vendor" value="{{.Item.Vendor}}" maxlength="80"></label><label><span>AstroBox ID</span><input name="astrobox_id" value="{{.Item.AstroBoxID}}" maxlength="120"></label><label class="choice-card"><input type="checkbox" name="enabled" {{if .Item.Enabled}}checked{{end}}><span><strong>允许新资源选择</strong><small>停用不移除历史绑定</small></span></label><div class="span-2 actions"><button class="filled-button" type="submit">{{if .New}}创建设备{{else}}保存设备{{end}}</button></div></form></section>
  <section class="panel"><div class="section-header"><div><h2>关联统计</h2></div></div><div class="metrics compact-metrics"><article><span>资源</span><strong>{{.Item.ResourceCount}}</strong></article><article><span>安装包</span><strong>{{.Item.ArtifactCount}}</strong></article></div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>关联资源</h2><p>所有历史修订中绑定过该设备的资源</p></div></div><div class="table-wrap"><table><thead><tr><th>资源</th><th>创作者</th><th>类型</th><th>状态</th><th>更新时间</th><th></th></tr></thead><tbody>{{range .Resources}}<tr><td>{{.Name}}<span class="cell-note"><code>{{.Slug}}</code></span></td><td>{{.Owner}}</td><td>{{kindLabel .Kind}}</td><td><span class="status {{statusClass .ModerationState}}">{{statusLabel .ModerationState}}</span></td><td>{{dateTime .UpdatedAt}}</td><td><a class="row-action" href="/admin/resources/{{.ID}}">查看</a></td></tr>{{else}}<tr><td class="table-empty" colspan="6">暂无关联资源</td></tr>{{end}}</tbody></table></div></section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_review"}}
{{template "admin_open" .}}<header class="page-header"><div><h1>审核中心</h1><p>审核清单可独立保存；批量分配严格执行全有或全无</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>{{if .Decided}}<div class="notice success toast-notice" data-toast>审核决定已保存</div>{{end}}{{if .BulkDone}}<div class="notice success toast-notice" data-toast>批量操作已完成</div>{{end}}<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/review"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="资源、创作者、修订或审核 ID"></label><label><span>状态</span><select name="state"><option value="">全部</option><option value="pending" {{if eqs .Query.State "pending"}}selected{{end}}>待审核</option><option value="approved" {{if eqs .Query.State "approved"}}selected{{end}}>已通过</option><option value="rejected" {{if eqs .Query.State "rejected"}}selected{{end}}>已拒绝</option><option value="superseded" {{if eqs .Query.State "superseded"}}selected{{end}}>已替代</option></select></label><label><span>类型</span><select name="kind"><option value="">全部</option><option value="quickapp" {{if eqs .Query.Kind "quickapp"}}selected{{end}}>快应用</option><option value="watchface" {{if eqs .Query.Kind "watchface"}}selected{{end}}>表盘</option></select></label><label><span>目标</span><select name="target"><option value="">全部</option><option value="oronbox" {{if eqs .Query.Target "oronbox"}}selected{{end}}>OronBox</option><option value="bandbbs" {{if eqs .Query.Target "bandbbs"}}selected{{end}}>米坛</option><option value="astrobox" {{if eqs .Query.Target "astrobox"}}selected{{end}}>AstroBox</option></select></label><label><span>创作者</span><input name="owner" value="{{.Query.Owner}}"></label><label><span>开始</span><input type="date" name="from" value="{{.From}}"></label><label><span>结束</span><input type="date" name="to" value="{{.To}}"></label><button class="filled-button">筛选</button></form><form method="post" action="/admin/review/bulk"><input type="hidden" name="return_to" value="{{.ReturnTo}}"><div class="filter-bar embedded"><label><span>分配给</span><select name="reviewer_id"><option value="">取消分配</option>{{range .Reviewers}}<option value="{{.ID}}">{{.Username}}（{{.Role}}）</option>{{end}}</select></label><button class="outlined-button" name="bulk_action" value="assign">分配所选</button><span class="muted">最多 100 项；不会部分成功</span></div><div class="table-wrap"><table><thead><tr><th><span class="sr-only">选择</span></th><th>资源</th><th>创作者</th><th>类型</th><th>目标</th><th>审核员</th><th>清单</th><th>状态</th><th>更新时间</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td>{{if eqs .State "pending"}}<input type="checkbox" name="review_ids" value="{{.ID}}" aria-label="选择 {{.RevisionName}}">{{end}}</td><td><a class="resource-name" href="/admin/review/{{.ID}}">{{.RevisionName}}</a><span class="cell-note">#{{.RevisionNumber}} · <code>{{.ResourceSlug}}</code></span></td><td>{{.Owner}}</td><td>{{kindLabel .ResourceKind}}</td><td>{{join .Targets " · "}}</td><td>{{if .Reviewer}}{{.Reviewer}}{{else}}未分配{{end}}</td><td>{{len .Items}} 项</td><td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span></td><td>{{dateTime .UpdatedAt}}</td><td><a class="row-action" href="/admin/review/{{.ID}}">审核</a></td></tr>{{else}}<tr><td class="table-empty" colspan="10">没有符合筛选条件的审核单</td></tr>{{end}}</tbody></table></div></form>{{template "pagination" .}}</section>{{template "admin_close" .}}{{end}}

{{define "admin_review_detail"}}
{{template "admin_open" .}}{{$review := .Detail.Review}}{{$current := .Detail.Current}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/review">← 审核中心</a><div class="title-line"><h1>{{$current.Name}}</h1><span class="status {{statusClass $review.State}}">{{statusLabel $review.State}}</span></div><p>{{$review.Owner}} · 修订 #{{$current.Number}}{{if $review.Reviewer}} · {{$review.Reviewer}}{{end}}</p></div><div class="header-actions"><a class="outlined-button" href="/admin/resources/{{$review.ResourceID}}/draft?base={{$current.ID}}">边审核边编辑</a></div></header>{{if .Saved}}<div class="notice success toast-notice" data-toast>审核清单已保存，尚未作出决定</div>{{end}}<div class="metrics"><article><span>元数据</span><strong>{{if .Detail.Diff.MetadataChanged}}有变化{{else}}无变化{{end}}</strong><small>{{join .Detail.Diff.MetadataFields " · "}}</small></article><article><span>媒体变化</span><strong>+{{.Detail.Diff.Media.Added}} / -{{.Detail.Diff.Media.Removed}}</strong></article><article><span>文件变化</span><strong>+{{.Detail.Diff.Artifacts.Added}} / -{{.Detail.Diff.Artifacts.Removed}}</strong></article><article><span>设备变化</span><strong>+{{.Detail.Diff.Devices.Added}} / -{{.Detail.Diff.Devices.Removed}}</strong></article></div><div class="content-grid detail-grid"><section class="panel"><div class="section-header"><h2>当前提交</h2></div><dl class="settings"><dt>名称</dt><dd>{{$current.Name}}</dd><dt>简介</dt><dd>{{$current.Summary}}</dd><dt>付费类型</dt><dd>{{paidTypeLabel $current.PaidType}}</dd><dt>发布计划</dt><dd><pre class="diagnostic">{{prettyJSON $current.PublicationPlan}}</pre></dd></dl></section><section class="panel"><div class="section-header"><h2>基础修订</h2></div>{{if .Detail.Diff.HasBase}}<dl class="settings"><dt>名称</dt><dd>{{.Detail.Base.Name}}</dd><dt>简介</dt><dd>{{.Detail.Base.Summary}}</dd><dt>付费类型</dt><dd>{{paidTypeLabel .Detail.Base.PaidType}}</dd></dl>{{else}}<p class="muted">首次提交，没有基础修订</p>{{end}}</section>{{if eqs $review.State "pending"}}<section class="panel span-2"><div class="section-header"><div><h2>审核项清单</h2><p>每行一项，可独立保存，不会批准或拒绝提交。</p></div></div><form method="post" action="/admin/review/{{$review.ID}}/checklist"><label>检查项<textarea name="items" rows="7" placeholder="例如：预览图内容符合规范">{{join $review.Items "\n"}}</textarea></label><div class="actions"><button class="outlined-button">仅保存清单</button></div></form></section><section class="panel span-2"><div class="section-header"><h2>审核决定</h2></div><form method="post" action="/admin/review/{{$current.ID}}" class="decision-form"><input type="hidden" name="items" value="{{join $review.Items "\n"}}"><fieldset><legend>确认标签</legend><div class="choice-grid">{{range .Attributes}}<label class="choice-card"><input type="checkbox" name="attributes" value="{{.ID}}" {{if containsString $current.Attributes .ID}}checked{{end}}><span><strong>{{.NameZH}}</strong></span></label>{{end}}</div></fieldset><label>策展等级<select name="curation_grade"><option value="standard">普通</option><option value="featured">精选</option></select></label><label>审核意见<textarea name="note" rows="5"></textarea></label><div class="actions"><button class="filled-button" name="decision" value="approve">批准</button><button class="outlined-button danger" name="decision" value="reject">退回</button></div></form></section>{{else}}<section class="panel span-2"><h2>审核结果</h2><dl class="settings"><dt>审核员</dt><dd>{{$review.Reviewer}}</dd><dt>意见</dt><dd>{{$review.Note}}</dd><dt>清单</dt><dd>{{join $review.Items "；"}}</dd></dl></section>{{end}}</div>{{template "admin_close" .}}{{end}}

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
{{template "pagination" .}}
</div>
</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_user_workspace"}}
{{template "admin_open" .}}{{$u:=.Detail.User}}
<header class="page-header"><div><a class="back-link" href="/admin/users">← 用户</a><h1>{{$u.Username}}</h1><p>米坛 ID {{$u.BandBBSUserID}} · <code>{{$u.ID}}</code></p></div><a class="outlined-button" href="/admin/coins?user={{$u.ID}}">硬币账户</a></header>
<div class="metrics"><article><span>资源</span><strong>{{.Detail.Resources.Total}}</strong></article><article><span>评论</span><strong>{{.Detail.Comments.Total}}</strong></article><article><span>工单</span><strong>{{.Detail.Tickets.Total}}</strong></article><article><span>余额</span><strong>{{.Detail.Coin.Balance}}</strong></article></div>
<div class="content-grid detail-grid">
<section class="panel"><h2>账号状态</h2><dl class="settings"><dt>角色</dt><dd>{{$u.Role}}</dd><dt>封禁</dt><dd>{{if $u.BannedAt}}{{$u.BanReason}}{{else}}否{{end}}</dd><dt>创作者冻结</dt><dd>{{if $u.CreatorFrozenAt}}是{{else}}否{{end}}</dd><dt>投币冻结</dt><dd>{{if .Detail.Coin.VotingFrozenAt}}{{.Detail.Coin.VotingFrozenReason}}{{else}}否{{end}}</dd></dl></section>
<section class="panel"><div class="section-header"><h2>有效会话</h2><form method="post" action="/admin/users/{{$u.ID}}/sessions"><button class="outlined-button danger" name="action" value="revoke_all" data-confirm="确定撤销全部会话吗？">全部撤销</button></form></div>{{range .Detail.Sessions.Items}}<div class="stack-row"><span>{{.AppID}} · {{.Platform}} · {{dateTime .LastSeenAt}}</span><form method="post" action="/admin/users/{{$u.ID}}/sessions"><input type="hidden" name="session_id" value="{{.ID}}"><button name="action" value="revoke">撤销</button></form></div>{{else}}<p>无有效会话</p>{{end}}{{template "pagination" .SessionsPager}}</section>
<section class="panel span-2"><h2>资源</h2><div class="table-wrap"><table><thead><tr><th>名称</th><th>类型</th><th>状态</th><th>下载</th></tr></thead><tbody>{{range .Detail.Resources.Items}}<tr><td><a href="/admin/resources/{{.ID}}">{{.Name}}</a></td><td>{{kindLabel .Kind}}</td><td>{{statusLabel .ModerationState}}</td><td>{{.DownloadCount}}</td></tr>{{else}}<tr><td colspan="4">暂无</td></tr>{{end}}</tbody></table></div>{{template "pagination" .ResourcesPager}}</section>
<section class="panel span-2"><h2>硬币流水</h2><div class="table-wrap"><table><thead><tr><th>时间</th><th>类型</th><th>变动</th><th>关联</th></tr></thead><tbody>{{range .Detail.Ledger.Items}}<tr><td>{{dateTime .CreatedAt}}</td><td>{{.Kind}}</td><td>{{.DeltaUnits}}</td><td>{{.ReferenceType}} · {{.ReferenceID}}</td></tr>{{else}}<tr><td colspan="4">暂无</td></tr>{{end}}</tbody></table></div>{{template "pagination" .LedgerPager}}</section>
<section class="panel"><h2>评论</h2>{{range .Detail.Comments.Items}}<article class="stack-row"><a href="/admin/resources/{{.ResourceID}}">{{.ResourceName}}</a><p>{{.Body}}</p></article>{{else}}<p>暂无</p>{{end}}{{template "pagination" .CommentsPager}}</section>
<section class="panel"><h2>反馈与举报</h2>{{range .Detail.Tickets.Items}}<a class="stack-row" href="{{if reportKind .Kind}}/admin/reports/{{else}}/admin/feedback/{{end}}{{.ID}}">{{.Subject}} · {{statusLabel .Status}}</a>{{else}}<p>暂无</p>{{end}}{{template "pagination" .TicketsPager}}</section>
<section class="panel span-2"><h2>系统消息</h2><div class="table-wrap"><table><thead><tr><th>时间</th><th>类型</th><th>标题</th><th>内容</th><th>状态</th></tr></thead><tbody>{{range .Detail.Messages.Items}}<tr><td>{{dateTime .CreatedAt}}</td><td>{{.Kind}}</td><td>{{.Title}}</td><td>{{.Body}}</td><td>{{if .ReadAt}}已读{{else}}未读{{end}}</td></tr>{{else}}<tr><td colspan="5">暂无</td></tr>{{end}}</tbody></table></div>{{template "pagination" .MessagesPager}}</section>
<section class="panel span-2"><h2>管理审计</h2>{{range .Detail.Audit.Items}}<div class="stack-row"><code>{{.Action}}</code><span>{{statusLabel .Result}} · {{dateTime .CreatedAt}}</span></div>{{else}}<p>暂无</p>{{end}}{{template "pagination" .AuditPager}}</section>
</div>{{template "admin_close" .}}{{end}}

{{define "admin_revision_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/resources/{{.Detail.Resource.ID}}">← {{.Detail.Resource.Name}}</a><div class="title-line"><h1>{{.Detail.Revision.Name}}</h1><span class="status {{statusClass .Detail.Revision.State}}">{{statusLabel .Detail.Revision.State}}</span></div><p>修订 #{{.Detail.Revision.Number}} · {{if eqs .Detail.Revision.CreatedVia "admin"}}管理员修订{{else}}创作者修订{{end}}</p></div><div class="header-actions"><form method="post" action="/admin/resources/{{.Detail.Resource.ID}}/revisions/{{.Detail.Revision.ID}}/rollback"><button class="filled-button" data-confirm="将完整复制此历史快照为新的管理草稿，并在提交后进入正常审核。确定继续吗？">创建回滚管理修订</button></form></div></header>
<div class="content-grid detail-grid">
  <section class="panel"><div class="section-header"><div><h2>修订信息</h2></div></div><dl class="settings"><dt>修订 ID</dt><dd><code data-copy="{{.Detail.Revision.ID}}">{{.Detail.Revision.ID}}</code></dd><dt>名称</dt><dd>{{.Detail.Revision.Name}}</dd><dt>简介</dt><dd>{{if .Detail.Revision.Summary}}{{.Detail.Revision.Summary}}{{else}}—{{end}}</dd><dt>付费类型</dt><dd>{{paidTypeLabel .Detail.Revision.PaidType}}</dd><dt>状态</dt><dd><span class="status {{statusClass .Detail.Revision.State}}">{{statusLabel .Detail.Revision.State}}</span></dd><dt>来源</dt><dd>{{if eqs .Detail.Revision.CreatedVia "admin"}}管理员{{else}}创作者{{end}}</dd>{{if .Detail.Revision.BaseRevisionID}}<dt>基础修订</dt><dd><a href="/admin/resources/{{.Detail.Resource.ID}}/revisions/{{.Detail.Revision.BaseRevisionID}}"><code>{{.Detail.Revision.BaseRevisionID}}</code></a></dd>{{end}}<dt>创建时间</dt><dd>{{dateTime .Detail.Revision.CreatedAt}}</dd></dl></section>
  <section class="panel"><div class="section-header"><div><h2>审核与发布</h2></div></div><dl class="settings"><dt>审核</dt><dd>{{if .Detail.Revision.ReviewState}}<span class="status {{statusClass .Detail.Revision.ReviewState}}">{{statusLabel .Detail.Revision.ReviewState}}</span>{{else}}—{{end}}</dd><dt>审核员</dt><dd>{{if .Detail.Revision.Reviewer}}{{.Detail.Revision.Reviewer}}{{else}}—{{end}}</dd><dt>内容标签</dt><dd><div class="tag-stack">{{range .Detail.Revision.Attributes}}<span class="status neutral">{{.}}</span>{{else}}—{{end}}</div></dd><dt>发布计划</dt><dd><pre class="diagnostic compact-diagnostic">{{rawJSON .Detail.Revision.PublicationPlan}}</pre></dd></dl></section>
  <section class="panel span-2"><div class="section-header"><div><h2>链接</h2></div></div><div class="stack-list">{{range .Detail.Links}}<a class="stack-row" href="{{.URL}}" target="_blank" rel="noopener noreferrer"><strong>{{.Title}}</strong><span class="cell-note">{{.URL}}</span></a>{{else}}<p class="muted">没有外部链接</p>{{end}}</div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>资源文件</h2></div></div><div class="table-wrap"><table><thead><tr><th>文件</th><th>格式</th><th>Package ID</th><th>版本</th><th>大小</th><th>设备</th><th></th></tr></thead><tbody>{{range .Detail.Artifacts}}<tr><td>{{.OriginalName}}</td><td><code>{{.PackageFormat}}</code></td><td><code>{{.PackageID}}</code></td><td>{{.Version}}</td><td>{{.SizeBytes}} B</td><td>{{join .Devices " · "}}</td><td><a class="row-action" href="/admin/blobs/{{.SHA256}}?download=1&amp;name={{urlquery .OriginalName}}">下载</a></td></tr>{{else}}<tr><td class="table-empty" colspan="7">没有资源文件</td></tr>{{end}}</tbody></table></div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>媒体</h2></div></div>{{if .Detail.Media}}<div class="media-gallery">{{range .Detail.Media}}<a class="media-preview" href="/admin/blobs/{{.SHA256}}" target="_blank" rel="noopener"><img src="/admin/blobs/{{.SHA256}}" alt="{{mediaRoleLabel .Role}} #{{.Position}}"><span>{{mediaRoleLabel .Role}} · {{.Width}} × {{.Height}}</span></a>{{end}}</div>{{else}}<p class="muted">没有媒体文件</p>{{end}}</section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_revision_editor"}}
{{template "admin_open" .}}
{{$revision := .Detail.Revision}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/resources/{{.Detail.Resource.ID}}">← {{.Detail.Resource.Name}}</a><div class="title-line"><h1>编辑资源修订</h1>{{if .IsDraft}}<span class="status warning">管理草稿 #{{$revision.Number}}</span>{{else}}<span class="status info">基于修订 #{{$revision.Number}}</span>{{end}}</div><p>保存会创建或更新管理草稿，不会覆盖历史修订或立即改变线上版本</p></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>管理草稿已保存</div>{{end}}
<form id="revision-metadata-form" class="revision-editor" method="post" action="/admin/resources/{{.Detail.Resource.ID}}/draft">
  <input type="hidden" name="draft_revision_id" value="{{if .IsDraft}}{{$revision.ID}}{{end}}">
  <input type="hidden" name="base_revision_id" value="{{if .IsDraft}}{{$revision.BaseRevisionID}}{{else}}{{$revision.ID}}{{end}}">
  <section class="panel"><div class="section-header"><div><h2>基本信息</h2><p>与创作者提交端使用相同的长度和付费类型约束</p></div></div><div class="editor-field-grid"><label><span>资源名称</span><input name="name" value="{{$revision.Name}}" maxlength="120" required></label><label><span>付费类型</span><select name="paid_type"><option value="free" {{if eqs $revision.PaidType "free"}}selected{{end}}>免费</option><option value="paid" {{if eqs $revision.PaidType "paid"}}selected{{end}}>付费</option><option value="force_paid" {{if eqs $revision.PaidType "force_paid"}}selected{{end}}>强制付费</option></select></label><label class="span-2"><span>简介</span><textarea name="summary" rows="6" maxlength="4000">{{$revision.Summary}}</textarea></label></div></section>
  <section class="panel"><div class="section-header"><div><h2>内容标签</h2></div></div><div class="choice-grid">{{range .Attributes}}<label class="choice-card"><input type="checkbox" name="attributes" value="{{.ID}}" {{if containsString $revision.Attributes .ID}}checked{{end}}><span><strong>{{.NameZH}}</strong>{{if .NameEN}}<small>{{.NameEN}}</small>{{end}}</span></label>{{end}}</div></section>
  <section class="panel"><div class="section-header"><div><h2>外部链接</h2><p>可添加最多 16 个 HTTP 或 HTTPS 链接</p></div><button class="outlined-button" type="button" data-add-link>添加链接</button></div><div class="link-editor" data-link-list>{{range .Detail.Links}}<div class="link-row"><input name="link_title" value="{{.Title}}" placeholder="链接标题"><input name="link_url" value="{{.URL}}" type="url" placeholder="https://"><button class="icon-button danger" type="button" data-remove-link aria-label="删除链接"><span class="material-symbols-outlined">delete</span></button></div>{{end}}<div class="link-row"><input name="link_title" placeholder="链接标题"><input name="link_url" type="url" placeholder="https://"><button class="icon-button danger" type="button" data-remove-link aria-label="删除链接"><span class="material-symbols-outlined">delete</span></button></div></div></section>
  <section class="panel"><div class="section-header"><div><h2>发布计划</h2><p>高级配置，保存草稿不会创建发布任务；AstroBox 免费类型仍按空值发布</p></div></div><label><span>JSON</span><textarea class="json-textarea" name="publication_plan" rows="12" required>{{rawJSON $revision.PublicationPlan}}</textarea></label></section>
</form>
{{if .IsDraft}}
<section class="panel"><div class="section-header"><div><h2>媒体</h2><p>图标和封面会替换同角色文件；序号仅在同角色内调整并始终连续</p></div></div><form class="filter-bar embedded" method="post" enctype="multipart/form-data" action="/admin/resources/{{.Detail.Resource.ID}}/draft/{{$revision.ID}}/media"><label><span>角色</span><select name="role"><option value="preview">预览图</option><option value="icon">图标</option><option value="cover">封面</option></select></label><label class="search-field"><span>图片</span><input type="file" name="file" accept="image/png,image/jpeg,image/webp,image/gif" required></label><button class="filled-button">上传媒体</button></form><div class="media-gallery">{{range .Detail.Media}}<article class="media-preview"><img src="/admin/blobs/{{.SHA256}}" alt="{{mediaRoleLabel .Role}} #{{.Position}}"><span>{{mediaRoleLabel .Role}} #{{.Position}} · {{.Width}} × {{.Height}}</span><form class="filter-bar embedded" method="post" action="/admin/resources/{{$.Detail.Resource.ID}}/draft/{{$revision.ID}}/media/{{.ID}}/move"><label><span>同角色序号</span><input type="number" name="position" min="0" value="{{.Position}}" required></label><button class="text-link">移动</button></form><form method="post" action="/admin/resources/{{$.Detail.Resource.ID}}/draft/{{$revision.ID}}/media/{{.ID}}/delete"><button class="text-link danger" data-confirm="确定删除这个媒体文件吗？">删除</button></form></article>{{else}}<p class="muted">暂无媒体</p>{{end}}</div></section>
<section class="panel"><div class="section-header"><div><h2>安装包与设备</h2><p>上传后由服务端重新分析，至少绑定一个启用设备</p></div></div><form method="post" enctype="multipart/form-data" action="/admin/resources/{{.Detail.Resource.ID}}/draft/{{$revision.ID}}/artifacts"><label><span>安装包</span><input type="file" name="file" required></label><fieldset><legend>适配设备</legend><div class="choice-grid">{{range .Devices}}<label class="choice-card"><input type="checkbox" name="device_ids" value="{{.ID}}"><span><strong>{{.DisplayName}}</strong><small>{{.Codename}}</small></span></label>{{end}}</div></fieldset><div class="actions"><button class="filled-button">上传并分析</button></div></form><div class="table-wrap"><table><thead><tr><th>文件</th><th>分析</th><th>设备绑定</th><th></th></tr></thead><tbody>{{range .Detail.Artifacts}}<tr><td>{{.OriginalName}}<span class="cell-note"><code>{{.PackageID}}</code> · {{.Version}}</span></td><td><code>{{.PackageFormat}}</code></td><td><form method="post" action="/admin/resources/{{$.Detail.Resource.ID}}/draft/{{$revision.ID}}/artifacts/{{.ID}}/devices"><div class="choice-grid compact-choices">{{$artifact := .}}{{range $.Devices}}<label class="choice-card"><input type="checkbox" name="device_ids" value="{{.ID}}" {{if containsDevice $artifact.DeviceBindings .ID}}checked{{end}}><span><strong>{{.DisplayName}}</strong><small>{{.Codename}}</small></span></label>{{end}}</div><button class="outlined-button">保存绑定</button></form></td><td><form method="post" action="/admin/resources/{{$.Detail.Resource.ID}}/draft/{{$revision.ID}}/artifacts/{{.ID}}/delete"><button class="text-link danger" data-confirm="确定删除这个安装包吗？">删除</button></form></td></tr>{{else}}<tr><td colspan="4" class="table-empty">暂无安装包</td></tr>{{end}}</tbody></table></div></section>
<section class="panel"><div class="section-header"><div><h2>来源、合集与协作者</h2><p>审核通过后统一应用，不会立即改变线上关系</p></div></div><form method="post" action="/admin/resources/{{.Detail.Resource.ID}}/draft/{{$revision.ID}}/governance" class="editor-field-grid"><label><span>原作者</span><input name="author_name" value="{{.Governance.AuthorName}}" maxlength="120"></label><label><span>来源 URL</span><input type="url" name="source_url" value="{{.Governance.SourceURL}}"></label><label><span>许可证</span><input name="license_name" value="{{.Governance.LicenseName}}" maxlength="120"></label><label><span>所属合集</span><select name="collection_id"><option value="">不属于合集</option>{{range .Collections}}<option value="{{.ID}}" {{if eqs $.Governance.CollectionID .ID}}selected{{end}}>{{.LatestRevisionName}} · {{.Slug}}</option>{{end}}</select></label><label><span>合集排序</span><input type="number" min="0" name="collection_position" value="{{.Governance.CollectionPosition}}"></label><label class="span-2"><span>授权说明</span><textarea name="authorization_note" rows="4" maxlength="4000">{{.Governance.AuthorizationNote}}</textarea></label><label class="span-2"><span>协作者用户 ID</span><textarea name="collaborator_ids" rows="3" placeholder="UUID，使用逗号或换行分隔">{{join .Governance.CollaboratorIDs ", "}}</textarea></label><div class="span-2 actions"><button class="filled-button">保存关系快照</button></div></form></section>
{{else}}<section class="panel"><p class="muted">先保存管理草稿后即可编辑媒体、安装包和设备绑定。</p></section>{{end}}
<footer class="sticky-actions"><span>历史修订保持不可变</span><div><a class="outlined-button" href="/admin/resources/{{.Detail.Resource.ID}}">取消</a>{{if .IsDraft}}<form method="post" action="/admin/resources/{{.Detail.Resource.ID}}/draft/{{$revision.ID}}/discard"><button class="outlined-button danger" data-confirm="确定丢弃此管理草稿吗？此操作不会影响任何历史或线上修订。">丢弃草稿</button></form><button class="outlined-button" type="submit" form="revision-metadata-form" formaction="/admin/resources/{{.Detail.Resource.ID}}/draft/{{$revision.ID}}/submit" data-confirm="提交后此修订将进入审核队列，并创建发布任务。确定继续吗？">提交审核</button>{{end}}<button class="filled-button" type="submit" form="revision-metadata-form">保存管理草稿</button></div></footer>
<script>(function(){var list=document.querySelector('[data-link-list]');function bind(row){row.querySelector('[data-remove-link]')?.addEventListener('click',function(){if(list.children.length>1)row.remove();else row.querySelectorAll('input').forEach(function(input){input.value='';});});}list.querySelectorAll('.link-row').forEach(bind);document.querySelector('[data-add-link]').addEventListener('click',function(){if(list.children.length>=16)return;var row=document.createElement('div');row.className='link-row';row.innerHTML='<input name="link_title" placeholder="链接标题"><input name="link_url" type="url" placeholder="https://"><button class="icon-button danger" type="button" data-remove-link aria-label="删除链接"><span class="material-symbols-outlined">delete</span></button>';list.appendChild(row);bind(row);});})();</script>
{{template "admin_close" .}}
{{end}}

{{define "admin_resource_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="/admin/resources">← 全部资源</a><div class="title-line"><h1>{{.Item.Name}}</h1><span class="status {{statusClass .Item.ModerationState}}">{{statusLabel .Item.ModerationState}}</span></div><p>{{.Item.Owner}} · <code>{{.Item.Slug}}</code></p></div><div class="header-actions">{{if .Item.LatestRevisionID}}<a class="filled-button" href="/admin/resources/{{.Item.ID}}/draft?base={{.Item.LatestRevisionID}}">编辑资源</a>{{end}}</div></header>
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
  <section class="panel"><div class="section-header"><div><h2>外部绑定</h2><p>后续发布将原地更新对应资源</p></div></div><div class="stack-list">{{range .Detail.Bindings}}<article class="stack-row binding-row"><div><strong>{{targetLabel .Provider}}</strong>{{if .Repository}}<span class="cell-note">{{.Repository}}</span>{{end}}</div><div class="binding-details">{{range .Entries}}<div><span><span class="secondary">{{.Label}}</span><code>{{.Value}}</code></span>{{if .URL}}<a class="outlined-button" href="{{.URL}}" target="_blank" rel="noopener noreferrer">查看资源</a>{{end}}</div>{{end}}</div><div class="binding-actions">{{if .Repository}}<a class="outlined-button" href="https://github.com/{{.Repository}}" target="_blank" rel="noopener noreferrer">资源仓库</a>{{end}}{{if .ExternalURL}}<a class="outlined-button" href="{{.ExternalURL}}" target="_blank" rel="noopener noreferrer">发布页面</a>{{end}}</div></article>{{else}}<p class="muted">没有外部绑定</p>{{end}}</div></section>
  <section class="panel span-2"><div class="section-header"><div><h2>修订历史</h2></div></div><div class="table-wrap"><table><thead><tr><th>修订</th><th>名称</th><th>付费类型</th><th>状态</th><th>审核</th><th>来源</th><th>内容</th><th>时间</th><th></th></tr></thead><tbody>{{range .Detail.Revisions}}<tr><td>#{{.Number}}</td><td><a class="resource-name" href="/admin/resources/{{$.Item.ID}}/revisions/{{.ID}}">{{.Name}}</a></td><td>{{paidTypeLabel .PaidType}}</td><td><span class="status {{statusClass .State}}">{{statusLabel .State}}</span></td><td>{{if .ReviewState}}<span class="status {{statusClass .ReviewState}}">{{statusLabel .ReviewState}}</span>{{else}}—{{end}}{{if .ReviewNote}}<span class="cell-note">{{.ReviewNote}}</span>{{end}}</td><td>{{if eqs .CreatedVia "admin"}}管理员{{else}}创作者{{end}}</td><td>{{.ArtifactCount}} 文件 · {{.MediaCount}} 图片</td><td class="secondary nowrap">{{dateTime .CreatedAt}}</td><td><a class="row-action" href="/admin/resources/{{$.Item.ID}}/revisions/{{.ID}}">查看</a></td></tr>{{else}}<tr><td class="table-empty" colspan="9">尚未提交修订</td></tr>{{end}}</tbody></table></div></section>
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
  <td><a class="resource-name" href="/admin/users/{{.ID}}">{{.Username}}</a><span class="cell-note"><code>{{.ID}}</code></span></td>
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
{{template "pagination" .}}
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
  <header><div><div class="title-line"><span class="status danger">{{kindLabel .Kind}}</span><h2><a class="resource-name" href="/admin/reports/{{.ID}}?return_to={{urlquery $.ReturnTo}}">{{.Subject}}</a></h2></div><p class="ticket-meta">{{.Username}} · {{dateTime .CreatedAt}} · <code>{{.ID}}</code></p></div><span class="status {{statusClass .Status}}">{{statusLabel .Status}}</span></header>
  <p class="ticket-excerpt">{{.Message}}</p>
  <div class="ticket-target">{{if .TargetSource}}{{targetLabel .TargetSource}} · {{end}}{{if .TargetID}}<code>{{.TargetID}}</code>{{else}}未关联资源{{end}}<a class="row-action" href="/admin/reports/{{.ID}}?return_to={{urlquery $.ReturnTo}}">查看并处理</a></div>
</article>
{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>没有举报</h2><p>当前筛选条件下没有举报记录</p></section>{{end}}
</div>
{{template "pagination" .}}
{{template "admin_close" .}}
{{end}}

{{define "admin_report_detail"}}
{{template "admin_open" .}}
<header class="page-header detail-header"><div><a class="back-link" href="{{.BackURL}}">← 返回筛选结果</a><div class="title-line"><span class="status {{if .IsReport}}danger{{else}}info{{end}}">{{kindLabel .Ticket.Kind}}</span><h1>{{.Ticket.Subject}}</h1></div><p>{{.Ticket.Username}} · {{dateTime .Ticket.CreatedAt}} · <code>{{.Ticket.ID}}</code></p></div><span class="status {{statusClass .Ticket.Status}}">{{statusLabel .Ticket.Status}}</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>{{if eqs .Action "internal_note"}}内部备注已保存{{else if eqs .Action "status"}}状态已更新{{else}}用户可见回复已发送{{end}}</div>{{end}}
<div class="content-grid detail-grid">
  <section class="panel span-2"><div class="section-header"><div><h2>用户提交内容</h2></div></div><div class="ticket-message">{{.Ticket.Message}}</div><dl class="target-box">{{if .Ticket.TargetSource}}<div><dt>来源</dt><dd>{{targetLabel .Ticket.TargetSource}}</dd></div>{{end}}{{if .Ticket.TargetID}}<div><dt>目标 ID</dt><dd><code>{{.Ticket.TargetID}}</code></dd></div>{{end}}{{if .Ticket.TargetURL}}<div><dt>目标链接</dt><dd><a href="{{.Ticket.TargetURL}}" target="_blank" rel="noopener noreferrer">{{.Ticket.TargetURL}} ↗</a></dd></div>{{end}}</dl></section>
  <section class="panel"><div class="section-header"><div><h2>目标快照</h2><p>提交时固化，目标后续修改或删除也保留</p></div></div><dl class="settings compact">{{with .Detail.TargetSnapshot}}{{if .Kind}}<dt>类型</dt><dd>{{.Kind}}</dd>{{end}}{{if .Title}}<dt>标题</dt><dd>{{.Title}}</dd>{{end}}{{if .Body}}<dt>内容</dt><dd>{{.Body}}</dd>{{end}}{{if .Owner}}<dt>作者</dt><dd>{{.Owner}}</dd>{{end}}{{if .State}}<dt>当时状态</dt><dd>{{statusLabel .State}}</dd>{{end}}{{if .ID}}<dt>目标</dt><dd><code>{{.ID}}</code></dd>{{end}}{{if .URL}}<dt>链接</dt><dd><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">打开 ↗</a></dd>{{end}}{{else}}<dd>无目标快照</dd>{{end}}</dl></section>
  <section class="panel"><div class="section-header"><div><h2>状态时间线</h2></div></div><div class="reply-thread">{{range .Detail.StatusHistory}}<article><div><strong>{{if .FromStatus}}{{statusLabel .FromStatus}} → {{end}}{{statusLabel .ToStatus}}</strong><time>{{dateTime .CreatedAt}}</time></div><p>{{.Actor}}</p></article>{{else}}<p class="muted">暂无状态记录</p>{{end}}</div></section>
  <section class="panel"><div class="section-header"><div><h2>公开回复</h2><p>以下内容全部对提交用户可见</p></div></div><div class="reply-thread">{{range .Ticket.Replies}}<article><div><strong>{{.Author}}</strong><time>{{dateTime .CreatedAt}}</time></div><p>{{.Message}}</p></article>{{else}}<p class="muted">尚未公开回复</p>{{end}}</div><form method="post" action="{{if .IsReport}}/admin/reports/{{else}}/admin/feedback/{{end}}{{.Ticket.ID}}" class="reply-form"><input type="hidden" name="action" value="public_reply"><input type="hidden" name="return_to" value="{{.ReturnTo}}"><label>发送用户可见回复<textarea name="message" rows="4" required maxlength="10000"></textarea></label><button class="filled-button" type="submit">发送回复</button></form></section>
  <section class="panel"><div class="section-header"><div><h2>内部备注</h2><p>仅后台人员可见，不发送给用户</p></div></div><div class="reply-thread">{{range .Detail.InternalNotes}}<article><div><strong>{{.Author}}</strong><time>{{dateTime .CreatedAt}}</time></div><p>{{.Message}}</p></article>{{else}}<p class="muted">暂无内部备注</p>{{end}}</div><form method="post" action="{{if .IsReport}}/admin/reports/{{else}}/admin/feedback/{{end}}{{.Ticket.ID}}" class="reply-form"><input type="hidden" name="action" value="internal_note"><input type="hidden" name="return_to" value="{{.ReturnTo}}"><label>新增内部备注<textarea name="internal_note" rows="4" required maxlength="10000"></textarea></label><button class="outlined-button" type="submit">保存内部备注</button></form></section>
  <section class="panel span-2"><div class="section-header"><div><h2>变更处理状态</h2><p>状态变化会进入永久时间线；举报状态变化仍按现有规则通知用户</p></div></div><form method="post" action="{{if .IsReport}}/admin/reports/{{else}}/admin/feedback/{{end}}{{.Ticket.ID}}" class="report-form"><input type="hidden" name="action" value="status"><input type="hidden" name="return_to" value="{{.ReturnTo}}"><label><span>处理状态</span><select name="status"><option value="open" {{if eqs .Ticket.Status "open"}}selected{{end}}>待处理</option><option value="investigating" {{if eqs .Ticket.Status "investigating"}}selected{{end}}>处理中</option><option value="replied" {{if eqs .Ticket.Status "replied"}}selected{{end}}>已回复</option><option value="resolved" {{if eqs .Ticket.Status "resolved"}}selected{{end}}>已解决</option><option value="dismissed" {{if eqs .Ticket.Status "dismissed"}}selected{{end}}>已驳回</option><option value="closed" {{if eqs .Ticket.Status "closed"}}selected{{end}}>已关闭</option></select></label><div class="actions"><button class="filled-button" type="submit">更新状态</button></div></form></section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_feedback"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>举报与反馈</h1><p>查看全部资源举报、评论举报和用户意见</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
<nav class="subtabs"><a href="/admin/reports">举报</a><a class="active" href="/admin/feedback">全部反馈</a></nav>
{{if .Replied}}<div class="notice success toast-notice" data-toast>答复已发送</div>{{end}}
<form class="filter-bar" method="get" action="/admin/feedback"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="主题、用户名、内容或目标"></label><label><span>类型</span><select name="kind"><option value="">全部</option><option value="reports" {{if eqs .Query.Kind "reports"}}selected{{end}}>全部举报</option><option value="resource_report" {{if eqs .Query.Kind "resource_report"}}selected{{end}}>资源举报</option><option value="comment_report" {{if eqs .Query.Kind "comment_report"}}selected{{end}}>评论举报</option><option value="feedback" {{if eqs .Query.Kind "feedback"}}selected{{end}}>意见反馈</option></select></label><label><span>来源</span><input name="source" value="{{.Query.TargetSource}}" placeholder="oronbox"></label><label><span>状态</span><select name="status"><option value="">全部</option><option value="open" {{if eqs .Query.Status "open"}}selected{{end}}>待处理</option><option value="investigating" {{if eqs .Query.Status "investigating"}}selected{{end}}>处理中</option><option value="replied" {{if eqs .Query.Status "replied"}}selected{{end}}>已回复</option><option value="resolved" {{if eqs .Query.Status "resolved"}}selected{{end}}>已解决</option><option value="dismissed" {{if eqs .Query.Status "dismissed"}}selected{{end}}>已驳回</option><option value="closed" {{if eqs .Query.Status "closed"}}selected{{end}}>已关闭</option></select></label><button class="filled-button" type="submit">筛选</button><a class="text-link filter-reset" href="/admin/feedback">清除</a></form>
<div class="ticket-list">{{range .Items}}<article class="ticket-card"><header><div><div class="title-line"><span class="status {{if reportKind .Kind}}danger{{else}}info{{end}}">{{kindLabel .Kind}}</span><h2><a class="resource-name" href="{{if reportKind .Kind}}/admin/reports/{{else}}/admin/feedback/{{end}}{{.ID}}?return_to={{urlquery $.ReturnTo}}">{{.Subject}}</a></h2></div><p class="ticket-meta">{{.Username}} · {{dateTime .CreatedAt}} · <code>{{.ID}}</code></p></div><span class="status {{statusClass .Status}}">{{statusLabel .Status}}</span></header><div class="ticket-message">{{.Message}}</div><div class="ticket-target">{{if .TargetSource}}{{targetLabel .TargetSource}} · {{end}}{{if .TargetID}}<code>{{.TargetID}}</code>{{end}}<a class="row-action" href="{{if reportKind .Kind}}/admin/reports/{{else}}/admin/feedback/{{end}}{{.ID}}?return_to={{urlquery $.ReturnTo}}">查看完整处理记录</a></div></article>{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>没有反馈</h2><p>当前筛选条件下没有记录</p></section>{{end}}</div>
{{template "pagination" .}}
{{template "admin_close" .}}
{{end}}

{{define "admin_comments"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>评论管理</h1><p>检索全部评论、处理 AI 待复审记录并配置审核策略</p></div><span class="count-badge">{{.Total}} 项</span></header>
<form class="filter-bar" method="get" action="/admin/comments"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="正文、用户或 ID"></label><label><span>状态</span><select name="state"><option value="">全部</option><option value="review" {{if eqs .Query.State "review"}}selected{{end}}>待人工复审</option><option value="visible" {{if eqs .Query.State "visible"}}selected{{end}}>可见</option><option value="hidden" {{if eqs .Query.State "hidden"}}selected{{end}}>隐藏</option><option value="deleted" {{if eqs .Query.State "deleted"}}selected{{end}}>已删除</option></select></label><label><span>资源 ID</span><input name="resource" value="{{.Query.Resource}}"></label><label><span>用户</span><input name="user" value="{{.Query.User}}" placeholder="用户名或 UUID"></label><label><span>排序</span><select name="sort"><option value="newest">最新</option><option value="oldest" {{if eqs .Query.Sort "oldest"}}selected{{end}}>最早</option><option value="username" {{if eqs .Query.Sort "username"}}selected{{end}}>用户</option><option value="state" {{if eqs .Query.Sort "state"}}selected{{end}}>状态</option></select></label><button class="filled-button">筛选</button><a class="text-link filter-reset" href="/admin/comments">清除</a></form>
<div class="content-grid">
  <section class="panel span-2"><div class="section-header"><div><h2>评论记录</h2></div></div>
    <div class="ticket-list">{{range .Items}}<article class="ticket-card"><header><div><strong><a href="/admin/users/{{.UserID}}">{{.Username}}</a></strong><p class="ticket-meta">米坛 ID {{.BandBBSUserID}} · {{dateTime .CreatedAt}} · <a href="/admin/resources/{{.ResourceID}}">资源</a></p></div>{{if .Deleted}}<span class="status neutral">已删除</span>{{else}}<span class="status {{statusClass .ModerationState}}">{{statusLabel .ModerationState}}</span>{{end}}</header><div class="ticket-message">{{.Body}}</div>{{if .ModerationAction}}<p class="muted">AI：{{.ModerationAction}} · {{.ModerationModel}} · {{.ModerationReason}}{{if .HumanReviewed}} · 已人工处理{{end}}</p>{{end}}{{if and (eqs .ModerationAction "review") (not .HumanReviewed)}}<form method="post" action="/admin/comments/{{.ID}}" class="actions"><button class="filled-button" name="action" value="approve">通过</button><button class="outlined-button danger" name="action" value="hide">隐藏</button></form>{{end}}</article>{{else}}<section class="empty-state"><div class="empty-mark">✓</div><h2>没有符合条件的评论</h2></section>{{end}}</div>
    {{template "pagination" .}}
  </section>
  <section class="panel"><div class="section-header"><div><h2>审核提示词</h2></div></div><form method="post" action="/admin/comments/prompt"><label><span>提示词</span><textarea name="prompt" rows="12" required>{{.Prompt}}</textarea></label><div class="actions"><button class="filled-button" type="submit">保存</button></div></form></section>
  <section class="panel"><div class="section-header"><div><h2>测试台</h2><p>用当前提示词测试任意文本</p></div></div><form method="post" action="/admin/comments/test"><label><span>测试内容</span><textarea name="text" rows="8" required>{{.TestText}}</textarea></label><div class="actions"><button class="filled-button" type="submit">运行审核</button></div></form>{{if .TestResult}}<h3>原始 JSON</h3><pre class="diagnostic">{{.TestResult}}</pre>{{end}}</section>
</div>
{{template "admin_close" .}}
{{end}}

{{define "admin_collections"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>合集管理</h1><p>查看全部合集、历史修订、审核状态和成员资源</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>合集操作已完成</div>{{end}}
<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/collections"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="名称、简介、Slug 或 ID"></label><label><span>创作者</span><input name="owner" value="{{.Query.Owner}}"></label><label><span>类型</span><select name="kind"><option value="">全部</option><option value="quickapp" {{if eqs .Query.Kind "quickapp"}}selected{{end}}>快应用</option><option value="watchface" {{if eqs .Query.Kind "watchface"}}selected{{end}}>表盘</option></select></label><label><span>状态</span><select name="state"><option value="">全部</option><option value="pending" {{if eqs .Query.State "pending"}}selected{{end}}>待审核</option><option value="approved" {{if eqs .Query.State "approved"}}selected{{end}}>已通过</option><option value="rejected" {{if eqs .Query.State "rejected"}}selected{{end}}>已拒绝</option></select></label><div class="filter-actions"><button class="filled-button">应用筛选</button><a class="text-link" href="/admin/collections">清除</a></div></form><div class="table-wrap"><table><thead><tr><th>合集</th><th>创作者</th><th>类型</th><th>最新修订</th><th>成员</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/collections/{{.ID}}">{{if .LatestRevisionName}}{{.LatestRevisionName}}{{else}}{{.Slug}}{{end}}</a><span class="cell-note"><code>{{.Slug}}</code></span></td><td>{{.Owner}}</td><td>{{kindLabel .Kind}}</td><td>#{{.LatestRevisionNumber}} · <span class="status {{statusClass .LatestRevisionState}}">{{statusLabel .LatestRevisionState}}</span></td><td>{{.MemberCount}}</td><td><a class="row-action" href="/admin/collections/{{.ID}}">详情</a></td></tr>{{else}}<tr><td class="table-empty" colspan="6">没有符合筛选条件的合集</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_collection_detail"}}
{{template "admin_open" .}}
{{$collection := .Detail.Collection}}<header class="page-header detail-header"><div><a class="back-link" href="/admin/collections">← 合集管理</a><div class="title-line"><h1>{{$collection.LatestRevisionName}}</h1><span class="status {{if $collection.Enabled}}success{{else}}muted{{end}}">{{if $collection.Enabled}}已启用{{else}}已停用{{end}}</span><span class="status {{statusClass $collection.LatestRevisionState}}">{{statusLabel $collection.LatestRevisionState}}</span></div><p>{{$collection.Owner}} · <code>{{$collection.Slug}}</code></p></div></header>{{if .Action}}<div class="notice success toast-notice" data-toast>新的合集管理修订已创建，审核通过前线上内容不变</div>{{end}}
<div class="content-grid detail-grid"><section class="panel"><div class="section-header"><div><h2>创建完整管理修订</h2><p>名称、简介、启停、成员顺序与代表资源形成不可变快照</p></div></div><form method="post" action="/admin/collections/{{$collection.ID}}/draft" class="editor-field-grid"><label class="span-2"><span>名称</span><input name="name" value="{{$collection.CurrentRevisionName}}" maxlength="120" required></label><label class="span-2"><span>简介</span><textarea name="summary" rows="5" maxlength="4000">{{range .Detail.Revisions}}{{if eqs .ID $collection.CurrentRevisionID}}{{.Summary}}{{end}}{{end}}</textarea></label><label><span>生命周期</span><span class="checkbox-row"><input type="checkbox" name="enabled" {{if $collection.Enabled}}checked{{end}}> 审核后启用合集</span></label><label><span>代表/封面资源</span><select name="representative_resource_id"><option value="">自动使用首个成员</option>{{range .Detail.Members}}<option value="{{.ID}}" {{if eqs .ID $collection.RepresentativeResourceID}}selected{{end}}>{{.CurrentRevisionName}} · {{.Slug}}</option>{{end}}</select></label><label class="span-2"><span>有序成员资源 ID</span><textarea name="resource_ids" rows="9" placeholder="每行一个资源 UUID；行顺序即展示顺序">{{range .Detail.Members}}{{.ID}}
{{end}}</textarea><small>只能加入同一创作者、同一资源类型且当前未属于其他合集的资源；代表资源必须在此列表中。</small></label><div class="span-2 actions"><button class="filled-button">创建待审核管理修订</button></div></form></section><section class="panel"><div class="section-header"><div><h2>线上合集状态</h2></div></div><dl class="settings"><dt>合集 ID</dt><dd><code data-copy="{{$collection.ID}}">{{$collection.ID}}</code></dd><dt>当前修订</dt><dd>#{{$collection.CurrentRevisionNumber}}</dd><dt>生命周期</dt><dd>{{if $collection.Enabled}}已启用{{else}}已停用{{end}}</dd><dt>成员</dt><dd>{{$collection.MemberCount}}</dd><dt>代表资源</dt><dd>{{if $collection.RepresentativeResourceID}}<code>{{$collection.RepresentativeResourceID}}</code>{{else}}—{{end}}</dd></dl></section><section class="panel span-2"><div class="section-header"><div><h2>当前生效成员</h2></div></div><div class="table-wrap"><table><thead><tr><th>顺序</th><th>资源</th><th>创作者</th><th>状态</th><th></th></tr></thead><tbody>{{range .Detail.Members}}<tr><td>{{.Position}}</td><td>{{.CurrentRevisionName}}<span class="cell-note"><code>{{.Slug}}</code></span></td><td>{{.Owner}}</td><td>{{statusLabel .ModerationState}}</td><td><a class="row-action" href="/admin/resources/{{.ID}}">查看</a></td></tr>{{else}}<tr><td class="table-empty" colspan="5">暂无成员</td></tr>{{end}}</tbody></table></div></section><section class="panel span-2"><div class="section-header"><div><h2>不可变修订历史</h2></div></div>{{range .Detail.Revisions}}<details class="history-item" {{if eqs .State "pending"}}open{{end}}><summary><strong>#{{.Number}} · {{.Name}}</strong> <span class="status {{statusClass .State}}">{{statusLabel .State}}</span> · {{if .Enabled}}启用{{else}}停用{{end}} · {{len .Members}} 个成员</summary><dl class="settings"><dt>简介</dt><dd>{{if .Summary}}{{.Summary}}{{else}}—{{end}}</dd><dt>来源</dt><dd>{{.CreatedVia}}</dd><dt>基础修订</dt><dd>{{if .BaseRevisionID}}<code>{{.BaseRevisionID}}</code>{{else}}—{{end}}</dd><dt>代表资源</dt><dd>{{if .RepresentativeResourceID}}<code>{{.RepresentativeResourceID}}</code>{{else}}—{{end}}</dd><dt>审核员</dt><dd>{{if .Reviewer}}{{.Reviewer}}{{else}}—{{end}}</dd><dt>意见</dt><dd>{{if .ReviewNote}}{{.ReviewNote}}{{else}}—{{end}}</dd></dl><ol>{{range .Members}}<li><a href="/admin/resources/{{.ID}}">{{.CurrentRevisionName}}</a> <code>{{.ID}}</code></li>{{else}}<li>无成员</li>{{end}}</ol>{{if eqs .State "pending"}}<form method="post" action="/admin/collections/{{.ID}}" class="inline-form"><textarea name="note" placeholder="审核意见"></textarea><button name="decision" value="approve" class="filled-button">审核通过并应用</button><button name="decision" value="reject" class="outlined-button">拒绝</button></form>{{end}}</details>{{end}}</section></div>
{{template "admin_close" .}}
{{end}}

{{define "admin_plugins"}}{{template "admin_open" .}}<header class="page-header"><div><h1>插件管理</h1><p>查看全部插件和不可变版本历史</p></div><span class="count-badge">{{.Page.Total}} 项</span></header>{{if .Action}}<div class="notice success toast-notice" data-toast>插件操作已完成</div>{{end}}<section class="panel list-panel"><form class="filter-bar embedded" method="get" action="/admin/plugins"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}"></label><label><span>状态</span><select name="state"><option value="">全部</option><option value="pending">待审核</option><option value="listed">已上架</option><option value="rejected">已拒绝</option><option value="delisted">已下架</option></select></label><label><span>上传者</span><input name="uploader" value="{{.Query.Uploader}}"></label><label><span>运行时</span><select name="runtime"><option value="">全部</option><option value="js">JS</option><option value="wasm">Wasm</option><option value="hybrid">Hybrid</option></select></label><button class="filled-button">筛选</button></form><div class="table-wrap"><table><thead><tr><th>插件</th><th>上传者</th><th>版本</th><th>运行时</th><th>状态</th><th>大小</th><th></th></tr></thead><tbody>{{range .Items}}<tr><td><a class="resource-name" href="/admin/plugins/{{.ID}}">{{.Name}}</a><span class="cell-note"><code>{{.ID}}</code></span></td><td>{{.UploaderName}}</td><td>{{.Version}}</td><td>{{.Runtime}}</td><td>{{if .PendingVersionID}}<span class="status warning">有待审版本</span>{{else}}<span class="status {{statusClass .State}}">{{statusLabel .State}}</span>{{end}}</td><td>{{.PackageSize}} B</td><td><a class="row-action" href="/admin/plugins/{{.ID}}">详情</a></td></tr>{{else}}<tr><td colspan="7" class="table-empty">没有符合筛选条件的插件</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>{{template "admin_close" .}}{{end}}

{{define "admin_plugin_workspace"}}
{{template "admin_open" .}}{{$p:=.Detail.Plugin}}
<header class="page-header"><div><a class="back-link" href="/admin/plugins">← 插件</a><h1>{{$p.Name}}</h1><p><code>{{$p.ID}}</code> · {{$p.UploaderName}}</p></div><span class="status {{statusClass $p.State}}">{{statusLabel $p.State}}</span></header>
{{if .Action}}<div class="notice success" data-toast>插件管理修订已创建</div>{{end}}
<div class="content-grid detail-grid">
<section class="panel"><h2>元数据管理修订</h2><form method="post" action="/admin/plugins/{{$p.ID}}/metadata"><label>名称<input name="name" value="{{$p.Name}}" required></label><label>作者<input name="author" value="{{$p.Author}}"></label><label>描述<textarea name="description" rows="5">{{$p.Description}}</textarea></label><button class="filled-button">创建待审版本</button></form></section>
<section class="panel"><h2>替换插件包</h2><p>服务端读取包内 manifest，校验插件 ID、运行时、权限和入口；审核通过前继续提供当前公开版本。</p><form method="post" action="/admin/plugins/{{$p.ID}}/package" enctype="multipart/form-data"><label>.obp 安装包<input type="file" name="package" accept=".obp,application/zip" required></label><button class="filled-button">上传并创建待审版本</button></form></section>
<section class="panel"><h2>当前公开版本</h2><dl class="settings"><dt>版本</dt><dd>{{$p.Version}}</dd><dt>运行时</dt><dd>{{$p.Runtime}}</dd><dt>权限</dt><dd>{{join $p.Permissions " · "}}</dd><dt>包</dt><dd><a href="/admin/blobs/{{$p.PackageSHA256}}?download=1"><code>{{$p.PackageSHA256}}</code></a></dd></dl></section>
<section class="panel">{{if $p.PendingVersionID}}<h2>待审版本</h2><form method="post" action="/admin/plugins/{{$p.ID}}/review"><label>审核意见<textarea name="note"></textarea></label><button class="filled-button" name="decision" value="approve">通过</button><button class="outlined-button danger" name="decision" value="reject">拒绝</button></form>{{else}}<h2>审核状态</h2><p>当前没有待审版本。</p>{{end}}</section>
<section class="panel span-2"><h2>不可变版本历史</h2><div class="table-wrap"><table><thead><tr><th>修订</th><th>版本</th><th>名称</th><th>来源</th><th>状态</th><th>时间</th></tr></thead><tbody>{{range .Detail.Versions}}<tr><td>#{{.Number}}</td><td>{{.Version}}</td><td>{{.Name}}</td><td>{{.CreatedVia}}</td><td>{{statusLabel .State}}</td><td>{{dateTime .CreatedAt}}</td></tr>{{end}}</tbody></table></div></section>
</div>{{template "admin_close" .}}{{end}}

{{define "admin_coins"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>硬币管理</h1><p>查看发行与流转、冻结异常账号、调账并作废异常投币</p></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>硬币操作已完成</div>{{end}}
<div class="metric-grid"><article><span>已发行</span><strong>{{.Stats.IssuedUnits}}</strong><small>0.1 枚 / 单位</small></article><article><span>资源投币</span><strong>{{.Stats.SpentUnits}}</strong><small>支出单位</small></article><article><span>创作者奖励</span><strong>{{.Stats.RewardedUnits}}</strong><small>奖励单位</small></article><article><span>冻结账号</span><strong>{{.Stats.FrozenVoters}}</strong><small>活跃投币用户 {{.Stats.ActiveVoters}}</small></article></div>
<div class="content-grid"><section class="panel"><div class="section-header"><div><h2>账号操作</h2><p>余额使用 0.1 枚为一个单位；选择用户时可预览当前余额和冻结状态</p></div></div><form method="post" action="/admin/coins/users" class="reply-form"><label><span>用户</span><select name="user_id" required><option value="">选择用户</option>{{range .Users}}<option value="{{.ID}}">{{.Username}} · {{.BalanceUnits}} 单位{{if .Frozen}} · 已冻结{{end}}</option>{{end}}</select></label><label><span>动作</span><select name="action"><option value="adjust">调账</option><option value="freeze">冻结投币</option><option value="unfreeze">解除冻结</option></select></label><label><span>余额变动单位</span><input name="delta_units" type="number" value="0"></label><label><span>原因</span><textarea name="reason" required rows="3"></textarea></label><div class="actions"><button class="filled-button">执行</button></div></form></section><section class="panel"><div class="section-header"><div><h2>作废投币</h2><p>退回投币并回滚可追回的创作者奖励</p></div></div><form method="post" action="/admin/coins/invalidate" class="reply-form"><label><span>资源 UUID</span><input name="resource_id" required></label><label><span>投币用户</span><select name="user_id" required><option value="">选择用户</option>{{range .Users}}<option value="{{.ID}}">{{.Username}}</option>{{end}}</select></label><label><span>原因</span><textarea name="reason" required rows="3"></textarea></label><div class="actions"><button class="outlined-button danger">作废投币</button></div></form></section></div>
<section class="panel"><div class="section-header"><div><h2>不可变账本</h2><p>全部流水可检索和分页，原始记录不可删除</p></div><span class="count-badge">{{.Page.Total}} 条</span></div><form class="filter-bar embedded" method="get" action="/admin/coins"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="用户、备注、流水或关联 ID"></label><label><span>用户</span><input name="user" value="{{.Query.User}}" placeholder="用户名或 UUID"></label><label><span>类型</span><input name="kind" value="{{.Query.Kind}}" placeholder="admin_adjustment"></label><label><span>关联类型</span><input name="reference_type" value="{{.Query.ReferenceType}}" placeholder="resource"></label><label><span>从</span><input type="date" name="from"></label><label><span>至</span><input type="date" name="to"></label><label><span>排序</span><select name="sort"><option value="newest">最新</option><option value="oldest" {{if eqs .Query.Sort "oldest"}}selected{{end}}>最早</option><option value="delta_desc" {{if eqs .Query.Sort "delta_desc"}}selected{{end}}>变动降序</option><option value="delta_asc" {{if eqs .Query.Sort "delta_asc"}}selected{{end}}>变动升序</option></select></label><button class="filled-button">筛选</button><a class="text-link" href="/admin/coins">清除</a></form><div class="table-wrap"><table><thead><tr><th>时间</th><th>用户</th><th>类型</th><th>变动</th><th>关联</th><th>原因</th></tr></thead><tbody>{{range .Ledger}}<tr><td class="secondary nowrap">{{.CreatedAt}}</td><td><a href="/admin/users/{{.UserID}}">{{.Username}}</a><span class="cell-note"><code>{{.UserID}}</code></span></td><td><code>{{.Kind}}</code></td><td>{{.DeltaUnits}}</td><td>{{.ReferenceType}} <code>{{.ReferenceID}}</code></td><td>{{.Note}}</td></tr>{{else}}<tr><td class="table-empty" colspan="6">暂无硬币流水</td></tr>{{end}}</tbody></table></div>{{template "pagination" .}}</section>
{{template "admin_close" .}}
{{end}}

{{define "admin_home"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>首页编排</h1><p>按客户端首页的展示顺序管理内容，保存后立即生效</p></div><div class="header-actions"><a class="outlined-button" href="/admin/blog">管理文章</a><a class="filled-button" href="/api/home" target="_blank" rel="noopener">查看数据</a></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>首页配置已更新</div>{{end}}
<section class="panel selector-toolbar"><div class="section-header"><div><h2>内容选择器</h2><p>下面所有资源下拉框使用当前搜索页；可搜索并分页切换，不再截断为最近 100 条</p></div></div><form class="filter-bar embedded" method="get" action="/admin/home"><label class="search-field"><span>搜索资源</span><input name="selector_q" value="{{.SelectorQ}}" placeholder="名称、Slug、创作者或 ID"></label><button class="filled-button">更新选择器</button><a class="text-link" href="/admin/home">清除</a></form>{{template "pagination" .SelectorPager}}</section>
<div class="home-composer">
<section class="panel composer-section"><div class="section-header"><div><h2>Banner</h2><p>客户端首页顶部轮播</p></div><span class="count-badge">{{len .Banners}} 项</span></div>
<div class="composer-list">{{range .Banners}}<article class="composer-item"><div class="composer-cover">{{if .CoverSHA256}}<img src="/api/blobs/{{.CoverSHA256}}" alt="">{{else}}<span class="material-symbols-outlined">panorama</span>{{end}}</div><div class="composer-copy"><div class="title-line"><h3>{{.Title}}</h3>{{if .Enabled}}<span class="status success">展示中</span>{{else}}<span class="status neutral">已停用</span>{{end}}</div><p>{{.Subtitle}}</p><span class="cell-note">{{if eqs .Type "resource"}}资源 · {{.ResourceID}}{{else if eqs .Type "blog"}}文章 · {{.BlogSlug}}{{else}}外部链接 · {{.LinkURL}}{{end}}</span></div><div class="composer-actions"><form method="post" action="/admin/home/banners/{{.ID}}/move"><button class="icon-button" name="delta" value="-1" title="上移"><span class="material-symbols-outlined">arrow_upward</span></button><button class="icon-button" name="delta" value="1" title="下移"><span class="material-symbols-outlined">arrow_downward</span></button></form><details class="composer-editor"><summary class="outlined-button">编辑</summary><form method="post" action="/admin/home/banners/{{.ID}}/save" class="composer-form" data-target-form><label>标题<input name="title" value="{{.Title}}" required></label><label>副标题<input name="subtitle" value="{{.Subtitle}}"></label><label>点击后打开<select name="type" data-target-type><option value="resource" {{if eqs .Type "resource"}}selected{{end}}>OronBox 资源</option><option value="blog" {{if eqs .Type "blog"}}selected{{end}}>Blog 文章</option><option value="link" {{if eqs .Type "link"}}selected{{end}}>外部链接</option></select></label><label data-target="resource">目标资源<select name="resource_id"><option value="{{.ResourceID}}">{{if .ResourceID}}{{.ResourceID}}{{else}}选择资源{{end}}</option>{{range $.Resources}}<option value="{{.ID}}">{{.Name}} · {{.Slug}}</option>{{end}}</select></label><label data-target="blog">目标文章<select name="blog_slug"><option value="{{.BlogSlug}}">{{if .BlogSlug}}{{.BlogSlug}}{{else}}选择文章{{end}}</option>{{range $.Posts}}<option value="{{.Slug}}">{{.Title}}</option>{{end}}</select></label><label data-target="link">外部链接<input name="link_url" value="{{.LinkURL}}" type="url"></label><label>封面<input name="cover_sha256" value="{{.CoverSHA256}}" placeholder="上传图片后自动填写" data-cover-field></label><label class="toggle-label"><input type="checkbox" name="enabled" {{if .Enabled}}checked{{end}}>在首页展示</label><div class="actions"><button class="outlined-button" type="button" data-upload-cover>上传封面</button><button class="filled-button">保存</button></div></form></details><form method="post" action="/admin/home/banners/{{.ID}}/delete"><button class="icon-button danger" data-confirm="确定删除这个 Banner 吗"><span class="material-symbols-outlined">delete</span></button></form></div></article>{{else}}<div class="composer-empty">还没有 Banner</div>{{end}}</div>
<details class="composer-add"><summary class="outlined-button">+ 添加 Banner</summary><form method="post" action="/admin/home/banners" class="composer-form" data-target-form><label>标题<input name="title" required></label><label>副标题<input name="subtitle"></label><label>点击后打开<select name="type" data-target-type><option value="resource">OronBox 资源</option><option value="blog">Blog 文章</option><option value="link">外部链接</option></select></label><label data-target="resource">目标资源<select name="resource_id"><option value="">选择资源</option>{{range .Resources}}<option value="{{.ID}}">{{.Name}} · {{.Slug}}</option>{{end}}</select></label><label data-target="blog">目标文章<select name="blog_slug"><option value="">选择文章</option>{{range .Posts}}<option value="{{.Slug}}">{{.Title}}</option>{{end}}</select></label><label data-target="link">外部链接<input name="link_url" type="url"></label><label>封面<input name="cover_sha256" placeholder="上传图片后自动填写" data-cover-field></label><label class="toggle-label"><input type="checkbox" name="enabled" checked>在首页展示</label><div class="actions"><button class="outlined-button" type="button" data-upload-cover>上传封面</button><button class="filled-button">创建 Banner</button></div></form></details></section>
<section class="panel composer-section builtin-section"><div><span class="section-kicker">内置分区</span><h2>最新动态</h2><p>自动展示所有已发布文章，不需要重复加入首页分区</p></div><a class="outlined-button" href="/admin/blog">管理文章</a></section>
{{range .Sections}}<section class="panel composer-section"><div class="section-header"><div><span class="section-kicker">自定义分区</span><h2>{{.Name}}</h2><p>{{.Description}}</p></div>{{if .Enabled}}<span class="status success">展示中</span>{{else}}<span class="status neutral">已停用</span>{{end}}</div><div class="composer-list compact">{{range index $.Cards .ID}}<article class="composer-item"><span class="material-symbols-outlined composer-kind">{{if eqs .Type "resource"}}deployed_code{{else}}article{{end}}</span><div class="composer-copy"><strong>{{if eqs .Type "resource"}}资源{{else}}文章{{end}}</strong><span class="cell-note">{{if eqs .Type "resource"}}{{.ResourceID}}{{else}}{{.BlogSlug}}{{end}}</span></div><div class="composer-actions"><form method="post" action="/admin/home/cards/{{.ID}}/move"><input type="hidden" name="section_id" value="{{.SectionID}}"><button class="icon-button" name="delta" value="-1"><span class="material-symbols-outlined">arrow_upward</span></button><button class="icon-button" name="delta" value="1"><span class="material-symbols-outlined">arrow_downward</span></button></form><form method="post" action="/admin/home/cards/{{.ID}}/delete"><button class="icon-button danger"><span class="material-symbols-outlined">close</span></button></form></div></article>{{else}}<div class="composer-empty">这个分区还没有内容</div>{{end}}</div><div class="composer-footer"><details class="composer-add"><summary class="outlined-button">+ 添加内容</summary><form method="post" action="/admin/home/cards" class="composer-form" data-target-form><input type="hidden" name="section_id" value="{{.ID}}"><label>内容类型<select name="type" data-target-type><option value="resource">资源</option><option value="blog">文章</option></select></label><label data-target="resource">选择资源<select name="resource_id"><option value="">选择资源</option>{{range $.Resources}}<option value="{{.ID}}">{{.Name}} · {{.Slug}}</option>{{end}}</select></label><label data-target="blog">选择文章<select name="blog_slug"><option value="">选择文章</option>{{range $.Posts}}<option value="{{.Slug}}">{{.Title}}</option>{{end}}</select></label><div class="actions"><button class="filled-button">添加</button></div></form></details><details class="composer-editor"><summary class="outlined-button">设置分区</summary><form method="post" action="/admin/home/sections/{{.ID}}/save" class="composer-form"><label>名称<input name="name" value="{{.Name}}" required></label><label>描述<input name="description" value="{{.Description}}"></label><label class="toggle-label"><input type="checkbox" name="enabled" {{if .Enabled}}checked{{end}}>在首页展示</label><div class="actions"><button class="filled-button">保存设置</button></div></form></details><form method="post" action="/admin/home/sections/{{.ID}}/move"><button class="icon-button" name="delta" value="-1"><span class="material-symbols-outlined">arrow_upward</span></button><button class="icon-button" name="delta" value="1"><span class="material-symbols-outlined">arrow_downward</span></button></form><form method="post" action="/admin/home/sections/{{.ID}}/delete"><button class="icon-button danger" data-confirm="确定删除这个首页分区吗"><span class="material-symbols-outlined">delete</span></button></form></div></section>{{end}}
<details class="panel composer-create"><summary><span class="material-symbols-outlined">add</span>新建首页分区</summary><form method="post" action="/admin/home/sections" class="composer-form"><label>分区名称<input name="name" required></label><label>分区标识<input name="id" placeholder="例如 editor-picks" required></label><label>简短说明<input name="description"></label><label class="toggle-label"><input type="checkbox" name="enabled" checked>创建后展示</label><div class="actions"><button class="filled-button">创建分区</button></div></form></details></div>
<input type="file" id="home-cover-file" accept="image/png,image/jpeg,image/webp,image/gif" hidden>
<script>document.querySelectorAll('[data-target-form]').forEach(function(form){var select=form.querySelector('[data-target-type]');function sync(){form.querySelectorAll('[data-target]').forEach(function(field){field.hidden=field.dataset.target!==select.value;});}select.addEventListener('change',sync);sync();});(function(){var input=document.getElementById('home-cover-file');var target=null;document.querySelectorAll('[data-upload-cover]').forEach(function(button){button.addEventListener('click',function(){target=button.closest('form').querySelector('[data-cover-field]');input.click();});});input.addEventListener('change',async function(){if(!input.files.length||!target)return;var body=new FormData();body.append('file',input.files[0]);var response=await fetch('/admin/blobs',{method:'POST',body:body});input.value='';if(!response.ok){target.setCustomValidity('图片上传失败，请重试');target.reportValidity();return;}target.setCustomValidity('');target.value=(await response.json()).sha256;});})();</script>
<script src="/admin/home-selector.js"></script>
{{template "admin_close" .}}
{{end}}

{{define "admin_blog"}}
{{template "admin_open" .}}
<header class="page-header"><div><h1>Blog 管理</h1><p>撰写、发布和维护客户端文章</p></div><div class="header-actions"><a class="outlined-button" href="/admin/home">首页编排</a><button class="filled-button" type="button" data-open-create>+ 新建文章</button></div></header>
{{if .Action}}<div class="notice success toast-notice" data-toast>文章操作已完成</div>{{end}}
<form class="filter-bar" method="get" action="/admin/blog"><label class="search-field"><span>搜索</span><input name="q" value="{{.Query.Search}}" placeholder="标题、作者、Slug 或正文"></label><label><span>状态</span><select name="published"><option value="">全部</option><option value="published" {{if eqs .Query.Published "published"}}selected{{end}}>已发布</option><option value="draft" {{if eqs .Query.Published "draft"}}selected{{end}}>草稿</option></select></label><label><span>排序</span><select name="sort"><option value="updated_desc">最近更新</option><option value="created_desc">最近创建</option><option value="published_desc">最近发布</option><option value="title_asc">标题</option></select></label><button class="filled-button">筛选</button></form><div class="blog-toolbar"><span class="count-badge">{{.Page.Total}} 篇文章</span><span class="muted">已发布文章会自动出现在客户端“最新动态”</span></div>
<section class="blog-list">{{range .Posts}}<article class="blog-list-card">{{if .CoverSHA256}}<img src="/api/blobs/{{.CoverSHA256}}" alt="">{{else}}<div class="blog-cover-placeholder"><span class="material-symbols-outlined">article</span></div>{{end}}<div class="blog-list-copy"><div class="title-line"><h2>{{.Title}}</h2>{{if .Published}}<span class="status success">已发布</span>{{else}}<span class="status warning">草稿</span>{{end}}</div><p>{{if .Subtitle}}{{.Subtitle}}{{else}}暂无摘要{{end}}</p><div class="blog-meta"><span>{{if eqs .Type "announcement"}}公告{{else if eqs .Type "recommendation"}}推荐{{else}}文档{{end}}</span><span>{{if .Author}}{{.Author}}{{else}}未填写作者{{end}}</span><span>更新于 {{dateTime .UpdatedAt}}</span></div></div><div class="blog-list-actions"><a class="filled-button" href="/admin/blog/{{.Slug}}">{{if .Published}}编辑{{else}}继续编辑{{end}}</a><form method="post" action="/admin/blog/{{.Slug}}/delete"><button class="icon-button danger" data-confirm="确定删除这篇文章吗"><span class="material-symbols-outlined">delete</span></button></form></div></article>{{else}}<section class="empty-state"><div class="empty-mark">+</div><h2>还没有文章</h2><p>创建第一篇公告、推荐或文档</p></section>{{end}}</section>
<dialog class="admin-dialog" data-create-dialog aria-labelledby="create-blog-title" aria-describedby="create-blog-description"><form method="post" action="/admin/blog" class="composer-form"><div class="section-header"><div><h2 id="create-blog-title">新建文章</h2><p id="create-blog-description">创建后进入完整编辑器</p></div><button class="icon-button" type="button" data-close-create aria-label="关闭新建文章对话框"><span class="material-symbols-outlined" aria-hidden="true">close</span></button></div><label>文章标识<input name="slug" placeholder="例如 2026-08-update" required></label><label>文章类型<select name="type"><option value="announcement">公告</option><option value="recommendation">推荐</option><option value="docs">文档</option></select></label><div class="actions"><button class="outlined-button" type="button" data-close-create>取消</button><button class="filled-button" type="submit">创建并编辑</button></div></form></dialog>
{{template "pagination" .}}<script>(function(){var dialog=document.querySelector('[data-create-dialog]');var trigger=document.querySelector('[data-open-create]');trigger.addEventListener('click',function(){dialog.showModal();window.setTimeout(function(){dialog.querySelector('input, select, button').focus();},0);});document.querySelectorAll('[data-close-create]').forEach(function(button){button.addEventListener('click',function(){dialog.close();});});dialog.addEventListener('close',function(){trigger.focus();});})();</script>
{{template "admin_close" .}}
{{end}}

{{define "admin_blog_edit"}}
{{template "admin_open" .}}
<header class="page-header"><div><a class="back-link" href="/admin/blog">← 全部文章</a><h1>{{.Post.Title}}</h1><p><code>{{.Post.Slug}}</code></p></div>{{if .Post.Published}}<span class="status success">已发布</span>{{else}}<span class="status warning">草稿</span>{{end}}</header>
{{if .Action}}<div class="notice success toast-notice" data-toast>文章已保存</div>{{end}}
<form method="post" action="/admin/blog/{{.Post.Slug}}" class="blog-editor"><section class="panel blog-fields"><label>标题<input name="title" value="{{.Post.Title}}" required></label><label>摘要<textarea name="subtitle" rows="3">{{.Post.Subtitle}}</textarea></label><div class="blog-field-grid"><label>类型<select name="type"><option value="announcement" {{if eqs .Post.Type "announcement"}}selected{{end}}>公告</option><option value="recommendation" {{if eqs .Post.Type "recommendation"}}selected{{end}}>推荐</option><option value="docs" {{if eqs .Post.Type "docs"}}selected{{end}}>文档</option></select></label><label>作者<input name="author" value="{{.Post.Author}}"></label></div><div class="cover-field"><div class="composer-cover large">{{if .Post.CoverSHA256}}<img src="/api/blobs/{{.Post.CoverSHA256}}" alt="当前封面">{{else}}<span class="material-symbols-outlined">panorama</span>{{end}}</div><div><strong>文章封面</strong><p class="muted">建议使用 16:9 图片</p><input name="cover_sha256" id="cover-field" value="{{.Post.CoverSHA256}}" hidden><button class="outlined-button" type="button" data-upload-cover>{{if .Post.CoverSHA256}}替换封面{{else}}上传封面{{end}}</button></div></div></section><section class="panel blog-writing"><div class="writing-toolbar"><strong>正文</strong><button class="outlined-button" type="button" data-upload-image>插入图片</button></div><div class="writing-grid"><label>Markdown<textarea name="body" id="body-field" rows="30">{{.Post.Body}}</textarea></label><div class="writing-preview"><span>内容预览</span><article id="body-preview"></article></div></div></section><footer class="blog-editor-actions"><span class="muted">{{if .Post.Published}}保存后立即更新客户端内容{{else}}草稿仅管理员可见{{end}}</span><div>{{if .Post.Published}}<button class="outlined-button danger" type="submit" name="publication_action" value="unpublish">下线文章</button><button class="filled-button" type="submit" name="publication_action" value="save">保存更改</button>{{else}}<button class="outlined-button" type="submit" name="publication_action" value="save">保存草稿</button><button class="filled-button" type="submit" name="publication_action" value="publish">发布文章</button>{{end}}</div></footer></form>
<input type="file" id="image-file" accept="image/png,image/jpeg,image/webp,image/gif" hidden>
<script>
(function() {
  var fileInput = document.getElementById('image-file');
  var mode = 'body';
  document.querySelector('[data-upload-image]').addEventListener('click', function() { mode = 'body'; fileInput.click(); });
  document.querySelector('[data-upload-cover]').addEventListener('click', function() { mode = 'cover'; fileInput.click(); });
  fileInput.addEventListener('change', async function() {
    if (!fileInput.files.length) return;
    var body = new FormData();
    body.append('file', fileInput.files[0]);
    var response = await fetch('/admin/blobs', {method: 'POST', body: body});
    fileInput.value = '';
    if (!response.ok) { fileInput.setCustomValidity('图片上传失败，请重试'); fileInput.reportValidity(); return; }
    fileInput.setCustomValidity('');
    var sha = (await response.json()).sha256;
    if (mode === 'cover') {
      document.getElementById('cover-field').value = sha;
      return;
    }
    var field = document.getElementById('body-field');
    var insert = '![](/api/blobs/' + sha + ')';
    var start = field.selectionStart || field.value.length;
    field.value = field.value.slice(0, start) + insert + field.value.slice(field.selectionEnd || start);
    field.focus();
    field.selectionStart = field.selectionEnd = start + insert.length;
  });
  var bodyField = document.getElementById('body-field');
  var preview = document.getElementById('body-preview');
  function renderPreview() { preview.textContent = bodyField.value || '正文预览会显示在这里'; }
  bodyField.addEventListener('input', renderPreview);
  renderPreview();
})();
</script>
{{template "admin_close" .}}
{{end}}
`
