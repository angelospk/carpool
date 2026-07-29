// Package testdb provides the integration-test database harness.
//
// Tests run against a real PostgreSQL + PostGIS: the matching SQL depends on
// PostGIS semantics that no fake can reproduce. The DSN comes from the
// environment so the same suite runs unchanged against a compose database, a
// testcontainer, or a locally installed server.
package testdb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DSNEnv names the environment variable holding the test database DSN.
const DSNEnv = "CARPOOL_TEST_DSN"

var dbCounter atomic.Int64

// Fresh returns a pool connected to an empty database created for this test.
//
// Each test gets its own database rather than sharing one with truncation
// between tests: it makes concurrency tests honest and stops one test's schema
// state leaking into another. The database is dropped on cleanup.
//
// The test is skipped when no DSN is configured, so unit-only runs stay fast.
func Fresh(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping integration test", DSNEnv)
	}

	ctx := context.Background()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to %s: %v", DSNEnv, err)
	}
	defer admin.Close()

	name := fmt.Sprintf("carpool_test_%d_%d", os.Getpid(), dbCounter.Add(1))
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdent(name)); err != nil {
		t.Fatalf("creating test database %s: %v", name, err)
	}

	pool, err := pgxpool.New(ctx, replaceDatabase(dsn, name))
	if err != nil {
		t.Fatalf("connecting to test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		pool.Close()

		cleanup, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			t.Logf("could not reconnect to drop %s: %v", name, err)
			return
		}
		defer cleanup.Close()

		if _, err := cleanup.Exec(context.Background(),
			`DROP DATABASE IF EXISTS `+quoteIdent(name)+` WITH (FORCE)`); err != nil {
			t.Logf("dropping test database %s: %v", name, err)
		}
	})

	return pool
}

// replaceDatabase swaps the database name in a postgres URL.
func replaceDatabase(dsn, name string) string {
	slash := strings.LastIndex(dsn, "/")
	if slash < 0 {
		return dsn
	}
	tail := ""
	if q := strings.Index(dsn[slash:], "?"); q >= 0 {
		tail = dsn[slash+q:]
	}
	return dsn[:slash+1] + name + tail
}

// quoteIdent quotes an identifier for use in DDL. Names here are generated, not
// user input, but quoting keeps the DDL well-formed regardless.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
