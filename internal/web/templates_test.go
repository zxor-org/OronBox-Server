package web

import (
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminTemplatesRender(t *testing.T) {
	templates := NewTemplates()
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
	feedbackPage := store.FeedbackPage{
		Items: []store.FeedbackTicket{}, Total: 0, Page: 1, PerPage: 25,
		TotalPages: 0, Query: store.AdminFeedbackQuery{Page: 1, PerPage: 25},
	}
	cases := map[string]any{
		"admin_login":     map[string]any{"Title": "t", "AuthorizeURL": "/login"},
		"admin_dashboard": map[string]any{"Title": "t", "Stats": model.Stats{}, "Events": []any{}, "Clients": []any{}},
		"admin_events":    map[string]any{"Title": "t", "Events": []any{}},
		"admin_states":    map[string]any{"Title": "t", "States": []any{}},
		"admin_tickets":   map[string]any{"Title": "t", "Tickets": []any{}},
		"admin_clients":   map[string]any{"Title": "t", "Clients": []any{}},
		"admin_settings":  map[string]any{"Title": "t", "Config": config, "BandBBSSecretState": "已配置", "GitHubSecretState": "已配置", "Announcements": []store.Announcement{}},
		"admin_health":    map[string]any{"Title": "t", "DBStatus": "ok", "Stats": model.Stats{}},
		"admin_audit":     map[string]any{"Title": "t", "Logs": []any{}},
		"admin_review":    map[string]any{"Title": "t", "Items": []any{}},
		"admin_comments":  map[string]any{"Title": "t", "Items": []store.AdminCommentItem{}, "Total": 0, "Page": 1, "Prompt": "prompt"},
		"admin_releases":  map[string]any{"Title": "t", "Items": []store.AppRelease{}},
		"admin_resources": map[string]any{"Title": "t", "Items": resourcePage.Items, "Page": resourcePage, "Query": resourcePage.Query},
		"admin_resource_detail": map[string]any{
			"Title": "t", "Item": resourceItem, "Detail": resourceDetail,
			"Publications": []store.AdminPublication{}, "Artifacts": []store.AdminArtifact{},
			"Media": []store.AdminMedia{}, "Snapshot": "{}",
		},
		"admin_reports":  map[string]any{"Title": "t", "Items": feedbackPage.Items, "Page": feedbackPage, "Query": feedbackPage.Query},
		"admin_feedback": map[string]any{"Title": "t", "Items": feedbackPage.Items, "Page": feedbackPage, "Query": feedbackPage.Query},
		"admin_report_detail": map[string]any{
			"Title": "t", "Item": store.FeedbackTicket{}, "Ticket": store.FeedbackTicket{},
		},
		"server_home": map[string]any{"Title": "Server"},
		"transition_page": TransitionPageData{
			Title: "登录成功", Heading: "登录成功", Description: "正在返回 OronBox",
			ButtonLabel: "打开 OronBox", Target: template.URL("oronbox://oauth?ticket=test"),
			Auto: true, Tone: "success",
		},
	}
	for name, data := range cases {
		recorder := httptest.NewRecorder()
		if err := templates.Render(recorder, name, data); err != nil {
			t.Errorf("render %s: %v", name, err)
		}
	}
}

func TestTransitionPageUsesLoliFontsAndEscapesJavaScriptTarget(t *testing.T) {
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
		`https://fonts.loli.net/css2`,
		`https://gstatic.loli.net`,
		`class="standalone-card transition-card success"`,
		`location.replace("oronbox://oauth?ticket=a\"b\u0026next=\u003cscript\u003e")`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("transition page is missing %q\n%s", expected, body)
		}
	}
	if strings.Contains(body, `fonts.googleapis.com`) || strings.Contains(body, `<script>`) && strings.Contains(body, `next=<script>`) {
		t.Errorf("transition page contains an unsafe or disallowed asset value")
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
			PackageFormat: "rpk",
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
		`>下载</a>`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("resource detail is missing %q\n%s", expected, body)
		}
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

func TestAdminReportDetailUsesOneVisibleReplyField(t *testing.T) {
	t.Parallel()
	templates := NewTemplates()
	ticket := store.FeedbackTicket{
		ID: "report-id", Kind: "report", Subject: "侵权内容", Message: "举报正文",
		TargetSource: "oronbox", TargetID: "resource-id", Status: "investigating",
		Username: "reporter",
		Replies:  []store.FeedbackReply{{Author: "moderator", Message: "正在核查"}},
	}
	resource := store.AdminResourceDetail{Resource: store.AdminResourceItem{
		ID: "resource-id", Name: "Resource", Owner: "creator", ModerationState: "visible",
	}}
	recorder := httptest.NewRecorder()
	if err := templates.Render(recorder, "admin_report_detail", map[string]any{
		"Title": ticket.Subject, "Ticket": ticket, "Resource": &resource,
	}); err != nil {
		t.Fatalf("render report: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`action="/admin/reports/report-id"`,
		`name="message"`,
		`value="investigating" selected`,
		`href="/admin/resources/resource-id"`,
		`正在核查`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("report detail is missing %q", expected)
		}
	}
	if strings.Contains(body, `name="resolution"`) {
		t.Error("report detail exposes a second resolution field")
	}
}
