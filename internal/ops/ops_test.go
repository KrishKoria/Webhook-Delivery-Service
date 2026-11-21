package ops_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/ops"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/testdb"
)

func setup(t *testing.T) (*ops.Ops, *database.Queries, *sql.DB) {
	t.Helper()
	conn, q := testdb.New(t)
	exec(t, conn, `INSERT INTO subscriptions (id, target_url, secret) VALUES ('s1', 'https://example.com', 'topsecret')`)
	return ops.New(conn, nil), q, conn
}

func exec(t *testing.T, conn *sql.DB, query string) {
	t.Helper()
	if _, err := conn.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestEnqueueIdempotencyKey(t *testing.T) {
	o, _, _ := setup(t)
	ctx := context.Background()
	in := ops.EnqueueInput{SubscriptionID: "s1", Payload: `{"a":1}`, EventType: "order.created", IdempotencyKey: "k1"}

	first, dup, err := o.Enqueue(ctx, in)
	if err != nil || dup {
		t.Fatalf("first enqueue: id=%s dup=%v err=%v", first, dup, err)
	}
	second, dup, err := o.Enqueue(ctx, in)
	if err != nil || !dup || second != first {
		t.Fatalf("second enqueue should return the first task: id=%s dup=%v err=%v", second, dup, err)
	}
	third, dup, err := o.Enqueue(ctx, ops.EnqueueInput{SubscriptionID: "s1", Payload: "{}"})
	if err != nil || dup || third == first {
		t.Fatalf("enqueue without key must always create: id=%s dup=%v err=%v", third, dup, err)
	}
}

func TestRetryDLQIsAtomicUnderConcurrency(t *testing.T) {
	o, q, conn := setup(t)
	ctx := context.Background()
	exec(t, conn, `INSERT INTO dead_letter_tasks (id, original_task_id, subscription_id, payload, reason, status, event_type)
		VALUES ('d1', 't0', 's1', '{}', 'boom', 'dead', 'order.created')`)

	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Go(func() { _, results[i] = o.RetryDLQ(ctx, "d1", "test") })
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ops.ErrConflict):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one retry should succeed, got %d", succeeded)
	}
	var tasks int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM delivery_tasks WHERE event_type = 'order.created'`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 {
		t.Fatalf("expected 1 requeued task carrying the event type, got %d", tasks)
	}
	if _, err := o.RetryDLQ(ctx, "missing", ""); !errors.Is(err, ops.ErrNotFound) {
		t.Fatalf("unknown id: got %v, want ErrNotFound", err)
	}
	row, err := q.GetDeadLetterTask(ctx, "d1")
	if err != nil || row.Status != "retried" {
		t.Fatalf("dlq row status = %q err=%v", row.Status, err)
	}
}

func TestUpdateSubscriptionKeepsSecretUnlessCleared(t *testing.T) {
	o, q, _ := setup(t)
	ctx := context.Background()

	if err := o.UpdateSubscription(ctx, ops.UpdateSubscriptionInput{ID: "s1", TargetURL: "https://new.example.com"}); err != nil {
		t.Fatal(err)
	}
	sub, _ := q.GetSubscription(ctx, "s1")
	if sub.Secret.String != "topsecret" || sub.TargetUrl != "https://new.example.com" {
		t.Fatalf("secret must survive an update without one: %+v", sub)
	}

	newSecret := "rotated"
	if err := o.UpdateSubscription(ctx, ops.UpdateSubscriptionInput{ID: "s1", TargetURL: sub.TargetUrl, Secret: &newSecret}); err != nil {
		t.Fatal(err)
	}
	sub, _ = q.GetSubscription(ctx, "s1")
	if sub.Secret.String != "rotated" {
		t.Fatalf("secret should be replaced: %q", sub.Secret.String)
	}

	if err := o.UpdateSubscription(ctx, ops.UpdateSubscriptionInput{ID: "s1", TargetURL: sub.TargetUrl, ClearSecret: true}); err != nil {
		t.Fatal(err)
	}
	sub, _ = q.GetSubscription(ctx, "s1")
	if sub.Secret.Valid {
		t.Fatalf("secret should be cleared")
	}

	if err := o.UpdateSubscription(ctx, ops.UpdateSubscriptionInput{ID: "nope", TargetURL: "https://x"}); !errors.Is(err, ops.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestDeleteSubscriptionCascades(t *testing.T) {
	o, _, conn := setup(t)
	ctx := context.Background()
	if _, _, err := o.Enqueue(ctx, ops.EnqueueInput{SubscriptionID: "s1", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}
	exec(t, conn, `INSERT INTO dead_letter_tasks (id, original_task_id, subscription_id, payload, reason, status) VALUES ('d1','t0','s1','{}','x','dead')`)
	exec(t, conn, `INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for) VALUES ('w1','s1','{}',CURRENT_TIMESTAMP)`)

	if err := o.DeleteSubscription(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"subscriptions", "delivery_tasks", "dead_letter_tasks", "scheduled_webhooks"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s still has %d rows", table, n)
		}
	}
	if err := o.DeleteSubscription(ctx, "s1"); !errors.Is(err, ops.ErrNotFound) {
		t.Fatalf("second delete: got %v, want ErrNotFound", err)
	}
}

func TestPauseAndRedeliver(t *testing.T) {
	o, q, _ := setup(t)
	ctx := context.Background()
	if err := o.SetPaused(ctx, "s1", true); err != nil {
		t.Fatal(err)
	}
	sub, _ := q.GetSubscription(ctx, "s1")
	if sub.Paused != 1 {
		t.Fatalf("paused = %d", sub.Paused)
	}
	if err := o.SetPaused(ctx, "nope", true); !errors.Is(err, ops.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	orig, _, _ := o.Enqueue(ctx, ops.EnqueueInput{SubscriptionID: "s1", Payload: `{"x":1}`, EventType: "e"})
	copyID, err := o.Redeliver(ctx, orig)
	if err != nil || copyID == orig {
		t.Fatalf("redeliver: id=%s err=%v", copyID, err)
	}
	task, _ := q.GetDeliveryTask(ctx, copyID)
	if task.Payload != `{"x":1}` || task.EventType.String != "e" || task.Status != "pending" {
		t.Fatalf("redelivered task mismatch: %+v", task)
	}
	if _, err := o.Redeliver(ctx, "missing"); !errors.Is(err, ops.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
