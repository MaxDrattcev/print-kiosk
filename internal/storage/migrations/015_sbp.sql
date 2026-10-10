INSERT OR IGNORE INTO settings(key,value) VALUES('payment_qr_enabled','false'),('paymaster_token',''),('paymaster_merchant_id','');
UPDATE settings SET value='false' WHERE key='payment_qr_enabled';
CREATE TABLE sbp_payments (
 attempt TEXT PRIMARY KEY, order_id TEXT NOT NULL, merchant TEXT NOT NULL,
 amount INTEGER NOT NULL, test INTEGER NOT NULL, payment_id TEXT NOT NULL DEFAULT '',
 qr_url TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'pending',
 expires INTEGER NOT NULL, cancel_requested INTEGER NOT NULL DEFAULT 0,
 orphan INTEGER NOT NULL DEFAULT 0, accounted INTEGER NOT NULL DEFAULT 0,
 refund_id TEXT NOT NULL DEFAULT '', refund_state TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX sbp_one_pending ON sbp_payments((1)) WHERE state IN ('pending','unknown');
CREATE UNIQUE INDEX sbp_one_paid_order ON sbp_payments(order_id) WHERE state='paid';
