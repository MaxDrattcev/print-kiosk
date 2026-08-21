CREATE TABLE operation_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at INTEGER NOT NULL,
    operation TEXT NOT NULL,
    job_id TEXT NOT NULL DEFAULT '',
    pages INTEGER NOT NULL DEFAULT 0,
    sheets INTEGER NOT NULL DEFAULT 0,
    amount REAL NOT NULL DEFAULT 0,
    success INTEGER NOT NULL DEFAULT 0,
    error_text TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_operation_history_created_at
    ON operation_history(created_at);
