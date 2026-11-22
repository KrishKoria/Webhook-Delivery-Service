-- +goose up
-- Canonical timestamp format is SQLite's own 'YYYY-MM-DD HH:MM:SS' (UTC).
-- Older rows written from Go used RFC3339 ('...T...Z'), which compares
-- incorrectly as text against CURRENT_TIMESTAMP. datetime() rewrites them.
UPDATE delivery_logs SET timestamp = datetime(timestamp) WHERE timestamp LIKE '%T%';
UPDATE delivery_tasks SET last_attempt_at = datetime(last_attempt_at) WHERE last_attempt_at LIKE '%T%';
UPDATE delivery_tasks SET next_attempt_at = datetime(next_attempt_at) WHERE next_attempt_at LIKE '%T%';
UPDATE delivery_tasks SET created_at = datetime(created_at) WHERE created_at LIKE '%T%';
UPDATE dead_letter_tasks SET failed_at = datetime(failed_at) WHERE failed_at LIKE '%T%';
UPDATE dead_letter_tasks SET last_attempt_at = datetime(last_attempt_at) WHERE last_attempt_at LIKE '%T%';
UPDATE scheduled_webhooks SET scheduled_for = datetime(scheduled_for) WHERE scheduled_for LIKE '%T%';

-- Every task now carries an explicit due time, which keeps the claim predicate index-friendly.
UPDATE delivery_tasks SET next_attempt_at = created_at WHERE next_attempt_at IS NULL;

-- Dead-lettered rows were inserted as 'pending' while the UI and stats expect 'dead'.
UPDATE dead_letter_tasks SET status = 'dead' WHERE status = 'pending';

ALTER TABLE delivery_tasks ADD COLUMN event_type TEXT;
ALTER TABLE delivery_tasks ADD COLUMN idempotency_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_tasks_idempotency
    ON delivery_tasks(subscription_id, idempotency_key);

ALTER TABLE subscriptions ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;

-- +goose down
DROP INDEX IF EXISTS idx_delivery_tasks_idempotency;
UPDATE dead_letter_tasks SET status = 'pending' WHERE status = 'dead';
