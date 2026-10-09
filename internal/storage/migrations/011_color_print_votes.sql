CREATE TABLE IF NOT EXISTS color_print_votes (
    session_id TEXT PRIMARY KEY NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('print', 'copy')),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
