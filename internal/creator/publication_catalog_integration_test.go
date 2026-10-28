package creator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
	resourcecore "github.com/zxor-org/OronBox-Server/internal/resource"
)

func TestPublicationCatalogOverridesClaimsAndRetainsLastValidSnapshot(t *testing.T) {
	db := testDatabase(t)
	s := New(db, nil, Limits{})
	s.RequirePublicationCatalog = true
	ctx := context.Background()
	var device string
	if err := db.QueryRow(`SELECT id::text FROM devices WHERE codename='n67'`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	body := `{"resource_prefixes":{"app":91,"watchface":92},"bandbbs":{"n67":{"category_id":777}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/org/config/raw/publish.json" || r.Header.Get("Authorization") != "token private" {
			t.Error("incorrect Gitea request")
		}
		w.Write([]byte(body))
	}))
	defer upstream.Close()
	cfg := config.DeviceCatalogConfig{BaseURL: upstream.URL, Repository: "org/config", Ref: "main", Token: "private"}
	artifacts := []verifiedArtifact{{sha256: "digest-a", deviceIDs: []string{device}, analysis: resourcecore.Analysis{PackageID: "app.id"}}}
	requests := []PublicationRequest{{Target: PublishBandBBS, Config: map[string]any{"targets": []any{map[string]any{"category_id": 1}}}}}
	if err := s.mapPublications(ctx, QuickApp, artifacts, requests, true); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing config accepted", err)
	}
	if err := s.SyncPublicationCatalog(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	version := s.PublicationOptions().Version
	if err := s.mapPublications(ctx, QuickApp, artifacts, requests, true); err != nil {
		t.Fatal(err)
	}
	target := requests[0].Config["targets"].([]any)[0].(map[string]any)
	if target["category_id"] != 777 || target["prefix_id"] != 91 || target["package_id"] != "app.id" || target["artifact_sha256"] != "digest-a" || requests[0].Config["config_version"] != version {
		t.Fatal("untrusted client mapping retained", target)
	}
	body = `{"resource_prefixes":{"app":91}}`
	if err := s.SyncPublicationCatalog(ctx, cfg); err == nil {
		t.Fatal("invalid config accepted")
	}
	if s.PublicationOptions().Version != version {
		t.Fatal("last valid snapshot lost")
	}
	artifacts = append(artifacts, verifiedArtifact{sha256: "digest-b", deviceIDs: []string{device}, analysis: resourcecore.Analysis{PackageID: "app.id"}})
	if err := s.mapPublications(ctx, QuickApp, artifacts, requests, true); !errors.Is(err, ErrInvalid) {
		t.Fatal("different packages in shared category accepted", err)
	}
}
