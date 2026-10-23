-- Session shape updates: promotions and cap changes are written back to the
-- audit row. Columns already exist from 0002; the guards keep this file
-- idempotent on databases that skipped an intermediate migration.
-- Every statement is idempotent so the file can be re-applied on boot.

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'solo';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS max_participants INT NOT NULL DEFAULT 0;
