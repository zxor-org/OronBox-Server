package coordinator

import (
	"context"
	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceRetainsPluginHistoryAndBlogBodyBlobs(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adminDB.Close() })
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName); err != nil {
			t.Errorf("cleanup test database: %v", err)
		}
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

	local, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storage := store.New(db)
	objects := []blob.Object{}
	for _, name := range []string{"package", "historical-icon", "blog-body", "real-orphan"} {
		obj, err := local.Put(ctx, strings.NewReader(name))
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.EnsureBlob(ctx, obj.SHA256, obj.Size, "application/octet-stream", obj.Key); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, obj)
	}
	if _, err := db.Exec(`UPDATE blobs SET created_at=now()-interval '30 days'`); err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,999,'owner')`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO plugins(id,uploader_id,name,version,runtime,package_sha256)VALUES('org.test.plugin',$1,'plugin','1','js',$2)`, owner, objects[0].SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO plugin_versions(id,plugin_id,revision_no,version,name,runtime,package_sha256,icon_sha256,state)VALUES($1,'org.test.plugin',1,'0','old','js',$2,$3,'superseded')`, uuid.NewString(), objects[0].SHA256, objects[1].SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO blog_posts(slug,title,body)VALUES('draft-blog','Draft',$1)`, "![](/api/blobs/"+objects[2].SHA256+")"); err != nil {
		t.Fatal(err)
	}
	c := &Coordinator{db: db, store: storage, local: local, cfg: config.Config{Retention: config.RetentionConfig{OrphanBlobs: 7 * 24 * time.Hour}}}
	if err := c.maintain(ctx); err != nil {
		t.Fatal(err)
	}
	for _, obj := range objects[:3] {
		f, err := local.Open(ctx, obj.Key)
		if err != nil {
			t.Fatalf("referenced file deleted: %s %v", obj.SHA256, err)
		}
		f.Close()
	}
	if f, err := local.Open(ctx, objects[3].Key); err == nil {
		f.Close()
		t.Fatal("true orphan was retained")
	}
}
