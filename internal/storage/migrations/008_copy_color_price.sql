INSERT OR IGNORE INTO settings (key, value)
SELECT 'price_copy_color', COALESCE(
    (SELECT value FROM settings WHERE key = 'price_color'),
    '15'
);
