CREATE TABLE conversations (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    channel    TEXT NOT NULL CHECK (channel IN ('web','cli','speaker')),
    created_at INTEGER NOT NULL,  -- unix milliseconds
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_conversations_updated ON conversations(updated_at DESC);

CREATE TABLE messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    tool_calls      TEXT,          -- JSON array, NULL when none
    tool_call_id    TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL
);
CREATE INDEX idx_messages_conv ON messages(conversation_id, id);

CREATE TABLE providers (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL DEFAULT 'openai',
    name       TEXT NOT NULL DEFAULT '',
    config     TEXT,               -- JSON; secrets are not stored here in MVP
    updated_at INTEGER NOT NULL
);

CREATE TABLE devices (
    id         TEXT PRIMARY KEY,
    driver     TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    token      TEXT,               -- JSON
    updated_at INTEGER NOT NULL
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE audit_log (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    ts     INTEGER NOT NULL,
    kind   TEXT NOT NULL,
    actor  TEXT NOT NULL DEFAULT '',
    detail TEXT                    -- JSON
);
CREATE INDEX idx_audit_ts ON audit_log(ts DESC);
