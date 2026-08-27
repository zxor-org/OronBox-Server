package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

const (
	// CSRFFieldName is the hidden field every rendered POST form carries.
	CSRFFieldName = "csrf_token"
	// CSRFHeaderName carries the same token for the few fetch() callers.
	CSRFHeaderName = "X-CSRF-Token"
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
	// CSRFToken is unused by this page but has to exist because the shared
	// head partial reads it, and html/template treats a missing struct field
	// as an execution error rather than an empty value.
	CSRFToken string
}

// csrfFormPattern matches the opening tag of every state-changing form.
const csrfFieldTemplate = `{{define "csrf"}}{{with .CSRFToken}}<input type="hidden" name="` +
	CSRFFieldName + `" value="{{.}}">{{end}}{{end}}`

var csrfFormPattern = regexp.MustCompile(`(?i)<form[^>]*\bmethod="post"[^>]*>`)

// withCSRFFields injects the hidden token into every POST form at parse time.
// Doing it here rather than by hand in each template means a form added later
// cannot silently ship without CSRF protection.
func withCSRFFields(source string) string {
	return csrfFormPattern.ReplaceAllStringFunc(source, func(tag string) string {
		return tag + `{{template "csrf" $}}`
	})
}

func NewTemplates() *Templates {
	return &Templates{
		t: parseAdminTemplates(template.Must(template.New("root").Funcs(template.FuncMap{
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
			"rfc3339":        rfc3339,
			"safeURL":        safeURL,
			"waited":         waited,
			"aiVerdict":      moderationVerdict,
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
			"reviewEventLabel":      reviewEventLabel,
			"reviewEventClass":      reviewEventClass,
			"publicationEventLabel": publicationEventLabel,
			"publicationPhaseLabel": publicationPhaseLabel,
			"splitStates": func() []string {
				return []string{"pending", "running", "reviewing", "published", "failed", "cancelled"}
			},
			"splitMessageKinds": func() []string {
				return []string{"review_result", "moderation", "comment_reply", "report_result", "admin_message", "account", "announcement"}
			},
			"sub1":                   func(value int) int { return value - 1 },
			"add1":                   func(value int) int { return value + 1 },
			"defaultReviewChecklist": func() []string { return defaultReviewChecklist },
			"extraReviewItems":       extraReviewItems,
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
		}).Parse(csrfFieldTemplate))),
	}
}

//go:embed templates/*.gohtml
var templateFS embed.FS

//go:embed assets/icons.svg
var iconSprite string

// iconSpriteTemplate makes the sprite available to the shell. It is inlined
// rather than linked because cross-file <use href="file.svg#id"> is not
// reliably supported across browsers, and inlining also avoids a request.
func iconSpriteTemplate() string {
	return `{{define "icon_sprite"}}` + iconSprite + `{{end}}`
}

// parseAdminTemplates loads every group file. Parsing happens once at startup
// and panics on failure, because a broken template is a build mistake and
// serving a half-rendered console would be worse than refusing to start.
func parseAdminTemplates(root *template.Template) *template.Template {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		panic(err)
	}
	if _, err := root.Parse(iconSpriteTemplate()); err != nil {
		panic(fmt.Errorf("parse icon sprite: %w", err))
	}
	for _, entry := range entries {
		source, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			panic(err)
		}
		if _, err := root.Parse(withCSRFFields(string(source))); err != nil {
			panic(fmt.Errorf("parse %s: %w", entry.Name(), err))
		}
	}
	return root
}

// safeURL filters links that came out of the database before they become an
// href. Anything that is not plain http(s) is dropped rather than escaped, so a
// stored javascript: or data: URL cannot turn into script execution for
// whoever opens the page. html/template already refuses such schemes in a URL
// context, but that produces a broken #ZgotmplZ link instead of no link, and
// several of these values also reach non-URL contexts.
func safeURL(value any) string {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	lowered := strings.ToLower(trimmed)
	if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") {
		return trimmed
	}
	return ""
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

var defaultReviewChecklist = []string{
	"图片合规",
	"安装包可安装",
	"描述与实际功能一致",
	"设备适配正确",
	"发布计划完整",
}

func extraReviewItems(saved []string) []string {
	standard := map[string]struct{}{}
	for _, item := range defaultReviewChecklist {
		standard[item] = struct{}{}
	}
	extras := []string{}
	for _, item := range saved {
		if _, ok := standard[item]; !ok && strings.TrimSpace(item) != "" {
			extras = append(extras, item)
		}
	}
	return extras
}

func statusClass(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "active", "approved", "published", "success", "ok", "closed", "committed", "resolved", "visible", "listed":
		return "success"
	case "pending", "submitted", "reviewing", "receiving", "verifying", "open", "investigating", "review":
		return "warning"
	case "replied", "running":
		return "info"
	case "archived", "superseded", "cancelled", "expired", "used", "dismissed", "deleted":
		return "neutral"
	case "rejected", "failed", "failure", "aborted", "error", "suspended", "frozen", "delisted", "hidden":
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
		"visible": "可见", "hidden": "已隐藏", "deleted": "已删除", "review": "待复审",
		"suspended": "已下架", "frozen": "已冻结",
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

// moderationVerdict names what the model decided. It is deliberately separate
// from statusLabel because a verdict is a recommendation, not the state the
// comment ended up in, and conflating the two hides who actually decided.
func moderationVerdict(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "approve", "allow", "pass":
		return "建议通过"
	case "hide", "block", "reject":
		return "建议隐藏"
	case "review":
		return "转人工"
	case "":
		return "未判定"
	default:
		return fmt.Sprint(value)
	}
}

// waited renders a queue age the way a reviewer would say it out loud. The
// exact minute stops mattering once a case has been waiting for days, so the
// unit grows with the duration instead of printing 73h14m.
func waited(value time.Duration) string {
	switch {
	case value <= 0:
		return "刚提交"
	case value < time.Hour:
		return fmt.Sprintf("%d 分钟", int(value.Minutes()))
	case value < 24*time.Hour:
		return fmt.Sprintf("%d 小时", int(value.Hours()))
	default:
		return fmt.Sprintf("%d 天", int(value.Hours()/24))
	}
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

func rfc3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
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

func reviewEventLabel(value string) string {
	switch value {
	case "assigned":
		return "已指派"
	case "unassigned":
		return "取消指派"
	case "checklist_saved":
		return "保存检查项"
	case "approved":
		return "批准发布"
	case "rejected":
		return "退回修改"
	case "reopened":
		return "重新打开"
	case "priority_changed":
		return "调整优先级"
	case "appeal_opened":
		return "收到申诉"
	case "appeal_resolved":
		return "申诉已处理"
	case "note":
		return "补充说明"
	default:
		return value
	}
}

func reviewEventClass(value string) string {
	switch value {
	case "approved":
		return "success"
	case "rejected":
		return "danger"
	case "appeal_opened", "reopened":
		return "warning"
	case "assigned", "priority_changed", "appeal_resolved":
		return "info"
	default:
		return "neutral"
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
