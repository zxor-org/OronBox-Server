package astrobox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestSyncForkRejectsStaleFork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/upstream/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"current"}}`))
	})
	mux.HandleFunc("POST /repos/creator/catalog/merge-upstream", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /repos/creator/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"stale"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{api: server.URL, http: server.Client(), cfg: config.AstroBoxConfig{RepoOwner: "upstream", RepoName: "catalog", RepoBranch: "main"}}
	err := client.syncFork(context.Background(), "token", repo{Owner: "creator", Name: "catalog", Branch: "main"})
	if err == nil {
		t.Fatal("stale fork was accepted")
	}
}
