package astrobox

import "testing"

func TestReviewStatusTracksReopenedFindingsInOrder(t *testing.T) {
	bodies := []string{"[ABCC_FIXED_before] premature", "[ABCC_NEEDFIX_icons] wrong icon", "[ABCC_NEEDFIX_package] bad binary", "[ABCC_FIXED_icons] corrected"}
	state, items := DeriveReviewStatus(bodies)
	if state != "changes_requested" || len(items) != 2 || !items[0].Fixed || items[1].Fixed {
		t.Fatal(state, items)
	}
	bodies = append(bodies, "[ABCC_FIXED_package] corrected")
	state, _ = DeriveReviewStatus(bodies)
	if state != "fixed_waiting" {
		t.Fatal(state)
	}
	bodies = append(bodies, "[ABCC_NEEDFIX_icons] still incorrect")
	state, items = DeriveReviewStatus(bodies)
	if state != "changes_requested" || items[0].Fixed || items[0].Message != "still incorrect" {
		t.Fatal(state, items)
	}
}
