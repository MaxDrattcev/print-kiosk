ALTER TABLE vendista_payments ADD COLUMN refund_state TEXT NOT NULL DEFAULT '';
ALTER TABLE vendista_payments ADD COLUMN refund_error TEXT NOT NULL DEFAULT '';
