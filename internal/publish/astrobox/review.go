package astrobox

import (
	"regexp"
	"strings"
)

var abccTagRe = regexp.MustCompile(`(?is)^\s*\[ABCC_(NEEDFIX|FIXED|CLOSE|REOPEN|REFUSE)(?:_([^\]]+))?\]\s*([\s\S]*)$`)

type ReviewItem struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Fixed   bool   `json:"fixed"`
}

// DeriveReviewStatus replicates ABCC's algorithm: NEEDFIX opens an item, FIXED
// resolves it (only when a NEEDFIX exists). No NEEDFIX → waiting_review.
func DeriveReviewStatus(bodies []string) (state string, items []ReviewItem) {
	need := map[string]string{}
	fixed := map[string]bool{}
	var order []string
	for _, body := range bodies {
		m := abccTagRe.FindStringSubmatch(strings.TrimSpace(body))
		if m == nil {
			continue
		}
		kind := strings.ToUpper(m[1])
		id := strings.TrimSpace(m[2])
		msg := strings.TrimSpace(m[3])
		switch kind {
		case "NEEDFIX":
			if _, ok := need[id]; !ok {
				order = append(order, id)
			}
			need[id] = msg
			delete(fixed, id)
		case "FIXED":
			if _, ok := need[id]; ok {
				fixed[id] = true
			}
		}
	}
	if len(need) == 0 {
		return "waiting_review", nil
	}
	items = make([]ReviewItem, 0, len(need))
	unresolved := false
	for _, id := range order {
		it := ReviewItem{ID: id, Message: need[id], Fixed: fixed[id]}
		if !it.Fixed {
			unresolved = true
		}
		items = append(items, it)
	}
	if unresolved {
		return "changes_requested", items
	}
	return "fixed_waiting", items
}
