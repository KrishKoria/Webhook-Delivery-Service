-- name: CreateScheduledWebhook :exec
INSERT INTO scheduled_webhooks (
    id, subscription_id, payload, scheduled_for, recurrence, status
) VALUES (
    ?, ?, ?, ?, ?, 'pending'
);

-- name: ListScheduledWebhooks :many
SELECT * FROM scheduled_webhooks
WHERE subscription_id = ?
ORDER BY scheduled_for DESC
LIMIT ? OFFSET ?;

-- name: GetDueScheduledWebhooks :many
SELECT * FROM scheduled_webhooks
WHERE scheduled_for <= ? AND status = 'pending';

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