-- +goose up
CREATE TABLE IF NOT EXISTS subscriptions (
    id TEXT PRIMARY KEY,
    target_url TEXT NOT NULL,
    secret TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS delivery_tasks (
    id TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL, -- pending, delivered, failed
    last_attempt_at DATETIME,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id)
);

CREATE TABLE IF NOT EXISTS delivery_logs (
    id TEXT PRIMARY KEY,
    delivery_task_id TEXT NOT NULL,
    subscription_id TEXT NOT NULL,
    target_url TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    attempt_number INTEGER NOT NULL,
    outcome TEXT NOT NULL, -- success, failed_attempt, failure
    http_status INTEGER,
    error_details TEXT,
    FOREIGN KEY(delivery_task_id) REFERENCES delivery_tasks(id),
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id)
);

-- +goose down
DROP TABLE IF EXISTS delivery_logs;
DROP TABLE IF EXISTS delivery_tasks;
DROP TABLE IF EXISTS subscriptions;