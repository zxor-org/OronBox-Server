package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// TestWritePreview renders every page with the same fixtures the render tests
// use and writes them to disk, so the admin console can be inspected in a
// browser without a database. It is opt-in because it is a tool, not a check.
//
//	ADMIN_PREVIEW_DIR=/tmp/preview go test ./internal/web/ -run TestWritePreview
func TestWritePreview(t *testing.T) {
	directory := os.Getenv("ADMIN_PREVIEW_DIR")
	if directory == "" {
		t.Skip("ADMIN_PREVIEW_DIR is not set")
	}
	// The templates link assets by absolute path, so the preview has to mirror
	// the real /assets/ layout for the pages to look like production.
	assets := filepath.Join(directory, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []struct{ name, body string }{
		{"app.css", CSS}, {"theme.js", ThemeJS},
		{"admin.js", AdminJS}, {"transition.js", TransitionJS},
	} {
		if err := os.WriteFile(filepath.Join(assets, asset.name), []byte(asset.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	templates := NewTemplates()
	written := 0
	cases := renderableTemplateCases()
	// The render tests only need an empty slice to prove a template parses, but
	// an empty table shows nothing about the layout, so the queue is populated
	// with the states a reviewer actually has to tell apart.
	if values, ok := cases["admin_review"].(map[string]any); ok {
		values["Items"] = previewReviewQueue()
		values["Reviewers"] = []store.AdminUserItem{
			{ID: "r1", Username: "lin", Role: "reviewer"},
			{ID: "r2", Username: "mei", Role: "admin"},
		}
	}
	// The decision form is the single most used form in the console, but it
	// only renders for a pending case with attributes to tick, so the empty
	// render fixture never showed it at all.
	if values, ok := cases["admin_review_detail"].(map[string]any); ok {
		values["Detail"] = previewReviewDetail()
		values["Attributes"] = []creator.ResourceAttribute{
			{ID: "no-ads", NameZH: "无广告", NameEN: "No ads", Enabled: true},
			{ID: "open-source", NameZH: "开源", NameEN: "Open source", Enabled: true},
			{ID: "offline", NameZH: "可离线使用", NameEN: "Offline", Enabled: true},
			{ID: "no-login", NameZH: "无需登录", NameEN: "No login", Enabled: true},
		}
	}
	if values, ok := cases["admin_comments"].(map[string]any); ok {
		values["Items"] = previewCommentQueue()
		values["Total"] = 3
	}
	for name, data := range cases {
		if values, ok := data.(map[string]any); ok {
			values["CSRFToken"] = "preview-token"
		}
		file, err := os.Create(filepath.Join(directory, name+".html"))
		if err != nil {
			t.Fatal(err)
		}
		if err := templates.t.ExecuteTemplate(file, name, data); err != nil {
			file.Close()
			t.Fatalf("render %s: %v", name, err)
		}
		file.Close()
		written++
	}
	// The reviewer console is a different information architecture, not just a
	// shorter menu, so it gets its own previews to look at.
	for _, name := range []string{"admin_review", "admin_forbidden"} {
		data, ok := cases[name].(map[string]any)
		if !ok {
			continue
		}
		reviewer := make(map[string]any, len(data))
		for key, value := range data {
			reviewer[key] = value
		}
		reviewer["Role"] = "reviewer"
		reviewer["Nav"] = NavigationFor("reviewer")
		reviewer["HomePath"] = HomePathFor("reviewer")
		file, err := os.Create(filepath.Join(directory, name+"_reviewer.html"))
		if err != nil {
			t.Fatal(err)
		}
		if err := templates.t.ExecuteTemplate(file, name, reviewer); err != nil {
			file.Close()
			t.Fatalf("render %s as reviewer: %v", name, err)
		}
		file.Close()
		written++
	}
	t.Logf("wrote %d pages to %s", written, directory)
}

func previewReviewDetail() store.AdminReviewDetail {
	now := time.Now()
	return store.AdminReviewDetail{
		Review: store.AdminReviewItem{
			ID: "1", State: "pending", RevisionName: "天气小组件", RevisionNumber: 4,
			ResourceSlug: "weather-widget", ResourceKind: "quickapp", ResourceState: "listed",
			Owner: "chen", OwnerID: "u1", CurationGrade: "standard",
			Targets: []string{"OronBox", "米坛"}, Items: []string{"图片合规", "安装包可安装", "描述与实际功能一致"},
			Priority: 3, DueAt: now.Add(-6 * time.Hour), FirstSubmittedAt: now.Add(-96 * time.Hour),
			CreatedAt: now.Add(-96 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
			Reports: 2, OwnerRejections: 1,
		},
		Current: store.AdminReviewRevisionSnapshot{
			ID: "rev-4", Number: 4, Name: "天气小组件", State: "submitted",
			Summary:    "在锁屏和表盘上显示实时天气、空气质量与未来三小时降水概率。",
			PaidType:   "free",
			Attributes: []string{"no-ads", "offline"},
			CreatedBy:  "chen", CreatedVia: "client", CreatedAt: now.Add(-96 * time.Hour),
		},
	}
}

// previewCommentQueue shows the three cases the console has to separate: an AI
// referral still waiting on a person, one already decided, and a plain visible
// comment reached through search rather than through the queue.
func previewCommentQueue() []store.AdminCommentItem {
	now := time.Now()
	return []store.AdminCommentItem{
		{
			Comment: store.Comment{
				ID: "c1", ResourceID: "r1", UserID: "u1", Username: "chen", BandBBSUserID: 10241,
				Body: "这个表盘的电量显示一直不准，作者能修一下吗", ModerationState: "visible",
				CreatedAt: now.Add(-90 * time.Minute),
			},
			ModerationAction: "review", ModerationModel: "deepseek-chat",
			ModerationReason: "疑似包含联系方式，需人工确认",
		},
		{
			Comment: store.Comment{
				ID: "c2", ResourceID: "r2", UserID: "u2", Username: "wang", BandBBSUserID: 20480,
				Body: "加我微信发你破解版", ModerationState: "hidden",
				CreatedAt: now.Add(-5 * time.Hour),
			},
			ModerationAction: "hide", ModerationModel: "deepseek-chat",
			ModerationReason: "推广盗版资源", HumanReviewed: true,
		},
		{
			Comment: store.Comment{
				ID: "c3", ResourceID: "r3", UserID: "u3", Username: "zhao", BandBBSUserID: 30512,
				Body: "很好用，感谢作者", ModerationState: "visible",
				CreatedAt: now.Add(-26 * time.Hour),
			},
		},
	}
}

// previewReviewQueue covers the combinations the queue has to distinguish at a
// glance: overdue versus in time, every priority, assigned versus unassigned,
// and cases carrying reports or a history of rejections.
func previewReviewQueue() []store.AdminReviewItem {
	now := time.Now()
	return []store.AdminReviewItem{
		{
			ID: "1", State: "pending", RevisionName: "天气小组件", RevisionNumber: 4,
			ResourceSlug: "weather-widget", ResourceKind: "quickapp", Owner: "chen",
			Targets: []string{"OronBox", "米坛"}, Items: []string{"a", "b", "c"},
			Priority: 3, DueAt: now.Add(-6 * time.Hour), FirstSubmittedAt: now.Add(-96 * time.Hour),
			CreatedAt: now.Add(-96 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
			Reports: 2, OwnerRejections: 1,
		},
		{
			ID: "2", State: "pending", RevisionName: "极简表盘 Pro", RevisionNumber: 1,
			ResourceSlug: "minimal-face", ResourceKind: "watchface", Owner: "wang",
			Targets: []string{"OronBox"}, Items: []string{"a", "b"},
			Priority: 2, DueAt: now.Add(5 * time.Hour), FirstSubmittedAt: now.Add(-40 * time.Hour),
			CreatedAt: now.Add(-40 * time.Hour), UpdatedAt: now.Add(-40 * time.Hour),
			Reviewer: "lin", ReviewerID: "r1", OwnerRejections: 2,
		},
		{
			ID: "3", State: "pending", RevisionName: "记账助手", RevisionNumber: 2,
			ResourceSlug: "ledger", ResourceKind: "quickapp", Owner: "zhao",
			Targets: []string{"OronBox", "AstroBox"}, Items: []string{"a"},
			Priority: 0, DueAt: now.Add(44 * time.Hour), FirstSubmittedAt: now.Add(-3 * time.Hour),
			CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-30 * time.Minute),
		},
		{
			ID: "4", State: "approved", RevisionName: "番茄钟", RevisionNumber: 7,
			ResourceSlug: "pomodoro", ResourceKind: "quickapp", Owner: "sun",
			Targets: []string{"OronBox"}, Items: []string{"a", "b", "c", "d"},
			Priority: 1, FirstSubmittedAt: now.Add(-120 * time.Hour),
			CreatedAt: now.Add(-120 * time.Hour), UpdatedAt: now.Add(-20 * time.Hour),
			Reviewer: "mei", ReviewerID: "r2",
		},
	}
}
