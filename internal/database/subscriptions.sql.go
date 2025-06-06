// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: subscriptions.sql

package database

import (
	"context"
	"database/sql"
)

const createSubscription = `-- name: CreateSubscription :exec
INSERT INTO subscriptions (id, target_url, secret, event_types)
VALUES (?, ?, ?, ?)
`

type CreateSubscriptionParams struct {
	ID         string
	TargetUrl  string
	Secret     sql.NullString
	EventTypes sql.NullString
}

func (q *Queries) CreateSubscription(ctx context.Context, arg CreateSubscriptionParams) error {
	_, err := q.db.ExecContext(ctx, createSubscription,
		arg.ID,
		arg.TargetUrl,
		arg.Secret,
		arg.EventTypes,
	)
	return err
}

const deleteSubscription = `-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = ?
`

func (q *Queries) DeleteSubscription(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, deleteSubscription, id)
	return err
}

const getSubscription = `-- name: GetSubscription :one
SELECT id, target_url, secret, created_at, updated_at, event_types FROM subscriptions WHERE id = ?
`

func (q *Queries) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	row := q.db.QueryRowContext(ctx, getSubscription, id)
	var i Subscription
	err := row.Scan(
		&i.ID,
		&i.TargetUrl,
		&i.Secret,
		&i.CreatedAt,
		&i.UpdatedAt,
		&i.EventTypes,
	)
	return i, err
}

const listSubscriptions = `-- name: ListSubscriptions :many
SELECT id, target_url, secret, created_at, updated_at, event_types FROM subscriptions
`

func (q *Queries) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := q.db.QueryContext(ctx, listSubscriptions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Subscription
	for rows.Next() {
		var i Subscription
		if err := rows.Scan(
			&i.ID,
			&i.TargetUrl,
			&i.Secret,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.EventTypes,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateSubscription = `-- name: UpdateSubscription :exec
UPDATE subscriptions
SET target_url = ?, secret = ?, event_types = ?
WHERE id = ?
`

type UpdateSubscriptionParams struct {
	TargetUrl  string
	Secret     sql.NullString
	EventTypes sql.NullString
	ID         string
}

func (q *Queries) UpdateSubscription(ctx context.Context, arg UpdateSubscriptionParams) error {
	_, err := q.db.ExecContext(ctx, updateSubscription,
		arg.TargetUrl,
		arg.Secret,
		arg.EventTypes,
		arg.ID,
	)
	return err
}
