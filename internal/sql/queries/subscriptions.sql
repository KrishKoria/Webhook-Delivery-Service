-- name: CreateSubscription :exec
INSERT INTO subscriptions (id, target_url, secret, created_at, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = ?;

-- name: ListSubscriptions :many
SELECT * FROM subscriptions ORDER BY created_at DESC;

-- name: UpdateSubscription :exec
UPDATE subscriptions
SET target_url = ?, secret = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = ?;