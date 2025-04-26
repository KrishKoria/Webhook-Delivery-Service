-- name: InsertDeadLetterTask :exec
INSERT INTO dead_letter_tasks (
    id, original_task_id, subscription_id, payload, failed_at, reason, last_attempt_at, attempt_count, status, target_url, event_type, error_details
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
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
SET status = ?, last_attempt_at = ?, attempt_count = attempt_count + 1, error_details = ?
WHERE id = ?;

-- name: DeleteDeadLetterTask :exec
DELETE FROM dead_letter_tasks
WHERE id = ?;