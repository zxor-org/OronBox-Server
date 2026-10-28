package astrobox

import (
	"strings"
	"testing"
)

func TestTimelineCombinesReviewsAndCommentsWithoutIDCollision(t *testing.T) {
	timeline, err := LoadTimeline("/repos/official/catalog/issues/4", "creator", func(path string) ([]byte, error) {
		if strings.Contains(path, "/reviews?") {
			return []byte(`[{"id":7,"body":"[ABCC_NEEDFIX_icon] Fix icon","state":"CHANGES_REQUESTED","submitted_at":"2026-10-01T01:00:00Z","author_association":"MEMBER","user":{"login":"reviewer"}},{"id":8,"state":"PENDING"}]`), nil
		}
		return []byte(`[{"id":7,"body":"[ABCC_FIXED_icon] Done","created_at":"2026-10-01T02:00:00Z","user":{"login":"creator"}}]`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.State != "fixed_waiting" || len(timeline.Items) != 2 || len(timeline.ReviewItems) != 1 || !timeline.ReviewItems[0].Fixed {
		t.Fatalf("timeline: %#v", timeline)
	}
	if timeline.Items[0]["type"] != "review" || timeline.Items[1]["type"] != "comment" {
		t.Fatal(timeline.Items)
	}
}
