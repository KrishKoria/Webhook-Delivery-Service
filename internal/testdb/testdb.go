// Package testdb provides a migrated, throwaway SQLite database for tests.
package testdb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/migrate"
)

// New returns a fresh file-backed SQLite database with all migrations applied.
// It is closed automatically when the test ends.
func New(t testing.TB) (*sql.DB, *database.Queries) {
	t.Helper()
	conn := Open(t)
	if err := migrate.Up(context.Background(), conn, "sqlite3"); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return conn, database.New(conn)
}

// Open returns a fresh, unmigrated SQLite database.
func Open(t testing.TB) *sql.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "test.db"))
	conn, _, err := db.Open(context.Background(), db.Config{URL: "file:" + path})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
