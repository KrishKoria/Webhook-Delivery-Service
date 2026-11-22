package delivery

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/testdb"
)

// --- recurrence math ---------------------------------------------------------

func TestNextOccurrence(t *testing.T) {
	ts := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	tests := []struct {
		name        string
		base, now   string
		recurrence  string
		want        string
		wantSkipped int
		wantRecur   bool
	}{
		{"daily on time", "2026-01-01T10:00:00Z", "2026-01-01T10:00:05Z", "daily", "2026-01-02T10:00:00Z", 0, true},
		{"daily fast-forward", "2026-01-01T10:00:00Z", "2026-01-05T09:00:00Z", "daily", "2026-01-05T10:00:00Z", 3, true},
		{"weekly fast-forward", "2026-01-01T10:00:00Z", "2026-01-20T00:00:00Z", "weekly", "2026-01-22T10:00:00Z", 2, true},
		{"monthly clamps to Feb 28", "2027-01-31T08:00:00Z", "2027-01-31T08:00:01Z", "monthly", "2027-02-28T08:00:00Z", 0, true},
		{"monthly clamps to leap Feb 29", "2028-01-31T08:00:00Z", "2028-01-31T08:00:01Z", "monthly", "2028-02-29T08:00:00Z", 0, true},
		// Occurrences come from base, so the day returns to 31 after a short month.
		{"monthly fast-forward does not drift", "2026-01-31T08:00:00Z", "2026-04-15T00:00:00Z", "monthly", "2026-04-30T08:00:00Z", 2, true},
		{"monthly fast-forward to 31st", "2026-01-31T08:00:00Z", "2026-05-01T00:00:00Z", "monthly", "2026-05-31T08:00:00Z", 3, true},
		{"none is one-off", "2026-01-01T10:00:00Z", "2026-02-01T00:00:00Z", "none", "", 0, false},
		{"unknown is one-off", "2026-01-01T10:00:00Z", "2026-02-01T00:00:00Z", "hourly", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, skipped, recurring := nextOccurrence(ts(tt.base), tt.recurrence, ts(tt.now))
			if recurring != tt.wantRecur {
				t.Fatalf("recurring = %v, want %v", recurring, tt.wantRecur)
			}
			if !recurring {
				return
			}
			if !next.Equal(ts(tt.want)) || skipped != tt.wantSkipped {
				t.Fatalf("next = %s (skipped %d), want %s (skipped %d)", next.Format(time.RFC3339), skipped, tt.want, tt.wantSkipped)
			}
			if !next.After(ts(tt.now)) {
				t.Fatalf("next %s is not after now %s", next, tt.now)
			}
		})
	}
}

// --- scheduler -----------------------------------------------------------------

func TestScheduledClaimIsExclusive(t *testing.T) {
	conn, _ := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	const n = 40
	for i := range n {
		mustExec(t, conn, `INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for, recurrence)
			VALUES (?, 's1', '{}', datetime('now', '-1 minute'), 'none')`, fmt.Sprintf("sch-%02d", i))
	}
	// A future row must never be claimed.
	mustExec(t, conn, `INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for, recurrence)
		VALUES ('future', 's1', '{}', datetime('now', '+1 hour'), 'none')`)

	var (
		mu   sync.Mutex
		seen = map[string]int{}
		wg   sync.WaitGroup
	)
	for range 4 {
		w := NewScheduledWorker(conn)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				rows, err := w.claimDue(context.Background(), 7)
				if err != nil {
					t.Error(err)
					return
				}
				if len(rows) == 0 {
					return
				}
				mu.Lock()
				for _, r := range rows {
					seen[r.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(seen) != n {
		t.Fatalf("claimed %d distinct rows, want %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("row %s claimed %d times", id, c)
		}
	}
	if _, ok := seen["future"]; ok {
		t.Fatal("future row was claimed")
	}
}

func TestScheduledFireEnqueuesTaskAndNextOccurrence(t *testing.T) {
	conn, _ := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	// Three full days and an hour overdue: three missed occurrences are skipped.
	mustExec(t, conn, `INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for, recurrence)
		VALUES ('daily', 's1', '{"k":"daily"}', datetime('now', '-3 days', '-1 hour'), 'daily')`)
	mustExec(t, conn, `INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for, recurrence)
		VALUES ('once', 's1', '{"k":"once"}', datetime('now', '-1 minute'), 'none')`)

	w := NewScheduledWorker(conn)
	n, err := w.processBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("processed %d rows, want 2", n)
	}

	if c := countRows(t, conn, `SELECT COUNT(*) FROM delivery_tasks WHERE subscription_id='s1' AND status='pending'`); c != 2 {
		t.Fatalf("enqueued %d delivery tasks, want 2", c)
	}
	if c := countRows(t, conn, `SELECT COUNT(*) FROM scheduled_webhooks WHERE id IN ('daily','once') AND status='enqueued'`); c != 2 {
		t.Fatalf("%d fired rows marked enqueued, want 2", c)
	}
	// Exactly one next occurrence: pending, in the future, canonical UTC text, ~23h away.
	if c := countRows(t, conn, `SELECT COUNT(*) FROM scheduled_webhooks
		WHERE status='pending' AND recurrence='daily' AND payload='{"k":"daily"}'
		  AND scheduled_for > CURRENT_TIMESTAMP
		  AND scheduled_for <= datetime('now', '+1 day')
		  AND scheduled_for NOT LIKE '%T%'`); c != 1 {
		t.Fatalf("next daily occurrences = %d, want 1", c)
	}
	if c := countRows(t, conn, `SELECT COUNT(*) FROM scheduled_webhooks WHERE status='pending' AND recurrence='none'`); c != 0 {
		t.Fatalf("one-off row produced %d follow-ups", c)
	}

	// A second pass finds nothing due.
	if n, err := w.processBatch(context.Background()); err != nil || n != 0 {
		t.Fatalf("second pass processed %d (err %v), want 0", n, err)
	}
}

// --- cleanup -------------------------------------------------------------------

func TestCleanupBatchedRetention(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")

	task := func(id, status, lastAttempt string) {
		mustExec(t, conn, `INSERT INTO delivery_tasks (id, subscription_id, payload, status, last_attempt_at)
			VALUES (?, 's1', '{}', ?, datetime('now', ?))`, id, status, lastAttempt)
	}
	logRow := func(id, taskID, age string) {
		mustExec(t, conn, `INSERT INTO delivery_logs (id, delivery_task_id, subscription_id, target_url, timestamp, attempt_number, outcome)
			VALUES (?, ?, 's1', 'http://example.invalid/', datetime('now', ?), 1, 'success')`, id, taskID, age)
	}

	// Finished and older than 1 day: deleted along with their logs.
	for _, id := range []string{"old-d1", "old-d2", "old-d3"} {
		task(id, "delivered", "-2 days")
		logRow("log-"+id, id, "-1 minute") // recent log, removed with its task
	}
	task("old-f1", "failed", "-2 days")
	// Kept: finished but recent, and unfinished but old.
	task("new-d1", "delivered", "-1 hour")
	task("old-p1", "pending", "-5 days")
	// Logs on a kept task: 5 older than the 1h retention (deleted), 1 recent (kept).
	for _, id := range []string{"l1", "l2", "l3", "l4", "l5"} {
		logRow(id, "new-d1", "-3 hours")
	}
	logRow("l-recent", "new-d1", "-1 minute")

	dlq := func(id, status, lastAttempt string) {
		mustExec(t, conn, `INSERT INTO dead_letter_tasks (id, original_task_id, subscription_id, payload, reason, status, last_attempt_at)
			VALUES (?, 'x', 's1', '{}', 'test', ?, datetime('now', ?))`, id, status, lastAttempt)
	}
	dlq("dlq-old-retried", "retried", "-40 days")
	dlq("dlq-new-retried", "retried", "-1 day")
	dlq("dlq-old-dead", "dead", "-40 days")

	// BatchSize 2 forces several select/delete rounds for both tasks and logs.
	w := NewCleanupWorker(conn, CleanupConfig{LogRetentionHours: 1, TaskRetentionDays: 1, DLQRetriedRetentionDays: 30, BatchSize: 2})
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := map[string]int{
		`SELECT COUNT(*) FROM delivery_tasks WHERE id LIKE 'old-d%' OR id = 'old-f1'`:           0,
		`SELECT COUNT(*) FROM delivery_tasks WHERE id IN ('new-d1','old-p1')`:                   2,
		`SELECT COUNT(*) FROM delivery_logs WHERE delivery_task_id LIKE 'old-%'`:                0,
		`SELECT COUNT(*) FROM delivery_logs WHERE id IN ('l1','l2','l3','l4','l5')`:             0,
		`SELECT COUNT(*) FROM delivery_logs WHERE id = 'l-recent'`:                              1,
		`SELECT COUNT(*) FROM dead_letter_tasks WHERE id = 'dlq-old-retried'`:                   0,
		`SELECT COUNT(*) FROM dead_letter_tasks WHERE id IN ('dlq-new-retried','dlq-old-dead')`: 2,
	}
	for query, n := range want {
		if got := countRows(t, conn, query); got != n {
			t.Errorf("%s = %d, want %d", query, got, n)
		}
	}

	// Idempotent: a second run deletes nothing more.
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetDeliveryTask(context.Background(), "new-d1"); err != nil {
		t.Fatalf("kept task vanished: %v", err)
	}
}
