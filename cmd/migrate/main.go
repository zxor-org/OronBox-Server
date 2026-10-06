package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

type backup struct {
	CreatedAt time.Time                   `json:"created_at"`
	Tables    map[string][]map[string]any `json:"tables"`
}

var legacyDisambiguationMap = map[string]string{"def29e25-6c2c-45fd-b38c-75e2d4f3aa0b": "com.SoDictionary.solan(en)", "249e8e0d-122a-4cbe-8f64-ae91599b0139": "com.SoDictionary.solan(ja)", "3ff239f7-c0d0-4615-8d79-effd24768eac": "com.vt.starry(guixu)"}
var backupTables = []string{"users", "resources", "resource_comments", "user_messages", "external_bindings", "oauth_grants"}
var deviceMap = map[string]string{"n67": "xmb9p", "p67": "xmb10p"}

func main() {
	mode := flag.String("mode", "", "backup, init, migrate, verify, export-gitops")
	url := flag.String("database-url", os.Getenv("DATABASE_URL"), "target PostgreSQL URL")
	legacy := flag.String("legacy-database-url", os.Getenv("LEGACY_DATABASE_URL"), "legacy PostgreSQL URL")
	input := flag.String("input", "", "backup JSON file")
	output := flag.String("output", "", "backup destination")
	gitopsDir := flag.String("gitops-dir", "", "directory for export-gitops output")
	flag.Parse()
	ctx := context.Background()
	var err error
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "init":
		err = initDB(ctx, *url)
	case "backup":
		err = runBackup(ctx, *legacy, *url, *output)
	case "migrate":
		err = runMigrate(ctx, *legacy, *url, *input)
	case "verify":
		err = runVerify(ctx, *legacy, *url, *input)
	case "export-gitops":
		err = exportGitOps(*input, *gitopsDir)
	default:
		flag.Usage()
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func exportGitOps(input, destination string) error {
	if input == "" {
		return errors.New("--input backup JSON is required")
	}
	raw, e := os.ReadFile(input)
	if e != nil {
		return e
	}
	var b backup
	if e = json.Unmarshal(raw, &b); e != nil {
		return e
	}
	if destination == "" {
		destination = "gitops-export"
	}
	if e = os.MkdirAll(destination, 0750); e != nil {
		return e
	}
	rows := b.Tables["resource_interactions"]
	if len(rows) == 0 {
		rows = b.Tables["resources"]
	}
	indexRecords := [][]string{{"id", "name", "restype", "repo_owner", "repo_name", "repo_commit_hash", "icon", "cover", "tags", "device_vendors", "devices", "paid_type"}}
	for _, row := range rows {
		id := fmt.Sprint(row["resource_id"])
		if id == "" {
			id = fmt.Sprint(row["id"])
		}
		if id == "" {
			continue
		}
		repoName := repositoryName(id)
		dir := filepath.Join(destination, repoName)
		if e = os.MkdirAll(filepath.Join(dir, "media"), 0750); e != nil {
			return e
		}
		manifest := map[string]any{"item": map[string]any{"id": id, "title": row["title"], "version": row["latest_version"]}, "downloads": []any{}, "device_map": deviceMap}
		data, _ := json.MarshalIndent(manifest, "", "  ")
		if e = os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0640); e != nil {
			return e
		}
		for name, value := range map[string]any{"changelog.json": []any{}, "devices.json": deviceMap, "publish.json": map[string]any{"categories": []any{}}} {
			extra, _ := json.MarshalIndent(value, "", "  ")
			if e = os.WriteFile(filepath.Join(dir, name), append(extra, '\n'), 0640); e != nil {
				return e
			}
		}
		indexRecords = append(indexRecords, []string{id, stringField(row, "title", id), defaultString(stringField(row, "restype", "quick_app"), "quick_app"), "OronBoxCommunity", repoName, "", "media/icon.webp", "media/cover.webp", "", "", stringField(row, "devices", ""), "free"})
	}
	var indexBuffer bytes.Buffer
	writer := csv.NewWriter(&indexBuffer)
	if e := writer.WriteAll(indexRecords); e != nil {
		return e
	}
	indexData := indexBuffer.Bytes()
	if e := os.WriteFile(filepath.Join(destination, "index.csv"), indexData, 0640); e != nil {
		return e
	}
	if baseURL, tokenValue := strings.TrimSpace(os.Getenv("GITEA_URL")), strings.TrimSpace(os.Getenv("GITEA_BOT_TOKEN")); baseURL != "" && tokenValue != "" {
		catalog := os.Getenv("GITEA_CATALOG_REPO")
		owner, repo := splitRepo(catalog)
		if owner == "" || repo == "" {
			return errors.New("GITEA_CATALOG_REPO must be owner/repository")
		}
		client := syndication.GiteaClient{BaseURL: baseURL, Token: tokenValue}
		for _, row := range rows {
			id := fmt.Sprint(row["resource_id"])
			if id == "" {
				id = fmt.Sprint(row["id"])
			}
			if id == "" {
				continue
			}
			name := repositoryName(id)
			if err := client.CreateRepository(context.Background(), owner, name, false); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
				return err
			}
			dir := filepath.Join(destination, name)
			files := map[string][]byte{}
			for _, file := range []string{"manifest.json", "changelog.json", "devices.json", "publish.json"} {
				contents, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					continue
				}
				files[file] = contents
			}
			if len(files) > 0 {
				if _, err := client.CommitFilesTree(context.Background(), owner, name, "main", "migrate legacy resource", files); err != nil {
					return err
				}
			}
		}
		if err := client.UpsertFile(context.Background(), owner, repo, "index.csv", "main", "migrate legacy resource index", indexData); err != nil {
			return err
		}
	}
	return nil
}

func repositoryName(id string) string {
	var b strings.Builder
	b.WriteString("oronbox-resource-")
	lastDash := false
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func stringField(row map[string]any, key, fallback string) string {
	value := fmt.Sprint(row[key])
	if value == "<nil>" || value == "" {
		return fallback
	}
	return value
}

func splitRepo(value string) (string, string) {
	parts := strings.SplitN(strings.Trim(value, "/"), "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
func initDB(ctx context.Context, url string) error {
	s, e := store.Open(ctx, url)
	if e != nil {
		return e
	}
	defer s.Close()
	return store.Migrate(ctx, s)
}
func sourceDB(ctx context.Context, legacy, fallback string) (*store.Store, error) {
	if strings.TrimSpace(legacy) == "" {
		legacy = fallback
	}
	if legacy == "" {
		return nil, errors.New("a legacy database URL is required")
	}
	return store.Open(ctx, legacy)
}
func runBackup(ctx context.Context, legacy, fallback, out string) error {
	src, e := sourceDB(ctx, legacy, fallback)
	if e != nil {
		return e
	}
	defer src.Close()
	b := backup{CreatedAt: time.Now().UTC(), Tables: map[string][]map[string]any{}}
	tables := []string{}
	qRows, qErr := src.Pool.Query(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name")
	if qErr == nil {
		defer qRows.Close()
		for qRows.Next() {
			var t string
			if err := qRows.Scan(&t); err == nil {
				tables = append(tables, t)
			}
		}
	}
	if len(tables) == 0 {
		tables = backupTables
	}
	for _, table := range tables {
		rows, e := exportTable(ctx, src, table)
		if e != nil {
			if strings.Contains(strings.ToLower(e.Error()), "does not exist") {
				continue
			}
			return fmt.Errorf("export %s: %w", table, e)
		}
		b.Tables[table] = rows
	}
	if out == "" {
		out = filepath.Join("backups", "legacy_backup_"+b.CreatedAt.Format("20060102T150405Z")+".json")
	}
	if e := os.MkdirAll(filepath.Dir(out), 0750); e != nil {
		return e
	}
	raw, _ := json.MarshalIndent(b, "", "  ")
	if e := os.WriteFile(out, append(raw, '\n'), 0600); e != nil {
		return e
	}
	fmt.Println("backup written:", out)
	return nil
}
func exportTable(ctx context.Context, db *store.Store, table string) ([]map[string]any, error) {
	rows, e := db.Pool.Query(ctx, "SELECT * FROM "+table)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	out := []map[string]any{}
	for rows.Next() {
		values, e := rows.Values()
		if e != nil {
			return nil, e
		}
		item := map[string]any{}
		for i, f := range fields {
			item[string(f.Name)] = normalizeValue(values[i])
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func runMigrate(ctx context.Context, legacy, target, input string) error {
	if target == "" {
		return errors.New("DATABASE_URL is required")
	}
	dst, e := store.Open(ctx, target)
	if e != nil {
		return e
	}
	defer dst.Close()
	if e = store.Migrate(ctx, dst); e != nil {
		return e
	}
	if legacy != "" {
		src, e := store.Open(ctx, legacy)
		if e != nil {
			return e
		}
		defer src.Close()
		return migrateLegacy(ctx, src, dst)
	}
	if input == "" {
		return errors.New("provide --legacy-database-url or --input")
	}
	return migrateBackup(ctx, input, dst)
}
func migrateBackup(ctx context.Context, input string, dst *store.Store) error {
	raw, e := os.ReadFile(input)
	if e != nil {
		return e
	}
	var b backup
	if e = json.Unmarshal(raw, &b); e != nil {
		return e
	}
	counts := map[string]int{}
	e = dst.WithTx(ctx, func(tx pgx.Tx) error {
		for _, row := range b.Tables["users"] {
			id := stringValue(row, "id")
			uid := int64(number(firstValue(row, "bandbbs_uid", "bandbbs_user_id")))
			if id == "" || uid == 0 {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO users(id,bandbbs_uid,username,avatar_url,role,banned,ban_reason,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(bandbbs_uid) DO UPDATE SET username=EXCLUDED.username,avatar_url=EXCLUDED.avatar_url,role=EXCLUDED.role,banned=EXCLUDED.banned,ban_reason=EXCLUDED.ban_reason`, id, uid, stringValue(row, "username"), stringValue(row, "avatar_url"), defaultString(stringValue(row, "role"), "user"), boolValue(row["banned"]), stringValue(row, "ban_reason"), timeValue(row["created_at"]), timeValue(row["updated_at"]))
			if err != nil {
				return err
			}
			counts["users"]++
		}
		resourceIDs := map[string]string{}
		for _, row := range b.Tables["resources"] {
			legacyID := stringValue(row, "id")
			owner := stringValue(row, "owner_id")
			pkg := stringValue(row, "package_id")
			if pkg == "" {
				pkg = defaultString(stringValue(row, "slug"), legacyID)
			}
			if mapped, ok := legacyDisambiguationMap[legacyID]; ok {
				pkg = mapped
			}
			if legacyID == "" || pkg == "" || owner == "" {
				continue
			}
			resourceIDs[legacyID] = pkg
			_, err := tx.Exec(ctx, `INSERT INTO resource_interactions(resource_id,title,latest_version,owner_id,download_count,coin_count) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(resource_id) DO UPDATE SET download_count=EXCLUDED.download_count,coin_count=EXCLUDED.coin_count`, pkg, stringValue(row, "title"), defaultString(stringValue(row, "latest_version"), "0.0.1"), owner, int64(number(row["download_count"])), int64(number(row["coin_count"])))
			if err != nil {
				return err
			}
			counts["resources"]++
		}
		parents := map[string]string{}
		for _, row := range b.Tables["resource_comments"] {
			id, legacyResource, userID := stringValue(row, "id"), stringValue(row, "resource_id"), stringValue(row, "user_id")
			pkg := resourceIDs[legacyResource]
			if pkg == "" {
				pkg = legacyResource
			}
			if id == "" || pkg == "" || userID == "" {
				continue
			}
			parent := stringValue(row, "parent_id")
			if parent != "" {
				parents[id] = parent
			}
			_, err := tx.Exec(ctx, `INSERT INTO resource_comments(id,resource_id,user_id,content,state,is_deleted,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING`, id, pkg, userID, defaultString(stringValue(row, "body"), stringValue(row, "content")), defaultString(stringValue(row, "moderation_state"), "visible"), boolValue(row["is_deleted"]), timeValue(row["created_at"]))
			if err != nil {
				return err
			}
			counts["resource_comments"]++
		}
		for child, parent := range parents {
			if _, err := tx.Exec(ctx, `UPDATE resource_comments SET parent_id=$1 WHERE id=$2`, parent, child); err != nil {
				return err
			}
		}
		for _, row := range b.Tables["user_messages"] {
			id, userID := stringValue(row, "id"), stringValue(row, "user_id")
			if id == "" || userID == "" {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref,read_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING`, id, userID, defaultString(stringValue(row, "kind"), "system"), stringValue(row, "event"), jsonValue(row["data"]), nullableString(row["title"]), nullableString(row["body"]), nullableString(row["ref"]), nullableTime(row["read_at"]), timeValue(row["created_at"]))
			if err != nil {
				return err
			}
			counts["user_messages"]++
		}
		for _, row := range b.Tables["oauth_grants"] {
			userID, provider := stringValue(row, "user_id"), stringValue(row, "provider")
			if userID == "" || provider == "" {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO oauth_grants(user_id,provider,subject,scopes,access_token_cipher,refresh_token_cipher,expires_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(user_id,provider) DO UPDATE SET subject=EXCLUDED.subject,scopes=EXCLUDED.scopes,access_token_cipher=EXCLUDED.access_token_cipher,refresh_token_cipher=EXCLUDED.refresh_token_cipher,expires_at=EXCLUDED.expires_at,updated_at=EXCLUDED.updated_at`, userID, provider, stringValue(row, "subject"), stringSlice(row["scopes"]), bytesValue(row["access_token_cipher"]), nullableBytes(row["refresh_token_cipher"]), nullableTime(row["expires_at"]), timeValue(row["created_at"]), timeValue(row["updated_at"]))
			if err != nil {
				return err
			}
			counts["oauth_grants"]++
		}
		for _, row := range b.Tables["external_bindings"] {
			id, legacyResource := stringValue(row, "id"), stringValue(row, "resource_id")
			pkg := resourceIDs[legacyResource]
			if pkg == "" {
				pkg = legacyResource
			}
			if id == "" || pkg == "" {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO external_bindings(id,resource_id,provider,category_id,external_id,external_url,meta,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(resource_id,provider,category_id) DO UPDATE SET external_id=EXCLUDED.external_id,external_url=EXCLUDED.external_url,meta=EXCLUDED.meta`, id, pkg, stringValue(row, "provider"), int(number(row["category_id"])), stringValue(row, "external_id"), stringValue(row, "external_url"), jsonValue(row["meta"]), timeValue(row["created_at"]))
			if err != nil {
				return err
			}
			counts["external_bindings"]++
		}
		return nil
	})
	if e != nil {
		return e
	}
	fmt.Printf("migrated backup rows: users=%d resources=%d comments=%d messages=%d bindings=%d\n", counts["users"], counts["resources"], counts["resource_comments"], counts["user_messages"], counts["external_bindings"])
	return nil
}

func firstValue(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func stringValue(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	if b, ok := value.([16]byte); ok {
		return formatUUID(b)
	}
	return fmt.Sprint(value)
}

// normalizeValue makes pgx-native values JSON-safe. pgx v5 returns PostgreSQL
// uuid columns as [16]byte, which json.Marshal would otherwise render as a
// number array and break re-import.
func normalizeValue(value any) any {
	if b, ok := value.([16]byte); ok {
		return formatUUID(b)
	}
	return value
}

func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}

func timeValue(value any) time.Time {
	if v, ok := value.(time.Time); ok {
		return v
	}
	if s, ok := value.(string); ok {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999Z07:00", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	return time.Now().UTC()
}

func nullableTime(value any) *time.Time {
	if value == nil || value == "" {
		return nil
	}
	t := timeValue(value)
	return &t
}

func nullableString(value any) *string {
	if value == nil {
		return nil
	}
	s := fmt.Sprint(value)
	return &s
}

func jsonValue(value any) []byte {
	if value == nil {
		return []byte(`{}`)
	}
	if raw, ok := value.(json.RawMessage); ok {
		return raw
	}
	if raw, ok := value.([]byte); ok && json.Valid(raw) {
		return raw
	}
	raw, err := json.Marshal(value)
	if err != nil || !json.Valid(raw) {
		return []byte(`{}`)
	}
	return raw
}

func bytesValue(value any) []byte {
	s, ok := value.(string)
	if ok {
		if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
			return raw
		}
	}
	if raw, ok := value.([]byte); ok {
		return raw
	}
	return nil
}

func nullableBytes(value any) []byte {
	if value == nil || value == "" {
		return nil
	}
	return bytesValue(value)
}

func stringSlice(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	if values, ok := value.([]any); ok {
		out := make([]string, 0, len(values))
		for _, item := range values {
			out = append(out, fmt.Sprint(item))
		}
		return out
	}
	if value == nil {
		return nil
	}
	return []string{fmt.Sprint(value)}
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func migrateLegacy(ctx context.Context, src, dst *store.Store) error {
	rows, e := src.Pool.Query(ctx, `SELECT id,bandbbs_user_id,username,COALESCE(avatar_url,''),role,banned,COALESCE(ban_reason,''),created_at,updated_at FROM users`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id, avatar, username, role, reason string
		var uid int64
		var banned bool
		var created, updated time.Time
		if e = rows.Scan(&id, &uid, &username, &avatar, &role, &banned, &reason, &created, &updated); e != nil {
			return e
		}
		if _, e = dst.Pool.Exec(ctx, `INSERT INTO users(id,bandbbs_uid,username,avatar_url,role,banned,ban_reason,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(bandbbs_uid) DO UPDATE SET username=EXCLUDED.username,avatar_url=EXCLUDED.avatar_url,role=EXCLUDED.role,banned=EXCLUDED.banned`, id, uid, username, avatar, role, banned, reason, created, updated); e != nil {
			return e
		}
	}
	return migrateResources(ctx, src, dst)
}

func migrateResources(ctx context.Context, src, dst *store.Store) error {
	rows, err := src.Pool.Query(ctx, `SELECT r.id,r.owner_id,COALESCE(r.download_count,0),COALESCE(r.coin_count,0),COALESCE(ra.package_id,r.slug) FROM resources r LEFT JOIN resource_revisions rr ON rr.id=r.current_revision_id LEFT JOIN revision_artifacts ra ON ra.revision_id=rr.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := map[string]string{}
	for rows.Next() {
		var id, owner, pkg string
		var downloads, coins int64
		if err := rows.Scan(&id, &owner, &downloads, &coins, &pkg); err != nil {
			return err
		}
		if mapped, ok := legacyDisambiguationMap[id]; ok {
			pkg = mapped
		}
		ids[id] = pkg
		if _, err := dst.Pool.Exec(ctx, `INSERT INTO resource_interactions(resource_id,owner_id,download_count,coin_count) VALUES($1,$2,$3,$4) ON CONFLICT(resource_id) DO UPDATE SET download_count=EXCLUDED.download_count,coin_count=EXCLUDED.coin_count`, pkg, owner, downloads, coins); err != nil {
			return err
		}
	}
	comments, err := src.Pool.Query(ctx, `SELECT id,resource_id,user_id,parent_id,body,created_at FROM resource_comments`)
	if err == nil {
		defer comments.Close()
		parents := map[string]string{}
		for comments.Next() {
			var id, rid, uid, body string
			var parent *string
			var created time.Time
			if err := comments.Scan(&id, &rid, &uid, &parent, &body, &created); err != nil {
				return err
			}
			if pkg := ids[rid]; pkg != "" {
				if parent != nil {
					parents[id] = *parent
				}
				if _, err := dst.Pool.Exec(ctx, `INSERT INTO resource_comments(id,resource_id,user_id,parent_id,content,created_at) VALUES($1,$2,$3,NULL,$4,$5) ON CONFLICT(id) DO NOTHING`, id, pkg, uid, body, created); err != nil {
					return err
				}
			}
		}
		for child, parent := range parents {
			if _, err := dst.Pool.Exec(ctx, `UPDATE resource_comments SET parent_id=$1 WHERE id=$2`, parent, child); err != nil {
				return err
			}
		}
	} else if !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		return err
	}
	if err := migrateMessages(ctx, src, dst, ids); err != nil {
		return err
	}
	return migrateGrants(ctx, src, dst)
}

func migrateMessages(ctx context.Context, src, dst *store.Store, ids map[string]string) error {
	rows, err := src.Pool.Query(ctx, `SELECT id,user_id,kind,event,data,title,body,ref,read_at,created_at FROM user_messages`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, uid, kind, event, title, body, ref string
		var data any
		var readAt *time.Time
		var created time.Time
		if err := rows.Scan(&id, &uid, &kind, &event, &data, &title, &body, &ref, &readAt, &created); err != nil {
			return err
		}
		if _, err := dst.Pool.Exec(ctx, `INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref,read_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING`, id, uid, kind, event, data, title, body, ref, readAt, created); err != nil {
			return err
		}
	}
	rows2, err := src.Pool.Query(ctx, `SELECT id,resource_id,provider,external_id,external_url,meta,created_at FROM external_bindings`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var id, rid, provider, externalID, externalURL string
			var meta any
			var created time.Time
			if err := rows2.Scan(&id, &rid, &provider, &externalID, &externalURL, &meta, &created); err != nil {
				return err
			}
			pkg := ids[rid]
			if pkg == "" {
				pkg = rid
			}
			if _, err := dst.Pool.Exec(ctx, `INSERT INTO external_bindings(id,resource_id,provider,external_id,external_url,meta,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(resource_id,provider,category_id) DO UPDATE SET external_id=EXCLUDED.external_id,external_url=EXCLUDED.external_url`, id, pkg, provider, externalID, externalURL, meta, created); err != nil {
				return err
			}
		}
	} else if !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		return err
	}
	return nil
}

func migrateGrants(ctx context.Context, src, dst *store.Store) error {
	rows, err := src.Pool.Query(ctx, `SELECT user_id,provider,subject,scopes,access_token_cipher,refresh_token_cipher,expires_at,created_at,updated_at FROM oauth_grants`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var userID, provider, subject string
		var scopes []string
		var access, refresh []byte
		var expires *time.Time
		var created, updated time.Time
		if err := rows.Scan(&userID, &provider, &subject, &scopes, &access, &refresh, &expires, &created, &updated); err != nil {
			return err
		}
		if _, err := dst.Pool.Exec(ctx, `INSERT INTO oauth_grants(user_id,provider,subject,scopes,access_token_cipher,refresh_token_cipher,expires_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(user_id,provider) DO UPDATE SET subject=EXCLUDED.subject,scopes=EXCLUDED.scopes,access_token_cipher=EXCLUDED.access_token_cipher,refresh_token_cipher=EXCLUDED.refresh_token_cipher,expires_at=EXCLUDED.expires_at,updated_at=EXCLUDED.updated_at`, userID, provider, subject, scopes, access, refresh, expires, created, updated); err != nil {
			return err
		}
	}
	return rows.Err()
}

func runVerify(ctx context.Context, legacy, target, input string) error {
	var expected map[string]int
	if input != "" {
		raw, e := os.ReadFile(input)
		if e != nil {
			return e
		}
		var b backup
		if e = json.Unmarshal(raw, &b); e != nil {
			return e
		}
		expected = map[string]int{}
		for table, rows := range b.Tables {
			expected[table] = len(rows)
		}
		if n, ok := expected["resources"]; ok {
			expected["resource_interactions"] = n
		}
	}
	if legacy != "" {
		src, err := store.Open(ctx, legacy)
		if err != nil {
			return err
		}
		defer src.Close()
		expected = map[string]int{}
		for _, table := range backupTables {
			var count int
			if err := src.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err == nil {
				expected[table] = count
			}
		}
		if n, ok := expected["resources"]; ok {
			expected["resource_interactions"] = n
		}
	}
	db, e := store.Open(ctx, target)
	if e != nil {
		return e
	}
	defer db.Close()
	for _, table := range []string{"users", "resource_interactions", "resource_comments", "user_messages", "external_bindings", "oauth_grants"} {
		var count int64
		if e = db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil {
			if strings.Contains(strings.ToLower(e.Error()), "does not exist") {
				continue
			}
			return e
		}
		if want, ok := expected[table]; ok {
			fmt.Printf("%s: source=%d target=%d\n", table, want, count)
			if want != int(count) {
				return fmt.Errorf("row count mismatch for %s: source=%d target=%d", table, want, count)
			}
		} else {
			fmt.Printf("%s: target=%d\n", table, count)
		}
	}
	return nil
}
