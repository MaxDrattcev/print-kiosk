-- Remove the old prefilled example only from an unconfigured installation.
UPDATE settings SET value='', updated_at=datetime('now')
WHERE key='vendista_terminal_id' AND value='293416'
AND NOT EXISTS (SELECT 1 FROM settings WHERE key='vendista_token' AND value<>'')
AND NOT EXISTS (SELECT 1 FROM vendista_payments);
