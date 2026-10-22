-- Group conferences: sessions hold N participants with a mode and a cap,
-- tracked per member in session_participants.
-- Every statement is idempotent so the file can be re-applied on boot.

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'solo';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS max_participants INT NOT NULL DEFAULT 0;

-- The 1:1 columns stay for history but are no longer the source of truth.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'sessions' AND column_name = 'participant_a' AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE sessions ALTER COLUMN participant_a DROP NOT NULL;
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'sessions' AND column_name = 'participant_b' AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE sessions ALTER COLUMN participant_b DROP NOT NULL;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS session_participants (
    session_id TEXT        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    peer_id    TEXT        NOT NULL,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, peer_id)
);

CREATE INDEX IF NOT EXISTS session_participants_peer_idx ON session_participants (peer_id);
