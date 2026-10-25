package creator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"image"
	"image/png"
	"sync"
	"testing"
	"time"
)

func TestSavedRevisionSubmissionAndConcurrentEditors(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	uid := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,$2,'editor')`, uid, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	local, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, local, Limits{UploadMaxBytes: 1 << 20, PreviewMaxBytes: 1 << 20, PreviewMaxCount: 4})
	w, err := s.Create(ctx, uid, "cas-test", "CAS", Watchface)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := s.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	face := make([]byte, 128)
	copy(face, []byte{0x5a, 0xa5, 0x34, 0x12})
	copy(face[0x28:], "987654321")
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewNRGBA(image.Rect(0, 0, 300, 300))); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{"version": 1, "kind": "watchface", "name": "CAS", "summary": "short", "description": "# Full body\n正文", "description_format": "markdown", "media": map[string]any{"previews": []any{map[string]any{"file": "p.png", "sha256": testSHA256(pic.Bytes()), "width": 300, "height": 300}}}, "artifacts": []any{map[string]any{"file": "f.bin", "sha256": testSHA256(face), "original_name": "face.bin", "type": "velaos_watchface", "device_ids": []string{devices[0].ID}}}}
	bundle := testBundle(t, m, map[string][]byte{"p.png": pic.Bytes(), "f.bin": face})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.SaveDraftAt(ctx, uid, w.Resource.ID, bundle, 0); results <- err }()
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	w, err = s.Workspace(ctx, uid, w.Resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	rid := w.Revisions[0].ID
	w, err = s.SaveDraftAt(ctx, uid, w.Resource.ID, bundle, w.Resource.EditVersion)
	if err != nil {
		t.Fatal(err)
	}
	if w.Revisions[0].ID != rid {
		t.Fatal("draft identity changed")
	}
	w, err = s.SubmitDraftAt(ctx, uid, w.Resource.ID, rid, w.Resource.EditVersion)
	if err != nil {
		t.Fatal(err)
	}
	if w.Revisions[0].ID != rid || w.Revisions[0].State != RevisionSubmitted || w.Revisions[0].Description != "# Full body\n正文" {
		t.Fatalf("wrong submitted snapshot: %#v", w.Revisions[0])
	}
	if _, err = s.SubmitDraftAt(ctx, uid, w.Resource.ID, rid, 0); err != nil {
		t.Fatal("retry not idempotent", err)
	}
	if err = s.Review(ctx, rid, "", true, "approved", nil, nil, "standard"); err != nil {
		t.Fatal(err)
	}
	// Text only in the description must never match search.
	items, total, err := s.PublicResources(ctx, PublicQuery{Search: "Full body", Limit: 20})
	if err != nil || len(items) != 0 || total != 0 {
		t.Fatal("description leaked into search", err, total)
	}
	items, total, err = s.PublicResources(ctx, PublicQuery{Search: "short", Limit: 20})
	if err != nil || total != 1 {
		t.Fatal("summary missing from search", err, total)
	}
	detail, err := s.PublicResource(ctx, w.Resource.ID)
	if err != nil || detail.Description != "# Full body\n正文" {
		t.Fatal("public body missing", err)
	}
}

func TestV8ContentMigrationAndIndividualBindingUniqueness(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	// Reconstruct the previous schema in this isolated fixture.
	_, err := db.Exec(`ALTER TABLE admin_sessions DROP COLUMN absolute_expires_at,DROP COLUMN credential_hash,DROP COLUMN previous_hash,DROP COLUMN previous_until,DROP COLUMN generation,DROP COLUMN rotated_at;ALTER TABLE login_tickets DROP COLUMN purpose;DROP TABLE user_client_usage_daily;DROP TABLE user_activity_daily;ALTER TABLE oauth_grants DROP COLUMN pending_token_cipher,DROP COLUMN invalid_at;DROP TABLE session_rotation_receipts;ALTER TABLE github_device_flows DROP COLUMN next_poll_at;ALTER TABLE blog_posts DROP COLUMN source_sha,DROP COLUMN source_body_sha,DROP COLUMN source_commit,DROP COLUMN cover_url;ALTER TABLE app_releases DROP COLUMN tag,DROP COLUMN source_id,DROP COLUMN source_available,ADD COLUMN download_url text NOT NULL DEFAULT '';DROP TABLE creator_media_uploads;ALTER TABLE resource_collections DROP COLUMN edit_version;ALTER TABLE resource_collection_revisions DROP COLUMN description,DROP COLUMN icon_sha256,DROP COLUMN cover_sha256;DROP TABLE resource_deletion_steps;DROP TABLE resource_deletion_operations;DROP TRIGGER external_binding_identity_index ON external_bindings;DROP FUNCTION index_external_binding_identities();DROP TABLE external_binding_identities;ALTER TABLE external_bindings DROP COLUMN verified_at,DROP COLUMN verified_by;ALTER TABLE publications DROP COLUMN approved_snapshot;ALTER TABLE resource_revisions DROP COLUMN description,DROP COLUMN description_format,DROP COLUMN binding_claims;ALTER TABLE resources DROP COLUMN edit_version;DELETE FROM schema_migrations;INSERT INTO schema_migrations(version)VALUES(8)`)
	if err != nil {
		t.Fatal(err)
	}
	uid, rid, rev, pub := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	original := "  old 简介\n<strong>keep bytes</strong>  "
	if _, err = db.Exec(`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,$2,'migration')`, uid, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,'old','watchface')`, rid, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state)VALUES($1,$2,1,'old',$3,'approved')`, rev, rid, original); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO publications(id,revision_id,target)VALUES($1,$2,'bandbbs')`, pub, rev); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = store.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var description, summary, queued string
	if err = db.QueryRow(`SELECT description,summary FROM resource_revisions WHERE id=$1`, rev).Scan(&description, &summary); err != nil {
		t.Fatal(err)
	}
	if description != original || summary != "" {
		t.Fatal("migration rewrote or lost original body")
	}
	if err = db.QueryRow(`SELECT approved_snapshot->>'summary' FROM publications WHERE id=$1`, pub).Scan(&queued); err != nil || queued != original {
		t.Fatal("queued approved payload changed", err)
	}
	other := uuid.NewString()
	if _, err = db.Exec(`INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,'other','watchface')`, other, uid); err != nil {
		t.Fatal(err)
	}
	for i, r := range []string{rid, other} {
		_, err = db.Exec(`INSERT INTO external_bindings(id,resource_id,provider,external_id)VALUES($1,$2,'bandbbs',$3)`, uuid.NewString(), r, fmt.Sprintf(`{"%d":"12345"}`, 94+i))
		if (i == 0 && err != nil) || (i == 1 && err == nil) {
			t.Fatal("identity uniqueness failed", i, err)
		}
	}
}
