package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/zxor-org/OronBox-Server/internal/auth"
)

func main() {
	loadDotEnv(".env")
	key := os.Getenv("TOKEN_ENCRYPTION_KEY")
	databaseURL := os.Getenv("DATABASE_URL")
	if key == "" || databaseURL == "" {
		fmt.Fprintln(os.Stderr, "TOKEN_ENCRYPTION_KEY and DATABASE_URL are required (env or .env)")
		os.Exit(1)
	}
	secrets, err := auth.NewSecrets(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), `SELECT user_id::text,provider,subject,scopes::text,access_token_cipher,refresh_token_cipher,expires_at FROM oauth_grants ORDER BY provider,user_id`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer rows.Close()
	out := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer out.Flush()
	for rows.Next() {
		var userID, provider, subject, scopes string
		var accessCipher, refreshCipher []byte
		var expiresAt *time.Time
		if err := rows.Scan(&userID, &provider, &subject, &scopes, &accessCipher, &refreshCipher, &expiresAt); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		access, err := secrets.Decrypt(accessCipher)
		if err != nil {
			access = "<decrypt failed: " + err.Error() + ">"
		}
		refresh := ""
		if refreshCipher != nil {
			if value, err := secrets.Decrypt(refreshCipher); err == nil {
				refresh = value
			}
		}
		fmt.Fprintf(out, "user_id:\t%s\nprovider:\t%s\nsubject:\t%s\nscopes:\t%s\nexpires_at:\t%v\naccess_token:\t%s\nrefresh_token:\t%s\n\n", userID, provider, subject, scopes, expiresAt, access, refresh)
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
