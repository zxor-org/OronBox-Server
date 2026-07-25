package creator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestCreatorLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	userID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3)`, userID, time.Now().UnixNano(), "creator-test"); err != nil {
		t.Fatal(err)
	}
	local, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, local, Limits{UploadMaxBytes: 1 << 20, PreviewMaxBytes: 1 << 20, PreviewMaxCount: 4})
	workspace, err := service.Create(ctx, userID, "test-"+uuid.NewString(), Watchface)
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
			"version": 1,
			"kind":    "watchface",
			"name":    name,
			"summary": "Summary",
			"media":   map[string]any{"previews": previewRefs},
			"artifacts": []any{map[string]any{
				"file":          "artifacts/face.bin",
				"original_name": "face.bin",
				"type":          "velaos_watchface",
				"sha256":        testSHA256(watchface),
				"device_ids":    devices,
			}},
		}
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
	if err := service.Review(ctx, workspace.Revisions[0].ID, "", true, "", nil); err != nil {
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
	if err := service.Review(ctx, secondRevisionID, "", true, "", nil); err != nil {
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
	if err := service.Delete(ctx, userID, workspace.Resource.ID); err != nil {
		t.Fatalf("resource deletion error = %v", err)
	}
	if _, err := service.Workspace(ctx, userID, workspace.Resource.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("workspace after deletion error = %v, want not found", err)
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
