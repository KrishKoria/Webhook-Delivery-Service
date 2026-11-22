-- name: UISubscriptionCounts :one
SELECT
    COUNT(*) AS total,
    CAST(COALESCE(SUM(CASE WHEN paused <> 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS paused
FROM subscriptions;

-- name: UIListSubscriptions :many
SELECT * FROM subscriptions
ORDER BY created_at DESC, id
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: UIGetSubscription :one
SELECT * FROM subscriptions WHERE id = ?;

-- name: UICreateSubscription :exec
INSERT INTO subscriptions (id, target_url, secret, event_types)
VALUES (?, ?, ?, ?);

-- name: UITaskStatusCounts :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending,
    CAST(COALESCE(SUM(CASE WHEN status = 'in_flight' THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_flight,
    CAST(COALESCE(SUM(CASE WHEN status = 'delivered' THEN 1 ELSE 0 END), 0) AS INTEGER) AS delivered,
    CAST(COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed
FROM delivery_tasks;

-- name: UITaskStatusCountsForSubscription :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending,
    CAST(COALESCE(SUM(CASE WHEN status = 'in_flight' THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_flight,
    CAST(COALESCE(SUM(CASE WHEN status = 'delivered' THEN 1 ELSE 0 END), 0) AS INTEGER) AS delivered,
    CAST(COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed
FROM delivery_tasks
WHERE subscription_id = ?;

-- name: UIAttemptStats :one
-- Delivery attempts in the trailing window, across all subscriptions.
SELECT
    CAST(COALESCE(SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END), 0) AS INTEGER) AS successful,
    CAST(COALESCE(SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed
FROM delivery_logs
WHERE timestamp >= datetime('now', '-' || CAST(sqlc.arg(hours) AS INTEGER) || ' hours');

-- name: UIAttemptStatsForSubscription :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END), 0) AS INTEGER) AS successful,
    CAST(COALESCE(SUM(CASE WHEN outcome = 'failed_attempt' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed,
    CAST(COALESCE(AVG(CASE WHEN outcome = 'success' THEN attempt_number END), 0) AS REAL) AS avg_attempts_to_success,
    COALESCE(CAST(MAX(timestamp) AS TEXT), '') AS last_attempt_at
FROM delivery_logs
WHERE subscription_id = sqlc.arg(subscription_id)
  AND timestamp >= datetime('now', '-' || CAST(sqlc.arg(hours) AS INTEGER) || ' hours');

-- name: UIDLQCounts :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END), 0) AS INTEGER) AS dead,
    CAST(COALESCE(SUM(CASE WHEN status = 'retried' THEN 1 ELSE 0 END), 0) AS INTEGER) AS retried
FROM dead_letter_tasks;

-- name: UIDLQCountsForSubscription :one
SELECT
    CAST(COALESCE(SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END), 0) AS INTEGER) AS dead,
    CAST(COALESCE(SUM(CASE WHEN status = 'retried' THEN 1 ELSE 0 END), 0) AS INTEGER) AS retried
FROM dead_letter_tasks
WHERE subscription_id = ?;

-- name: UIScheduledCounts :one
-- Legacy rows marked 'delivered' were handed to the queue, so they count as enqueued.
SELECT
    CAST(COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS INTEGER) AS pending,
    CAST(COALESCE(SUM(CASE WHEN status IN ('enqueued', 'delivered') THEN 1 ELSE 0 END), 0) AS INTEGER) AS enqueued,
    CAST(COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed,
    CAST(COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END), 0) AS INTEGER) AS cancelled
FROM scheduled_webhooks;

-- name: UIListDeliveryLogs :many
-- Newest first. Pass an empty string to skip a filter; before_ts/before_id form a keyset cursor.
SELECT
    dl.id,
    dl.delivery_task_id,
    dl.timestamp,
    dl.attempt_number,
    dl.outcome,
    dl.http_status,
    dl.error_details,
    dt.status AS task_status,
    dt.event_type
FROM delivery_logs dl
LEFT JOIN delivery_tasks dt ON dt.id = dl.delivery_task_id
WHERE dl.subscription_id = sqlc.arg(subscription_id)
  AND (CAST(sqlc.arg(outcome) AS TEXT) = '' OR dl.outcome = sqlc.arg(outcome))
  AND (CAST(sqlc.arg(task_status) AS TEXT) = '' OR dt.status = sqlc.arg(task_status))
  AND (CAST(sqlc.arg(before_ts) AS TEXT) = ''
       OR dl.timestamp < sqlc.arg(before_ts)
       OR (dl.timestamp = sqlc.arg(before_ts) AND dl.id < CAST(sqlc.arg(before_id) AS TEXT)))
ORDER BY dl.timestamp DESC, dl.id DESC
LIMIT sqlc.arg(lim);

-- name: UIRecentFailuresForSubscription :many
SELECT
    dl.id,
    dl.delivery_task_id,
    dl.timestamp,
    dl.attempt_number,
    dl.http_status,
    dl.error_details
FROM delivery_logs dl
WHERE dl.subscription_id = sqlc.arg(subscription_id) AND dl.outcome = 'failed_attempt'
ORDER BY dl.timestamp DESC, dl.id DESC
LIMIT sqlc.arg(lim);

-- name: UIGetDeliveryTask :one
SELECT * FROM delivery_tasks WHERE id = ?;

-- name: UIListDLQ :many
-- Pass an empty string to skip a filter.
SELECT * FROM dead_letter_tasks
WHERE (CAST(sqlc.arg(status) AS TEXT) = '' OR status = sqlc.arg(status))
  AND (CAST(sqlc.arg(subscription_id) AS TEXT) = '' OR subscription_id = sqlc.arg(subscription_id))
ORDER BY failed_at DESC, id DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: UIGetDLQEntry :one
SELECT * FROM dead_letter_tasks WHERE id = ?;

-- name: UIDeleteDLQEntries :execrows
DELETE FROM dead_letter_tasks WHERE id IN (sqlc.slice('ids'));

-- name: UIListScheduled :many
-- Pass an empty subscription_id to list across all subscriptions.
SELECT * FROM scheduled_webhooks
WHERE (CAST(sqlc.arg(subscription_id) AS TEXT) = '' OR subscription_id = sqlc.arg(subscription_id))
ORDER BY scheduled_for ASC, id
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: UIGetScheduled :one
SELECT * FROM scheduled_webhooks WHERE id = ?;

-- name: UICreateScheduled :exec
INSERT INTO scheduled_webhooks (id, subscription_id, payload, scheduled_for, recurrence, status)
VALUES (
    sqlc.arg(id), sqlc.arg(subscription_id), sqlc.arg(payload),
    datetime(sqlc.arg(scheduled_for)), sqlc.narg(recurrence), 'pending'
);

-- name: UICancelScheduled :execrows
-- Only pending rows can be cancelled; zero rows means it already fired or was cancelled.
UPDATE scheduled_webhooks
SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = 'pending';

-- name: UIDeleteScheduled :execrows
DELETE FROM scheduled_webhooks WHERE id = ?;

-- name: UIBacklogStats :one
SELECT
    COUNT(*) AS total_pending,
    CAST(COALESCE(SUM(CASE WHEN attempt_count = 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS new_tasks,
    CAST(COALESCE(SUM(CASE WHEN attempt_count > 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS retrying_tasks,
    CAST(COALESCE(SUM(CASE WHEN next_attempt_at > CURRENT_TIMESTAMP THEN 1 ELSE 0 END), 0) AS INTEGER) AS waiting_for_retry,
    CAST(COALESCE(MAX(attempt_count), 0) AS INTEGER) AS max_attempts,
    COALESCE(CAST(MIN(created_at) AS TEXT), '') AS oldest_created_at
FROM delivery_tasks
WHERE status = 'pending';

-- name: UIInFlightCount :one
SELECT COUNT(*) FROM delivery_tasks WHERE status = 'in_flight';

-- name: UIBacklogBySubscription :many
SELECT
    dt.subscription_id,
    COALESCE(s.target_url, '') AS target_url,
    COUNT(*) AS pending_count,
    CAST(COALESCE(SUM(CASE WHEN dt.attempt_count > 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS retrying_count,
    CAST(COALESCE(MAX(dt.attempt_count), 0) AS INTEGER) AS max_attempts,
    COALESCE(CAST(MIN(dt.created_at) AS TEXT), '') AS oldest_created_at
FROM delivery_tasks dt
LEFT JOIN subscriptions s ON s.id = dt.subscription_id
WHERE dt.status = 'pending'
GROUP BY dt.subscription_id
ORDER BY pending_count DESC
LIMIT sqlc.arg(lim);

-- name: UIRetryDistribution :many
SELECT attempt_count, COUNT(*) AS task_count
FROM delivery_tasks
WHERE status = 'pending' AND attempt_count > 0
GROUP BY attempt_count
ORDER BY attempt_count ASC;

-- name: UIListTasksByStatus :many
-- In-flight or waiting tasks for the backlog view.
SELECT
    dt.id,
    dt.subscription_id,
    COALESCE(s.target_url, '') AS target_url,
    dt.status,
    dt.attempt_count,
    dt.created_at,
    dt.next_attempt_at,
    dt.claimed_by,
    dt.lease_until
FROM delivery_tasks dt
LEFT JOIN subscriptions s ON s.id = dt.subscription_id
WHERE dt.status = sqlc.arg(status)
  AND (CAST(sqlc.arg(retrying_only) AS INTEGER) = 0 OR dt.attempt_count > 0)
ORDER BY COALESCE(dt.next_attempt_at, dt.created_at) ASC
LIMIT sqlc.arg(lim);
