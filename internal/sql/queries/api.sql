-- Read and admin queries owned by the JSON API (internal/api).
-- Names carry an API prefix so they never collide with worker or UI queries.
--
-- Cursor pagination: list queries order by (<timestamp>, id) DESC and take an
-- optional cursor pair. Timestamps are compared as canonical SQLite text
-- ('YYYY-MM-DD HH:MM:SS', UTC), so cursor and range params are CAST AS TEXT.

-- name: APIListSubscriptions :many
SELECT * FROM subscriptions
WHERE (CAST(sqlc.narg(cursor_ts) AS TEXT) IS NULL
       OR created_at < CAST(sqlc.narg(cursor_ts) AS TEXT)
       OR (created_at = CAST(sqlc.narg(cursor_ts) AS TEXT) AND id < CAST(sqlc.narg(cursor_id) AS TEXT)))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: APIListDeliveryLogs :many
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
    dt.status AS task_status,
    dt.event_type AS event_type
FROM delivery_logs dl
LEFT JOIN delivery_tasks dt ON dt.id = dl.delivery_task_id
WHERE dl.subscription_id = sqlc.arg(subscription_id)
  AND (CAST(sqlc.narg(outcome) AS TEXT) IS NULL OR dl.outcome = CAST(sqlc.narg(outcome) AS TEXT))
  AND (CAST(sqlc.narg(task_status) AS TEXT) IS NULL OR dt.status = CAST(sqlc.narg(task_status) AS TEXT))
  AND (CAST(sqlc.narg(event_type) AS TEXT) IS NULL OR dt.event_type = CAST(sqlc.narg(event_type) AS TEXT))
  AND (CAST(sqlc.narg(since) AS TEXT) IS NULL OR dl.timestamp >= CAST(sqlc.narg(since) AS TEXT))
  AND (CAST(sqlc.narg(until) AS TEXT) IS NULL OR dl.timestamp < CAST(sqlc.narg(until) AS TEXT))
  AND (CAST(sqlc.narg(cursor_ts) AS TEXT) IS NULL
       OR dl.timestamp < CAST(sqlc.narg(cursor_ts) AS TEXT)
       OR (dl.timestamp = CAST(sqlc.narg(cursor_ts) AS TEXT) AND dl.id < CAST(sqlc.narg(cursor_id) AS TEXT)))
ORDER BY dl.timestamp DESC, dl.id DESC
LIMIT sqlc.arg(lim);

-- name: APIListDeliveryLogsForTask :many
SELECT * FROM delivery_logs
WHERE delivery_task_id = ?
ORDER BY attempt_number ASC, timestamp ASC;

-- name: APIListDLQ :many
SELECT * FROM dead_letter_tasks
WHERE (CAST(sqlc.narg(subscription_id) AS TEXT) IS NULL OR subscription_id = CAST(sqlc.narg(subscription_id) AS TEXT))
  AND (CAST(sqlc.narg(status) AS TEXT) IS NULL OR status = CAST(sqlc.narg(status) AS TEXT))
  AND (CAST(sqlc.narg(cursor_ts) AS TEXT) IS NULL
       OR failed_at < CAST(sqlc.narg(cursor_ts) AS TEXT)
       OR (failed_at = CAST(sqlc.narg(cursor_ts) AS TEXT) AND id < CAST(sqlc.narg(cursor_id) AS TEXT)))
ORDER BY failed_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: APIDeleteDLQTask :execrows
DELETE FROM dead_letter_tasks WHERE id = ?;

-- name: APIBulkDeleteDLQ :execrows
DELETE FROM dead_letter_tasks WHERE id IN (sqlc.slice('ids'));

-- name: APIListScheduled :many
-- Filtering by 'enqueued' also matches legacy 'delivered' rows.
SELECT * FROM scheduled_webhooks
WHERE (CAST(sqlc.narg(subscription_id) AS TEXT) IS NULL OR subscription_id = CAST(sqlc.narg(subscription_id) AS TEXT))
  AND (CAST(sqlc.narg(status) AS TEXT) IS NULL OR status = CAST(sqlc.narg(status) AS TEXT)
       OR (CAST(sqlc.narg(status) AS TEXT) = 'enqueued' AND status = 'delivered'))
  AND (CAST(sqlc.narg(cursor_ts) AS TEXT) IS NULL
       OR scheduled_for < CAST(sqlc.narg(cursor_ts) AS TEXT)
       OR (scheduled_for = CAST(sqlc.narg(cursor_ts) AS TEXT) AND id < CAST(sqlc.narg(cursor_id) AS TEXT)))
ORDER BY scheduled_for DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: APIDeleteScheduled :execrows
DELETE FROM scheduled_webhooks WHERE id = ?;

-- name: APICancelScheduled :execrows
-- Only a pending schedule can be cancelled; 0 rows means missing or already settled.
UPDATE scheduled_webhooks
SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = 'pending';

-- name: APITaskCounts :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending,
    CAST(COALESCE(SUM(CASE WHEN status = 'in_flight' THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_flight,
    CAST(COALESCE(SUM(CASE WHEN status = 'delivered' THEN 1 ELSE 0 END), 0) AS INTEGER) AS delivered,
    CAST(COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed
FROM delivery_tasks;

-- name: APIOutcomeStats :one
-- Delivery attempt outcomes over the last N hours, optionally for one subscription.
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END), 0) AS INTEGER) AS successful,
    CAST(COALESCE(SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed_attempts,
    CAST(COALESCE(AVG(CASE WHEN outcome = 'success' THEN attempt_number END), 0) AS REAL) AS avg_attempts_to_success
FROM delivery_logs
WHERE timestamp >= datetime('now', '-' || CAST(sqlc.arg(hours) AS INTEGER) || ' hours')
  AND (CAST(sqlc.narg(subscription_id) AS TEXT) IS NULL OR subscription_id = CAST(sqlc.narg(subscription_id) AS TEXT));

-- name: APIDeliveryLatency :one
-- Mean seconds from task creation to a successful attempt over the last N hours.
SELECT
    CAST(COUNT(*) AS INTEGER) AS samples,
    CAST(COALESCE(AVG((julianday(dl.timestamp) - julianday(dt.created_at)) * 86400), 0) AS REAL) AS avg_seconds
FROM delivery_logs dl
JOIN delivery_tasks dt ON dl.delivery_task_id = dt.id
WHERE dl.outcome = 'success'
  AND dl.timestamp >= datetime('now', '-' || CAST(sqlc.arg(hours) AS INTEGER) || ' hours');

-- name: APIDeliveryRateByHour :many
SELECT
    CAST(strftime('%Y-%m-%d %H:00', timestamp) AS TEXT) AS hour,
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END), 0) AS INTEGER) AS successful,
    CAST(COALESCE(SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed
FROM delivery_logs
WHERE timestamp >= datetime('now', '-' || CAST(sqlc.arg(hours) AS INTEGER) || ' hours')
GROUP BY strftime('%Y-%m-%d %H:00', timestamp)
ORDER BY hour DESC;

-- name: APIRecentFailures :many
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
    dt.status AS task_status,
    dt.event_type AS event_type
FROM delivery_logs dl
LEFT JOIN delivery_tasks dt ON dt.id = dl.delivery_task_id
WHERE dl.outcome = 'failed_attempt'
ORDER BY dl.timestamp DESC, dl.id DESC
LIMIT sqlc.arg(lim);

-- name: APIDLQCounts :one
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END), 0) AS INTEGER) AS dead,
    CAST(COALESCE(SUM(CASE WHEN status = 'retried' THEN 1 ELSE 0 END), 0) AS INTEGER) AS retried
FROM dead_letter_tasks;

-- name: APIScheduledCounts :one
-- Legacy 'delivered' schedules count as 'enqueued'.
SELECT
    CAST(COUNT(*) AS INTEGER) AS total,
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending,
    CAST(COALESCE(SUM(CASE WHEN status IN ('enqueued', 'delivered') THEN 1 ELSE 0 END), 0) AS INTEGER) AS enqueued,
    CAST(COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed,
    CAST(COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END), 0) AS INTEGER) AS cancelled
FROM scheduled_webhooks;

-- name: APIBacklogStats :one
-- The active backlog is every task not yet settled: pending plus in_flight.
SELECT
    CAST(COUNT(*) AS INTEGER) AS total_active,
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending,
    CAST(COALESCE(SUM(CASE WHEN status = 'in_flight' THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_flight,
    CAST(COALESCE(SUM(CASE WHEN attempt_count = 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS new_tasks,
    CAST(COALESCE(SUM(CASE WHEN attempt_count > 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS retrying_tasks,
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' AND next_attempt_at > CURRENT_TIMESTAMP THEN 1 ELSE 0 END), 0) AS INTEGER) AS waiting_for_retry,
    CAST(COALESCE(MAX(attempt_count), 0) AS INTEGER) AS max_attempts,
    CAST(COALESCE(AVG(attempt_count), 0) AS REAL) AS avg_attempts
FROM delivery_tasks
WHERE status IN ('pending', 'in_flight');

-- name: APIBacklogBySubscription :many
SELECT
    dt.subscription_id,
    CAST(COALESCE(s.target_url, '') AS TEXT) AS subscription_url,
    CAST(COUNT(*) AS INTEGER) AS active_count,
    CAST(COALESCE(SUM(CASE WHEN dt.status = 'in_flight' THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_flight_count,
    CAST(COALESCE(SUM(CASE WHEN dt.attempt_count > 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS retrying_count,
    CAST(COALESCE(MAX(dt.attempt_count), 0) AS INTEGER) AS max_attempts,
    CAST(MIN(dt.created_at) AS TEXT) AS oldest_task
FROM delivery_tasks dt
LEFT JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status IN ('pending', 'in_flight')
GROUP BY dt.subscription_id
ORDER BY active_count DESC
LIMIT sqlc.arg(lim);

-- name: APIRetryDistribution :many
SELECT
    attempt_count,
    CAST(COUNT(*) AS INTEGER) AS task_count
FROM delivery_tasks
WHERE status IN ('pending', 'in_flight') AND attempt_count > 0
GROUP BY attempt_count
ORDER BY attempt_count ASC;

-- name: APIListActiveTasks :many
-- Active backlog (pending + in_flight), oldest first.
SELECT
    dt.id, dt.subscription_id, dt.status, dt.attempt_count, dt.event_type,
    dt.created_at, dt.last_attempt_at, dt.next_attempt_at, dt.claimed_by, dt.lease_until,
    CAST(COALESCE(s.target_url, '') AS TEXT) AS subscription_url
FROM delivery_tasks dt
LEFT JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status IN ('pending', 'in_flight')
ORDER BY dt.created_at ASC, dt.id ASC
LIMIT sqlc.arg(lim);

-- name: APIListRetryingTasks :many
SELECT
    dt.id, dt.subscription_id, dt.status, dt.attempt_count, dt.event_type,
    dt.created_at, dt.last_attempt_at, dt.next_attempt_at, dt.claimed_by, dt.lease_until,
    CAST(COALESCE(s.target_url, '') AS TEXT) AS subscription_url
FROM delivery_tasks dt
LEFT JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status = 'pending' AND dt.attempt_count > 0
ORDER BY dt.next_attempt_at ASC, dt.id ASC
LIMIT sqlc.arg(lim);

-- name: APIListInFlightTasks :many
SELECT
    dt.id, dt.subscription_id, dt.status, dt.attempt_count, dt.event_type,
    dt.created_at, dt.last_attempt_at, dt.next_attempt_at, dt.claimed_by, dt.lease_until,
    CAST(COALESCE(s.target_url, '') AS TEXT) AS subscription_url
FROM delivery_tasks dt
LEFT JOIN subscriptions s ON dt.subscription_id = s.id
WHERE dt.status = 'in_flight'
ORDER BY dt.lease_until ASC, dt.id ASC
LIMIT sqlc.arg(lim);
