package astrobox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestFixRepliesPreserveContextAndDoNotRepeatOnRetry(t *testing.T) {
	posted := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"login":"creator"}`)) })
	mux.HandleFunc("GET /repos/official/catalog/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"state":"CHANGES_REQUESTED","body":"[ABCC_NEEDFIX_icon] Original reason","submitted_at":"2026-10-01T00:00:00Z","author_association":"MEMBER","user":{"login":"reviewer"}}]`))
	})
	mux.HandleFunc("GET /repos/official/catalog/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		if posted == 0 {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":2,"body":"[ABCC_FIXED_icon] Done","created_at":"2026-10-01T01:00:00Z","user":{"login":"creator"}}]`))
	})
	mux.HandleFunc("POST /repos/official/catalog/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body["body"], "[ABCC_FIXED_icon] Done") || !strings.Contains(body["body"], "> Original reason") {
			t.Error(body)
		}
		posted++
		w.WriteHeader(http.StatusCreated)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &Client{api: server.URL, http: server.Client()}
	client.cfg.RepoOwner = "official"
	client.cfg.RepoName = "catalog"
	for range 2 {
		if err := client.replyToFixes(context.Background(), "test", 42, []reviewFix{{ID: "icon", Note: "Done"}, {ID: "icon", Note: "Done"}}); err != nil {
			t.Fatal(err)
		}
	}
	if posted != 1 {
		t.Fatalf("posted %d replies", posted)
	}
}
