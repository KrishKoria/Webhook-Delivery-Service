-- name: MarkDLQRetried :one
-- Atomically claims a dead DLQ row for retry; returns no row if it was not 'dead'.
UPDATE dead_letter_tasks
SET status = 'retried',
    last_attempt_at = CURRENT_TIMESTAMP,
    attempt_count = attempt_count + 1,
    error_details = sqlc.arg(note)
WHERE id = sqlc.arg(id) AND status = 'dead'
RETURNING *;

-- name: InsertDeliveryTaskWithKey :execrows
-- Enqueues a task; an existing (subscription_id, idempotency_key) makes this a no-op.
INSERT INTO delivery_tasks (id, subscription_id, payload, event_type, idempotency_key, status, attempt_count, created_at, next_attempt_at)
VALUES (sqlc.arg(id), sqlc.arg(subscription_id), sqlc.arg(payload), sqlc.narg(event_type), sqlc.narg(idempotency_key),
        'pending', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (subscription_id, idempotency_key) DO NOTHING;

-- name: GetDeliveryTaskByIdempotencyKey :one
SELECT * FROM delivery_tasks
WHERE subscription_id = ? AND idempotency_key = ?;

-- name: SetSubscriptionPaused :execrows
UPDATE subscriptions
SET paused = sqlc.arg(paused), updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id);

-- name: UpdateSubscriptionFields :execrows
-- A NULL secret keeps the current one; use ClearSubscriptionSecret to remove it.
UPDATE subscriptions
SET target_url = sqlc.arg(target_url),
    event_types = sqlc.narg(event_types),
    secret = COALESCE(sqlc.narg(secret), secret),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id);

-- name: ClearSubscriptionSecret :execrows
UPDATE subscriptions
SET secret = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteDeliveryLogsForSubscription :exec
DELETE FROM delivery_logs WHERE subscription_id = ?;

-- name: DeleteDeliveryTasksForSubscription :exec
DELETE FROM delivery_tasks WHERE subscription_id = ?;

-- name: DeleteDeadLetterTasksForSubscription :exec
DELETE FROM dead_letter_tasks WHERE subscription_id = ?;

-- name: DeleteScheduledWebhooksForSubscription :exec
DELETE FROM scheduled_webhooks WHERE subscription_id = ?;

-- name: DeleteSubscriptionRow :execrows
DELETE FROM subscriptions WHERE id = ?;

-- name: CountSubscriptions :one
SELECT COUNT(*) FROM subscriptions;
