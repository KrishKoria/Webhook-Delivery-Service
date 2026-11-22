// Package migrate applies the embedded goose migrations.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/sql/schema"
	"github.com/pressly/goose/v3"
)

// Up applies all pending migrations and logs each one that ran.
func Up(ctx context.Context, conn *sql.DB, dialect goose.Dialect) error {
	provider, err := goose.NewProvider(dialect, conn, schema.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		slog.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	slog.Info("database schema up to date", "version", version)
	return nil
}
