package syndication

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// githubMock is a small GitHub API stub covering the endpoints the AB client uses.
func githubMock(t *testing.T, opts ...func(*githubMockState)) (*httptest.Server, *githubMockState) {
	st := &githubMockState{}
	for _, o := range opts {
		o(st)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case r.Method == "POST" && p == "/user/repos":
			if st.existing {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "r", "default_branch": "main", "owner": map[string]string{"login": "creator"}})
		case r.Method == "GET" && p == "/user":
			_, _ = w.Write([]byte(`{"login":"creator"}`))
		case r.Method == "GET" && strings.HasPrefix(p, "/repos/creator/r"):
			_, _ = w.Write([]byte(`{"name":"r","default_branch":"main","owner":{"login":"creator"}}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/forks"):
			_, _ = w.Write([]byte(`{"name":"r","default_branch":"main","owner":{"login":"creator"}}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/merge-upstream"):
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && strings.Contains(p, "/git/ref/heads/"):
			if st.emptyRepo {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"object":{"sha":"parent"}}`))
		case r.Method == "GET" && strings.Contains(p, "/git/commits/"):
			_, _ = w.Write([]byte(`{"tree":{"sha":"basetree"}}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/git/blobs"):
			_, _ = w.Write([]byte(`{"sha":"blob"}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/git/trees"):
			_, _ = w.Write([]byte(`{"sha":"tree"}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/git/commits"):
			_, _ = w.Write([]byte(`{"sha":"commit"}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/git/refs"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "PATCH" && strings.Contains(p, "/git/refs/heads/"):
			n := atomic.AddInt32(&st.patches, 1)
			if st.rejectNonForce && n == 1 && !strings.Contains(readBodyStr(r), `"force":true`) {
				w.WriteHeader(http.StatusConflict)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "GET" && strings.Contains(p, "/contents/"):
			_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte("hello")) + `","encoding":"base64"}`))
		case r.Method == "POST" && strings.HasSuffix(p, "/pulls"):
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/x/y/pull/7"}`))
		case r.Method == "PUT" && strings.Contains(p, "/pulls/") && strings.HasSuffix(p, "/merge"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "GET" && strings.HasSuffix(p, "/pulls/7"):
			_, _ = w.Write([]byte(`{"state":"open","merged":false,"html_url":"https://github.com/x/y/pull/7"}`))
		case r.Method == "GET" && strings.HasSuffix(p, "/comments"):
			_, _ = w.Write([]byte(`[{"body":"[ABCC_NEEDFIX_a] fix","created_at":"2026-01-01","user":{"login":"rev"}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, st
}

type githubMockState struct {
	existing       bool
	emptyRepo      bool
	rejectNonForce bool
	patches        int32
}

func readBodyStr(r *http.Request) string {
	buf := make([]byte, 1024)
	n, _ := r.Body.Read(buf)
	return string(buf[:n])
}

func TestGitHubCommitFilesTree(t *testing.T) {
	srv, _ := githubMock(t)
	defer srv.Close()
	c := GitHubClient{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	sha, err := c.CommitFilesTree(context.Background(), "o", "r", "main", "msg", map[string][]byte{"a.txt": []byte("x"), "b.txt": []byte("y")})
	if err != nil || sha != "commit" {
		t.Fatalf("sha=%s err=%v", sha, err)
	}
	sha, err = c.CommitFilesTreeBase(context.Background(), "o", "r", "branch", "msg", "basetree", map[string][]byte{"a.txt": []byte("x")})
	if err != nil || sha != "commit" {
		t.Fatalf("base sha=%s err=%v", sha, err)
	}
}

func TestGitHubEmptyRepoCreatesRef(t *testing.T) {
	srv, _ := githubMock(t, func(s *githubMockState) { s.emptyRepo = true })
	defer srv.Close()
	c := GitHubClient{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	if _, err := c.CommitFilesTree(context.Background(), "o", "r", "main", "init", map[string][]byte{"a": []byte("x")}); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubUpdateRefFallbacks(t *testing.T) {
	srv, _ := githubMock(t, func(s *githubMockState) { s.rejectNonForce = true })
	defer srv.Close()
	c := GitHubClient{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	if err := c.UpdateRef(context.Background(), "o", "r", "main", "sha1"); err != nil {
		t.Fatalf("force fallback failed: %v", err)
	}

	// 404 → creates the ref.
	srv2, _ := githubMock(t, func(s *githubMockState) { s.emptyRepo = true })
	defer srv2.Close()
	c2 := GitHubClient{BaseURL: srv2.URL, Token: "t", HTTP: srv2.Client()}
	if err := c2.UpdateRef(context.Background(), "o", "r", "main", "sha2"); err != nil {
		t.Fatalf("create ref fallback failed: %v", err)
	}
}

func TestGitHubRepoAndPRHelpers(t *testing.T) {
	srv, _ := githubMock(t)
	defer srv.Close()
	c := GitHubClient{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	ctx := context.Background()

	if owner, repo, branch, err := c.EnsureUserRepo(ctx, "r", "desc"); err != nil || owner != "creator" || repo != "r" || branch != "main" {
		t.Fatalf("ensure repo %s/%s@%s err=%v", owner, repo, branch, err)
	}
	if login, err := c.CurrentUser(ctx); err != nil || login != "creator" {
		t.Fatalf("current user %q err=%v", login, err)
	}
	if o, r, b, err := c.ForkRepo(ctx, "up", "repo"); err != nil || o != "creator" || r != "r" || b != "main" {
		t.Fatalf("fork %s/%s@%s err=%v", o, r, b, err)
	}
	if sha, err := c.GetRefSHA(ctx, "o", "r", "heads/main"); err != nil || sha != "parent" {
		t.Fatalf("ref sha %q err=%v", sha, err)
	}
	if err := c.CreateBranch(ctx, "o", "r", "b", "parent"); err != nil {
		t.Fatal(err)
	}
	if tree, err := c.CommitTree(ctx, "o", "r", "parent"); err != nil || tree != "basetree" {
		t.Fatalf("commit tree %q err=%v", tree, err)
	}
	if data, err := c.ReadFile(ctx, "o", "r", "f", "main"); err != nil || string(data) != "hello" {
		t.Fatalf("read file %q err=%v", data, err)
	}
	if n, url, err := c.CreatePullRequest(ctx, "o", "r", "t", "creator", "b", "main", "body"); err != nil || n != 7 || !strings.Contains(url, "/pull/7") {
		t.Fatalf("pr %d %q err=%v", n, url, err)
	}
	if err := c.MergePullRequest(ctx, "o", "r", 7); err != nil {
		t.Fatal(err)
	}
	if comments, err := c.ListIssueComments(ctx, "o", "r", 7); err != nil || len(comments) != 1 || comments[0].User.Login != "rev" {
		t.Fatalf("comments %+v err=%v", comments, err)
	}
	if pr, err := c.GetPullRequest(ctx, "o", "r", 7); err != nil || pr.State != "open" {
		t.Fatalf("pr status %+v err=%v", pr, err)
	}
	c.SyncForkDefaultBranch(ctx, "creator", "r", "main", "up", "repo")
}

func TestGitHubEnsureUserRepoExisting(t *testing.T) {
	srv, _ := githubMock(t, func(s *githubMockState) { s.existing = true })
	defer srv.Close()
	c := GitHubClient{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	owner, repo, branch, err := c.EnsureUserRepo(context.Background(), "r", "d")
	if err != nil || owner != "creator" || repo != "r" || branch != "main" {
		t.Fatalf("existing repo %s/%s@%s err=%v", owner, repo, branch, err)
	}
}

func TestEncodeRef(t *testing.T) {
	if encodeRef("feature/x") != "feature/x" {
		t.Fatal(encodeRef("feature/x"))
	}
}
