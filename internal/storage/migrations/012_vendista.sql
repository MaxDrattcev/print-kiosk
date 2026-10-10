CREATE TABLE IF NOT EXISTS vendista_payments (
 order_id TEXT PRIMARY KEY, terminal_id TEXT NOT NULL, amount INTEGER NOT NULL,
 baseline INTEGER NOT NULL, started_at TEXT NOT NULL, state TEXT NOT NULL,
 transaction_id INTEGER UNIQUE, command_id INTEGER
);
INSERT OR IGNORE INTO settings(key,value) VALUES ('vendista_terminal_id',''),('vendista_token',''),('vendista_timeout_sec','90');
CREATE UNIQUE INDEX IF NOT EXISTS vendista_one_pending ON vendista_payments(state) WHERE state='pending';
