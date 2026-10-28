package astrobox

import (
	"context"
	"fmt"
	"net/http"
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

type reviewFix struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

func (c *Client) replyToFixes(ctx context.Context, token string, number int, fixes []reviewFix) error {
	if len(fixes) == 0 {
		return nil
	}
	timeline, err := c.ReviewTimeline(ctx, token, number)
	if err != nil {
		return err
	}
	handled := map[string]bool{}
	for _, fix := range fixes {
		if handled[fix.ID] {
			continue
		}
		for _, item := range timeline.ReviewItems {
			if item.ID != fix.ID || item.Fixed {
				continue
			}
			body := fmt.Sprintf("[ABCC_FIXED_%s] %s\n\n> %s", item.ID, strings.TrimSpace(fix.Note), strings.ReplaceAll(item.Message, "\n", "\n> "))
			_, err = c.request(ctx, token, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.api, c.cfg.RepoOwner, c.cfg.RepoName, number), map[string]string{"body": body}, nil)
			if err != nil {
				return fmt.Errorf("reply to AstroBox requested change: %w", err)
			}
			handled[fix.ID] = true
		}
	}
	return nil
}
