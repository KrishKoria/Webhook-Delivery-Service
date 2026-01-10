-- +goose up
-- Add leasing columns for atomic task claiming and worker concurrency control
-- claimed_at: When a worker claimed this task
-- claimed_by: Worker identifier that claimed this task (for debugging/monitoring)
-- lease_until: When the lease expires (task becomes available for re-claim)

ALTER TABLE delivery_tasks ADD COLUMN claimed_at DATETIME;
ALTER TABLE delivery_tasks ADD COLUMN claimed_by TEXT;
ALTER TABLE delivery_tasks ADD COLUMN lease_until DATETIME;

-- Index for efficient claim queries:
-- Find tasks that are: pending, not claimed OR lease expired, and due for attempt
CREATE INDEX idx_delivery_tasks_claimable ON delivery_tasks(status, lease_until, next_attempt_at);

-- Index for monitoring claimed tasks per worker
CREATE INDEX idx_delivery_tasks_claimed_by ON delivery_tasks(claimed_by, lease_until);

-- +goose down
DROP INDEX IF EXISTS idx_delivery_tasks_claimable;
DROP INDEX IF EXISTS idx_delivery_tasks_claimed_by;
-- Note: SQLite doesn't support DROP COLUMN, so we leave the columns in place for down migration
