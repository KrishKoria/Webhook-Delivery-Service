// Package ops holds the mutating domain operations shared by the JSON API and
// the admin UI, so both surfaces get the same atomicity and validation.
package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/google/uuid"
)

var (
	// ErrNotFound means the target row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means the row exists but is not in a state that allows the operation.
	ErrConflict = errors.New("conflict")
)

// Ops performs domain operations against the database.
type Ops struct {
	DB *sql.DB
	Q  *database.Queries
	// Invalidate is called with a subscription ID after it changes (e.g. cache eviction). Optional.
	Invalidate func(subscriptionID string)
}

// New returns Ops bound to conn.
func New(conn *sql.DB, invalidate func(string)) *Ops {
	return &Ops{DB: conn, Q: database.New(conn), Invalidate: invalidate}
}

func (o *Ops) invalidate(id string) {
	if o.Invalidate != nil {
		o.Invalidate(id)
	}
}

// EnqueueInput describes a new delivery task.
type EnqueueInput struct {
	SubscriptionID string
	Payload        string
	EventType      string
	IdempotencyKey string
}

// Enqueue creates a pending delivery task. When IdempotencyKey matches an existing
// task for the subscription, it returns that task's ID with duplicate=true.
func (o *Ops) Enqueue(ctx context.Context, in EnqueueInput) (taskID string, duplicate bool, err error) {
	return enqueue(ctx, o.Q, in)
}

func enqueue(ctx context.Context, q *database.Queries, in EnqueueInput) (string, bool, error) {
	id := uuid.NewString()
	n, err := q.InsertDeliveryTaskWithKey(ctx, database.InsertDeliveryTaskWithKeyParams{
		ID:             id,
		SubscriptionID: in.SubscriptionID,
		Payload:        in.Payload,
		EventType:      nullString(in.EventType),
		IdempotencyKey: nullString(in.IdempotencyKey),
	})
	if err != nil {
		return "", false, fmt.Errorf("insert delivery task: %w", err)
	}
	if n == 1 {
		return id, false, nil
	}
	existing, err := q.GetDeliveryTaskByIdempotencyKey(ctx, database.GetDeliveryTaskByIdempotencyKeyParams{
		SubscriptionID: in.SubscriptionID,
		IdempotencyKey: nullString(in.IdempotencyKey),
	})
	if err != nil {
		return "", false, fmt.Errorf("load idempotent task: %w", err)
	}
	return existing.ID, true, nil
}

// RetryDLQ atomically moves a dead DLQ entry back into the delivery queue.
// It returns ErrNotFound for unknown IDs and ErrConflict if the entry is not 'dead'
// (for example, already retried), so double submits cannot enqueue twice.
func (o *Ops) RetryDLQ(ctx context.Context, dlqID, note string) (newTaskID string, err error) {
	err = db.WithTx(ctx, o.DB, func(q *database.Queries) error {
		newTaskID, err = retryDLQ(ctx, q, dlqID, note)
		return err
	})
	return newTaskID, err
}

func retryDLQ(ctx context.Context, q *database.Queries, dlqID, note string) (string, error) {
	row, err := q.MarkDLQRetried(ctx, database.MarkDLQRetriedParams{ID: dlqID, Note: nullString(note)})
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := q.GetDeadLetterTask(ctx, dlqID); errors.Is(getErr, sql.ErrNoRows) {
			return "", ErrNotFound
		} else if getErr != nil {
			return "", getErr
		}
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("mark dlq retried: %w", err)
	}
	id, _, err := enqueue(ctx, q, EnqueueInput{
		SubscriptionID: row.SubscriptionID,
		Payload:        row.Payload,
		EventType:      row.EventType.String,
	})
	return id, err
}

// RetryResult reports the outcome of one ID in a bulk retry.
type RetryResult struct {
	DLQID     string `json:"dlq_id"`
	NewTaskID string `json:"new_task_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// RetryDLQBulk retries each ID in its own transaction so one bad ID cannot block the rest.
func (o *Ops) RetryDLQBulk(ctx context.Context, ids []string, note string) []RetryResult {
	results := make([]RetryResult, 0, len(ids))
	for _, id := range ids {
		taskID, err := o.RetryDLQ(ctx, id, note)
		r := RetryResult{DLQID: id, NewTaskID: taskID}
		switch {
		case errors.Is(err, ErrNotFound):
			r.Error = "not_found"
		case errors.Is(err, ErrConflict):
			r.Error = "not_dead"
		case err != nil:
			r.Error = "internal_error"
		}
		results = append(results, r)
	}
	return results
}

// Redeliver enqueues a fresh copy of an existing delivery task, whatever its status.
func (o *Ops) Redeliver(ctx context.Context, taskID string) (string, error) {
	task, err := o.Q.GetDeliveryTask(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	id, _, err := o.Enqueue(ctx, EnqueueInput{
		SubscriptionID: task.SubscriptionID,
		Payload:        task.Payload,
		EventType:      task.EventType.String,
	})
	return id, err
}

// SetPaused pauses or resumes deliveries for a subscription.
func (o *Ops) SetPaused(ctx context.Context, subscriptionID string, paused bool) error {
	var v int64
	if paused {
		v = 1
	}
	n, err := o.Q.SetSubscriptionPaused(ctx, database.SetSubscriptionPausedParams{ID: subscriptionID, Paused: v})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	o.invalidate(subscriptionID)
	return nil
}

// UpdateSubscriptionInput changes a subscription. A nil Secret keeps the existing one.
type UpdateSubscriptionInput struct {
	ID          string
	TargetURL   string
	EventTypes  string
	Secret      *string
	ClearSecret bool
}

// UpdateSubscription updates a subscription without ever wiping its secret by accident.
func (o *Ops) UpdateSubscription(ctx context.Context, in UpdateSubscriptionInput) error {
	var secret sql.NullString
	if in.Secret != nil && *in.Secret != "" {
		secret = sql.NullString{String: *in.Secret, Valid: true}
	}
	err := db.WithTx(ctx, o.DB, func(q *database.Queries) error {
		n, err := q.UpdateSubscriptionFields(ctx, database.UpdateSubscriptionFieldsParams{
			ID:         in.ID,
			TargetUrl:  in.TargetURL,
			EventTypes: nullString(strings.TrimSpace(in.EventTypes)),
			Secret:     secret,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if in.ClearSecret {
			_, err = q.ClearSubscriptionSecret(ctx, in.ID)
		}
		return err
	})
	if err == nil {
		o.invalidate(in.ID)
	}
	return err
}

// DeleteSubscription removes a subscription and everything that references it.
func (o *Ops) DeleteSubscription(ctx context.Context, id string) error {
	err := db.WithTx(ctx, o.DB, func(q *database.Queries) error {
		for _, del := range []func(context.Context, string) error{
			q.DeleteDeliveryLogsForSubscription,
			q.DeleteDeliveryTasksForSubscription,
			q.DeleteDeadLetterTasksForSubscription,
			q.DeleteScheduledWebhooksForSubscription,
		} {
			if err := del(ctx, id); err != nil {
				return err
			}
		}
		n, err := q.DeleteSubscriptionRow(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err == nil {
		o.invalidate(id)
	}
	return err
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
