-- name: ListPendingDeliveryTasks :many
SELECT * FROM delivery_tasks
WHERE status = 'pending' AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP)
ORDER BY created_at ASC
LIMIT 10;

-- name: CreateDeliveryTask :exec
INSERT INTO delivery_tasks (id, subscription_id, payload, status, attempt_count, created_at)
VALUES (?, ?, ?, 'pending', 0, CURRENT_TIMESTAMP);

-- name: UpdateDeliveryTaskStatus :exec
UPDATE delivery_tasks
SET status = ?, last_attempt_at = ?, attempt_count = ?
WHERE id = ?;

-- name: CreateDeliveryLog :exec
INSERT INTO delivery_logs (
    id, delivery_task_id, subscription_id, target_url, timestamp,
    attempt_number, outcome, http_status, error_details
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDeliveryTask :one
SELECT * FROM delivery_tasks WHERE id = ?;

-- name: ListDeliveryLogsForTask :many
SELECT * FROM delivery_logs
WHERE delivery_task_id = ?
ORDER BY attempt_number ASC;

-- name: ListRecentDeliveryLogsForSubscription :many
SELECT
    dl.id,
    dl.delivery_task_id,
    dl.subscription_id,
    dl.target_url,
    dl.timestamp,
    dl.attempt_number,
    dl.outcome,
    dl.http_status,
    dl.error_details,
    dt.status AS task_status
FROM delivery_logs dl
LEFT JOIN delivery_tasks dt ON dl.delivery_task_id = dt.id
WHERE dl.subscription_id = ?
ORDER BY dl.timestamp DESC
LIMIT 20;

-- name: DeleteOldDeliveryLogs :exec
DELETE FROM delivery_logs
WHERE timestamp < datetime('now', '-72 hours');

-- name: UpdateDeliveryTaskNextAttemptAt :exec
UPDATE delivery_tasks
SET next_attempt_at = ?
WHERE id = ?;