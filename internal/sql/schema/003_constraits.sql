-- +goose up

ALTER TABLE delivery_logs RENAME TO delivery_logs_old;
ALTER TABLE delivery_tasks RENAME TO delivery_tasks_old;

CREATE TABLE delivery_tasks (
    id TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL,
    last_attempt_at DATETIME,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE
);

CREATE TABLE delivery_logs (
    id TEXT PRIMARY KEY,
    delivery_task_id TEXT NOT NULL,
    subscription_id TEXT NOT NULL,
    target_url TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    attempt_number INTEGER NOT NULL,
    outcome TEXT NOT NULL,
    http_status INTEGER,
    error_details TEXT,
    FOREIGN KEY(delivery_task_id) REFERENCES delivery_tasks(id) ON DELETE CASCADE,
    FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE
);
INSERT INTO delivery_tasks SELECT * FROM delivery_tasks_old;
INSERT INTO delivery_logs SELECT * FROM delivery_logs_old;

DROP TABLE delivery_logs_old;
DROP TABLE delivery_tasks_old;

-- +goose down
DROP TABLE IF EXISTS delivery_logs;
DROP TABLE IF EXISTS delivery_tasks;