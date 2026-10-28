package astrobox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type Timeline struct {
	Items       []map[string]any `json:"items"`
	State       string           `json:"review_state"`
	ReviewItems []ReviewItem     `json:"review_items"`
}

// LoadTimeline keeps GitHub review IDs separate from editable issue comments.
func LoadTimeline(issuePath, login string, fetch func(string) ([]byte, error)) (Timeline, error) {
	all := make([]map[string]any, 0)
	for _, source := range []struct{ path, kind string }{{issuePath + "/comments", "comment"}, {strings.Replace(issuePath, "/issues/", "/pulls/", 1) + "/reviews", "review"}} {
		for page := 1; ; page++ {
			if page > 100 {
				return Timeline{}, fmt.Errorf("AstroBox review timeline is too large")
			}
			data, err := fetch(fmt.Sprintf("%s?per_page=100&page=%d", source.path, page))
			if err != nil {
				return Timeline{}, err
			}
			var entries []map[string]any
			if err := json.Unmarshal(data, &entries); err != nil {
				return Timeline{}, fmt.Errorf("decode GitHub %s: %w", source.kind, err)
			}
			for _, item := range entries {
				if item["state"] == "PENDING" {
					continue
				}
				user, _ := item["user"].(map[string]any)
				author, _ := user["login"].(string)
				item["author"], item["user"], item["is_author"], item["type"] = author, author, strings.EqualFold(author, login), source.kind
				if source.kind == "review" {
					item["created_at"] = item["submitted_at"]
					item["review_state"] = item["state"]
				}
				all = append(all, item)
			}
			if len(entries) < 100 {
				break
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return fmt.Sprint(all[i]["created_at"]) < fmt.Sprint(all[j]["created_at"]) })
	var bodies []string
	for _, item := range all {
		association, _ := item["author_association"].(string)
		if item["is_author"] == true || association == "OWNER" || association == "MEMBER" || association == "COLLABORATOR" {
			body, _ := item["body"].(string)
			bodies = append(bodies, body)
		}
	}
	state, items := DeriveReviewStatus(bodies)
	return Timeline{Items: all, State: state, ReviewItems: items}, nil
}

func (c *Client) ReviewTimeline(ctx context.Context, token string, number int) (Timeline, error) {
	login, err := c.currentUser(ctx, token)
	if err != nil {
		return Timeline{}, err
	}
	return LoadTimeline(fmt.Sprintf("/repos/%s/%s/issues/%d", c.cfg.RepoOwner, c.cfg.RepoName, number), login, func(endpoint string) ([]byte, error) {
		var raw json.RawMessage
		_, err := c.request(ctx, token, http.MethodGet, c.api+endpoint, nil, &raw)
		return raw, err
	})
}
