-- name: CreateSubscription :exec
INSERT INTO subscriptions (id, target_url, secret, event_types)
VALUES (?, ?, ?, ?);

-- name: UpdateSubscription :exec
UPDATE subscriptions
SET target_url = ?, secret = ?, event_types = ?
WHERE id = ?;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = ?;

-- name: ListSubscriptions :many
SELECT id, target_url, secret, created_at, updated_at, event_types FROM subscriptions;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = ?;

