-- name: CreateSubscription :exec
INSERT INTO subscriptions (id, target_url, secret, event_types)
VALUES (?, ?, ?, ?);

-- name: UpdateSubscription :exec
UPDATE subscriptions
SET target_url = ?, secret = ?, event_types = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = ?;

-- name: ListSubscriptions :many
SELECT * FROM subscriptions ORDER BY created_at DESC;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = ?;

