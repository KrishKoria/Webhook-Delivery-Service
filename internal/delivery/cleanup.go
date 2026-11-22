package delivery

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
)

// CleanupConfig sets retention. Zero values take defaults.
//
// Environment variables (see LoadCleanupConfigFromEnv):
//   - LOG_RETENTION_HOURS        delivery_logs older than this are deleted (default 72)
//   - TASK_RETENTION_DAYS        delivered/failed tasks (and their logs) older than this are deleted (default 7)
//   - DLQ_RETRIED_RETENTION_DAYS DLQ rows already retried and older than this are deleted (default 30)
type CleanupConfig struct {
	LogRetentionHours       int
	TaskRetentionDays       int
	DLQRetriedRetentionDays int
	// Interval between runs (default 1h). A run also happens at startup.
	Interval time.Duration
	// BatchSize is the number of rows per delete statement (default 1000).
	BatchSize int
}

// LoadCleanupConfigFromEnv reads CleanupConfig from the environment.
func LoadCleanupConfigFromEnv() CleanupConfig {
	var c CleanupConfig
	if v, ok := envPositiveInt("LOG_RETENTION_HOURS"); ok {
		c.LogRetentionHours = v
	}
	if v, ok := envPositiveInt("TASK_RETENTION_DAYS"); ok {
		c.TaskRetentionDays = v
	}
	if v, ok := envPositiveInt("DLQ_RETRIED_RETENTION_DAYS"); ok {
		c.DLQRetriedRetentionDays = v
	}
	return c
}

func (c CleanupConfig) withDefaults() CleanupConfig {
	if c.LogRetentionHours <= 0 {
		c.LogRetentionHours = 72
	}
	if c.TaskRetentionDays <= 0 {
		c.TaskRetentionDays = 7
	}
	if c.DLQRetriedRetentionDays <= 0 {
		c.DLQRetriedRetentionDays = 30
	}
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 1000
	}
	return c
}

// CleanupWorker enforces retention on logs, finished tasks and retried DLQ rows.
type CleanupWorker struct {
	cfg CleanupConfig
	db  *sql.DB
	q   *database.Queries
	log *slog.Logger
}

// NewCleanupWorker builds the retention worker; it needs conn for transactions.
func NewCleanupWorker(conn *sql.DB, cfg CleanupConfig) *CleanupWorker {
	return &CleanupWorker{
		cfg: cfg.withDefaults(),
		db:  conn,
		q:   database.New(conn),
		log: slog.Default().With("component", "cleanup_worker"),
	}
}

// Start runs a cleanup immediately and then every Interval until ctx is cancelled.
func (w *CleanupWorker) Start(ctx context.Context) {
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.log.Error("cleanup", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce applies every retention rule once.
func (w *CleanupWorker) RunOnce(ctx context.Context) error {
	tasks, err := w.purgeFinishedTasks(ctx)
	if err != nil {
		return fmt.Errorf("purge finished tasks: %w", err)
	}
	logs, err := w.purgeLogs(ctx)
	if err != nil {
		return fmt.Errorf("purge delivery logs: %w", err)
	}
	if err := w.q.PurgeRetriedDLQTasks(ctx, int64(w.cfg.DLQRetriedRetentionDays)); err != nil {
		return fmt.Errorf("purge retried DLQ rows: %w", err)
	}
	if tasks > 0 || logs > 0 {
		w.log.Info("cleanup finished", "tasks_deleted", tasks, "logs_deleted", logs)
	}
	return nil
}

// purgeLogs deletes expired delivery logs in batches.
func (w *CleanupWorker) purgeLogs(ctx context.Context) (int64, error) {
	var total int64
	for {
		ids, err := w.q.SelectExpiredDeliveryLogIDs(ctx, database.SelectExpiredDeliveryLogIDsParams{
			OlderThanHours: int64(w.cfg.LogRetentionHours),
			MaxRows:        int64(w.cfg.BatchSize),
		})
		if err != nil {
			return total, err
		}
		if len(ids) > 0 {
			n, err := w.q.DeleteDeliveryLogsByIDs(ctx, ids)
			if err != nil {
				return total, err
			}
			total += n
		}
		if len(ids) < w.cfg.BatchSize || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// purgeFinishedTasks deletes expired delivered/failed tasks in batches, each
// batch deleting the tasks' logs first in the same transaction.
func (w *CleanupWorker) purgeFinishedTasks(ctx context.Context) (int64, error) {
	var total int64
	for {
		ids, err := w.q.SelectExpiredFinishedTaskIDs(ctx, database.SelectExpiredFinishedTaskIDsParams{
			OlderThanDays: int64(w.cfg.TaskRetentionDays),
			MaxRows:       int64(w.cfg.BatchSize),
		})
		if err != nil {
			return total, err
		}
		if len(ids) > 0 {
			err := db.WithTx(ctx, w.db, func(q *database.Queries) error {
				if _, err := q.DeleteDeliveryLogsForTaskIDs(ctx, ids); err != nil {
					return err
				}
				n, err := q.DeleteFinishedTasksByIDs(ctx, ids)
				total += n
				return err
			})
			if err != nil {
				return total, err
			}
		}
		if len(ids) < w.cfg.BatchSize || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}
