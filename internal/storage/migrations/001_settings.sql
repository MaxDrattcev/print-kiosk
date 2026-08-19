CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO settings (key, value) VALUES
    ('price_bw', '5'),
    ('price_color', '15'),
    ('paper_remaining', '500'),
    ('paper_alert_threshold', '50'),
    ('telegram_heartbeat_interval', '24h'),
    ('telegram_cartridge_alerts', 'true'),
    ('email_address', ''),
    ('email_login', ''),
    ('email_password', ''),
    ('email_poll_interval_sec', '30'),
    ('email_max_file_size_mb', '20'),
    ('payment_enabled', 'true'),
    ('session_timeout_sec', '120'),
    ('support_text', 'Обратитесь к администратору');
