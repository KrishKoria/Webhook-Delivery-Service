// Package delivery runs the background engines: the webhook delivery worker,
// the scheduled-webhook scheduler, and retention cleanup.
//
// Delivery task lifecycle:
//
//	pending --claim--> in_flight --2xx--> delivered
//	                       |------retryable failure--> pending (next_attempt_at = now + backoff)
//	                       |------final failure / 410--> failed (+ dead_letter_tasks row, status 'dead')
//
// A single claimer goroutine batch-claims due tasks (SelectClaimableTaskIDs then
// ClaimTasks) and hands them to a fixed pool of delivery goroutines. A claim
// takes a lease; a heartbeat extends it while the HTTP request runs. Every
// later write is guarded by claimed_by, so if a lease expires and another
// worker reclaims the task, the original worker's result is discarded.
package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/google/uuid"
)

// Task statuses.
const (
	StatusPending   = "pending"
	StatusInFlight  = "in_flight"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
)

// Delivery log outcomes (delivery_logs.outcome).
const (
	OutcomeSuccess       = "success"
	OutcomeFailedAttempt = "failed_attempt"
)

// Dead-letter reasons (dead_letter_tasks.reason).
const (
	ReasonMaxAttempts          = "max_attempts_exceeded"
	ReasonGone                 = "gone"
	ReasonSubscriptionNotFound = "subscription_not_found"
)

// UserAgent is sent with every delivery.
const UserAgent = "WebhookDeliveryService/1.0"

const (
	defaultConcurrency = 10
	defaultIdleMin     = 250 * time.Millisecond
	defaultIdleMax     = 2 * time.Second
	defaultMaxAttempts = 6
	minLease           = 60 * time.Second
	maxRetryAfter      = time.Hour
	pausedDeferral     = 60 * time.Second
	lookupErrDeferral  = 10 * time.Second
	maxErrorBody       = 4 << 10
	maxDrainBody       = 64 << 10
	dbOpTimeout        = 10 * time.Second
)

// DefaultRetrySchedule is the delay before each retry: after attempt 1 wait
// 10s, after attempt 2 wait 30s, and so on. Each delay gets ±20% jitter.
// Attempts past the end of the schedule reuse its last entry.
var DefaultRetrySchedule = []time.Duration{
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// WorkerConfig configures the delivery worker. Zero values take defaults.
//
// Environment variables (see LoadWorkerConfigFromEnv):
//   - WORKER_ID           identifier written to claimed_by (default: random)
//   - WORKER_CONCURRENCY  concurrent deliveries per process (default 10)
//   - WORKER_IDLE_MAX     max sleep between empty polls, Go duration (default 2s)
//   - WORKER_MAX_ATTEMPTS total attempts incl. the first, before dead-lettering (default 6)
//   - WEBHOOK_* variables, see HTTPClientConfig; the lease is max(60s, 2×WEBHOOK_TIMEOUT_SECONDS).
type WorkerConfig struct {
	WorkerID    string
	Concurrency int
	// IdleMin is the first sleep after an empty poll; it doubles up to IdleMax.
	IdleMin     time.Duration
	IdleMax     time.Duration
	MaxAttempts int
	// Lease is how long a claim lasts without a heartbeat. Default max(60s, 2×HTTP.Timeout).
	Lease time.Duration
	// DrainTimeout bounds how long in-flight deliveries may run after shutdown
	// begins. When it expires their requests are aborted and the tasks released
	// for immediate pickup by another instance, instead of staying in_flight
	// until the lease expires. 0 waits for every delivery to finish.
	DrainTimeout time.Duration
	HTTP         HTTPClientConfig
	// RetrySchedule overrides DefaultRetrySchedule (tests use short delays).
	RetrySchedule []time.Duration
	// DisableJitter turns off the ±20% jitter (tests only).
	DisableJitter bool
	Logger        *slog.Logger
}

// LoadWorkerConfigFromEnv reads WorkerConfig from the environment.
func LoadWorkerConfigFromEnv() WorkerConfig {
	cfg := WorkerConfig{
		WorkerID: os.Getenv("WORKER_ID"),
		HTTP:     LoadHTTPClientConfigFromEnv(),
	}
	if v, ok := envPositiveInt("WORKER_CONCURRENCY"); ok {
		cfg.Concurrency = v
	}
	if d, err := time.ParseDuration(os.Getenv("WORKER_IDLE_MAX")); err == nil && d > 0 {
		cfg.IdleMax = d
	}
	if v, ok := envPositiveInt("WORKER_MAX_ATTEMPTS"); ok {
		cfg.MaxAttempts = v
	}
	return cfg
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.WorkerID == "" {
		c.WorkerID = uuid.NewString()[:8]
	}
	if c.Concurrency <= 0 {
		c.Concurrency = defaultConcurrency
	}
	if c.IdleMin <= 0 {
		c.IdleMin = defaultIdleMin
	}
	if c.IdleMax <= 0 {
		c.IdleMax = defaultIdleMax
	}
	c.IdleMax = max(c.IdleMax, c.IdleMin)
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = defaultMaxAttempts
	}
	if c.HTTP.Timeout <= 0 {
		c.HTTP = DefaultHTTPClientConfig()
	}
	if c.Lease <= 0 {
		c.Lease = max(minLease, 2*c.HTTP.Timeout)
	}
	if len(c.RetrySchedule) == 0 {
		c.RetrySchedule = DefaultRetrySchedule
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Worker claims and delivers webhook tasks.
type Worker struct {
	cfg    WorkerConfig
	db     *sql.DB
	q      *database.Queries
	cache  *cache.RedisSubscriptionCache
	client *http.Client
	log    *slog.Logger
	// newID generates row ids; tests replace it to force constraint errors.
	newID func() string
}

// NewWorker builds a delivery worker. conn is needed for atomic finalize
// transactions; subCache may be nil.
func NewWorker(conn *sql.DB, subCache *cache.RedisSubscriptionCache, cfg WorkerConfig) *Worker {
	cfg = cfg.withDefaults()
	return &Worker{
		cfg:    cfg,
		db:     conn,
		q:      database.New(conn),
		cache:  subCache,
		client: NewSecureHTTPClient(cfg.HTTP),
		log:    cfg.Logger.With("component", "delivery_worker", "worker_id", cfg.WorkerID),
		newID:  uuid.NewString,
	}
}

// ID returns the identifier this worker writes to claimed_by.
func (w *Worker) ID() string { return w.cfg.WorkerID }

// Start runs until ctx is cancelled. On cancellation it stops claiming, lets
// deliveries already in progress finish (up to DrainTimeout, then aborts and
// releases them), releases tasks that were claimed but not started, and only
// then returns.
func (w *Worker) Start(ctx context.Context) {
	w.log.Info("delivery worker starting",
		"concurrency", w.cfg.Concurrency, "lease", w.cfg.Lease, "max_attempts", w.cfg.MaxAttempts)

	queue := make(chan database.DeliveryTask, w.cfg.Concurrency)
	freed := make(chan struct{}, 1)
	var busy atomic.Int64 // tasks claimed and not yet finished (queued or delivering)

	// abortCtx outlives ctx: in-flight requests keep running after shutdown
	// begins and are cancelled only when the drain budget runs out.
	abortCtx, abort := context.WithCancel(context.WithoutCancel(ctx))
	defer abort()
	stopDrainTimer := context.AfterFunc(ctx, func() {
		if w.cfg.DrainTimeout > 0 {
			time.AfterFunc(w.cfg.DrainTimeout, abort)
		}
	})
	defer stopDrainTimer()

	var wg sync.WaitGroup
	for range w.cfg.Concurrency {
		wg.Go(func() {
			for task := range queue {
				if ctx.Err() != nil {
					w.release(context.WithoutCancel(ctx), task, 0, "shutdown")
				} else {
					w.process(abortCtx, task)
				}
				busy.Add(-1)
				select {
				case freed <- struct{}{}:
				default:
				}
			}
		})
	}

	w.claimLoop(ctx, queue, &busy, freed)
	close(queue)
	wg.Wait()
	w.log.Info("delivery worker stopped")
}

// claimLoop claims as many tasks as there are free slots. A full batch claims
// again immediately; an empty batch backs off from IdleMin doubling to IdleMax.
func (w *Worker) claimLoop(ctx context.Context, queue chan<- database.DeliveryTask, busy *atomic.Int64, freed <-chan struct{}) {
	idle := w.cfg.IdleMin
	for ctx.Err() == nil {
		free := w.cfg.Concurrency - int(busy.Load())
		if free <= 0 {
			select {
			case <-ctx.Done():
				return
			case <-freed:
			}
			continue
		}

		tasks, err := w.claimBatch(ctx, free)
		if err != nil {
			w.log.Error("claim tasks", "err", err)
		}
		for _, t := range tasks {
			busy.Add(1)
			queue <- t // never blocks: queued <= busy <= Concurrency = cap(queue)
		}

		var wait time.Duration
		switch {
		case len(tasks) == free:
			idle = w.cfg.IdleMin
			continue
		case len(tasks) > 0:
			idle = w.cfg.IdleMin
			wait = idle
		default:
			wait = idle
			idle = min(idle*2, w.cfg.IdleMax)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// claimBatch claims up to n due tasks for this worker in two round-trips.
// The queries run on a context detached from cancellation so that rows the
// UPDATE claimed are always returned (and then released on shutdown).
func (w *Worker) claimBatch(ctx context.Context, n int) ([]database.DeliveryTask, error) {
	qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbOpTimeout)
	defer cancel()
	ids, err := w.q.SelectClaimableTaskIDs(qctx, int64(n))
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return w.q.ClaimTasks(qctx, database.ClaimTasksParams{
		WorkerID:     w.owner(),
		LeaseSeconds: ceilSeconds(w.cfg.Lease),
		Ids:          ids,
	})
}

func (w *Worker) owner() sql.NullString {
	return sql.NullString{String: w.cfg.WorkerID, Valid: true}
}

// process delivers one claimed task. Cancelling abort cancels only the HTTP
// request; a request aborted before any response arrives releases the task
// without counting an attempt. Database work always runs to completion.
func (w *Worker) process(abort context.Context, task database.DeliveryTask) {
	ctx := context.WithoutCancel(abort)
	log := w.taskLog(task)

	sub, err := w.cache.GetOrLoad(ctx, task.SubscriptionID, func(ctx context.Context) (database.Subscription, error) {
		return w.q.GetSubscription(ctx, task.SubscriptionID)
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		w.deadLetterOrphan(ctx, task)
		return
	case err != nil:
		log.Error("load subscription", "err", err)
		w.release(ctx, task, lookupErrDeferral, "subscription_lookup_error")
		return
	case sub.Paused != 0:
		w.release(ctx, task, pausedDeferral, "subscription_paused")
		return
	}

	stop := w.heartbeat(ctx, task)
	res := w.deliver(abort, task, sub)
	stop()
	if res.Aborted {
		w.release(ctx, task, 0, "shutdown_abort")
		return
	}

	d := w.decide(task, res)
	if err := w.finalize(ctx, task, sub.TargetUrl, &res, d); err != nil {
		w.logFinalizeError(log, err)
		return
	}
	log.Info("delivery attempt finished",
		"attempt", task.AttemptCount+1, "outcome", res.Outcome, "http_status", res.HTTPStatus,
		"status", d.Status, "retry_in", d.Delay, "dead_letter_reason", d.DLQReason)
}

// deadLetterOrphan handles a task whose subscription no longer exists. Without
// this it would be retried forever at the head of the queue.
func (w *Worker) deadLetterOrphan(ctx context.Context, task database.DeliveryTask) {
	log := w.taskLog(task)
	d := decision{Status: StatusFailed, DLQReason: ReasonSubscriptionNotFound}
	err := w.finalize(ctx, task, "", nil, d)
	if err != nil && !errors.Is(err, errStaleOwner) {
		// With foreign keys enforced the DLQ row (which references the missing
		// subscription) cannot be inserted. Fail the task without it rather than
		// leaving it in_flight to be reclaimed forever.
		log.Warn("dead-letter orphaned task failed; marking failed without DLQ row", "err", err)
		err = w.finalize(ctx, task, "", nil, decision{Status: StatusFailed})
	}
	if err != nil {
		w.logFinalizeError(log, err)
		return
	}
	log.Warn("subscription not found; task dead-lettered", "reason", ReasonSubscriptionNotFound)
}

func (w *Worker) logFinalizeError(log *slog.Logger, err error) {
	if errors.Is(err, errStaleOwner) {
		log.Warn("lease lost before finalize; result dropped (another worker owns the task)")
		return
	}
	log.Error("finalize task", "err", err)
}

func (w *Worker) taskLog(task database.DeliveryTask) *slog.Logger {
	return w.log.With("task_id", task.ID, "subscription_id", task.SubscriptionID)
}

// release hands a claimed task back without counting an attempt.
func (w *Worker) release(ctx context.Context, task database.DeliveryTask, delay time.Duration, why string) {
	ctx, cancel := context.WithTimeout(ctx, dbOpTimeout)
	defer cancel()
	n, err := w.q.ReleaseTask(ctx, database.ReleaseTaskParams{
		DelaySeconds: ceilSeconds(delay),
		ID:           task.ID,
		WorkerID:     w.owner(),
	})
	log := w.taskLog(task)
	switch {
	case err != nil:
		log.Error("release task", "reason", why, "err", err)
	case n == 0:
		log.Warn("release skipped; lease already lost", "reason", why)
	default:
		log.Info("task released", "reason", why, "retry_in", delay)
	}
}

// heartbeat extends the lease every Lease/3 until the returned stop is called.
func (w *Worker) heartbeat(ctx context.Context, task database.DeliveryTask) (stop func()) {
	hbCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(max(w.cfg.Lease/3, 10*time.Millisecond))
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				qctx, qcancel := context.WithTimeout(hbCtx, dbOpTimeout)
				n, err := w.q.ExtendTaskLeaseFor(qctx, database.ExtendTaskLeaseForParams{
					LeaseSeconds: ceilSeconds(w.cfg.Lease),
					ID:           task.ID,
					WorkerID:     w.owner(),
				})
				qcancel()
				switch {
				case err != nil && hbCtx.Err() == nil:
					w.taskLog(task).Warn("extend lease", "err", err)
				case err == nil && n == 0:
					w.taskLog(task).Warn("lease lost during delivery")
					return
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

// attemptResult is the outcome of one HTTP delivery attempt.
type attemptResult struct {
	Outcome    string // OutcomeSuccess or OutcomeFailedAttempt
	HTTPStatus int    // 0 if no response
	Error      string // transport error or status + truncated body
	RetryAfter time.Duration
	Aborted    bool // the request was cancelled by shutdown before any response
}

// decision is what finalize writes.
type decision struct {
	Status    string        // StatusDelivered, StatusPending (retry) or StatusFailed
	Delay     time.Duration // retry delay when Status is pending
	DLQReason string        // non-empty: also insert a dead-letter row
}

func (w *Worker) decide(task database.DeliveryTask, res attemptResult) decision {
	attempt := task.AttemptCount + 1
	switch {
	case res.Outcome == OutcomeSuccess:
		return decision{Status: StatusDelivered}
	case res.HTTPStatus == http.StatusGone:
		return decision{Status: StatusFailed, DLQReason: ReasonGone}
	case attempt >= int64(w.cfg.MaxAttempts):
		return decision{Status: StatusFailed, DLQReason: ReasonMaxAttempts}
	default:
		return decision{Status: StatusPending, Delay: w.retryDelay(attempt, res.RetryAfter)}
	}
}

// retryDelay is the jittered schedule delay for the given attempt, raised to
// honour a (1h-capped) Retry-After.
func (w *Worker) retryDelay(attempt int64, retryAfter time.Duration) time.Duration {
	jitter := jitter20
	if w.cfg.DisableJitter {
		jitter = func(d time.Duration) time.Duration { return d }
	}
	d := backoffDelay(w.cfg.RetrySchedule, attempt, jitter)
	return max(d, min(retryAfter, maxRetryAfter))
}

// backoffDelay returns the delay after the given (1-based) attempt. It is total:
// attempt <= 0 uses the first entry, attempts past the end use the last, and an
// empty schedule falls back to DefaultRetrySchedule.
func backoffDelay(schedule []time.Duration, attempt int64, jitter func(time.Duration) time.Duration) time.Duration {
	if len(schedule) == 0 {
		schedule = DefaultRetrySchedule
	}
	i := min(max(attempt-1, 0), int64(len(schedule)-1))
	return max(jitter(schedule[i]), 0)
}

// jitter20 spreads d uniformly over [0.8d, 1.2d].
func jitter20(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

// parseRetryAfter reads a Retry-After header in delay-seconds or HTTP-date form.
func parseRetryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(h, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil && t.After(now) {
		return min(t.Sub(now), maxRetryAfter)
	}
	return 0
}

var errStaleOwner = errors.New("task is no longer owned by this worker")

// finalize atomically records an attempt: it updates the task (guarded by
// claimed_by), inserts the delivery log when an attempt was made (res != nil),
// and inserts the dead-letter row when d.DLQReason is set. If the guard matches
// no row, nothing is written and errStaleOwner is returned.
func (w *Worker) finalize(ctx context.Context, task database.DeliveryTask, targetURL string, res *attemptResult, d decision) error {
	ctx, cancel := context.WithTimeout(ctx, dbOpTimeout)
	defer cancel()
	return db.WithTx(ctx, w.db, func(q *database.Queries) error {
		var (
			n   int64
			err error
		)
		if d.Status == StatusPending {
			n, err = q.ScheduleTaskRetry(ctx, database.ScheduleTaskRetryParams{
				DelaySeconds: ceilSeconds(d.Delay),
				ID:           task.ID,
				WorkerID:     w.owner(),
			})
		} else {
			n, err = q.CompleteTask(ctx, database.CompleteTaskParams{
				Status:   d.Status,
				ID:       task.ID,
				WorkerID: w.owner(),
			})
		}
		if err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		if n == 0 {
			return errStaleOwner
		}

		var errDetails string
		if res != nil {
			errDetails = res.Error
			if err := q.CreateDeliveryLog(ctx, database.CreateDeliveryLogParams{
				ID:             w.newID(),
				DeliveryTaskID: task.ID,
				SubscriptionID: task.SubscriptionID,
				TargetUrl:      targetURL,
				AttemptNumber:  task.AttemptCount + 1,
				Outcome:        res.Outcome,
				HttpStatus:     sql.NullInt64{Int64: int64(res.HTTPStatus), Valid: res.HTTPStatus != 0},
				ErrorDetails:   nullString(res.Error),
			}); err != nil {
				return fmt.Errorf("insert delivery log: %w", err)
			}
		}

		if d.DLQReason != "" {
			if err := q.InsertDeadLetterTask(ctx, database.InsertDeadLetterTaskParams{
				ID:             w.newID(),
				OriginalTaskID: task.ID,
				SubscriptionID: task.SubscriptionID,
				Payload:        task.Payload,
				Reason:         d.DLQReason,
				AttemptCount:   task.AttemptCount + 1,
				Status:         "dead",
				TargetUrl:      nullString(targetURL),
				EventType:      task.EventType,
				ErrorDetails:   nullString(errDetails),
			}); err != nil {
				return fmt.Errorf("insert dead letter: %w", err)
			}
		}
		return nil
	})
}

// deliver performs one HTTP POST of the task payload.
func (w *Worker) deliver(ctx context.Context, task database.DeliveryTask, sub database.Subscription) attemptResult {
	body := []byte(task.Payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.TargetUrl, strings.NewReader(task.Payload))
	if err != nil {
		return attemptResult{Outcome: OutcomeFailedAttempt, Error: "build request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if task.EventType.Valid && task.EventType.String != "" {
		req.Header.Set("X-Event-Type", task.EventType.String)
	}
	if sub.Secret.Valid && sub.Secret.String != "" {
		ts := time.Now().Unix()
		req.Header.Set("webhook-id", task.ID)
		req.Header.Set("webhook-timestamp", strconv.FormatInt(ts, 10))
		req.Header.Set("webhook-signature", Sign(sub.Secret.String, task.ID, ts, body))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return attemptResult{Outcome: OutcomeFailedAttempt, Error: err.Error(), Aborted: ctx.Err() != nil}
	}
	defer func() {
		// Drain (bounded) so the keep-alive connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBody))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return attemptResult{Outcome: OutcomeSuccess, HTTPStatus: resp.StatusCode}
	}
	res := attemptResult{Outcome: OutcomeFailedAttempt, HTTPStatus: resp.StatusCode, Error: resp.Status}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if s := strings.TrimSpace(strings.ToValidUTF8(string(snippet), "�")); s != "" {
		res.Error += ": " + s
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		res.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	}
	return res
}

// Sign returns the Standard Webhooks signature header value for a message:
// "v1," + base64(HMAC-SHA256(key=secret, msg=id + "." + timestamp + "." + body)).
func Sign(secret, id string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, SigningKey(secret))
	mac.Write([]byte(id))
	mac.Write([]byte{'.'})
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// SigningKey returns the HMAC key for a subscription secret. Standard Webhooks
// secrets ("whsec_" + base64) are keyed with the decoded bytes, so official
// verifier libraries accept the signatures; any other secret (including legacy
// hex secrets) is used as its raw UTF-8 bytes.
func SigningKey(secret string) []byte {
	if rest, ok := strings.CutPrefix(secret, "whsec_"); ok {
		if key, err := base64.StdEncoding.DecodeString(rest); err == nil && len(key) > 0 {
			return key
		}
	}
	return []byte(secret)
}

// ceilSeconds converts d to whole seconds, rounding up, for SQL datetime modifiers.
func ceilSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Ceil(d.Seconds()))
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
