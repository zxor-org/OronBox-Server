package web

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

var navigation = []NavGroup{
	{Label: "工作台", Items: []NavItem{
		{Path: "/console", Icon: "dashboard", Label: "概览"},
		{Path: "/console/resources", Icon: "inventory_2", Label: "资源管理"},
		{Path: "/console/reviews", Icon: "fact_check", Label: "审核"},
		{Path: "/console/comments", Icon: "forum", Label: "评论"},
		{Path: "/console/feedback", Icon: "report", Label: "工单"},
		{Path: "/console/plugins", Icon: "extension", Label: "插件"},
	}},
	{Label: "管理", Items: []NavItem{
		{Path: "/console/blogs", Icon: "article", Label: "博客", Role: RoleAdmin},
		{Path: "/console/users", Icon: "group", Label: "用户", Role: RoleAdmin},
		{Path: "/console/settings", Icon: "tune", Label: "前端版本", Role: RoleAdmin},
		{Path: "/console/system", Icon: "monitoring", Label: "系统", Role: RoleAdmin},
		{Path: "/console/governance", Icon: "tune", Label: "治理", Role: RoleAdmin},
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
		return "/console"
	}
	return "/console/reviews"
}
