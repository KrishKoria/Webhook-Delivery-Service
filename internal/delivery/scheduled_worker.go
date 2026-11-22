package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/google/uuid"
)

// Scheduled webhook statuses. Fired rows become 'enqueued' (older rows may say
// 'delivered', which meant the same thing).
const (
	ScheduledPending  = "pending"
	ScheduledEnqueued = "enqueued"
	ScheduledFailed   = "failed"
)

const (
	scheduledPollInterval = 10 * time.Second
	scheduledBatchSize    = 50
	// maxFastForward bounds the monthly fast-forward loop (100 years of months).
	maxFastForward = 1200
)

// ScheduledWorker turns due scheduled_webhooks rows into delivery tasks.
type ScheduledWorker struct {
	db       *sql.DB
	q        *database.Queries
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// NewScheduledWorker builds the scheduler; it needs conn for per-row transactions.
func NewScheduledWorker(conn *sql.DB) *ScheduledWorker {
	return &ScheduledWorker{
		db:       conn,
		q:        database.New(conn),
		log:      slog.Default().With("component", "scheduled_worker"),
		interval: scheduledPollInterval,
		now:      time.Now,
	}
}

// Start polls every 10s until ctx is cancelled. A batch in progress finishes
// before Start returns.
func (w *ScheduledWorker) Start(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		w.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// runOnce processes due rows, batch after batch, while every row in a full
// batch fires. A failed or contended row ends the pass so a persistent error
// cannot spin; it stays pending and is retried on the next tick.
func (w *ScheduledWorker) runOnce(ctx context.Context) {
	for ctx.Err() == nil {
		fired, err := w.processBatch(context.WithoutCancel(ctx))
		if err != nil {
			w.log.Error("process scheduled webhooks", "err", err)
			return
		}
		if fired < scheduledBatchSize {
			return
		}
	}
}

// processBatch fires up to one batch of due rows and returns how many it fired.
func (w *ScheduledWorker) processBatch(ctx context.Context) (int, error) {
	ids, err := w.dueIDs(ctx, scheduledBatchSize)
	if err != nil {
		return 0, err
	}
	fired := 0
	for _, id := range ids {
		if w.fire(ctx, id) {
			fired++
		}
	}
	return fired, nil
}

// dueIDs lists up to limit pending rows whose time has come.
func (w *ScheduledWorker) dueIDs(ctx context.Context, limit int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbOpTimeout)
	defer cancel()
	ids, err := w.q.SelectDueScheduledWebhookIDs(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("select due scheduled webhooks: %w", err)
	}
	return ids, nil
}

// fire claims one row and enqueues its delivery task and next occurrence in a
// single transaction. Any failure rolls the claim back, leaving the row pending
// for the next tick, so a transient error can neither lose an occurrence nor
// end a recurring chain. It reports whether this call fired the row.
func (w *ScheduledWorker) fire(ctx context.Context, id string) bool {
	log := w.log.With("scheduled_id", id)
	taskID := uuid.NewString()
	var (
		row                database.ScheduledWebhook
		next               time.Time
		skipped            int
		recurring, claimed bool
	)
	txCtx, cancel := context.WithTimeout(ctx, dbOpTimeout)
	defer cancel()
	err := db.WithTx(txCtx, w.db, func(q *database.Queries) error {
		var err error
		row, err = q.ClaimScheduledWebhook(txCtx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil // another scheduler won it, or it was cancelled
		}
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		claimed = true
		next, skipped, recurring = nextOccurrence(row.ScheduledFor, row.Recurrence.String, w.now())
		if err := q.CreateDeliveryTask(txCtx, database.CreateDeliveryTaskParams{
			ID:             taskID,
			SubscriptionID: row.SubscriptionID,
			Payload:        row.Payload,
		}); err != nil {
			return fmt.Errorf("enqueue delivery task: %w", err)
		}
		if !recurring {
			return nil
		}
		if err := q.CreateScheduledWebhook(txCtx, database.CreateScheduledWebhookParams{
			ID:             uuid.NewString(),
			SubscriptionID: row.SubscriptionID,
			Payload:        row.Payload,
			ScheduledFor:   next.UTC().Format(time.RFC3339),
			Recurrence:     row.Recurrence,
		}); err != nil {
			return fmt.Errorf("schedule next occurrence: %w", err)
		}
		return nil
	})
	if err != nil {
		log.Error("fire scheduled webhook; will retry next tick", "err", err)
		return false
	}
	if !claimed {
		return false
	}

	log = log.With("subscription_id", row.SubscriptionID)
	if rec := row.Recurrence.String; !recurring && rec != "" && rec != "none" {
		log.Warn("unknown recurrence; treated as one-off", "recurrence", rec)
	}
	attrs := []any{"task_id", taskID}
	if recurring {
		attrs = append(attrs, "next_occurrence", next.UTC())
	}
	if skipped > 0 {
		log.Warn("missed recurrences skipped", append(attrs, "skipped", skipped)...)
	}
	log.Info("scheduled webhook enqueued", attrs...)
	return true
}

// nextOccurrence returns the first occurrence of recurrence after base that is
// strictly after now, and how many due occurrences in between were skipped.
// recurring is false for "none", "" and unknown values.
//
// Monthly recurrence clamps the day of month (Jan 31 -> Feb 28/29 -> Mar 31):
// occurrences are computed from base, not chained, so a fast-forward never drifts.
func nextOccurrence(base time.Time, recurrence string, now time.Time) (next time.Time, skipped int, recurring bool) {
	base = base.UTC()
	var step func(k int) time.Time
	switch recurrence {
	case "daily":
		step = func(k int) time.Time { return base.AddDate(0, 0, k) }
	case "weekly":
		step = func(k int) time.Time { return base.AddDate(0, 0, 7*k) }
	case "monthly":
		step = func(k int) time.Time { return addMonthsClamped(base, k) }
	default:
		return time.Time{}, 0, false
	}

	k := 1
	// Daily and weekly have fixed lengths in UTC, so jump close to now directly.
	if recurrence != "monthly" && now.After(base) {
		period := 24 * time.Hour
		if recurrence == "weekly" {
			period *= 7
		}
		k = max(int(now.Sub(base)/period), 1)
	}
	for i := 0; i < maxFastForward && !step(k).After(now); i++ {
		k++
	}
	return step(k), k - 1, true
}

// addMonthsClamped adds n months to t, clamping the day to the target month's length.
func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}
