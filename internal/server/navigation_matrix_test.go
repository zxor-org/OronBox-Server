package server

import (
	"os"
	"regexp"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

// The drawer and the route table are two separate declarations of the same
// policy. If they drift, a reviewer either sees a link that answers with a 403
// or, worse, an admin-only page quietly becomes reachable from the menu. This
// test reads the route registrations and holds the drawer to them.
func TestNavigationRolesMatchTheRouteTable(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	route := regexp.MustCompile(`mux\.HandleFunc\("GET (/admin[^"]*)", a\.(requireAdminRole\("admin"|requireAdmin)`)
	required := map[string]string{}
	for _, match := range route.FindAllStringSubmatch(string(source), -1) {
		if match[2] == `requireAdminRole("admin"` {
			required[match[1]] = web.RoleAdmin
			continue
		}
		required[match[1]] = ""
	}
	if len(required) == 0 {
		t.Fatal("no admin GET routes were found, the pattern no longer matches")
	}

	for _, group := range web.NavigationFor(web.RoleAdmin) {
		for _, item := range group.Items {
			role, registered := required[item.Path]
			if !registered {
				t.Errorf("the drawer links to %s, which is not a registered admin GET route", item.Path)
				continue
			}
			if declared := navigationRoleFor(item.Path); declared != role {
				t.Errorf("%s is registered for role %q but the drawer declares %q", item.Path, role, declared)
			}
		}
	}
}

// TestReviewerDrawerOnlyContainsRoutesAReviewerCanOpen is the same guarantee
// stated from the reviewer's side, so a mistake in either direction fails.
func TestReviewerDrawerOnlyContainsRoutesAReviewerCanOpen(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	adminOnly := regexp.MustCompile(`mux\.HandleFunc\("GET (/admin[^"]*)", a\.requireAdminRole\("admin"`)
	restricted := map[string]bool{}
	for _, match := range adminOnly.FindAllStringSubmatch(string(source), -1) {
		restricted[match[1]] = true
	}
	for _, group := range web.NavigationFor("reviewer") {
		for _, item := range group.Items {
			if restricted[item.Path] {
				t.Errorf("the reviewer drawer offers %s, which requires the admin role", item.Path)
			}
		}
	}
}

func navigationRoleFor(path string) string {
	for _, group := range web.NavigationFor(web.RoleAdmin) {
		for _, item := range group.Items {
			if item.Path == path {
				return item.Role
			}
		}
	}
	return ""
}
