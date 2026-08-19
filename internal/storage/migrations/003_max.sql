-- MAX messenger settings and daily statistics
INSERT OR IGNORE INTO settings (key, value) VALUES
    ('max_enabled', 'false'),
    ('max_bot_token', ''),
    ('max_admin_id', ''),
    ('max_ink_alerts', 'true');

CREATE TABLE IF NOT EXISTS daily_stats (
    day TEXT PRIMARY KEY NOT NULL,
    revenue REAL NOT NULL DEFAULT 0,
    pages_bw INTEGER NOT NULL DEFAULT 0,
    pages_color INTEGER NOT NULL DEFAULT 0,
    scans INTEGER NOT NULL DEFAULT 0,
    copies INTEGER NOT NULL DEFAULT 0,
    sheets_used INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS kv_state (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL
);
