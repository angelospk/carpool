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
	return migrate(ctx, pool, 0)
}

// MigrateTo applies migrations up to and including version, and no further.
//
// It exists so that a backfill can be tested for what it actually does. Several
// migrations here expand, backfill and then contract, and the backfill is the
// part that touches rows somebody already has; running the whole chain against
// an empty database proves the schema compiles and nothing about the data.
func MigrateTo(ctx context.Context, pool *pgxpool.Pool, version int64) error {
	return migrate(ctx, pool, version)
}

// migrate applies every migration, or those up to version when it is non-zero.
func migrate(ctx context.Context, pool *pgxpool.Pool, version int64) error {
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

	if version > 0 {
		if err := goose.UpToContext(ctx, db, "migrations", version); err != nil {
			return fmt.Errorf("store: applying migrations up to %d: %w", version, err)
		}
		return nil
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("store: applying migrations: %w", err)
	}
	return nil
}

// MigrationVersions lists the embedded migrations, newest last. Tests use it so
// that adding a migration does not mean editing a hard-coded number.
func MigrationVersions() []string {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		panic("store: reading embedded migrations: " + err.Error())
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
