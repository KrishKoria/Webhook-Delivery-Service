// Package db opens the application database and provides transaction helpers.
//
// Remote databases (turso://, libsql://, https://) use Turso's pure-Go
// tursogo-serverless driver. file: URLs use modernc.org/sqlite for local
// development and tests. Both drivers scan DATETIME columns into time.Time.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/pressly/goose/v3"
	turso "turso.tech/database/tursogo-serverless"
	_ "modernc.org/sqlite"
)

// DB is the process-wide database handle, set by Init.
var DB *sql.DB

// Dialect is the goose dialect matching the opened database.
var Dialect goose.Dialect

// Config describes how to reach the database.
type Config struct {
	URL       string
	AuthToken string
}

// Open opens and pings a database for cfg, returning the handle and its goose dialect.
func Open(ctx context.Context, cfg Config) (*sql.DB, goose.Dialect, error) {
	if cfg.URL == "" {
		return nil, "", errors.New("TURSO_DATABASE_URL not set")
	}

	var (
		conn    *sql.DB
		dialect goose.Dialect
	)
	if IsLocalURL(cfg.URL) {
		dsn := cfg.URL
		if !strings.Contains(dsn, "_pragma=") {
			sep := "?"
			if strings.Contains(dsn, "?") {
				sep = "&"
			}
			// Foreign keys are enforced to match the remote Turso engine.
			dsn += sep + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
		}
		var err error
		conn, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, "", fmt.Errorf("open local sqlite db: %w", err)
		}
		// SQLite allows one writer; a single connection avoids SQLITE_BUSY churn.
		conn.SetMaxOpenConns(1)
		dialect = goose.DialectSQLite3
	} else {
		if cfg.AuthToken == "" {
			return nil, "", errors.New("TURSO_AUTH_TOKEN not set for remote database URL")
		}
		conn = sql.OpenDB(turso.NewConnector(cfg.URL, cfg.AuthToken))
		conn.SetMaxOpenConns(10)
		conn.SetMaxIdleConns(5)
		conn.SetConnMaxIdleTime(5 * time.Minute)
		dialect = goose.DialectTurso
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("ping db: %w", err)
	}
	return conn, dialect, nil
}

// Init opens the database and stores it in the package-level DB and Dialect.
func Init(ctx context.Context, cfg Config) error {
	conn, dialect, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	DB, Dialect = conn, dialect
	return nil
}

// IsLocalURL reports whether url points at a local SQLite file.
func IsLocalURL(url string) bool {
	return strings.HasPrefix(url, "file:")
}

// WithTx runs fn inside a transaction, committing on success and rolling back on error.
func WithTx(ctx context.Context, conn *sql.DB, fn func(q *database.Queries) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(database.New(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
