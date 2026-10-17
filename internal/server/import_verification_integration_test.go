package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	bandbbsoauth "github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func importVerificationDB(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	admin, err := store.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(raw)
	parsed.Path = "/" + name
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		admin, _ := store.Open(raw)
		defer admin.Close()
		admin.Exec(`DROP DATABASE ` + name)
	})
	if err = store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestImportedBindingRequiresProviderOwnershipAndRealAstroBoxCatalog(t *testing.T) {
	db := importVerificationDB(t)
	ctx := context.Background()
	uid := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,42,'owner')`, uid); err != nil {
		t.Fatal(err)
	}
	secrets, _ := authcore.NewSecrets("test-only")
	cipher, _ := secrets.Encrypt("provider-token")
	st := store.New(db)
	if err := st.UpsertOAuthGrant(ctx, store.GrantParams{UserID: uid, Provider: "bandbbs_publish", Subject: "42", Scopes: []string{"resource:write"}, AccessTokenCipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO github_grants(user_id,github_user_id,login,access_token_cipher)VALUES($1,7,'owner',$2)`, uid, cipher); err != nil {
		t.Fatal(err)
	}
	resourceOwner := 42
	canPush := true
	catalogID := "real-item"
	repoReads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer provider-token" {
			t.Error("provider token missing")
		}
		switch r.URL.Path {
		case "/resources/123":
			json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{"user_id": resourceOwner, "resource_category_id": 94}})
		case "/repos/official/catalog/contents/index_v2.csv":
			csv := "id,name,type,owner,repo,commit,a,b,c,d,e,f\n" + catalogID + ",Name,app,actual-owner,actual-repo,abcdef,a,b,c,d,e,f\n"
			json.NewEncoder(w).Encode(map[string]any{"content": base64.StdEncoding.EncodeToString([]byte(csv)), "encoding": "base64", "sha": "catalog-sha"})
		case "/repos/actual-owner/actual-repo":
			repoReads++
			json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]bool{"push": canPush}, "archived": false})
		default:
			t.Errorf("unexpected request destination: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	app := &App{store: st, secrets: secrets, cfg: config.Config{BandBBS: config.BandBBSConfig{APIURL: upstream.URL}, GitHub: config.GitHubConfig{APIURL: upstream.URL}, AstroBox: config.AstroBoxConfig{RepoOwner: "official", RepoName: "catalog", RepoBranch: "main", CatalogPath: "index_v2.csv"}}}
	app.bandbbs = bandbbsoauth.NewClient(app.cfg.BandBBS, time.Second)
	band := creator.ImportedBinding{Provider: "bandbbs", ExternalID: "123", ExternalURL: "http://127.0.0.1/attacker"}
	verified, err := app.verifyImportedBinding(ctx, uid, band)
	if err != nil || verified.ExternalID != `{"94":"123"}` || verified.ExternalURL == band.ExternalURL {
		t.Fatal("valid BandBBS binding not canonicalized", verified, err)
	}
	resourceOwner = 43
	if _, err = app.verifyImportedBinding(ctx, uid, band); err == nil {
		t.Fatal("foreign BandBBS resource accepted")
	}
	resourceOwner = 42
	band.ExternalID = `{"95":"123"}`
	if _, err = app.verifyImportedBinding(ctx, uid, band); err == nil {
		t.Fatal("incorrect category accepted")
	}
	ab := creator.ImportedBinding{Provider: "astrobox", ExternalID: "real-item", Meta: map[string]string{"repo_owner": "actual-owner", "repo_name": "actual-repo"}}
	verified, err = app.verifyImportedBinding(ctx, uid, ab)
	if err != nil || verified.Meta["repo_name"] != "actual-repo" || verified.Meta["item_id"] != "real-item" {
		t.Fatal("real AstroBox repository lost", verified, err)
	}
	canPush = false
	if _, err = app.verifyImportedBinding(ctx, uid, ab); err == nil {
		t.Fatal("read-only GitHub permission accepted")
	}
	canPush = true
	ab.Meta["repo_name"] = "invented-repo"
	if _, err = app.verifyImportedBinding(ctx, uid, ab); err == nil {
		t.Fatal("invented repository accepted")
	}
	ab.Meta = nil
	catalogID = "other-item"
	prior := repoReads
	if _, err = app.verifyImportedBinding(ctx, uid, ab); err == nil || repoReads != prior {
		t.Fatal("uncatalogued item caused repository lookup", err)
	}
}
