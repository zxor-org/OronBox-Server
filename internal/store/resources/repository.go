package resources

import (
	"context"
	"database/sql"
)

// DB accepts both a pool and a pinned connection. Transactions remain owned by each operation.
type DB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Repository struct{ db DB }

func New(db DB) *Repository { return &Repository{db: db} }
