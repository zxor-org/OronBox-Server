package web

import (
	"html/template"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminTemplatesRender(t *testing.T) {
	templates := NewTemplates()
	for name, data := range renderableTemplateCases() {
		recorder := httptest.NewRecorder()
		if err := templates.Render(recorder, name, data); err != nil {
			t.Errorf("render %s: %v", name, err)
		}
	}
}

// renderableTemplateCases is the shared corpus of every page template with data
// good enough to execute, so page-wide invariants can be asserted in one place.
func renderableTemplateCases() map[string]any {
	config := map[string]any{
		"BandBBS": map[string]any{
			"ClientID": "id", "RedirectURI": "uri",
			"Scopes": []string{"user:read"}, "PublishScopes": []string{"resource:write"},
		},
		"GitHub": map[string]any{
			"ClientID": "id", "RedirectURI": "uri", "Scopes": []string{"public_repo"},
		},
		"PublicURL": "https://example.com", "Version": "dev", "Commit": "",
	}
	resourceItem := store.AdminResourceItem{
		ID: "resource-id", Name: "Resource", Slug: "resource", Owner: "author",
		Kind: "quickapp", ModerationState: "visible",
	}
	resourcePage := store.AdminResourcePage{
		Items: []store.AdminResourceItem{resourceItem}, Total: 1, Page: 1,
		PerPage: 25, TotalPages: 1, Query: store.AdminResourceQuery{Page: 1, PerPage: 25},
	}
	resourceDetail := store.AdminResourceDetail{Resource: resourceItem}
	revision := store.AdminRevision{
		ID: "revision-id", Number: 1, Name: "Resource", PaidType: "free",
		State: "approved", CreatedVia: "creator", PublicationPlan: []byte("[]"),
	}
	revisionDetail := store.AdminRevisionDetail{Resource: resourceItem, Revision: revision}
	feedbackPage := store.FeedbackPage{
		Items: []store.FeedbackTicket{}, Total: 0, Page: 1, PerPage: 25,
		TotalPages: 0, Query: store.AdminFeedbackQuery{Page: 1, PerPage: 25},
	}
	cases := map[string]any{
		"admin_login":     map[string]any{"Title": "t", "AuthorizeURL": "/login"},
		"admin_dashboard": map[string]any{"Title": "t", "Stats": model.Stats{}, "Events": []any{}, "Clients": []any{}},
		"admin_events": map[string]any{"Title": "t", "Events": []any{},
			"Page": store.AdminOAuthEventPage{}, "Query": store.AdminOAuthEventQuery{}},
		"admin_states": map[string]any{"Title": "t", "States": []any{},
			"Page": store.AdminOAuthStatePage{}, "Query": store.AdminOAuthStateQuery{}},
		"admin_tickets": map[string]any{"Title": "t", "Tickets": []any{},
			"Page": store.AdminOAuthTicketPage{}, "Query": store.AdminOAuthTicketQuery{}},
		"admin_clients": map[string]any{"Title": "t", "Clients": []any{},
			"Page": store.AdminClientStatsPage{}, "Query": store.AdminClientStatsQuery{}},
		"admin_event_detail":  map[string]any{"Title": "t", "Detail": store.AdminOAuthEventDetail{}},
		"admin_state_detail":  map[string]any{"Title": "t", "Detail": store.AdminOAuthStateDetail{}},
		"admin_ticket_detail": map[string]any{"Title": "t", "Detail": store.AdminOAuthTicketDetail{}},
		"admin_client_detail": map[string]any{"Title": "t", "Detail": store.AdminClientDetail{}},
		"admin_devices": map[string]any{"Title": "t", "Items": []store.AdminDeviceItem{},
			"Page": store.AdminDevicePage{}, "Query": store.AdminDeviceQuery{}},
		"admin_device_detail":      map[string]any{"Title": "t", "Item": store.AdminDeviceItem{}, "Resources": []store.AdminResourceItem{}},
		"admin_publications":       map[string]any{"Title": "t", "Items": []store.AdminPublicationItem{}, "Page": store.AdminPublicationPage{}, "Query": store.AdminPublicationQuery{}},
		"admin_publication_detail": map[string]any{"Title": "t", "Item": store.AdminPublicationItem{}},
		"admin_settings":           map[string]any{"Title": "t", "Config": config, "BandBBSSecretState": "已配置", "GitHubSecretState": "已配置", "Announcements": []store.Announcement{}},
		"admin_health": map[string]any{
			"Title": "t", "DBStatus": "ok", "DBLatency": "1ms", "Uptime": "1h0m0s",
			"Stats": model.Stats{}, "Diagnostics": store.AdminHealthDiagnostics{},
		},
		"admin_blobs":       map[string]any{"Title": "t", "Items": []store.AdminBlobItem{}, "Page": store.AdminBlobPage{}, "Query": store.AdminBlobQuery{}},
		"admin_blob_detail": map[string]any{"Title": "t", "Detail": store.AdminBlobDetail{}},
		"admin_audit": map[string]any{"Title": "t", "Logs": []any{},
			"Page": store.AdminAuditLogPage{}, "Query": store.AdminAuditLogQuery{}, "ExportURL": "/admin/audit.csv"},
		"admin_audit_detail":      map[string]any{"Title": "t", "Item": store.AuditLog{}},
		"admin_review":            map[string]any{"Title": "t", "Items": []store.AdminReviewItem{}, "Page": store.AdminReviewPage{}, "Query": store.AdminReviewQuery{}},
		"admin_review_detail":     map[string]any{"Title": "t", "Detail": store.AdminReviewDetail{}, "Attributes": []any{}},
		"admin_collections":       map[string]any{"Title": "t", "Items": []store.AdminCollectionItem{}, "Page": store.AdminCollectionPage{}, "Query": store.AdminCollectionQuery{}},
		"admin_collection_detail": map[string]any{"Title": "t", "Detail": store.AdminCollectionDetail{}},
		"admin_plugins":           map[string]any{"Title": "t", "Items": []store.AdminPluginItem{}, "Page": store.AdminPluginPage{}, "Query": store.AdminPluginQuery{}},
		"admin_plugin_workspace":  map[string]any{"Title": "t", "Detail": store.AdminPluginDetail{}},
		"admin_comments":          map[string]any{"Title": "t", "Items": []store.AdminCommentItem{}, "Total": 0, "Page": 1, "Prompt": "prompt"},
		"admin_releases":          map[string]any{"Title": "t", "Items": []store.AdminReleaseItem{}, "Page": store.AdminReleasePage{}, "Query": store.AdminReleaseQuery{}},
		"admin_announcements":     map[string]any{"Title": "t", "Items": []store.AdminAnnouncementItem{}, "Page": store.AdminAnnouncementPage{}, "Query": store.AdminAnnouncementQuery{}},
		"admin_release_detail":    map[string]any{"Title": "t", "Item": store.AdminReleaseItem{}},
		"admin_resources":         map[string]any{"Title": "t", "Items": resourcePage.Items, "Page": resourcePage, "Query": resourcePage.Query},
		"admin_users":             map[string]any{"Title": "t", "Items": []store.AdminUserItem{}, "Page": store.AdminUserPage{}, "Query": store.AdminUserQuery{}},
		"admin_coins": map[string]any{
			"Title": "t", "Stats": store.AdminCoinStats{}, "Ledger": []store.AdminCoinEntry{},
			"Page": store.AdminCoinPage{}, "Query": store.AdminCoinQuery{}, "Users": []store.AdminCoinUserOption{},
		},
		"admin_user_workspace": map[string]any{"Title": "t", "Detail": store.AdminUserDetail{}},
		"admin_resource_detail": map[string]any{
			"Title": "t", "Item": resourceItem, "Detail": resourceDetail,
			"Publications": []store.AdminPublication{}, "Artifacts": []store.AdminArtifact{},
			"Media": []store.AdminMedia{}, "Snapshot": "{}",
		},
		"admin_revision_detail": map[string]any{
			"Title": "t", "Detail": revisionDetail,
		},
		"admin_revision_editor": map[string]any{
			"Title": "t", "Detail": revisionDetail, "Attributes": []any{}, "IsDraft": false,
		},
		"admin_reports":  map[string]any{"Title": "t", "Items": feedbackPage.Items, "Page": feedbackPage, "Query": feedbackPage.Query},
		"admin_feedback": map[string]any{"Title": "t", "Items": feedbackPage.Items, "Page": feedbackPage, "Query": feedbackPage.Query},
		"admin_report_detail": map[string]any{
			"Title": "t", "Item": store.FeedbackTicket{}, "Ticket": store.FeedbackTicket{},
		},
		"admin_messages": map[string]any{"Title": "t", "Items": []store.AdminMessage{},
			"Page": store.AdminMessagePage{}, "Query": store.AdminMessageQuery{}},
		"admin_message_detail": map[string]any{"Title": "t", "Item": store.AdminMessage{}},
		"admin_home": map[string]any{"Title": "t", "Banners": []store.HomeBanner{},
			"Sections": []store.HomeSection{}, "Cards": map[string][]store.HomeSectionCard{},
			"Posts": []store.BlogPost{}, "Resources": []store.AdminResourceItem{}},
		"admin_blog":       map[string]any{"Title": "t", "Posts": []store.BlogPost{}, "Page": store.AdminBlogPage{}, "Query": store.AdminBlogQuery{}},
		"admin_blog_edit":  map[string]any{"Title": "t", "Post": store.BlogPost{Slug: "hello", Title: "Hello"}},
		"admin_form_error": map[string]any{"Title": "t", "Message": "m", "BackURL": "/admin", "RetryURL": "/admin/blog", "Fields": []any{}},
		"admin_forbidden":  map[string]any{"Title": "t", "Path": "/admin/users"},
		"server_home":      map[string]any{"Title": "Server"},
		"transition_page": TransitionPageData{
			Title: "授权完成", Heading: "授权完成", Description: "可以返回 OronBox 继续使用",
			Target: template.URL("oronbox://oauth?ticket=test"),
			Auto:   true, Tone: "success",
		},
	}
	// The shell reads the drawer and the role-aware home path off the page data
	// because the server injects them for every render, so the fixtures have to
	// do the same or every page would render without navigation.
	for _, data := range cases {
		values, ok := data.(map[string]any)
		if !ok {
			continue
		}
		values["Role"] = RoleAdmin
		values["Nav"] = NavigationFor(RoleAdmin)
		values["HomePath"] = HomePathFor(RoleAdmin)
	}
	return cases
}

func TestAdminRevisionEditorExposesManagementRevisionFields(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	detail := store.AdminRevisionDetail{
		Resource: store.AdminResourceItem{ID: "resource-id", Name: "Resource"},
		Revision: store.AdminRevision{
			ID: "revision-id", Number: 2, Name: "Resource", PaidType: "paid",
			State: "approved", PublicationPlan: []byte(`[{"target":"astrobox","config":{}}]`),
		},
	}
	if err := NewTemplates().Render(recorder, "admin_revision_editor", map[string]any{
		"Title": "编辑资源", "Detail": detail, "Attributes": []any{}, "IsDraft": false,
	}); err != nil {
		t.Fatalf("render revision editor: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`name="name"`, `name="paid_type"`, `value="paid" selected`,
		`name="publication_plan"`, `保存管理草稿`, `不会覆盖历史修订`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("revision editor is missing %q", expected)
		}
	}
}

func TestAdminRevisionEditorExposesAssetsForPendingReview(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	detail := store.AdminRevisionDetail{
		Resource: store.AdminResourceItem{ID: "resource-id", Name: "Resource"},
		Revision: store.AdminRevision{
			ID: "revision-id", Number: 2, Name: "Resource", PaidType: "free",
			State: "submitted", ReviewState: "pending", PublicationPlan: []byte("[]"),
		},
		Media:     []store.AdminMedia{{ID: "media-id", SHA256: strings.Repeat("a", 64), Role: "icon", Width: 1, Height: 1}},
		Artifacts: []store.AdminArtifact{{ID: "artifact-id", OriginalName: "resource.rpk", DeviceBindings: []store.AdminArtifactDevice{{ID: "device-id", DisplayName: "Device", Codename: "device"}}}},
	}
	if err := NewTemplates().Render(recorder, "admin_revision_editor", map[string]any{
		"Title": "编辑资源", "Detail": detail, "Attributes": []any{}, "Devices": []store.AdminDeviceItem{{ID: "device-id", DisplayName: "Device", Codename: "device"}},
		"CanEditAssets": true, "IsPendingReview": true,
	}); err != nil {
		t.Fatalf("render pending review editor: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`/draft/revision-id/media`, `/draft/revision-id/artifacts`, `待审核修订 #2`, `保存审核修正`, `name="draft_revision_id" value="revision-id"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("pending review editor is missing %q", expected)
		}
	}
	if strings.Contains(body, "先保存管理草稿后即可编辑媒体") {
		t.Error("pending review editor still hides asset editing")
	}
}

func TestAdminAuditDetailProvidesReverseLinksAndStructuredData(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	item := store.AuditLog{
		ID: 42, ActorUserID: "actor-id", Username: "admin", Action: "resource.suspend", Result: "success",
		Target: store.AuditTarget{Type: "resource", ID: "resource-id"}, Before: map[string]any{"state": "visible"}, After: map[string]any{"state": "suspended"},
	}
	if err := NewTemplates().Render(recorder, "admin_audit_detail", map[string]any{"Title": "审计详情", "Item": item}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"/admin/users/actor-id", "/admin/resources/resource-id", "resource.suspend", "visible", "suspended"} {
		if !strings.Contains(body, expected) {
			t.Errorf("audit detail is missing %q", expected)
		}
	}
}

func TestAdminPublicationDetailRendersImmutableHistory(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	item := store.AdminPublicationItem{
		AdminPublication: store.AdminPublication{ID: "publication-id", State: "failed", Target: "astrobox"},
		RevisionName:     "History resource",
		History: []store.AdminPublicationAttempt{{
			AttemptNumber: 2, Phase: "execute", Event: "execution_failed",
			StateFrom: "running", StateTo: "failed", ErrorMessage: "network timeout",
			Detail: []byte(`{"retry_delay_seconds":60}`),
		}},
	}
	if err := NewTemplates().Render(recorder, "admin_publication_detail", map[string]any{"Title": "发布历史", "Item": item}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"执行历史", "执行失败", "network timeout", "retry_delay_seconds", "#2"} {
		if !strings.Contains(body, expected) {
			t.Errorf("publication history is missing %q", expected)
		}
	}
}

func TestAdminPublicationListRendersFilteredBatchRetryConfirmation(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	page := store.AdminPublicationPage{Total: 42, Page: 1, PerPage: 25, Query: store.AdminPublicationQuery{State: "failed", Target: "astrobox", Search: "timeout"}}
	if err := NewTemplates().Render(recorder, "admin_publications", map[string]any{"Title": "发布任务", "Page": page, "Items": page.Items, "Query": page.Query}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`action="/admin/publications/retry-failed"`, `name="target" value="astrobox"`, `name="q" value="timeout"`, `data-confirm=`, `不限于当前页`, `批量重试（42）`} {
		if !strings.Contains(body, expected) {
			t.Errorf("batch retry UI is missing %q", expected)
		}
	}
}

func TestAdminMessagesRenderFiltersDetailAndUserLink(t *testing.T) {
	t.Parallel()
	now := time.Now()
	item := store.AdminMessage{ID: "message-id", UserID: "user-id", Username: "alice", Kind: "admin_message", Title: "维护通知", Body: "今晚维护", Ref: "resource-id", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	list := httptest.NewRecorder()
	page := store.AdminMessagePage{Items: []store.AdminMessage{item}, Total: 1, Page: 1, PerPage: 25, Query: store.AdminMessageQuery{Kind: "admin_message", Read: "unread", User: "alice"}}
	if err := NewTemplates().Render(list, "admin_messages", map[string]any{"Title": "系统消息", "Page": page, "Items": page.Items, "Query": page.Query}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`name="kind"`, `name="read"`, `name="user"`, `/admin/messages/message-id`, `/admin/users/user-id`, "维护通知"} {
		if !strings.Contains(list.Body.String(), expected) {
			t.Errorf("message list is missing %q", expected)
		}
	}
	detail := httptest.NewRecorder()
	if err := NewTemplates().Render(detail, "admin_message_detail", map[string]any{"Title": item.Title, "Item": item}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"今晚维护", "/admin/users/user-id", "resource-id", "未读"} {
		if !strings.Contains(detail.Body.String(), expected) {
			t.Errorf("message detail is missing %q", expected)
		}
	}
}

func TestAdminDevicesPresentProductNamesBeforeCodenames(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	item := store.AdminDeviceItem{ID: "device-id", DisplayName: "Xiaomi Smart Band 8 Pro", Codename: "m67", Platform: "vela_os", AstroBoxID: "m67"}
	if err := NewTemplates().Render(recorder, "admin_devices", map[string]any{
		"Title": "设备目录", "Items": []store.AdminDeviceItem{item},
		"Page": store.AdminDevicePage{Items: []store.AdminDeviceItem{item}, Total: 1}, "Query": store.AdminDeviceQuery{},
	}); err != nil {
		t.Fatalf("render devices: %v", err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, ">Xiaomi Smart Band 8 Pro</a>") || !strings.Contains(body, "<code>m67</code>") {
		t.Fatalf("device row does not present product name with codename as secondary text: %s", body)
	}
}

func TestAdminNavigationGroupsModulesAndDisambiguatesClientStats(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	if err := NewTemplates().Render(recorder, "admin_dashboard", map[string]any{
		"Title": "概览", "Stats": model.Stats{}, "Events": []any{}, "Clients": []any{},
		"Role": RoleAdmin, "Nav": NavigationFor(RoleAdmin), "HomePath": HomePathFor(RoleAdmin),
	}); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"审核工作台", "内容与发布", "社区与用户", "内容运营", "系统与诊断", "客户端统计"} {
		if !strings.Contains(body, expected) {
			t.Errorf("admin navigation is missing %q", expected)
		}
	}
}

func TestTransitionPageIsSelfHostedAndEscapesJavaScriptTarget(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	err := NewTemplates().Render(recorder, "transition_page", TransitionPageData{
		Title:       "登录成功",
		Heading:     "登录成功",
		Description: "正在返回 OronBox",
		ButtonLabel: "打开 OronBox",
		Target:      template.URL(`oronbox://oauth?ticket=a"b&next=<script>`),
		Auto:        true,
		Tone:        "success",
	})
	if err != nil {
		t.Fatalf("render transition page: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`class="standalone-card transition-card success"`,
		`data-transition-target="oronbox://oauth?ticket=a&#34;b&amp;next=&lt;script&gt;"`,
		`<script src="/assets/transition.js" defer></script>`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("transition page is missing %q\n%s", expected, body)
		}
	}
	if strings.Contains(body, `next=<script>`) {
		t.Errorf("transition page reflected an unescaped target")
	}
}

// Resource links, release download URLs and publication pages all come out of
// the database, so an operator-visible href must never carry a scheme that
// executes on click.
func TestSafeURLDropsExecutableSchemes(t *testing.T) {
	t.Parallel()
	for _, dangerous := range []string{
		"javascript:alert(1)",
		"  JavaScript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
		"",
	} {
		if got := safeURL(dangerous); got != "" {
			t.Errorf("safeURL(%q) = %q, want it dropped", dangerous, got)
		}
	}
	for _, allowed := range []string{"https://example.com/a?b=c", "http://example.com"} {
		if got := safeURL(allowed); got != allowed {
			t.Errorf("safeURL(%q) = %q", allowed, got)
		}
	}
}

func TestDatabaseSourcedLinksAreFiltered(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	if err := NewTemplates().Render(recorder, "admin_release_detail", map[string]any{
		"Title": "客户端版本",
		"Item":  store.AdminReleaseItem{AppRelease: store.AppRelease{ID: "release-id", DownloadURL: "javascript:alert(1)"}},
	}); err != nil {
		t.Fatalf("render release detail: %v", err)
	}
	if body := recorder.Body.String(); strings.Contains(body, `href="javascript:`) {
		t.Errorf("release download URL reached an href unfiltered: %s", body)
	}
}

// The redirect target reaches location.replace() from a data attribute, so the
// script has to refuse schemes that would execute instead of navigate.
func TestTransitionScriptRefusesExecutableSchemes(t *testing.T) {
	t.Parallel()
	if !strings.Contains(TransitionJS, `/^\s*(javascript|data|vbscript):/i`) {
		t.Error("transition script does not screen the redirect target")
	}
}

func TestAdminResourceDetailExposesProtectedMediaAndArtifactActions(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	item := store.AdminResourceItem{
		ID: "resource-id", Name: "Resource", Slug: "resource", Owner: "author",
		Kind: "quickapp", ModerationState: "visible",
	}
	detail := store.AdminResourceDetail{Resource: item}
	err := NewTemplates().Render(recorder, "admin_resource_detail", map[string]any{
		"Title": "Resource", "Item": item, "Detail": detail,
		"Publications": []store.AdminPublication{},
		"Artifacts": []store.AdminArtifact{{
			SHA256: "artifact-sha", OriginalName: "example app.rpk",
			PackageFormat: "rpk", Devices: []string{"Xiaomi Smart Band 8 Pro"},
		}},
		"Media": []store.AdminMedia{{
			SHA256: "preview-sha", Role: "preview", Width: 320, Height: 320,
		}},
		"Snapshot": "{}",
	})
	if err != nil {
		t.Fatalf("render resource detail: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`src="/admin/blobs/preview-sha"`,
		`href="/admin/blobs/artifact-sha?download=1&amp;name=example&#43;app.rpk"`,
		`Xiaomi Smart Band 8 Pro`,
		`>下载</a>`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("resource detail is missing %q\n%s", expected, body)
		}
	}
}

func TestAdminReviewUsesDedicatedDetailDecisionLayout(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	err := NewTemplates().Render(recorder, "admin_review_detail", map[string]any{
		"Title": "资源审核", "Detail": store.AdminReviewDetail{
			Review: store.AdminReviewItem{ID: "review-id", State: "pending", ResourceID: "resource-id", Items: []string{"preview checked"}},
			Current: store.AdminReviewRevisionSnapshot{ID: "revision-id", Name: "NeoMusic", Attributes: []string{"original"},
				PublicationPlan: []any{map[string]any{"target": "bandbbs", "config": map[string]any{"agreement": true, "targets": []any{map[string]any{"category_id": 100, "prefix_id": 82, "package_id": "app.neo"}}}}},
				Media:           []store.AdminMedia{{ID: "media-id", SHA256: "preview-sha", Role: "preview", Width: 320, Height: 320}},
				Artifacts:       []store.AdminArtifact{{ID: "artifact-id", SHA256: "artifact-sha", OriginalName: "neo.rpk", PackageID: "app.neo", Version: "1.0", DeviceBindings: []store.AdminArtifactDevice{{ID: "device-id", DisplayName: "Band 9"}}}},
			},
		},
		"Attributes": []map[string]any{{"ID": "original", "NameZH": "原创"}},
		"Devices":    []store.AdminDeviceItem{{ID: "device-id", DisplayName: "Band 9", Codename: "mili"}},
	})
	if err != nil {
		t.Fatalf("render review: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`action="/admin/review/revision-id"`,
		`action="/admin/review/review-id/checklist"`,
		`保存检查进度`,
		`preview checked`,
		`class="review-decision-form"`,
		`修正提交内容`,
		`src="/admin/blobs/preview-sha"`,
		`href="/admin/blobs/artifact-sha?download=1&amp;name=neo.rpk"`,
		`保存设备绑定`,
		`米坛`,
		`分类 100`,
		`前缀 82`,
		`name="curation_grade"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("review page is missing %q", expected)
		}
	}
	if strings.Contains(body, "边审核边编辑") {
		t.Error("review must not send the reviewer to a separate editor")
	}
	if !strings.Contains(CSS, `input[type="checkbox"]`) {
		t.Error("admin CSS is missing the checkbox size reset")
	}
}

func TestAdminReviewListHasSafeBulkControlsAndKeepsReturnURL(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	err := NewTemplates().Render(recorder, "admin_review", map[string]any{
		"Title": "审核中心", "Items": []store.AdminReviewItem{{ID: "review-id", State: "pending", RevisionName: "Neo", Items: []string{"preview"}}},
		"Page": store.AdminReviewPage{Total: 1}, "Query": store.AdminReviewQuery{}, "ReturnTo": "/admin/review?q=neo&page=2",
		"Reviewers": []store.AdminReviewerOption{{ID: "reviewer-id", Username: "Alice", Role: "reviewer"}},
	})
	if err != nil {
		t.Fatalf("render review list: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`action="/admin/review/bulk"`, `name="review_ids"`, `value="assign"`, `value="/admin/review?q=neo&amp;page=2"`, `Alice（reviewer）`} {
		if !strings.Contains(body, expected) {
			t.Errorf("review list is missing %q", expected)
		}
	}
	if strings.Contains(body, "approve_safe") || strings.Contains(body, "安全通过所选") {
		t.Error("review list must not expose bulk approval")
	}
}

func TestAdminHomeUsesComposerInsteadOfRawDatabaseForms(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	err := NewTemplates().Render(recorder, "admin_home", map[string]any{
		"Title": "首页编排", "Banners": []store.HomeBanner{},
		"Sections": []store.HomeSection{}, "Cards": map[string][]store.HomeSectionCard{},
		"Posts": []store.BlogPost{}, "Resources": []store.AdminResourceItem{},
	})
	if err != nil {
		t.Fatalf("render home composer: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"首页编排", "最新动态", `class="home-composer"`, `data-target-form`, "+ 添加 Banner"} {
		if !strings.Contains(body, expected) {
			t.Errorf("home composer is missing %q", expected)
		}
	}
	if strings.Contains(body, "封面 SHA-256") || strings.Contains(body, "资源类型必填") {
		t.Error("home composer exposes raw storage fields")
	}
}

func TestAdminBlogUsesContentListAndWritingWorkspace(t *testing.T) {
	t.Parallel()
	templates := NewTemplates()
	list := httptest.NewRecorder()
	if err := templates.Render(list, "admin_blog", map[string]any{"Title": "Blog 管理", "Posts": []store.BlogPost{}}); err != nil {
		t.Fatalf("render blog list: %v", err)
	}
	if !strings.Contains(list.Body.String(), `class="blog-list"`) || !strings.Contains(list.Body.String(), `data-create-dialog`) {
		t.Error("blog list is missing the content-oriented layout")
	}

	editor := httptest.NewRecorder()
	if err := templates.Render(editor, "admin_blog_edit", map[string]any{"Title": "编辑文章", "Post": store.BlogPost{Slug: "hello", Title: "Hello"}}); err != nil {
		t.Fatalf("render blog editor: %v", err)
	}
	body := editor.Body.String()
	for _, expected := range []string{`class="blog-editor"`, `class="writing-grid"`, "上传封面", "保存草稿", "发布文章"} {
		if !strings.Contains(body, expected) {
			t.Errorf("blog editor is missing %q", expected)
		}
	}
	if strings.Contains(body, "封面 SHA-256") {
		t.Error("blog editor exposes the cover digest as a primary field")
	}
}

func TestAdminResourcesExposeGovernanceFiltersAndPreserveThemAcrossPages(t *testing.T) {
	t.Parallel()
	templates := NewTemplates()
	query := store.AdminResourceQuery{
		Search:            "music",
		Owner:             "creator",
		Kind:              "quickapp",
		Moderation:        "suspended",
		RevisionState:     "approved",
		ReviewState:       "approved",
		PublicationTarget: "astrobox",
		PublicationState:  "reviewing",
		Sort:              "owner",
		Page:              2,
		PerPage:           25,
	}
	item := store.AdminResourceItem{
		ID: "resource-id", Name: "Music", Slug: "music", Owner: "creator",
		Kind: "quickapp", ModerationState: "suspended",
		Publications: []store.AdminPublication{{Target: "astrobox", State: "reviewing"}},
	}
	page := store.AdminResourcePage{
		Items: []store.AdminResourceItem{item}, Total: 70, Page: 2,
		PerPage: 25, TotalPages: 3, Query: query,
	}
	recorder := httptest.NewRecorder()
	if err := templates.Render(recorder, "admin_resources", map[string]any{
		"Title": "资源", "Items": page.Items, "Page": page, "Query": query,
		"Pager": NewPagination("/admin/resources", url.Values{
			"q": {query.Search}, "owner": {query.Owner}, "kind": {query.Kind},
			"moderation": {query.Moderation}, "revision_state": {query.RevisionState},
			"review_state": {query.ReviewState}, "target": {query.PublicationTarget},
			"publication_state": {query.PublicationState}, "sort": {query.Sort},
		}, page.Page, page.PerPage, page.Total),
		"PageSizes": []int{25, 50, 100},
	}); err != nil {
		t.Fatalf("render resources: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`name="revision_state"`,
		`name="review_state"`,
		`revision_state=approved`,
		`review_state=approved`,
		`target=astrobox`,
		`publication_state=reviewing`,
		`title="AstroBox · 外部审核"`,
		`href="#admin-content"`,
		`id="admin-content"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("resource page is missing %q", expected)
		}
	}
}

func TestAdminReportDetailSeparatesPublicReplyAndInternalNote(t *testing.T) {
	t.Parallel()
	templates := NewTemplates()
	ticket := store.FeedbackTicket{
		ID: "report-id", Kind: "resource_report", Subject: "侵权内容", Message: "举报正文",
		TargetSource: "oronbox", TargetID: "resource-id", Status: "investigating",
		Username: "reporter",
		Replies:  []store.FeedbackReply{{Author: "moderator", Message: "正在核查"}},
	}
	resource := store.AdminResourceDetail{Resource: store.AdminResourceItem{
		ID: "resource-id", Name: "Resource", Owner: "creator", ModerationState: "visible",
	}}
	recorder := httptest.NewRecorder()
	if err := templates.Render(recorder, "admin_report_detail", map[string]any{
		"Title": ticket.Subject, "Ticket": ticket, "IsReport": true, "BackURL": "/admin/reports?status=open", "ReturnTo": "/admin/reports?status=open",
		"Detail": store.AdminFeedbackDetail{Ticket: ticket, TargetSnapshot: store.FeedbackTargetSnapshot{Kind: "resource", ID: "resource-id", Title: resource.Resource.Name, Owner: resource.Resource.Owner, State: "visible"}, InternalNotes: []store.FeedbackInternalNote{{Author: "reviewer", Message: "仅内部可见"}}, StatusHistory: []store.FeedbackStatusEvent{{Actor: "moderator", FromStatus: "open", ToStatus: "investigating"}}},
	}); err != nil {
		t.Fatalf("render report: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`action="/admin/reports/report-id"`,
		`name="message"`,
		`name="internal_note"`,
		`name="action" value="status"`,
		`href="/admin/reports?status=open"`,
		`Resource`,
		`正在核查`,
		`仅内部可见`,
		`待处理 → 处理中`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("report detail is missing %q", expected)
		}
	}
	if strings.Count(body, `name="message"`) != 1 {
		t.Error("report detail must expose exactly one user-visible reply field")
	}
}

func TestAdminShellProvidesAccessibleInteractionPrimitives(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	if err := NewTemplates().Render(recorder, "admin_dashboard", map[string]any{
		"Title": "概览", "Stats": model.Stats{}, "Events": []any{}, "Clients": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`class="skip-link" href="#admin-content"`,
		`id="admin-content" tabindex="-1"`,
		`id="admin-live-region" role="status" aria-live="polite"`,
		`aria-labelledby="confirm-dialog-title"`,
		`aria-describedby="confirm-dialog-description"`,
		`<script src="/assets/admin.js" defer></script>`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("admin shell is missing accessibility primitive %q", expected)
		}
	}
	for _, expected := range []string{
		`confirmAction?.focus()`,
		`dialogTrigger?.focus()`,
		`event.key === 'Escape'`,
		`form.dataset.submitting === 'true'`,
		`className = 'form-error-summary'`,
		`cell.dataset.label = labels[index]`,
	} {
		if !strings.Contains(AdminJS, expected) {
			t.Errorf("admin script is missing accessibility primitive %q", expected)
		}
	}
	if strings.Contains(AdminJS, "alert(") || strings.Contains(AdminJS, "confirm(") {
		t.Error("admin shell must not use native alert/confirm dialogs")
	}
}

// The console runs under a Content-Security-Policy without 'unsafe-inline', so
// a rendered page that carries executable script would silently stop working.
func TestRenderedPagesCarryNoInlineScript(t *testing.T) {
	t.Parallel()
	inlineScript := regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>[^<]`)
	for name, data := range renderableTemplateCases() {
		recorder := httptest.NewRecorder()
		if err := NewTemplates().Render(recorder, name, data); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if match := inlineScript.FindString(recorder.Body.String()); match != "" {
			t.Errorf("%s carries an inline script near %q", name, match)
		}
	}
}

func TestAdminCSSSupportsResponsiveAccessiblePresentation(t *testing.T) {
	t.Parallel()
	for _, expected := range []string{
		// Dark is the baseline palette, so light is what has to be opted into
		// either explicitly or by the system preference.
		`:root.light-theme`,
		`@media (prefers-color-scheme: light)`,
		`:root:not(.dark-theme):not(.light-theme)`,
		`@media (prefers-reduced-motion: reduce)`,
		`@media (forced-colors: active)`,
		`.table-wrap tbody td[data-label]::before`,
		`content: attr(data-label)`,
		`.form-error-summary`,
		`[aria-invalid="true"]`,
		`button:focus-visible`,
	} {
		if !strings.Contains(CSS, expected) {
			t.Errorf("admin CSS is missing %q", expected)
		}
	}
}

func TestAdminRevisionManagementActionsRender(t *testing.T) {
	t.Parallel()
	templates := NewTemplates()
	resource := store.AdminResourceItem{ID: "resource-id", Name: "Resource"}
	revision := store.AdminRevision{ID: "revision-id", Number: 3, Name: "Historical", State: "approved", CreatedVia: "creator", PublicationPlan: []byte("[]")}
	detail := store.AdminRevisionDetail{Resource: resource, Revision: revision}
	recorder := httptest.NewRecorder()
	if err := templates.Render(recorder, "admin_revision_detail", map[string]any{"Title": "Historical", "Detail": detail}); err != nil {
		t.Fatal(err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `action="/admin/resources/resource-id/revisions/revision-id/rollback"`) || !strings.Contains(body, "创建回滚管理修订") {
		t.Fatalf("historical revision is missing explicit rollback action: %s", body)
	}

	revision.State, revision.CreatedVia = "draft", "admin"
	detail.Revision = revision
	detail.Media = []store.AdminMedia{{ID: "media-id", Role: "preview", Position: 0, SHA256: strings.Repeat("a", 64), Width: 10, Height: 10}}
	recorder = httptest.NewRecorder()
	if err := templates.Render(recorder, "admin_revision_editor", map[string]any{"Title": "Draft", "Detail": detail, "IsDraft": true, "CanEditAssets": true}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`action="/admin/resources/resource-id/draft/revision-id/discard"`,
		`action="/admin/resources/resource-id/draft/revision-id/media/media-id/move"`,
		`name="position" min="0" value="0"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("admin draft editor is missing %q", expected)
		}
	}
}

func TestBlogCreateDialogHasAccessibleNameAndFocusLifecycle(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	if err := NewTemplates().Render(recorder, "admin_blog", map[string]any{
		"Title": "Blog 管理", "Posts": []store.BlogPost{},
	}); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`aria-labelledby="create-blog-title"`,
		`aria-describedby="create-blog-description"`,
		`aria-label="关闭新建文章对话框"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("blog dialog is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`dialog.querySelector('input, select, button').focus()`,
		`dialog.addEventListener('close'`,
		`trigger.focus()`,
	} {
		if !strings.Contains(AdminJS, expected) {
			t.Errorf("blog dialog script is missing %q", expected)
		}
	}
}

// Every rendered POST form must carry the session token; the hidden field is
// injected at parse time precisely so no form can be forgotten.
func TestEveryPostFormCarriesCSRFField(t *testing.T) {
	t.Parallel()
	openingForm := regexp.MustCompile(`(?i)<form[^>]*method="post"[^>]*>`)
	for name, data := range renderableTemplateCases() {
		values, ok := data.(map[string]any)
		if !ok {
			continue
		}
		values["CSRFToken"] = "token-value"
		recorder := httptest.NewRecorder()
		if err := NewTemplates().Render(recorder, name, values); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		body := recorder.Body.String()
		forms := openingForm.FindAllStringIndex(body, -1)
		for _, form := range forms {
			field := `<input type="hidden" name="csrf_token" value="token-value">`
			if !strings.HasPrefix(body[form[1]:], field) {
				t.Errorf("%s has a POST form without the CSRF field: %s", name, body[form[0]:form[1]])
			}
		}
		if name == "admin_dashboard" && len(forms) == 0 {
			t.Error("admin shell no longer renders the logout form, the assertion above is vacuous")
		}
	}
}
