package delivery

import (
	"context"
	"database/sql"
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

// runOnce processes due rows, batch after batch, until fewer than a full batch is due.
func (w *ScheduledWorker) runOnce(ctx context.Context) {
	for ctx.Err() == nil {
		n, err := w.processBatch(context.WithoutCancel(ctx))
		if err != nil {
			w.log.Error("process scheduled webhooks", "err", err)
			return
		}
		if n < scheduledBatchSize {
			return
		}
	}
}

// processBatch claims up to one batch of due rows and fires each one.
func (w *ScheduledWorker) processBatch(ctx context.Context) (int, error) {
	rows, err := w.claimDue(ctx, scheduledBatchSize)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		w.fire(ctx, row)
	}
	return len(rows), nil
}

// claimDue atomically moves up to limit due rows from pending to enqueued and
// returns the rows this call won.
func (w *ScheduledWorker) claimDue(ctx context.Context, limit int) ([]database.ScheduledWebhook, error) {
	ctx, cancel := context.WithTimeout(ctx, dbOpTimeout)
	defer cancel()
	ids, err := w.q.SelectDueScheduledWebhookIDs(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("select due scheduled webhooks: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := w.q.ClaimScheduledWebhooks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("claim scheduled webhooks: %w", err)
	}
	return rows, nil
}

// fire enqueues the delivery task and the next occurrence in one transaction.
// If that fails, the claimed row is marked failed so it is visible to operators.
func (w *ScheduledWorker) fire(ctx context.Context, row database.ScheduledWebhook) {
	log := w.log.With("scheduled_id", row.ID, "subscription_id", row.SubscriptionID)
	recurrence := row.Recurrence.String
	next, skipped, recurring := nextOccurrence(row.ScheduledFor, recurrence, w.now())
	if !recurring && recurrence != "" && recurrence != "none" {
		log.Warn("unknown recurrence; treating as one-off", "recurrence", recurrence)
	}

	taskID := uuid.NewString()
	txCtx, cancel := context.WithTimeout(ctx, dbOpTimeout)
	defer cancel()
	err := db.WithTx(txCtx, w.db, func(q *database.Queries) error {
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
		log.Error("fire scheduled webhook", "err", err)
		markCtx, markCancel := context.WithTimeout(ctx, dbOpTimeout)
		defer markCancel()
		if mErr := w.q.UpdateScheduledWebhookStatus(markCtx, database.UpdateScheduledWebhookStatusParams{
			Status: ScheduledFailed,
			ID:     row.ID,
		}); mErr != nil {
			log.Error("mark scheduled webhook failed", "err", mErr)
		}
		return
	}

	attrs := []any{"task_id", taskID}
	if recurring {
		attrs = append(attrs, "next_occurrence", next.UTC())
	}
	if skipped > 0 {
		log.Warn("missed recurrences skipped", append(attrs, "skipped", skipped)...)
	}
	log.Info("scheduled webhook enqueued", attrs...)
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
