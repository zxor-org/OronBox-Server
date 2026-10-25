package schema

import (
	"context"
	"database/sql"
)

const activityV16 = `
CREATE TABLE user_activity_daily(day date NOT NULL,user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(day,user_id));
CREATE INDEX user_activity_daily_user_idx ON user_activity_daily(user_id,day);
CREATE TABLE user_client_usage_daily(day date NOT NULL,user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,version varchar(120) NOT NULL,platform varchar(16) NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(day,user_id,version,platform));
CREATE INDEX user_client_usage_daily_dimensions_idx ON user_client_usage_daily(version,platform,day);
`

func migrateSchemaV16(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, activityV16); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(16)`)
	return err
}
