package bandbbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// A plan that fills only one side of the title/notes pair used to sink the
// publication. The client now defaults the missing side from the revision
// metadata, so the version update is still well-formed.
func TestPublishDefaultsMissingVersionTitleOrNotes(t *testing.T) {
	t.Parallel()
	var updateForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/resources/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"resource":{"resource_id":7568,"view_url":"https://www.bandbbs.cn/resources/7568/"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/resource-updates/":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			updateForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"update":{"resource_update_id":4242}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	price := 100.0
	var snap snapshot
	snap.Revision.Name = "高中学习盒子"
	snap.Revision.Summary = "更新说明应该从这里补齐"
	snap.Revision.PurchaseLink = "https://ifdian.net/item/example"
	snap.Revision.PurchasePrice = &price
	snap.Revision.PurchaseCurrency = "CNY"
	rawSnapshot, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}

	// Only the title is set: the notes fall back to the revision summary.
	rawConfig, err := json.Marshal(config{
		Agreement:      true,
		Targets:        []target{{CategoryID: 95}},
		VersionTitle:   "更新内容",
		VersionMessage: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := New(server.URL, nil, nil)
	result, err := client.Publish(context.Background(), "token", nil, rawSnapshot, rawConfig)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resources["95"].UpdateID != "4242" {
		t.Fatalf("update was not created: %#v", result)
	}
	if got := updateForm.Get("title"); got != "更新内容" {
		t.Fatalf("update title = %q, want the configured title", got)
	}
	if got := updateForm.Get("message"); got != snap.Revision.Summary {
		t.Fatalf("update message = %q, want the revision summary", got)
	}

	// The other direction: only notes configured, title defaults to the name.
	rawConfig, err = json.Marshal(config{
		Agreement:      true,
		Targets:        []target{{CategoryID: 95}},
		VersionMessage: "修复了闪退",
	})
	if err != nil {
		t.Fatal(err)
	}
	updateForm = nil
	result, err = client.Publish(context.Background(), "token", nil, rawSnapshot, rawConfig)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resources["95"].UpdateID != "4242" {
		t.Fatalf("update was not created: %#v", result)
	}
	if got := updateForm.Get("title"); got != snap.Revision.Name {
		t.Fatalf("update title = %q, want the revision name", got)
	}
	if got := updateForm.Get("message"); got != "修复了闪退" {
		t.Fatalf("update message = %q, want the configured notes", got)
	}

	// Both empty still means no update at all.
	rawConfig, err = json.Marshal(config{Agreement: true, Targets: []target{{CategoryID: 95}}})
	if err != nil {
		t.Fatal(err)
	}
	updateForm = nil
	result, err = client.Publish(context.Background(), "token", nil, rawSnapshot, rawConfig)
	if err != nil {
		t.Fatal(err)
	}
	if updateForm != nil {
		t.Fatalf("no update was expected, but one was posted: %#v", updateForm)
	}
}
