package migrate_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/migrate"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/sql/schema"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/testdb"
	"github.com/pressly/goose/v3"
)

func TestUpIsIdempotent(t *testing.T) {
	conn, _ := testdb.New(t)
	if err := migrate.Up(context.Background(), conn, goose.DialectSQLite3); err != nil {
		t.Fatalf("second Up should be a no-op: %v", err)
	}
}

// Rows written before migration 009 used RFC3339 ('T' separator, 'Z' suffix).
// As text, '2026-01-01T…' sorts after '2026-01-01 …', so a due retry looked like
// it was in the future until UTC midnight. 009 must rewrite them.
func TestMigration009NormalizesLegacyTimestamps(t *testing.T) {
	ctx := context.Background()
	conn := testdb.Open(t)
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, schema.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}

	mustExec(t, conn, `INSERT INTO subscriptions (id, target_url) VALUES ('s1', 'https://example.com')`)
	// A retry that became due one minute ago, stored in the legacy format.
	mustExec(t, conn, `INSERT INTO delivery_tasks (id, subscription_id, payload, status, attempt_count, next_attempt_at)
		VALUES ('t1', 's1', '{}', 'pending', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 minute'))`)
	mustExec(t, conn, `INSERT INTO dead_letter_tasks (id, original_task_id, subscription_id, payload, reason, status)
		VALUES ('d1', 't0', 's1', '{}', 'boom', 'pending')`)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}

	q := database.New(conn)
	id, err := q.GetNextClaimableTaskId(ctx)
	if err != nil {
		t.Fatalf("legacy-format retry should be claimable after 009: %v", err)
	}
	if id != "t1" {
		t.Fatalf("claimable id = %q, want t1", id)
	}

	dlq, err := q.GetDeadLetterTask(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if dlq.Status != "dead" {
		t.Fatalf("dlq status = %q, want dead", dlq.Status)
	}
}

func TestRetryClaimableAfterDelayNotBefore(t *testing.T) {
	ctx := context.Background()
	conn, q := testdb.New(t)
	mustExec(t, conn, `INSERT INTO subscriptions (id, target_url) VALUES ('s1', 'https://example.com')`)
	if err := q.CreateDeliveryTask(ctx, database.CreateDeliveryTaskParams{ID: "t1", SubscriptionID: "s1", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}

	if err := q.UpdateDeliveryTaskNextAttemptAt(ctx, database.UpdateDeliveryTaskNextAttemptAtParams{ID: "t1", DelaySeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetNextClaimableTaskId(ctx); err == nil {
		t.Fatal("task should not be claimable before its backoff elapses")
	}

	if err := q.UpdateDeliveryTaskNextAttemptAt(ctx, database.UpdateDeliveryTaskNextAttemptAtParams{ID: "t1", DelaySeconds: -1}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetNextClaimableTaskId(ctx); err != nil {
		t.Fatalf("task should be claimable once its backoff has elapsed: %v", err)
	}

	task, err := q.GetDeliveryTask(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if task.CreatedAt.IsZero() || !task.NextAttemptAt.Valid {
		t.Fatalf("timestamps should scan into time.Time: created=%v next=%v", task.CreatedAt, task.NextAttemptAt)
	}
}

func mustExec(t *testing.T, conn *sql.DB, query string) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
