-- +goose up
-- For delivery_tasks
CREATE INDEX IF NOT EXISTS idx_delivery_tasks_subscription_id ON delivery_tasks(subscription_id);
CREATE INDEX IF NOT EXISTS idx_delivery_tasks_next_attempt_at ON delivery_tasks(next_attempt_at);

-- For delivery_logs
CREATE INDEX IF NOT EXISTS idx_delivery_logs_delivery_task_id ON delivery_logs(delivery_task_id);
CREATE INDEX IF NOT EXISTS idx_delivery_logs_subscription_id ON delivery_logs(subscription_id);

-- +goose down
DROP INDEX IF EXISTS idx_delivery_tasks_subscription_id;
DROP INDEX IF EXISTS idx_delivery_tasks_next_attempt_at;
DROP INDEX IF EXISTS idx_delivery_logs_delivery_task_id;
DROP INDEX IF EXISTS idx_delivery_logs_subscription_id;