-- Initial schema for the random video chat MVP.
-- Every statement is idempotent so the file can be re-applied on boot.

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    status          TEXT        NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    suspended_until TIMESTAMPTZ,
    suspend_reason  TEXT,
    last_ip         TEXT
);

CREATE INDEX IF NOT EXISTS users_suspended_until_idx ON users (suspended_until);

CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    participant_a TEXT        NOT NULL,
    participant_b TEXT        NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,
    status        TEXT        NOT NULL DEFAULT 'active',
    end_reason    TEXT
);

CREATE INDEX IF NOT EXISTS sessions_started_at_idx ON sessions (started_at DESC);
CREATE INDEX IF NOT EXISTS sessions_participant_a_idx ON sessions (participant_a);
CREATE INDEX IF NOT EXISTS sessions_participant_b_idx ON sessions (participant_b);

CREATE TABLE IF NOT EXISTS reports (
    id          TEXT PRIMARY KEY,
    reporter_id TEXT        NOT NULL,
    target_id   TEXT        NOT NULL,
    session_id  TEXT,
    category    TEXT        NOT NULL DEFAULT 'other',
    reason      TEXT        NOT NULL DEFAULT '',
    details     TEXT,
    status      TEXT        NOT NULL DEFAULT 'open',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS reports_status_idx ON reports (status, created_at DESC);
CREATE INDEX IF NOT EXISTS reports_target_idx ON reports (target_id, created_at DESC);

CREATE TABLE IF NOT EXISTS blocks (
    blocker_id TEXT        NOT NULL,
    blocked_id TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);

CREATE INDEX IF NOT EXISTS blocks_blocked_idx ON blocks (blocked_id);

CREATE TABLE IF NOT EXISTS moderation_actions (
    id         TEXT PRIMARY KEY,
    target_id  TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    reason     TEXT,
    actor      TEXT        NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS moderation_actions_created_idx ON moderation_actions (created_at DESC);
