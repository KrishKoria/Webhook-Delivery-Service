-- name: CreateScheduledWebhook :exec
INSERT INTO scheduled_webhooks (
    id, subscription_id, payload, scheduled_for, recurrence, status
) VALUES (
    sqlc.arg(id), sqlc.arg(subscription_id), sqlc.arg(payload),
    -- datetime() normalizes any RFC3339 input (including offsets) to canonical UTC.
    datetime(sqlc.arg(scheduled_for)), sqlc.arg(recurrence), 'pending'
);

-- name: GetScheduledWebhook :one
SELECT * FROM scheduled_webhooks
WHERE id = ?;

-- name: ListScheduledWebhooks :many
SELECT * FROM scheduled_webhooks
WHERE subscription_id = ?
ORDER BY scheduled_for DESC
LIMIT ? OFFSET ?;

-- name: GetDueScheduledWebhooks :many
-- Deprecated: not exclusive; the scheduler uses SelectDueScheduledWebhookIDs + ClaimScheduledWebhooks.
SELECT * FROM scheduled_webhooks
WHERE scheduled_for <= CURRENT_TIMESTAMP AND status = 'pending';

-- name: UpdateScheduledWebhookStatus :exec
UPDATE scheduled_webhooks
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteScheduledWebhook :exec
DELETE FROM scheduled_webhooks WHERE id = ?;

-- name: ListAllScheduledWebhooks :many
SELECT id, subscription_id, payload, scheduled_for, recurrence, status, created_at, updated_at
FROM scheduled_webhooks
ORDER BY scheduled_for ASC
LIMIT ? OFFSET ?;

-- name: CountScheduledWebhooksByStatus :one
-- The scheduler now marks fired rows 'enqueued' (older rows say 'delivered').
-- delivered counts both for compatibility; enqueued counts only the new status.
SELECT
    COUNT(*) as total,
    SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
    SUM(CASE WHEN status IN ('delivered', 'enqueued') THEN 1 ELSE 0 END) as delivered,
    SUM(CASE WHEN status = 'enqueued' THEN 1 ELSE 0 END) as enqueued,
    SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
    SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) as cancelled
FROM scheduled_webhooks;

-- name: SelectDueScheduledWebhookIDs :many
SELECT id FROM scheduled_webhooks
WHERE status = 'pending' AND scheduled_for <= CURRENT_TIMESTAMP
ORDER BY scheduled_for
LIMIT sqlc.arg(max_rows);

-- name: ClaimScheduledWebhook :one
-- Claims one due row inside the transaction that enqueues its delivery task.
-- Returns no row when another scheduler already won it; a rolled-back
-- transaction leaves the row pending, so the next tick retries it.
UPDATE scheduled_webhooks
SET status = 'enqueued', updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND status = 'pending' AND scheduled_for <= CURRENT_TIMESTAMP
RETURNING *;
