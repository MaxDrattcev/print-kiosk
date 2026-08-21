ALTER TABLE daily_stats ADD COLUMN uptime_seconds INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS total_stats (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revenue REAL NOT NULL DEFAULT 0,
    pages_bw INTEGER NOT NULL DEFAULT 0,
    pages_color INTEGER NOT NULL DEFAULT 0,
    scans INTEGER NOT NULL DEFAULT 0,
    copies INTEGER NOT NULL DEFAULT 0,
    sheets_used INTEGER NOT NULL DEFAULT 0,
    uptime_seconds INTEGER NOT NULL DEFAULT 0
);

INSERT OR IGNORE INTO total_stats
    (id, revenue, pages_bw, pages_color, scans, copies, sheets_used, uptime_seconds)
SELECT
    1,
    COALESCE(SUM(revenue), 0),
    COALESCE(SUM(pages_bw), 0),
    COALESCE(SUM(pages_color), 0),
    COALESCE(SUM(scans), 0),
    COALESCE(SUM(copies), 0),
    COALESCE(SUM(sheets_used), 0),
    0
FROM daily_stats;
