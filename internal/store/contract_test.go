package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// parseTableColumns extracts all table definitions and their column names from SchemaSQL
func parseTableColumns() map[string]map[string]bool {
	tableCols := make(map[string]map[string]bool)

	// Regex to match table definition: CREATE TABLE IF NOT EXISTS <name> ( <body... > );
	reTable := regexp.MustCompile(`(?is)CREATE TABLE IF NOT EXISTS\s+([a-zA-Z0-9_]+)\s*\((.*?)\);`)
	matches := reTable.FindAllStringSubmatch(SchemaSQL, -1)

	for _, m := range matches {
		tableName := strings.TrimSpace(m[1])
		body := m[2]
		cols := make(map[string]bool)

		lines := strings.Split(body, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			// Skip constraints like PRIMARY KEY, UNIQUE, CHECK, FOREIGN KEY
			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "PRIMARY KEY") ||
				strings.HasPrefix(upper, "UNIQUE") ||
				strings.HasPrefix(upper, "CHECK") ||
				strings.HasPrefix(upper, "FOREIGN KEY") ||
				strings.HasPrefix(upper, "CONSTRAINT") {
				continue
			}

			// Column name is the first token
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				colName := strings.ToLower(parts[0])
				cols[colName] = true
			}
		}
		tableCols[tableName] = cols
	}

	return tableCols
}

func TestSchemaColumnContracts(t *testing.T) {
	tables := parseTableColumns()

	// 1. Invariant: login_tickets schema
	if lt, ok := tables["login_tickets"]; !ok {
		t.Fatal("missing table login_tickets")
	} else {
		if !lt["ticket_hash"] {
			t.Error("login_tickets must have ticket_hash")
		}
		if !lt["used_at"] {
			t.Error("login_tickets must have used_at")
		}
		if lt["ticket"] {
			t.Error("login_tickets must not have ticket (ticket_hash is used)")
		}
		if lt["consumed"] {
			t.Error("login_tickets must not have consumed (used_at is used)")
		}
	}

	// 2. Invariant: sessions schema
	if s, ok := tables["sessions"]; !ok {
		t.Fatal("missing table sessions")
	} else {
		if !s["last_seen_at"] {
			t.Error("sessions must have last_seen_at")
		}
		if s["updated_at"] {
			t.Error("sessions does not have updated_at")
		}
	}

	// 3. Invariant: resource_collection_revisions schema
	if rcr, ok := tables["resource_collection_revisions"]; !ok {
		t.Fatal("missing table resource_collection_revisions")
	} else {
		if !rcr["updated_at"] {
			t.Error("resource_collection_revisions must have updated_at")
		}
		if rcr["reviewed_at"] {
			t.Error("resource_collection_revisions does not have reviewed_at")
		}
	}

	// 4. Invariant: blob_replicas schema
	if br, ok := tables["blob_replicas"]; !ok {
		t.Fatal("missing table blob_replicas")
	} else {
		if !br["object_key"] {
			t.Error("blob_replicas must have object_key")
		}
		if !br["state"] {
			t.Error("blob_replicas must have state")
		}
		if br["external_url"] {
			t.Error("blob_replicas does not have external_url")
		}
		if br["status"] {
			t.Error("blob_replicas does not have status")
		}
	}

	// 5. Invariant: github_grants schema
	if gg, ok := tables["github_grants"]; !ok {
		t.Fatal("missing table github_grants")
	} else {
		if !gg["access_token_cipher"] {
			t.Error("github_grants must have access_token_cipher")
		}
	}

	// 6. Invariant: resource_collaborators schema
	if rc, ok := tables["resource_collaborators"]; !ok {
		t.Fatal("missing table resource_collaborators")
	} else {
		if !rc["resource_id"] || !rc["user_uid"] {
			t.Error("resource_collaborators must have (resource_id, user_uid)")
		}
		if rc["id"] {
			t.Error("resource_collaborators does not have id column")
		}
	}
}

// TestCodebaseSQLColumnConsistency scans all Go files under internal/ and checks
// that columns referenced in queries exist in the schema definition.
func TestCodebaseSQLColumnConsistency(t *testing.T) {
	tables := parseTableColumns()

	reSessionsUpdatedAt := regexp.MustCompile(`(?is)UPDATE\s+sessions\s+SET[^;"]*\bupdated_at\s*=`)
	reRCRReviewedAt := regexp.MustCompile(`(?is)(UPDATE|INSERT INTO)\s+resource_collection_revisions[^;"]*\breviewed_at\b`)
	reLoginTicketsConsumed := regexp.MustCompile(`(?is)(UPDATE|INSERT INTO|SELECT)[^;"]*\blogin_tickets\b[^;"]*\bconsumed\b`)
	reLoginTicketsTicket := regexp.MustCompile(`(?is)(UPDATE|INSERT INTO)\s+login_tickets\b[^;"]*\bticket\b\s*[,=]`)
	reBlobReplicasStatus := regexp.MustCompile(`(?is)(UPDATE|INSERT INTO|SELECT)[^;"]*\bblob_replicas\b[^;"]*\b(external_url|status\s*=)`)
	reResourceCollabID := regexp.MustCompile(`(?is)\bresource_collaborators\s+rc\b[^;"]*\brc\.id\b`)

	root := ".."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasSuffix(path, "store/schema.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		code := string(content)

		// Check for known illegal column references that were fixed
		if reLoginTicketsConsumed.MatchString(code) {
			t.Errorf("file %s references obsolete consumed column in login_tickets SQL", path)
		}
		if reLoginTicketsTicket.MatchString(code) {
			t.Errorf("file %s references obsolete ticket column instead of ticket_hash in login_tickets SQL", path)
		}
		if reSessionsUpdatedAt.MatchString(code) {
			t.Errorf("file %s references non-existent updated_at column in sessions UPDATE SQL", path)
		}
		if reRCRReviewedAt.MatchString(code) {
			t.Errorf("file %s references non-existent reviewed_at column in resource_collection_revisions SQL", path)
		}
		if reBlobReplicasStatus.MatchString(code) {
			t.Errorf("file %s references non-existent external_url/status column in blob_replicas SQL", path)
		}
		if reResourceCollabID.MatchString(code) {
			t.Errorf("file %s references non-existent rc.id column in resource_collaborators SQL", path)
		}

		_ = tables
		return nil
	})

	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}
}
