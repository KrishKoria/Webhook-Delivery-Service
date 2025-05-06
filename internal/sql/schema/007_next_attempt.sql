-- +goose up
ALTER TABLE delivery_tasks ADD COLUMN next_attempt_at DATETIME;

-- Create an index for more efficient queries
CREATE INDEX idx_delivery_tasks_next_attempt ON delivery_tasks(next_attempt_at);

-- +goose down
DROP INDEX IF EXISTS idx_delivery_tasks_next_attempt;