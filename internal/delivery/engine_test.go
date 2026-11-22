package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/testdb"
)

// --- helpers ---------------------------------------------------------------

func mustExec(t *testing.T, conn *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func insertSub(t *testing.T, conn *sql.DB, id, target, secret string) {
	t.Helper()
	mustExec(t, conn, `INSERT INTO subscriptions (id, target_url, secret) VALUES (?, ?, NULLIF(?, ''))`, id, target, secret)
}

func insertTask(t *testing.T, q *database.Queries, id, subID, eventType string) {
	t.Helper()
	err := q.CreateDeliveryTask(context.Background(), database.CreateDeliveryTaskParams{
		ID: id, SubscriptionID: subID, Payload: `{"n":"` + id + `"}`,
		EventType: sql.NullString{String: eventType, Valid: eventType != ""},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func getTask(t *testing.T, q *database.Queries, id string) database.DeliveryTask {
	t.Helper()
	task, err := q.GetDeliveryTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task %s: %v", id, err)
	}
	return task
}

func countRows(t *testing.T, conn *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func testWorker(conn *sql.DB, id string, cfg WorkerConfig) *Worker {
	cfg.WorkerID = id
	if cfg.HTTP.Timeout == 0 {
		cfg.HTTP = DefaultHTTPClientConfig()
		cfg.HTTP.Timeout = 5 * time.Second
		cfg.HTTP.AllowPrivateIPs = true // httptest servers listen on loopback
	}
	cfg.DisableJitter = true
	if cfg.IdleMin == 0 {
		cfg.IdleMin, cfg.IdleMax = 10*time.Millisecond, 50*time.Millisecond
	}
	return NewWorker(conn, nil, cfg)
}

func claimOne(t *testing.T, w *Worker) database.DeliveryTask {
	t.Helper()
	tasks, err := w.claimBatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("claimed %d tasks, want 1", len(tasks))
	}
	return tasks[0]
}

// runWorker starts w and returns a stop func that cancels it and waits for Start to return.
func runWorker(t *testing.T, w *Worker) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Start(ctx); close(done) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("worker did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// --- claiming --------------------------------------------------------------

func TestConcurrentClaimersClaimEachTaskExactlyOnce(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	for i := range 50 {
		insertTask(t, q, fmt.Sprintf("t%02d", i), "s1", "")
	}

	var (
		mu      sync.Mutex
		claimed = map[string]string{} // task -> worker
		dupes   []string
		wg      sync.WaitGroup
	)
	for c := range 8 {
		w := testWorker(conn, fmt.Sprintf("w%d", c), WorkerConfig{})
		wg.Go(func() {
			for {
				tasks, err := w.claimBatch(context.Background(), 3)
				if err != nil {
					t.Error(err)
					return
				}
				if len(tasks) == 0 {
					return
				}
				mu.Lock()
				for _, task := range tasks {
					if prev, ok := claimed[task.ID]; ok {
						dupes = append(dupes, task.ID+" by "+prev+" and "+w.ID())
					}
					claimed[task.ID] = w.ID()
					if task.Status != StatusInFlight || task.ClaimedBy.String != w.ID() || !task.LeaseUntil.Valid {
						t.Errorf("returned row not claimed: %+v", task)
					}
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if len(dupes) > 0 {
		t.Fatalf("double claims: %v", dupes)
	}
	if len(claimed) != 50 {
		t.Fatalf("claimed %d tasks, want 50", len(claimed))
	}
	for id, owner := range claimed {
		if got := getTask(t, q, id).ClaimedBy.String; got != owner {
			t.Fatalf("task %s claimed_by=%q, want %q", id, got, owner)
		}
	}
}

func TestExpiredLeaseIsReclaimable(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	insertTask(t, q, "t1", "s1", "")
	w1 := testWorker(conn, "w1", WorkerConfig{})
	w2 := testWorker(conn, "w2", WorkerConfig{})
	claimOne(t, w1)

	if tasks, _ := w2.claimBatch(context.Background(), 5); len(tasks) != 0 {
		t.Fatal("task with a live lease must not be reclaimed")
	}
	mustExec(t, conn, `UPDATE delivery_tasks SET lease_until = datetime('now', '-1 seconds') WHERE id = 't1'`)
	if task := claimOne(t, w2); task.ClaimedBy.String != "w2" {
		t.Fatalf("claimed_by = %q", task.ClaimedBy.String)
	}
}

func TestNullNextAttemptIsTreatedAsDue(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	insertTask(t, q, "t1", "s1", "")
	mustExec(t, conn, `UPDATE delivery_tasks SET next_attempt_at = NULL`)
	claimOne(t, testWorker(conn, "w1", WorkerConfig{}))
}

func TestRetryNotClaimableBeforeDelayButClaimableAfter(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	insertTask(t, q, "t1", "s1", "")
	// A 2s delay: with second-resolution timestamps it is due 1-2s from now.
	w := testWorker(conn, "w1", WorkerConfig{RetrySchedule: []time.Duration{2 * time.Second}})
	task := claimOne(t, w)

	res := attemptResult{Outcome: OutcomeFailedAttempt, HTTPStatus: 500, Error: "500"}
	d := w.decide(task, res)
	if d.Status != StatusPending || d.Delay != 2*time.Second {
		t.Fatalf("decision %+v", d)
	}
	if err := w.finalize(context.Background(), task, "http://example.invalid/", &res, d); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if tasks, _ := w.claimBatch(context.Background(), 1); len(tasks) != 0 {
		t.Fatal("retry claimable before its delay")
	}
	waitFor(t, 5*time.Second, "retry to become claimable", func() bool {
		tasks, err := w.claimBatch(context.Background(), 1)
		return err == nil && len(tasks) == 1
	})
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("retry claimable after only %v", elapsed)
	}
	if got := getTask(t, q, "t1"); got.AttemptCount != 1 {
		t.Fatalf("attempt_count = %d", got.AttemptCount)
	}
}

// --- finalize --------------------------------------------------------------

func TestFinalizeIsAtomic(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	insertTask(t, q, "t1", "s1", "evt")
	w := testWorker(conn, "w1", WorkerConfig{})
	task := claimOne(t, w)

	// Force the DLQ insert (the last statement) to fail with a primary-key conflict.
	mustExec(t, conn, `INSERT INTO dead_letter_tasks (id, original_task_id, subscription_id, payload, reason, status)
		VALUES ('fixed-id', 'x', 's1', '{}', 'r', 'dead')`)
	w.newID = func() string { return "fixed-id" }

	res := attemptResult{Outcome: OutcomeFailedAttempt, HTTPStatus: http.StatusGone, Error: "410 Gone"}
	err := w.finalize(context.Background(), task, "http://example.invalid/", &res, w.decide(task, res))
	if err == nil {
		t.Fatal("finalize should fail when the DLQ insert fails")
	}

	got := getTask(t, q, "t1")
	if got.Status != StatusInFlight || got.AttemptCount != 0 || got.ClaimedBy.String != "w1" {
		t.Fatalf("status change was not rolled back: %+v", got)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_logs`); n != 0 {
		t.Fatalf("delivery log was not rolled back: %d rows", n)
	}
}

func TestStaleOwnerFinalizeIsRejected(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	insertTask(t, q, "t1", "s1", "")
	w1 := testWorker(conn, "w1", WorkerConfig{})
	w2 := testWorker(conn, "w2", WorkerConfig{})
	task := claimOne(t, w1)

	// w1's lease expires and w2 reclaims the task.
	mustExec(t, conn, `UPDATE delivery_tasks SET lease_until = datetime('now', '-1 seconds') WHERE id = 't1'`)
	claimOne(t, w2)

	res := attemptResult{Outcome: OutcomeSuccess, HTTPStatus: 200}
	err := w1.finalize(context.Background(), task, "http://example.invalid/", &res, decision{Status: StatusDelivered})
	if !errors.Is(err, errStaleOwner) {
		t.Fatalf("want errStaleOwner, got %v", err)
	}
	got := getTask(t, q, "t1")
	if got.Status != StatusInFlight || got.ClaimedBy.String != "w2" || got.AttemptCount != 0 {
		t.Fatalf("stale finalize changed the task: %+v", got)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_logs`); n != 0 {
		t.Fatalf("stale finalize wrote %d logs", n)
	}
	// Heartbeats and releases are guarded the same way.
	if n, err := q.ExtendTaskLeaseFor(context.Background(), database.ExtendTaskLeaseForParams{
		LeaseSeconds: 60, ID: "t1", WorkerID: w1.owner(),
	}); err != nil || n != 0 {
		t.Fatalf("stale heartbeat: n=%d err=%v", n, err)
	}
}

// --- end to end --------------------------------------------------------------

func TestEndToEndRetriesThenDelivers(t *testing.T) {
	conn, q := testdb.New(t)
	var (
		calls   atomic.Int32
		mu      sync.Mutex
		lastReq *http.Request
		lastBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		lastReq, lastBody = r, body
		mu.Unlock()
		if calls.Add(1) <= 2 {
			http.Error(w, "upstream exploded", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	insertSub(t, conn, "s1", srv.URL+"/hook", "s3cret")
	insertTask(t, q, "t1", "s1", "order.created")

	w := testWorker(conn, "w1", WorkerConfig{RetrySchedule: []time.Duration{0}})
	stop := runWorker(t, w)
	waitFor(t, 10*time.Second, "task delivered", func() bool {
		return getTask(t, q, "t1").Status == StatusDelivered
	})
	stop()

	task := getTask(t, q, "t1")
	if task.AttemptCount != 3 || task.ClaimedBy.Valid || task.LeaseUntil.Valid {
		t.Fatalf("final task: %+v", task)
	}
	logs, err := q.ListDeliveryLogsForTask(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("got %d logs, want 3", len(logs))
	}
	for i, l := range logs {
		wantOutcome := OutcomeFailedAttempt
		wantStatus := int64(500)
		if i == 2 {
			wantOutcome, wantStatus = OutcomeSuccess, 200
		}
		if l.AttemptNumber != int64(i+1) || l.Outcome != wantOutcome || l.HttpStatus.Int64 != wantStatus {
			t.Fatalf("log %d: %+v", i, l)
		}
	}
	if !logs[0].ErrorDetails.Valid || logs[0].ErrorDetails.String != "500 Internal Server Error: upstream exploded" {
		t.Fatalf("error details = %q", logs[0].ErrorDetails.String)
	}

	mu.Lock()
	defer mu.Unlock()
	h := lastReq.Header
	if h.Get("webhook-id") != "t1" || h.Get("X-Event-Type") != "order.created" ||
		h.Get("User-Agent") != UserAgent || h.Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %v", h)
	}
	ts, err := strconv.ParseInt(h.Get("webhook-timestamp"), 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)) > time.Minute {
		t.Fatalf("webhook-timestamp %q", h.Get("webhook-timestamp"))
	}
	if want := Sign("s3cret", "t1", ts, lastBody); h.Get("webhook-signature") != want {
		t.Fatalf("signature %q, want %q", h.Get("webhook-signature"), want)
	}
	if string(lastBody) != `{"n":"t1"}` {
		t.Fatalf("body %q", lastBody)
	}
}

func TestNoSignatureWithoutSecret(t *testing.T) {
	conn, q := testdb.New(t)
	got := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got <- r.Header }))
	defer srv.Close()
	insertSub(t, conn, "s1", srv.URL, "")
	insertTask(t, q, "t1", "s1", "")
	runWorker(t, testWorker(conn, "w1", WorkerConfig{}))
	h := <-got
	if h.Get("webhook-signature") != "" || h.Get("X-Event-Type") != "" {
		t.Fatalf("unexpected headers: %v", h)
	}
}

func TestGoneDeadLettersImmediately(t *testing.T) {
	conn, q := testdb.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "endpoint removed", http.StatusGone)
	}))
	defer srv.Close()
	insertSub(t, conn, "s1", srv.URL, "")
	insertTask(t, q, "t1", "s1", "invoice.paid")

	stop := runWorker(t, testWorker(conn, "w1", WorkerConfig{}))
	waitFor(t, 10*time.Second, "task failed", func() bool { return getTask(t, q, "t1").Status == StatusFailed })
	stop()

	dlq, err := q.ListDeadLetterTasksForSubscription(context.Background(), database.ListDeadLetterTasksForSubscriptionParams{
		SubscriptionID: "s1", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dlq) != 1 {
		t.Fatalf("got %d DLQ rows", len(dlq))
	}
	d := dlq[0]
	if d.Status != "dead" || d.EventType.String != "invoice.paid" || d.Reason != ReasonGone ||
		d.OriginalTaskID != "t1" || d.TargetUrl.String != srv.URL || d.AttemptCount != 1 || d.Payload != `{"n":"t1"}` {
		t.Fatalf("dlq row: %+v", d)
	}
	if d.ErrorDetails.String != "410 Gone: endpoint removed" {
		t.Fatalf("error details %q", d.ErrorDetails.String)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_logs WHERE delivery_task_id='t1'`); n != 1 {
		t.Fatalf("logs = %d, want 1", n)
	}
}

func TestMaxAttemptsDeadLetters(t *testing.T) {
	conn, q := testdb.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	insertSub(t, conn, "s1", srv.URL, "")
	insertTask(t, q, "t1", "s1", "")

	stop := runWorker(t, testWorker(conn, "w1", WorkerConfig{MaxAttempts: 2, RetrySchedule: []time.Duration{0}}))
	waitFor(t, 10*time.Second, "task failed", func() bool { return getTask(t, q, "t1").Status == StatusFailed })
	stop()

	if task := getTask(t, q, "t1"); task.AttemptCount != 2 {
		t.Fatalf("attempt_count = %d", task.AttemptCount)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM dead_letter_tasks WHERE original_task_id='t1' AND status='dead' AND reason=?`, ReasonMaxAttempts); n != 1 {
		t.Fatalf("dlq rows = %d", n)
	}
}

func TestPausedSubscriptionDefersWithoutCountingAttempt(t *testing.T) {
	conn, q := testdb.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("paused subscription must not be called")
	}))
	defer srv.Close()
	insertSub(t, conn, "s1", srv.URL, "")
	mustExec(t, conn, `UPDATE subscriptions SET paused = 1`)
	insertTask(t, q, "t1", "s1", "")
	w := testWorker(conn, "w1", WorkerConfig{})

	w.process(context.Background(), claimOne(t, w))

	task := getTask(t, q, "t1")
	if task.Status != StatusPending || task.AttemptCount != 0 || task.ClaimedBy.Valid {
		t.Fatalf("task after pause: %+v", task)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_tasks WHERE id='t1' AND next_attempt_at > datetime('now', '50 seconds')`); n != 1 {
		t.Fatal("paused task should be deferred by ~60s")
	}
}

func TestMissingSubscriptionIsDeadLetteredNotRetried(t *testing.T) {
	conn, q := testdb.New(t)
	// Orphaned rows can only predate foreign-key enforcement, so create one with it off.
	mustExec(t, conn, `PRAGMA foreign_keys = OFF`)
	insertTask(t, q, "t1", "ghost", "evt")
	mustExec(t, conn, `PRAGMA foreign_keys = ON`)
	w := testWorker(conn, "w1", WorkerConfig{})

	w.process(context.Background(), claimOne(t, w))

	task := getTask(t, q, "t1")
	if task.Status != StatusFailed || task.ClaimedBy.Valid {
		t.Fatalf("orphaned task should be failed, got %+v", task)
	}
	if tasks, _ := w.claimBatch(context.Background(), 5); len(tasks) != 0 {
		t.Fatal("orphaned task must not be claimable again")
	}
}

func TestMissingSubscriptionDLQRowWhenAllowed(t *testing.T) {
	conn, q := testdb.New(t)
	mustExec(t, conn, `PRAGMA foreign_keys = OFF`) // e.g. an engine without FK enforcement
	insertTask(t, q, "t1", "ghost", "evt")
	w := testWorker(conn, "w1", WorkerConfig{})

	w.process(context.Background(), claimOne(t, w))

	if n := countRows(t, conn, `SELECT COUNT(*) FROM dead_letter_tasks WHERE original_task_id='t1' AND reason=? AND status='dead' AND event_type='evt'`,
		ReasonSubscriptionNotFound); n != 1 {
		t.Fatalf("dlq rows = %d", n)
	}
}

func TestHeartbeatKeepsLeaseDuringSlowDelivery(t *testing.T) {
	conn, q := testdb.New(t)
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(3 * time.Second)
	}))
	defer srv.Close()
	insertSub(t, conn, "s1", srv.URL, "")
	insertTask(t, q, "t1", "s1", "")

	// A 2s lease would expire mid-request without the heartbeat (every ~667ms).
	w1 := testWorker(conn, "w1", WorkerConfig{Lease: 2 * time.Second})
	w2 := testWorker(conn, "w2", WorkerConfig{})
	stop := runWorker(t, w1)
	<-started
	for range 14 {
		if tasks, _ := w2.claimBatch(context.Background(), 1); len(tasks) != 0 {
			t.Fatal("task was reclaimed while its owner was still delivering")
		}
		time.Sleep(200 * time.Millisecond)
	}
	waitFor(t, 5*time.Second, "delivery", func() bool { return getTask(t, q, "t1").Status == StatusDelivered })
	stop()
}

func TestShutdownFinishesInFlightAndClaimsOnlyFreeSlots(t *testing.T) {
	conn, q := testdb.New(t)
	started := make(chan string, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.Header.Get("webhook-id")
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	insertSub(t, conn, "s1", srv.URL, "k")
	for i := range 3 {
		insertTask(t, q, fmt.Sprintf("t%d", i), "s1", "")
	}

	stop := runWorker(t, testWorker(conn, "w1", WorkerConfig{Concurrency: 1}))
	first := <-started
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_tasks WHERE status='in_flight'`); n != 1 {
		t.Fatalf("with concurrency 1, %d tasks are in flight", n)
	}
	stop() // must wait for the in-flight delivery

	if s := getTask(t, q, first).Status; s != StatusDelivered {
		t.Fatalf("in-flight delivery %s did not finish: %s", first, s)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_tasks WHERE status='in_flight' OR claimed_by IS NOT NULL`); n != 0 {
		t.Fatalf("%d tasks left claimed after shutdown", n)
	}
	if n := countRows(t, conn, `SELECT COUNT(*) FROM delivery_tasks WHERE status='pending' AND attempt_count=0`); n != 2 {
		t.Fatalf("pending tasks after shutdown = %d, want 2", n)
	}
}

func TestReleaseOnShutdownOfQueuedTask(t *testing.T) {
	conn, q := testdb.New(t)
	insertSub(t, conn, "s1", "http://example.invalid/", "")
	insertTask(t, q, "t1", "s1", "")
	w := testWorker(conn, "w1", WorkerConfig{})
	task := claimOne(t, w)

	w.release(context.Background(), task, 0, "shutdown")

	got := getTask(t, q, "t1")
	if got.Status != StatusPending || got.ClaimedBy.Valid || got.LeaseUntil.Valid || got.AttemptCount != 0 {
		t.Fatalf("released task: %+v", got)
	}
	claimOne(t, w) // immediately claimable again
}
