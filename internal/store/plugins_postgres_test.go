package store_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// TestPluginModerationLifecycle walks a plugin through upload, review,
// update, delist and restore against a real schema, asserting public
// visibility and uploader notifications at every step.
func TestPluginModerationLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName)
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)

	uploaderID, strangerID := uuid.NewString(), uuid.NewString()
	identitySeed := time.Now().UnixNano()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3),($4,$5,$6)`,
		uploaderID, identitySeed, "plugin-uploader-"+uploaderID,
		strangerID, identitySeed+1, "plugin-stranger-"+strangerID,
	); err != nil {
		t.Fatal(err)
	}
	blobSeed := strings.ReplaceAll(uuid.NewString(), "-", "")
	blobSHA := blobSeed + blobSeed
	if _, err := db.ExecContext(ctx, `INSERT INTO blobs(sha256,size_bytes,media_type,local_key) VALUES($1,1,'application/zip',$2)`, blobSHA, "sha256/"+blobSHA); err != nil {
		t.Fatal(err)
	}

	record := store.PluginRecord{
		ID:            "com.example.moderated",
		UploaderID:    uploaderID,
		Name:          "Moderated",
		Version:       "1.0.0",
		Runtime:       "js",
		Permissions:   []string{"ui"},
		PackageSHA256: blobSHA,
	}
	upload := func() {
		t.Helper()
		owned, err := s.UpsertPlugin(ctx, record)
		if err != nil || !owned {
			t.Fatalf("upsert failed: owned=%t err=%v", owned, err)
		}
	}
	pluginState := func() store.PluginRecord {
		t.Helper()
		plugin, err := s.Plugin(ctx, record.ID)
		if err != nil {
			t.Fatal(err)
		}
		return plugin
	}
	publicCount := func() int {
		t.Helper()
		plugins, err := s.ListPlugins(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		return len(plugins)
	}
	viewerEntries := func() []store.PluginRecord {
		t.Helper()
		plugins, err := s.ListPlugins(ctx, uploaderID)
		if err != nil {
			t.Fatal(err)
		}
		return plugins
	}
	lastMessage := func() (string, map[string]any) {
		t.Helper()
		var event string
		var data []byte
		err := db.QueryRowContext(ctx, `SELECT event,data FROM user_messages WHERE user_id=$1 AND kind='moderation' ORDER BY created_at DESC LIMIT 1`, uploaderID).Scan(&event, &data)
		if err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatalf("invalid notification metadata: %v", err)
		}
		return event, metadata
	}
	setState := func(state, reason string) {
		t.Helper()
		if _, err := s.SetPluginState(ctx, record.ID, state, reason); err != nil {
			t.Fatal(err)
		}
	}

	upload()
	if got := pluginState().State; got != "pending" {
		t.Fatalf("fresh upload must be pending, got %s", got)
	}
	if got := publicCount(); got != 0 {
		t.Fatalf("pending plugin leaked into public catalog: %d entries", got)
	}
	if entries := viewerEntries(); len(entries) != 1 || entries[0].State != "pending" {
		t.Fatalf("uploader must see own pending plugin: %+v", entries)
	}

	setState("listed", "")
	if got := publicCount(); got != 1 {
		t.Fatalf("approved plugin missing from public catalog: %d entries", got)
	}
	if event, data := lastMessage(); event != "plugin.approved" || data["plugin_id"] != record.ID {
		t.Fatalf("unexpected approval message: %s %#v", event, data)
	}
	var moderationMessages int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND kind='moderation'`, uploaderID).Scan(&moderationMessages); err != nil {
		t.Fatal(err)
	}
	if moderationMessages != 1 {
		t.Fatalf("initial approval notifications = %d, want 1", moderationMessages)
	}

	upload()
	if got := pluginState(); got.State != "listed" || got.PendingVersionID == "" {
		t.Fatalf("update must keep current version listed and expose pending version, got %+v", got)
	}
	if got := publicCount(); got != 1 {
		t.Fatalf("approved current version disappeared during update review: %d entries", got)
	}

	setState("rejected", "contains malware")
	if plugin := pluginState(); plugin.State != "listed" || plugin.PendingVersionID != "" || plugin.ModerationReason != "contains malware" {
		t.Fatalf("rejection not recorded: %+v", plugin)
	}
	if event, data := lastMessage(); event != "plugin.rejected" || data["reason"] != "contains malware" {
		t.Fatalf("unexpected rejection message: %s %#v", event, data)
	}

	setState("listed", "")
	setState("delisted", "abuse report")
	if got := publicCount(); got != 0 {
		t.Fatalf("delisted plugin stayed public: %d entries", got)
	}
	if entries := viewerEntries(); len(entries) != 1 || entries[0].State != "delisted" || entries[0].ModerationReason != "abuse report" {
		t.Fatalf("uploader must see own delisted plugin with reason: %+v", entries)
	}
	if event, data := lastMessage(); event != "plugin.delisted" || data["reason"] != "abuse report" {
		t.Fatalf("unexpected delist message: %s %#v", event, data)
	}

	setState("listed", "")
	if got := publicCount(); got != 1 {
		t.Fatalf("restored plugin missing from public catalog: %d entries", got)
	}
	if event, _ := lastMessage(); event != "plugin.relisted" {
		t.Fatalf("unexpected restore message: %s", event)
	}

	if _, err := s.SetPluginState(ctx, "com.example.missing", "listed", ""); err == nil {
		t.Fatal("SetPluginState on missing plugin must fail")
	}
	if removed, err := s.DeletePlugin(ctx, record.ID, strangerID); err != nil || removed {
		t.Fatalf("stranger must not delete the plugin: removed=%t err=%v", removed, err)
	}
	if removed, err := s.DeletePlugin(ctx, record.ID, uploaderID); err != nil || !removed {
		t.Fatalf("uploader delete failed: removed=%t err=%v", removed, err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM plugins WHERE id=$1`, record.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("deleted plugin row survived")
	}
}
