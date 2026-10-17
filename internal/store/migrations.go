package store

import (
	"context"
	"database/sql"

	"github.com/zxor-org/OronBox-Server/internal/store/schema"
)

func Migrate(ctx context.Context, db *sql.DB) error { return schema.Migrate(ctx, db) }
