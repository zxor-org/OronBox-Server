package creator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestGiteaDeviceSyncPreservesBindingsAndRollsBackBadCatalog(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	s := New(db, nil, Limits{})
	var original string
	if err := db.QueryRow(`SELECT id::text FROM devices WHERE codename='n67'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	body := `{"n67":{"id":"xmb9p","name":"Updated name","vendor":"xiaomi"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/org/config/raw/devices.json" || r.URL.Query().Get("ref") != "main" || r.Header.Get("Authorization") != "token private" {
			t.Error("incorrect Gitea request")
		}
		w.Write([]byte(body))
	}))
	defer upstream.Close()
	cfg := config.DeviceCatalogConfig{BaseURL: upstream.URL, Repository: "org/config", Ref: "main", Token: "private"}
	if err := s.SyncDeviceCatalog(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	items, err := s.Devices(ctx)
	if err != nil || len(items) != 1 || items[0].ID != original || items[0].Name != "Updated name" {
		t.Fatalf("devices: %#v %v", items, err)
	}
	body = `{"bad":{"id":"duplicate","name":"One"},"bad2":{"id":"duplicate","name":"Two"}}`
	if err := s.SyncDeviceCatalog(ctx, cfg); err == nil {
		t.Fatal("duplicate external ID accepted")
	}
	items, err = s.Devices(ctx)
	if err != nil || len(items) != 1 || items[0].ID != original {
		t.Fatal("invalid catalog damaged previous snapshot")
	}
}

func TestUnusedTagsAvailableOnlyForCreator(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	s := New(db, nil, Limits{})
	// Unused enabled definitions belong in creator selection, never filter results.
	if err := s.UpsertAttribute(ctx, ResourceAttribute{ID: "unused-test", NameZH: "unused", Coefficient: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	filter, err := s.PublicTags(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range filter {
		if tag.ID == "unused-test" {
			t.Fatal("unused filter tag leaked")
		}
	}
	creator, err := s.PublicTags(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tag := range creator {
		found = found || tag.ID == "unused-test"
	}
	if !found {
		t.Fatal("creator cannot choose unused tag")
	}
}
