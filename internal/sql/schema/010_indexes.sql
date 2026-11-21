-- +goose up
CREATE INDEX IF NOT EXISTS idx_delivery_tasks_status_next ON delivery_tasks(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_delivery_tasks_status_lease ON delivery_tasks(status, lease_until);
CREATE INDEX IF NOT EXISTS idx_delivery_logs_timestamp ON delivery_logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_delivery_logs_sub_timestamp ON delivery_logs(subscription_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_delivery_logs_outcome_timestamp ON delivery_logs(outcome, timestamp);
CREATE INDEX IF NOT EXISTS idx_dead_letter_status_failed ON dead_letter_tasks(status, failed_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_status_for ON scheduled_webhooks(status, scheduled_for);

-- +goose down
DROP INDEX IF EXISTS idx_delivery_tasks_status_next;
DROP INDEX IF EXISTS idx_delivery_tasks_status_lease;
DROP INDEX IF EXISTS idx_delivery_logs_timestamp;
DROP INDEX IF EXISTS idx_delivery_logs_sub_timestamp;
DROP INDEX IF EXISTS idx_delivery_logs_outcome_timestamp;
DROP INDEX IF EXISTS idx_dead_letter_status_failed;
DROP INDEX IF EXISTS idx_scheduled_status_for;
