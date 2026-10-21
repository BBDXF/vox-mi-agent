CREATE TABLE aliases (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    title   TEXT NOT NULL,
    artist  TEXT NOT NULL DEFAULT '',
    aliases TEXT NOT NULL DEFAULT '[]',   -- JSON array of strings
    updated_at INTEGER NOT NULL,
    UNIQUE (title, artist)
);
