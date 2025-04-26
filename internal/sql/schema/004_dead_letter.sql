-- +goose up
CREATE TABLE IF NOT EXISTS dead_letter_tasks (
    id TEXT PRIMARY KEY,
    original_task_id TEXT NOT NULL,
    subscription_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    failed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reason TEXT NOT NULL,
    last_attempt_at DATETIME,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, retried, resolved
    target_url TEXT,
    event_type TEXT,
    error_details TEXT,
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id)
);

CREATE INDEX IF NOT EXISTS idx_dead_letter_subscription_id ON dead_letter_tasks(subscription_id);
CREATE INDEX IF NOT EXISTS idx_dead_letter_status ON dead_letter_tasks(status);

-- +goose down
DROP TABLE IF EXISTS dead_letter_tasks;