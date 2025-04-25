-- +goose up
ALTER TABLE subscriptions ADD COLUMN event_types TEXT;
-- +goose down
ALTER TABLE subscriptions DROP COLUMN event_types;