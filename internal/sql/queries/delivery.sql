-- name: ListPendingDeliveryTasks :many
SELECT * FROM delivery_tasks
WHERE status = 'pending'
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
SELECT * FROM delivery_logs
WHERE subscription_id = ?
ORDER BY timestamp DESC
LIMIT 20;