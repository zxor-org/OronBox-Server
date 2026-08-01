package creator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// testDatabase provisions a per-test database so packages running in
// parallel processes never share serializable predicate locks.
func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		admin, err := store.Open(databaseURL)
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE `+name)
	})
	return db
}

func TestCreatorLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)
	userID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3)`, userID, time.Now().UnixNano(), "creator-test"); err != nil {
		t.Fatal(err)
	}
	local, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, local, Limits{UploadMaxBytes: 1 << 20, PreviewMaxBytes: 1 << 20, PreviewMaxCount: 4})
	workspace, err := service.Create(ctx, userID, "test-"+uuid.NewString(), "Test watchface", Watchface)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM resources WHERE id=$1`, workspace.Resource.ID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	deviceRows, err := service.Devices(ctx)
	if err != nil || len(deviceRows) == 0 {
		t.Fatalf("devices: %v, count=%d", err, len(deviceRows))
	}
	watchface := make([]byte, 128)
	copy(watchface, []byte{0x5a, 0xa5, 0x34, 0x12})
	copy(watchface[0x28:], fmt.Sprintf("%09d", time.Now().UnixNano()%1_000_000_000))
	var preview bytes.Buffer
	if err := png.Encode(&preview, image.NewNRGBA(image.Rect(0, 0, 300, 300))); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"artifacts/face.bin":  watchface,
		"media/preview-0.png": preview.Bytes(),
		"media/preview-1.png": preview.Bytes(),
	}
	manifest := func(name string, previews int, devices []string) map[string]any {
		previewRefs := make([]any, 0, previews)
		for index := 0; index < previews; index++ {
			previewRefs = append(previewRefs, map[string]any{
				"file":   fmt.Sprintf("media/preview-%d.png", index),
				"sha256": testSHA256(preview.Bytes()),
				"width":  300,
				"height": 300,
			})
		}
		return map[string]any{
			"version":    1,
			"kind":       "watchface",
			"name":       name,
			"summary":    "Summary",
			"attributes": []string{"original"},
			"media":      map[string]any{"previews": previewRefs},
			"artifacts": []any{map[string]any{
				"file":          "artifacts/face.bin",
				"original_name": "face.bin",
				"type":          "velaos_watchface",
				"sha256":        testSHA256(watchface),
				"device_ids":    devices,
			}},
		}
	}
	draftManifest := map[string]any{
		"version":   1,
		"kind":      "watchface",
		"name":      "Saved draft",
		"summary":   "Incomplete work",
		"media":     map[string]any{"previews": []any{}},
		"artifacts": []any{},
	}
	workspace, err = service.SaveDraft(
		ctx,
		userID,
		workspace.Resource.ID,
		testBundle(t, draftManifest, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Revisions) != 1 || workspace.Revisions[0].State != RevisionDraft || workspace.Review != nil {
		t.Fatalf("saved draft workspace = %#v review=%#v", workspace.Revisions, workspace.Review)
	}
	draftManifest["name"] = "Saved draft again"
	workspace, err = service.SaveDraft(ctx, userID, workspace.Resource.ID, testBundle(t, draftManifest, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Revisions) != 1 || workspace.Revisions[0].Name != "Saved draft again" {
		t.Fatalf("replacement draft workspace = %#v", workspace.Revisions)
	}
	draftManifest["name"] = ""
	workspace, err = service.SaveDraft(ctx, userID, workspace.Resource.ID, testBundle(t, draftManifest, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, userID, workspace.Resource.ID, testBundle(t, manifest("", 1, []string{deviceRows[0].ID}), files)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("publish with an empty name error = %v, want invalid", err)
	}
	if _, err := service.Publish(ctx, userID, workspace.Resource.ID, testBundle(t, manifest("Test face", 1, nil), files)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("publish without device bindings error = %v, want invalid", err)
	}
	workspace, err = service.Publish(ctx, userID, workspace.Resource.ID, testBundle(t, manifest("Test face", 2, []string{deviceRows[0].ID}), files))
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Revisions) != 1 || workspace.Revisions[0].State != RevisionSubmitted {
		t.Fatalf("published workspace = %#v", workspace.Revisions)
	}
	if len(workspace.Artifacts) != 1 || workspace.Artifacts[0].SizeBytes != int64(len(watchface)) {
		t.Fatalf("artifacts = %#v", workspace.Artifacts)
	}
	if len(workspace.Media) != 2 || workspace.Review == nil || workspace.Review.State != ReviewPending {
		t.Fatalf("media/review = %#v %#v", workspace.Media, workspace.Review)
	}
	if err := service.Review(ctx, workspace.Revisions[0].ID, "", true, "", nil, []string{"original"}, "standard"); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.Workspace(ctx, userID, workspace.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstRevisionID := workspace.Resource.CurrentRevisionID
	if firstRevisionID == "" {
		t.Fatal("approved revision did not become current")
	}
	workspace, err = service.Publish(ctx, userID, workspace.Resource.ID, testBundle(t, manifest("Test face 2", 1, []string{deviceRows[0].ID}), files))
	if err != nil {
		t.Fatal(err)
	}
	secondRevisionID := workspace.Revisions[0].ID
	if secondRevisionID == firstRevisionID {
		t.Fatal("second publish reused the immutable revision")
	}
	if err := service.Review(ctx, secondRevisionID, "", true, "", nil, []string{"original"}, "featured"); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.Workspace(ctx, userID, workspace.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Resource.CurrentRevisionID != secondRevisionID {
		t.Fatalf("current revision = %q, want %q", workspace.Resource.CurrentRevisionID, secondRevisionID)
	}
	states := make(map[string]RevisionState, len(workspace.Revisions))
	for _, revision := range workspace.Revisions {
		states[revision.ID] = revision.State
	}
	if states[firstRevisionID] != RevisionSuperseded || states[secondRevisionID] != RevisionApproved {
		t.Fatalf("revision states = %#v", states)
	}
	workspace, err = service.Publish(ctx, userID, workspace.Resource.ID, testBundle(t, manifest("Rejected face", 1, []string{deviceRows[0].ID}), files))
	if err != nil {
		t.Fatal(err)
	}
	rejectedRevisionID := workspace.Revisions[0].ID
	if err := service.Review(ctx, rejectedRevisionID, "", false, "needs work", nil, nil, "standard"); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.Workspace(ctx, userID, workspace.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Resource.CurrentRevisionID != secondRevisionID || workspace.Resource.CurationGrade != "featured" {
		t.Fatalf("rejected revision changed public resource: current=%q grade=%q", workspace.Resource.CurrentRevisionID, workspace.Resource.CurationGrade)
	}
	public, total, err := service.PublicResources(ctx, PublicQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(public) != 1 || public[0].ID != workspace.Resource.ID {
		t.Fatalf("recommended public resources = %#v, total=%d", public, total)
	}
	public, total, err = service.PublicResources(ctx, PublicQuery{Limit: 20, Search: "creator-test"})
	if err != nil || total != 1 || len(public) != 1 || public[0].ID != workspace.Resource.ID {
		t.Fatalf("author-search public resources = %#v, total=%d, error=%v", public, total, err)
	}
	collection, err := service.CreateCollection(ctx, userID, "collection-"+uuid.NewString(), "Test collection", "Summary", Watchface)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetCollectionResources(ctx, userID, collection.ID, workspace.Resource.ID, []string{workspace.Resource.ID}); err != nil {
		t.Fatal(err)
	}
	if collection.PendingRevision == nil {
		t.Fatal("new collection has no pending metadata revision")
	}
	if err := service.ReviewCollection(ctx, collection.PendingRevision.ID, "", true, ""); err != nil {
		t.Fatal(err)
	}
	public, total, err = service.PublicResources(ctx, PublicQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(public) != 1 || public[0].CardType != "collection" || public[0].ID != collection.ID {
		t.Fatalf("collection public card = %#v, total=%d", public, total)
	}
	public, total, err = service.PublicResources(ctx, PublicQuery{Limit: 20, Search: "creator-test"})
	if err != nil || total != 1 || len(public) != 1 || public[0].ID != collection.ID {
		t.Fatalf("author-search collection = %#v, total=%d, error=%v", public, total, err)
	}
	public, total, err = service.PublicResources(ctx, PublicQuery{Limit: 20, Attributes: []string{"original"}})
	if err != nil || total != 1 || len(public) != 1 {
		t.Fatalf("attribute-filtered collection = %#v, total=%d, error=%v", public, total, err)
	}
	if err := service.DeleteCollection(ctx, userID, collection.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, userID, workspace.Resource.ID); err != nil {
		t.Fatalf("resource deletion error = %v", err)
	}
	if _, err := service.Workspace(ctx, userID, workspace.Resource.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("workspace after deletion error = %v, want not found", err)
	}
}

func TestCreatorModerationLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)
	ownerID, adminID := uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3),($4,$5,$6)`,
		ownerID, time.Now().UnixNano(), "moderation-owner",
		adminID, time.Now().UnixNano()+1, "moderation-admin",
	); err != nil {
		t.Fatal(err)
	}
	local, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, local, Limits{UploadMaxBytes: 1 << 20, PreviewMaxBytes: 1 << 20, PreviewMaxCount: 4})
	workspace, err := service.Create(ctx, ownerID, "moderation-"+uuid.NewString(), "Moderation watchface", Watchface)
	if err != nil {
		t.Fatal(err)
	}
	resourceID := workspace.Resource.ID
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM resources WHERE id=$1`, resourceID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerID, adminID)
	})
	deviceRows, err := service.Devices(ctx)
	if err != nil || len(deviceRows) == 0 {
		t.Fatalf("devices: %v, count=%d", err, len(deviceRows))
	}
	watchface := make([]byte, 128)
	copy(watchface, []byte{0x5a, 0xa5, 0x34, 0x12})
	var preview bytes.Buffer
	if err := png.Encode(&preview, image.NewNRGBA(image.Rect(0, 0, 300, 300))); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"artifacts/face.bin":  watchface,
		"media/preview-0.png": preview.Bytes(),
	}
	manifest := map[string]any{
		"version": 1,
		"kind":    "watchface",
		"name":    "Moderation face",
		"summary": "Summary",
		"media": map[string]any{"previews": []any{map[string]any{
			"file": "media/preview-0.png", "sha256": testSHA256(preview.Bytes()), "width": 300, "height": 300,
		}}},
		"artifacts": []any{map[string]any{
			"file":          "artifacts/face.bin",
			"original_name": "face.bin",
			"type":          "velaos_watchface",
			"sha256":        testSHA256(watchface),
			"device_ids":    []string{deviceRows[0].ID},
		}},
	}
	publish := func() Workspace {
		t.Helper()
		workspace, err := service.Publish(ctx, ownerID, resourceID, testBundle(t, manifest, files))
		if err != nil {
			t.Fatal(err)
		}
		return workspace
	}
	admin := store.AdminSession{UserID: adminID, Username: "moderation-admin"}
	resourceStore := store.New(db)

	workspace = publish()
	if err := service.Review(ctx, workspace.Revisions[0].ID, "", true, "", nil, nil, "standard"); err != nil {
		t.Fatal(err)
	}

	// Owner takedown hides the resource and the owner can restore it.
	workspace, err = service.SetModeration(ctx, ownerID, resourceID, "takedown")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Resource.ModerationState != "suspended" || workspace.Resource.ModerationBy != "owner" {
		t.Fatalf("after takedown = %q/%q", workspace.Resource.ModerationState, workspace.Resource.ModerationBy)
	}
	workspace, err = service.SetModeration(ctx, ownerID, resourceID, "restore")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Resource.ModerationState != "visible" {
		t.Fatalf("after restore = %q", workspace.Resource.ModerationState)
	}

	// Admin suspend blocks owner restore but still allows a new revision;
	// approving that revision returns the resource to visible.
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "suspend", "policy violation", admin); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetModeration(ctx, ownerID, resourceID, "restore"); !errors.Is(err, ErrConflict) {
		t.Fatalf("owner restore after admin suspend error = %v, want conflict", err)
	}
	workspace = publish()
	if err := service.Review(ctx, workspace.Revisions[0].ID, "", true, "", nil, nil, "standard"); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.Workspace(ctx, ownerID, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Resource.ModerationState != "visible" {
		t.Fatalf("after review approval = %q, want visible", workspace.Resource.ModerationState)
	}

	// Frozen resources reject publish until an admin unfreezes them.
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "freeze", "", admin); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, ownerID, resourceID, testBundle(t, manifest, files)); !errors.Is(err, ErrConflict) {
		t.Fatalf("publish while frozen error = %v, want conflict", err)
	}
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "unfreeze", "", admin); err != nil {
		t.Fatal(err)
	}
	workspace = publish()
	if workspace.Revisions[0].State != RevisionSubmitted {
		t.Fatalf("revision after unfreeze = %q", workspace.Revisions[0].State)
	}
}

func TestCreatorDraftLimit(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)
	userID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3)`, userID, time.Now().UnixNano(), "draft-limit-test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	service := New(db, nil, Limits{UploadMaxBytes: 1 << 20, PreviewMaxBytes: 1 << 20, PreviewMaxCount: 4})
	for index := 0; index < maxDraftResources; index++ {
		if _, err := service.Create(ctx, userID, "draft-"+uuid.NewString(), "Draft", Watchface); err != nil {
			t.Fatalf("create draft %d: %v", index, err)
		}
	}
	if _, err := service.Create(ctx, userID, "draft-"+uuid.NewString(), "Draft", Watchface); !errors.Is(err, ErrConflict) {
		t.Fatalf("create beyond draft limit error = %v, want conflict", err)
	}
}

func testSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func testBundle(t *testing.T, manifest map[string]any, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := writer.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(raw); err != nil {
		t.Fatal(err)
	}
	for name, payload := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
