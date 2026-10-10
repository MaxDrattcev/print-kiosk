package storage

import (
	"fmt"
	"time"
)

type Refund struct {
	Payment
	RefundState string
}

func (r *SettingsRepo) QueueScanRefund(order string) error {
	tx, e := r.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE vendista_payments SET state='refund_pending',refund_state='queued' WHERE order_id=? AND state='paid' AND transaction_id IS NOT NULL`, order); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE sbp_payments SET state='refund_pending',refund_state='queued' WHERE order_id=? AND state='paid'`, order); e != nil {
		return e
	}
	return tx.Commit()
}
func (r *SettingsRepo) ScanRefunds() ([]Refund, error) {
	rows, e := r.db.Query(`SELECT order_id,terminal_id,amount,transaction_id,refund_state FROM vendista_payments WHERE refund_state IN ('queued','sending','sent','unknown')`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Refund
	for rows.Next() {
		var p Refund
		if e := rows.Scan(&p.OrderID, &p.TerminalID, &p.Amount, &p.TransactionID, &p.RefundState); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *SettingsRepo) ClaimRefund(order string) (bool, error) {
	res, e := r.db.Exec(`UPDATE vendista_payments SET refund_state='sending' WHERE order_id=? AND refund_state='queued'`, order)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}
func (r *SettingsRepo) SetRefundState(order, state, message string) error {
	_, e := r.db.Exec(`UPDATE vendista_payments SET refund_state=?,refund_error=? WHERE order_id=? AND refund_state<>'confirmed'`, state, message, order)
	return e
}

// ConfirmRefund updates the journal and revenue in one transaction, once.
func (r *SettingsRepo) ConfirmRefund(order string) error {
	tx, e := r.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var amount int64
	var state string
	if e = tx.QueryRow(`SELECT amount,refund_state FROM vendista_payments WHERE order_id=?`, order).Scan(&amount, &state); e != nil {
		return e
	}
	if state == "confirmed" {
		return nil
	}
	if state == "" {
		return fmt.Errorf("возврат не запрошен")
	}
	if _, e = tx.Exec(`UPDATE vendista_payments SET state='refunded',refund_state='confirmed',refund_error='' WHERE order_id=?`, order); e != nil {
		return e
	}
	day := time.Now().Format("2006-01-02")
	value := float64(amount) / 100
	if _, e = tx.Exec(`INSERT OR IGNORE INTO daily_stats(day) VALUES(?)`, day); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE daily_stats SET revenue=revenue-? WHERE day=?`, value, day); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE total_stats SET revenue=revenue-? WHERE id=1`, value); e != nil {
		return e
	}
	return tx.Commit()
}

func (r *SettingsRepo) QueueJobRefunds(jobID string) error {
	prefix := jobID + ":scan:"
	tx, e := r.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE vendista_payments SET state='refund_pending',refund_state='queued' WHERE substr(order_id,1,?)=? AND state='paid' AND transaction_id IS NOT NULL`, len(prefix), prefix); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE sbp_payments SET state='refund_pending',refund_state='queued' WHERE substr(order_id,1,?)=? AND state='paid'`, len(prefix), prefix); e != nil {
		return e
	}
	return tx.Commit()
}

type RefundInfo struct {
	OrderID string `json:"order_id"`
	Amount  int64  `json:"amount_kopecks"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

func (r *SettingsRepo) RecentRefunds() ([]RefundInfo, error) {
	rows, e := r.db.Query(`SELECT order_id,amount,refund_state,refund_error FROM vendista_payments WHERE refund_state<>'' UNION ALL SELECT order_id,amount,refund_state,error FROM sbp_payments WHERE refund_state<>'' LIMIT 40`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []RefundInfo{}
	for rows.Next() {
		var p RefundInfo
		if e = rows.Scan(&p.OrderID, &p.Amount, &p.State, &p.Error); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
