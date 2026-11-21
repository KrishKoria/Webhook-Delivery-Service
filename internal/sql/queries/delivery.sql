-- name: ListPendingDeliveryTasks :many
SELECT * FROM delivery_tasks
WHERE status = 'pending' AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP)
ORDER BY created_at ASC
LIMIT 10;

-- name: CreateDeliveryTask :exec
INSERT INTO delivery_tasks (id, subscription_id, payload, event_type, status, attempt_count, created_at, next_attempt_at)
VALUES (?, ?, ?, ?, 'pending', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- name: UpdateDeliveryTaskStatus :exec
UPDATE delivery_tasks
SET status = ?, last_attempt_at = CURRENT_TIMESTAMP, attempt_count = ?
WHERE id = ?;

-- name: CreateDeliveryLog :exec
INSERT INTO delivery_logs (
    id, delivery_task_id, subscription_id, target_url, timestamp,
    attempt_number, outcome, http_status, error_details
) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?);

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
SET next_attempt_at = datetime('now', CAST(sqlc.arg(delay_seconds) AS INTEGER) || ' seconds')
WHERE id = sqlc.arg(id);

-- name: ClaimDeliveryTask :one
-- Two-step atomic claim is required because Turso/libSQL doesn't support subqueries in WHERE.
-- This is now a simple direct claim by ID - use GetNextClaimableTaskId first to get the ID.
UPDATE delivery_tasks
SET claimed_at = CURRENT_TIMESTAMP,
    claimed_by = sqlc.arg(worker_id),
    lease_until = datetime(CURRENT_TIMESTAMP, '+' || sqlc.arg(lease_seconds) || ' seconds')
WHERE id = sqlc.arg(task_id)
  AND status = 'pending'
  AND (lease_until IS NULL OR lease_until < CURRENT_TIMESTAMP)
  AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP)
RETURNING *;

-- name: GetNextClaimableTaskId :one
-- Get the ID of the next task that can be claimed.
-- This must be called first, then ClaimDeliveryTask with the returned ID.
SELECT id FROM delivery_tasks
WHERE status = 'pending'
  AND (lease_until IS NULL OR lease_until < CURRENT_TIMESTAMP)
  AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP)
ORDER BY created_at ASC
LIMIT 1;

-- name: ReleaseTaskLease :exec
-- Release the lease on a task (called after successful delivery or when giving up).
UPDATE delivery_tasks
SET claimed_at = NULL,
    claimed_by = NULL,
    lease_until = NULL
WHERE id = ?;

-- name: ExtendTaskLease :exec
-- Extend the lease on a task that is taking longer than expected.
UPDATE delivery_tasks
SET lease_until = datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds')
WHERE id = ? AND claimed_by = ?;

-- name: ListClaimedTasksByWorker :many
-- List tasks currently claimed by a specific worker (for monitoring/debugging).
SELECT * FROM delivery_tasks
WHERE claimed_by = ? AND lease_until > CURRENT_TIMESTAMP
ORDER BY claimed_at ASC;

-- name: CountPendingClaimableTasks :one
-- Count tasks that are available to be claimed (for metrics/monitoring).
SELECT COUNT(*) as count FROM delivery_tasks
WHERE status = 'pending'
  AND (lease_until IS NULL OR lease_until < CURRENT_TIMESTAMP)
  AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP);

-- name: UpdateDeliveryTaskStatusAndReleaseLease :exec
-- Update task status and release the lease in one operation.
UPDATE delivery_tasks
SET status = ?,
    last_attempt_at = CURRENT_TIMESTAMP,
    attempt_count = ?,
    claimed_at = NULL,
    claimed_by = NULL,
    lease_until = NULL
WHERE id = ?;

-- name: GetDeliveryStats :one
-- Get delivery statistics for a time period.
SELECT
    COUNT(*) as total_deliveries,
    SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) as successful,
    SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END) as failed_attempts,
    AVG(CASE WHEN outcome = 'success' THEN attempt_number ELSE NULL END) as avg_attempts_to_success
FROM delivery_logs
WHERE timestamp >= datetime('now', '-' || ? || ' hours');

-- name: GetDeliveryStatsBySubscription :one
-- Get delivery statistics for a specific subscription.
SELECT
    COUNT(*) as total_deliveries,
    SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) as successful,
    SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END) as failed_attempts,
    AVG(CASE WHEN outcome = 'success' THEN attempt_number ELSE NULL END) as avg_attempts_to_success
FROM delivery_logs
WHERE subscription_id = ? AND timestamp >= datetime('now', '-' || ? || ' hours');

-- name: GetDeliveryLogsByOutcome :many
-- List delivery logs filtered by outcome.
SELECT * FROM delivery_logs
WHERE outcome = ?
ORDER BY timestamp DESC
LIMIT ?;

-- name: GetRecentFailedDeliveries :many
-- Get recent failed delivery attempts for monitoring.
SELECT
    dl.*,
    s.target_url as subscription_url,
    s.event_types as subscription_event_types
FROM delivery_logs dl
JOIN subscriptions s ON dl.subscription_id = s.id
WHERE dl.outcome = 'failed_attempt'
ORDER BY dl.timestamp DESC
LIMIT ?;

-- name: GetTaskQueueDepth :one
-- Get count of tasks by status for queue monitoring.
SELECT
    SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
    SUM(CASE WHEN status = 'delivered' THEN 1 ELSE 0 END) as delivered,
    SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed
FROM delivery_tasks;

-- name: GetAverageDeliveryLatency :one
-- Calculate average time from task creation to successful delivery.
SELECT AVG(
    (julianday(dl.timestamp) - julianday(dt.created_at)) * 86400
) as avg_latency_seconds
FROM delivery_logs dl
JOIN delivery_tasks dt ON dl.delivery_task_id = dt.id
WHERE dl.outcome = 'success'
  AND dl.timestamp >= datetime('now', '-' || ? || ' hours');

-- name: GetDeliveryRateByHour :many
-- Get delivery counts grouped by hour for the last N hours.
SELECT
    strftime('%Y-%m-%d %H:00', timestamp) as hour,
    COUNT(*) as total,
    SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) as successful,
    SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END) as failed
FROM delivery_logs
WHERE timestamp >= datetime('now', '-' || ? || ' hours')
GROUP BY strftime('%Y-%m-%d %H:00', timestamp)
ORDER BY hour DESC;

-- name: ListBacklogTasks :many
-- List tasks in the backlog (pending, not yet claimed, with retry info).
SELECT 
    dt.*,
    s.target_url as subscription_url
FROM delivery_tasks dt
JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status = 'pending'
ORDER BY dt.created_at ASC
LIMIT ?;

-- name: ListRetryingTasks :many
-- List tasks that are being retried (pending with attempt_count > 0).
SELECT 
    dt.*,
    s.target_url as subscription_url
FROM delivery_tasks dt
JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status = 'pending' AND dt.attempt_count > 0
ORDER BY dt.next_attempt_at ASC
LIMIT ?;

-- name: ListActiveClaimedTasks :many
-- List tasks currently being processed by workers.
SELECT 
    dt.*,
    s.target_url as subscription_url
FROM delivery_tasks dt
JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.claimed_at IS NOT NULL AND dt.lease_until > CURRENT_TIMESTAMP
ORDER BY dt.claimed_at ASC
LIMIT ?;

-- name: GetBacklogStats :one
-- Get comprehensive backlog statistics.
SELECT
    COUNT(*) as total_pending,
    SUM(CASE WHEN attempt_count = 0 THEN 1 ELSE 0 END) as new_tasks,
    SUM(CASE WHEN attempt_count > 0 THEN 1 ELSE 0 END) as retrying_tasks,
    SUM(CASE WHEN claimed_at IS NOT NULL AND lease_until > CURRENT_TIMESTAMP THEN 1 ELSE 0 END) as claimed_tasks,
    SUM(CASE WHEN next_attempt_at IS NOT NULL AND next_attempt_at > CURRENT_TIMESTAMP THEN 1 ELSE 0 END) as waiting_for_retry,
    MAX(attempt_count) as max_retry_count,
    AVG(attempt_count) as avg_retry_count
FROM delivery_tasks
WHERE status = 'pending';

-- name: GetBacklogBySubscription :many
-- Get backlog count grouped by subscription.
SELECT
    dt.subscription_id,
    s.target_url as subscription_url,
    COUNT(*) as pending_count,
    SUM(CASE WHEN dt.attempt_count > 0 THEN 1 ELSE 0 END) as retrying_count,
    MAX(dt.attempt_count) as max_retries,
    MIN(dt.created_at) as oldest_task
FROM delivery_tasks dt
JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status = 'pending'
GROUP BY dt.subscription_id
ORDER BY pending_count DESC
LIMIT ?;

-- name: GetRetryDistribution :many
-- Get distribution of retry counts.
SELECT
    attempt_count as retry_count,
    COUNT(*) as task_count
FROM delivery_tasks
WHERE status = 'pending' AND attempt_count > 0
GROUP BY attempt_count
ORDER BY attempt_count ASC;