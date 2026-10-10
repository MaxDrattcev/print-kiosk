package storage

import (
	"fmt"
	"time"
)

type SBPPayment struct {
	Attempt, OrderID, Merchant, PaymentID, QRURL, State, RefundID, RefundState, Error string
	Amount, Expires                                                                   int64
	Test, Cancel, Orphan, Accounted                                                   bool
}

const sbpColumns = `attempt,order_id,merchant,amount,test,payment_id,qr_url,state,expires,cancel_requested,orphan,accounted,refund_id,refund_state,error`

func (r *SettingsRepo) SBP(attempt string) (p SBPPayment, err error) {
	err = r.db.QueryRow(`SELECT `+sbpColumns+` FROM sbp_payments WHERE attempt=?`, attempt).Scan(&p.Attempt, &p.OrderID, &p.Merchant, &p.Amount, &p.Test, &p.PaymentID, &p.QRURL, &p.State, &p.Expires, &p.Cancel, &p.Orphan, &p.Accounted, &p.RefundID, &p.RefundState, &p.Error)
	return
}
func (r *SettingsRepo) SBPOrder(order string) (SBPPayment, error) {
	var attempt string
	err := r.db.QueryRow(`SELECT attempt FROM sbp_payments WHERE order_id=? AND state IN ('pending','unknown','paid','refunded','refund_pending') ORDER BY expires DESC LIMIT 1`, order).Scan(&attempt)
	if err != nil {
		return SBPPayment{}, err
	}
	return r.SBP(attempt)
}
func (r *SettingsRepo) SBPBlocked() (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM sbp_payments WHERE state IN ('pending','unknown')`).Scan(&n)
	return n > 0, err
}
func (r *SettingsRepo) CreateSBP(p SBPPayment) error {
	_, e := r.db.Exec(`INSERT INTO sbp_payments(attempt,order_id,merchant,amount,test,expires) VALUES(?,?,?,?,?,?)`, p.Attempt, p.OrderID, p.Merchant, p.Amount, p.Test, p.Expires)
	return e
}
func (r *SettingsRepo) SBPDetails(attempt, id, qr string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET payment_id=?,qr_url=? WHERE attempt=?`, id, qr, attempt)
	return e
}
func (r *SettingsRepo) SBPState(attempt, state, message string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET state=?,error=? WHERE attempt=?`, state, message, attempt)
	return e
}
func (r *SettingsRepo) CancelSBP(attempt string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET cancel_requested=1 WHERE attempt=? AND state IN ('pending','unknown')`, attempt)
	return e
}
func (r *SettingsRepo) OrphanSBP(attempt string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET orphan=1,cancel_requested=1 WHERE attempt=?`, attempt)
	return e
}
func (r *SettingsRepo) AccountSBP(order string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET accounted=1 WHERE order_id=? AND state='paid'`, order)
	return e
}
func (r *SettingsRepo) SBPWork() ([]SBPPayment, error) {
	rows, e := r.db.Query(`SELECT attempt FROM sbp_payments WHERE state IN ('pending','unknown','refund_pending') OR (state='paid' AND orphan=1)`)
	if e != nil {
		return nil, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var out []SBPPayment
	for _, id := range ids {
		p, e := r.SBP(id)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (r *SettingsRepo) QueueSBPRefund(order string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET state='refund_pending',refund_state='queued' WHERE order_id=? AND state='paid'`, order)
	return e
}
func (r *SettingsRepo) SBPRefund(attempt, id, state string) error {
	_, e := r.db.Exec(`UPDATE sbp_payments SET refund_id=?,refund_state=? WHERE attempt=?`, id, state, attempt)
	return e
}
func (r *SettingsRepo) ConfirmSBPRefund(attempt string) error {
	tx, e := r.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var amount int64
	var state string
	var accounted bool
	if e = tx.QueryRow(`SELECT amount,state,accounted FROM sbp_payments WHERE attempt=?`, attempt).Scan(&amount, &state, &accounted); e != nil {
		return e
	}
	if state == "refunded" {
		return nil
	}
	if state != "refund_pending" {
		return fmt.Errorf("возврат не запрошен")
	}
	if _, e = tx.Exec(`UPDATE sbp_payments SET state='refunded',refund_state='confirmed',error='' WHERE attempt=?`, attempt); e != nil {
		return e
	}
	if accounted {
		if _, e = tx.Exec(`INSERT OR IGNORE INTO daily_stats(day) VALUES(date('now','localtime'))`); e != nil {
			return e
		}
		if _, e = tx.Exec(`UPDATE daily_stats SET revenue=revenue-? WHERE day=date('now','localtime')`, float64(amount)/100); e != nil {
			return e
		}
		if _, e = tx.Exec(`UPDATE total_stats SET revenue=revenue-? WHERE id=1`, float64(amount)/100); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (r *SettingsRepo) SBPTestPaid(attempt string) error {
	res, e := r.db.Exec(`UPDATE sbp_payments SET state='paid' WHERE attempt=? AND test=1 AND state='pending' AND cancel_requested=0 AND expires>strftime('%s','now')`, attempt)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("платёж недоступен")
	}
	return nil
}

func (r *SettingsRepo) ResolveSBPNoCharge(p SBPPayment) error {
	res, e := r.db.Exec(`UPDATE sbp_payments SET state='cancelled',error='Администратор подтвердил отсутствие списания' WHERE attempt=? AND payment_id=? AND state IN ('pending','unknown') AND expires<?`, p.Attempt, p.PaymentID, time.Now().Unix()-180)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("состояние платежа изменилось; выполните проверку повторно")
	}
	return nil
}
