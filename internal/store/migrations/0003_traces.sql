CREATE TABLE traces (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    ts                INTEGER NOT NULL,           -- unix milliseconds (record start)
    conversation_id   TEXT NOT NULL DEFAULT '',
    turn_id           TEXT NOT NULL DEFAULT '',
    step              INTEGER NOT NULL DEFAULT 0, -- 0 = turn row, 1.. = LLM step (and its tool calls)
    kind              TEXT NOT NULL CHECK (kind IN ('turn','llm','tool')),
    name              TEXT NOT NULL DEFAULT '',   -- model or tool name
    status            TEXT NOT NULL CHECK (status IN ('ok','error','canceled')),
    duration_ms       INTEGER NOT NULL DEFAULT 0,
    first_token_ms    INTEGER NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    finish_reason     TEXT NOT NULL DEFAULT '',
    args              TEXT NOT NULL DEFAULT '',   -- truncated to 1 KiB
    result_summary    TEXT NOT NULL DEFAULT '',   -- truncated to 1 KiB
    error             TEXT NOT NULL DEFAULT '',
    channel           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_traces_conv_ts ON traces(conversation_id, ts);
CREATE INDEX idx_traces_turn ON traces(turn_id);
CREATE INDEX idx_traces_ts ON traces(ts);
CREATE INDEX idx_traces_status ON traces(status);
