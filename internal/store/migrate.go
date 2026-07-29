// Package store holds the pgx repositories and the schema migrations.
package store

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateLockID is an arbitrary but fixed key for the advisory lock that
// serialises migration runs. Every API instance migrates at boot, so without it
// two instances starting together would race into duplicate-object errors.
const migrateLockID int64 = 0x6361_7270_6f6f_6c01

// Migrate brings the database up to the latest schema version. It is safe to
// call concurrently from multiple processes and is a no-op when already current.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("store: acquiring connection for migration: %w", err)
	}
	defer conn.Release()

	// Hold the advisory lock on this one connection for the whole migration.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return fmt.Errorf("store: taking migration lock: %w", err)
	}
	defer func() {
		// Best effort: the lock is released with the session in any case.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrateLockID)
	}()

	// goose speaks database/sql, so hand it this same pgx connection rather than
	// opening a second one that would not hold the lock.
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: setting goose dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("store: applying migrations: %w", err)
	}
	return nil
}
