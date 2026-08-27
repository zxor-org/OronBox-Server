package web

// The console serves two very different jobs. A reviewer works a queue and
// needs the moderation surfaces; an admin also runs the platform. Until now the
// drawer listed everything to everyone and the admin-only pages answered a
// reviewer's click with a bare "forbidden", which reads like a broken link
// rather than a boundary. The navigation is therefore described once, here,
// with the role each destination needs, and rendered per session.

// RoleAdmin is the only role that unlocks the platform-administration pages.
// Anything else is treated as a reviewer, which is the least privileged
// console user, so a new role can never silently inherit admin surfaces.
const RoleAdmin = "admin"

type NavItem struct {
	Path  string
	Alias string
	Icon  string
	Label string
	// Role is empty when any console user may open the page, or RoleAdmin when
	// the route behind it is registered with requireAdminRole("admin").
	Role string
}

type NavGroup struct {
	Label string
	Items []NavItem
}

// navigation is the full drawer. NavigationFor filters it per session; nothing
// else should build a menu, so the drawer and the route table can be checked
// against each other in one place.
var navigation = []NavGroup{
	{Label: "审核工作台", Items: []NavItem{
		{Path: "/admin/review", Icon: "fact_check", Label: "待审核"},
		{Path: "/admin/collections", Icon: "collections_bookmark", Label: "合集审核"},
		{Path: "/admin/comments", Icon: "forum", Label: "评论审核"},
		{Path: "/admin/reports", Alias: "/admin/feedback", Icon: "report", Label: "举报与反馈"},
	}},
	{Label: "内容与发布", Items: []NavItem{
		{Path: "/admin", Icon: "dashboard", Label: "概览"},
		{Path: "/admin/resources", Icon: "inventory_2", Label: "全部资源"},
		{Path: "/admin/publications", Icon: "cloud_upload", Label: "发布任务"},
		{Path: "/admin/devices", Icon: "watch", Label: "设备目录"},
		{Path: "/admin/plugins", Icon: "extension", Label: "插件管理"},
	}},
	{Label: "社区与用户", Items: []NavItem{
		{Path: "/admin/users", Icon: "group", Label: "用户", Role: RoleAdmin},
		{Path: "/admin/coins", Icon: "toll", Label: "硬币管理", Role: RoleAdmin},
		{Path: "/admin/messages", Icon: "notifications", Label: "系统消息"},
	}},
	{Label: "内容运营", Items: []NavItem{
		{Path: "/admin/home", Icon: "home", Label: "首页编排"},
		{Path: "/admin/blog", Icon: "article", Label: "Blog 管理", Role: RoleAdmin},
		{Path: "/admin/announcements", Icon: "campaign", Label: "公告", Role: RoleAdmin},
		{Path: "/admin/releases", Icon: "new_releases", Label: "客户端版本"},
	}},
	{Label: "系统与诊断", Items: []NavItem{
		{Path: "/admin/oauth/events", Icon: "timeline", Label: "OAuth 事件"},
		{Path: "/admin/oauth/states", Icon: "key", Label: "OAuth States"},
		{Path: "/admin/oauth/tickets", Icon: "confirmation_number", Label: "登录 Tickets"},
		{Path: "/admin/clients", Icon: "devices", Label: "客户端统计"},
		{Path: "/admin/storage/blobs", Icon: "database", Label: "Blob 与副本"},
		{Path: "/admin/health", Icon: "monitoring", Label: "运行状态"},
		{Path: "/admin/audit", Icon: "history", Label: "审计日志"},
		{Path: "/admin/settings", Icon: "tune", Label: "设置", Role: RoleAdmin},
	}},
}

// NavigationFor returns the drawer for a role, dropping items the session may
// not open and any group left empty as a result.
func NavigationFor(role string) []NavGroup {
	groups := make([]NavGroup, 0, len(navigation))
	for _, group := range navigation {
		items := make([]NavItem, 0, len(group.Items))
		for _, item := range group.Items {
			if item.Role == RoleAdmin && role != RoleAdmin {
				continue
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			continue
		}
		groups = append(groups, NavGroup{Label: group.Label, Items: items})
	}
	return groups
}

// HomePathFor is where a session lands after login and where the brand link
// points. A reviewer's job starts at the queue, not at a platform dashboard
// they cannot act on.
func HomePathFor(role string) string {
	if role == RoleAdmin {
		return "/admin"
	}
	return "/admin/review"
}
