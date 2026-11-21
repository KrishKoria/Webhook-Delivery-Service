-- name: InsertDeadLetterTask :exec
INSERT INTO dead_letter_tasks (
    id, original_task_id, subscription_id, payload, failed_at, reason, last_attempt_at, attempt_count, status, target_url, event_type, error_details
) VALUES (
    ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?
);

-- name: ListDeadLetterTasksForSubscription :many
SELECT *
FROM dead_letter_tasks
WHERE subscription_id = ?
ORDER BY failed_at DESC
LIMIT ? OFFSET ?;

-- name: GetDeadLetterTask :one
SELECT *
FROM dead_letter_tasks
WHERE id = ?;

-- name: UpdateDeadLetterTaskStatus :exec
UPDATE dead_letter_tasks
SET status = ?, last_attempt_at = CURRENT_TIMESTAMP, attempt_count = attempt_count + 1, error_details = ?
WHERE id = ?;

-- name: DeleteDeadLetterTask :exec
DELETE FROM dead_letter_tasks
WHERE id = ?;

-- name: CountDeadLetterTasksForSubscription :one
SELECT COUNT(*) as count
FROM dead_letter_tasks
WHERE subscription_id = ?;

-- name: GetDLQSummary :many
SELECT 
    subscription_id,
    status,
    COUNT(*) as count,
    MAX(failed_at) as latest_failure
FROM dead_letter_tasks
GROUP BY subscription_id, status
ORDER BY latest_failure DESC;

-- name: ListDeadLetterTasksByStatus :many
SELECT *
FROM dead_letter_tasks
WHERE status = ?
ORDER BY failed_at DESC
LIMIT ? OFFSET ?;

-- name: GetDLQStats :one
SELECT 
    COUNT(*) as total,
    SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END) as dead,
    SUM(CASE WHEN status = 'retried' THEN 1 ELSE 0 END) as retried,
    SUM(CASE WHEN status = 'deleted' THEN 1 ELSE 0 END) as deleted
FROM dead_letter_tasks;

-- name: PurgeRetriedDLQTasks :exec
DELETE FROM dead_letter_tasks
WHERE status = 'retried' AND last_attempt_at < datetime('now', '-' || CAST(sqlc.arg(older_than_days) AS INTEGER) || ' days');

-- name: BulkDeleteDLQTasks :exec
DELETE FROM dead_letter_tasks
WHERE id IN (sqlc.slice('ids'));

-- name: ListAllDeadLetterTasks :many
SELECT *
FROM dead_letter_tasks
ORDER BY failed_at DESC
LIMIT ? OFFSET ?;