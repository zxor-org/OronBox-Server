package schema

import (
	"context"
	"database/sql"
)

const consoleAuthV17 = `
ALTER TABLE admin_sessions ADD COLUMN absolute_expires_at timestamptz,ADD COLUMN credential_hash text,ADD COLUMN previous_hash text,ADD COLUMN previous_until timestamptz,ADD COLUMN generation bigint NOT NULL DEFAULT 0,ADD COLUMN rotated_at timestamptz;
UPDATE admin_sessions SET id=encode(sha256(convert_to(id,'UTF8')),'hex'),absolute_expires_at=created_at+interval '7 days';
UPDATE admin_sessions SET credential_hash=id,expires_at=LEAST(expires_at,absolute_expires_at);
ALTER TABLE admin_sessions ALTER COLUMN absolute_expires_at SET NOT NULL,ALTER COLUMN credential_hash SET NOT NULL;
CREATE UNIQUE INDEX admin_sessions_credential_idx ON admin_sessions(credential_hash);
CREATE INDEX admin_sessions_previous_idx ON admin_sessions(previous_hash) WHERE previous_hash IS NOT NULL;
ALTER TABLE login_tickets ADD COLUMN purpose text NOT NULL DEFAULT 'client_login' CHECK(purpose IN('client_login','client_publish'));
DELETE FROM login_tickets WHERE return_uri ~ '/console/?$';
DELETE FROM oauth_states WHERE return_uri ~ '/console/?$';
`

func migrateSchemaV17(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, consoleAuthV17); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(17)`)
	return err
}
