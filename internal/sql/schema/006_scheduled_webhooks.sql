-- +goose up
CREATE TABLE IF NOT EXISTS scheduled_webhooks (
    id TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    scheduled_for DATETIME NOT NULL,
    recurrence TEXT DEFAULT 'none', -- 'none', 'daily', 'weekly', 'monthly'
    status TEXT NOT NULL DEFAULT 'pending', -- pending, delivered, failed
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id)
);

CREATE INDEX IF NOT EXISTS idx_scheduled_webhooks_subscription_id ON scheduled_webhooks(subscription_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_webhooks_scheduled_for ON scheduled_webhooks(scheduled_for);

-- +goose down
DROP TABLE IF EXISTS scheduled_webhooks;
DROP INDEX IF EXISTS idx_scheduled_webhooks_subscription_id;
DROP INDEX IF EXISTS idx_scheduled_webhooks_scheduled_for;