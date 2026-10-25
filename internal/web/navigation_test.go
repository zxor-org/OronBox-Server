package web

import (
	"strings"
	"testing"
)

func TestReviewerNavigationHidesAdminOnlyDestinations(t *testing.T) {
	t.Parallel()
	reviewer := NavigationFor("reviewer")
	for _, forbidden := range []string{"/console/users", "/console/settings", "/console/coins", "/console/blog", "/console/announcements"} {
		if navigationContains(reviewer, forbidden) {
			t.Errorf("the reviewer drawer still links to %s", forbidden)
		}
	}
	for _, expected := range []string{"/console/reviews", "/console/comments", "/console/feedback"} {
		if !navigationContains(reviewer, expected) {
			t.Errorf("the reviewer drawer is missing %s", expected)
		}
	}
}

func TestAdminNavigationKeepsEveryDestination(t *testing.T) {
	t.Parallel()
	admin, reviewer := NavigationFor(RoleAdmin), NavigationFor("reviewer")
	count := func(groups []NavGroup) int {
		total := 0
		for _, group := range groups {
			total += len(group.Items)
		}
		return total
	}
	if count(admin) <= count(reviewer) {
		t.Fatalf("the admin drawer should be the larger one, got %d admin and %d reviewer items", count(admin), count(reviewer))
	}
	for _, group := range navigation {
		for _, item := range group.Items {
			if !navigationContains(admin, item.Path) {
				t.Errorf("the admin drawer dropped %s", item.Path)
			}
		}
	}
}

func TestReviewersLandOnTheQueueAndAdminsOnTheDashboard(t *testing.T) {
	t.Parallel()
	if got := HomePathFor("reviewer"); got != "/console/reviews" {
		t.Errorf("a reviewer should start at the queue, got %q", got)
	}
	if got := HomePathFor(RoleAdmin); got != "/console" {
		t.Errorf("an admin should start at the dashboard, got %q", got)
	}
	// An unrecognised role must be treated as the least privileged one rather
	// than falling through to the admin surfaces.
	if got := HomePathFor(""); got != "/console/reviews" {
		t.Errorf("an unknown role should be treated as a reviewer, got %q", got)
	}
	if navigationContains(NavigationFor("something-new"), "/console/users") {
		t.Error("an unknown role was given an admin-only destination")
	}
}

func TestNavigationHasNoEmptyGroupsOrDuplicatePaths(t *testing.T) {
	t.Parallel()
	for _, role := range []string{RoleAdmin, "reviewer"} {
		seen := map[string]bool{}
		for _, group := range NavigationFor(role) {
			if len(group.Items) == 0 {
				t.Errorf("%s: group %q renders a label with nothing under it", role, group.Label)
			}
			for _, item := range group.Items {
				if seen[item.Path] {
					t.Errorf("%s: %s appears twice in the drawer", role, item.Path)
				}
				seen[item.Path] = true
			}
		}
	}
}

func TestEveryNavigationIconExistsInTheSprite(t *testing.T) {
	t.Parallel()
	for _, group := range navigation {
		for _, item := range group.Items {
			if !strings.Contains(iconSprite, `id="i-`+item.Icon+`"`) {
				t.Errorf("%s uses the icon %q which is not in the sprite", item.Path, item.Icon)
			}
		}
	}
}

func navigationContains(groups []NavGroup, path string) bool {
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Path == path {
				return true
			}
		}
	}
	return false
}
