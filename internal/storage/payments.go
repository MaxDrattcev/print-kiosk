package storage

import "database/sql"

type Payment struct {
	OrderID, TerminalID, StartedAt, State      string
	Amount, Baseline, TransactionID, CommandID int64
}

func (r *SettingsRepo) Payment(order string) (Payment, error) {
	var p Payment
	err := r.db.QueryRow(`SELECT order_id,terminal_id,amount,baseline,started_at,state,COALESCE(transaction_id,0),COALESCE(command_id,0) FROM vendista_payments WHERE order_id=?`, order).Scan(&p.OrderID, &p.TerminalID, &p.Amount, &p.Baseline, &p.StartedAt, &p.State, &p.TransactionID, &p.CommandID)
	return p, err
}
func (r *SettingsRepo) PendingPayment() (Payment, error) {
	var id string
	err := r.db.QueryRow(`SELECT order_id FROM vendista_payments WHERE state='pending' LIMIT 1`).Scan(&id)
	if err != nil {
		return Payment{}, err
	}
	return r.Payment(id)
}
func (r *SettingsRepo) CreatePayment(p Payment) error {
	_, err := r.db.Exec(`INSERT INTO vendista_payments(order_id,terminal_id,amount,baseline,started_at,state) VALUES(?,?,?,?,?,'pending')`, p.OrderID, p.TerminalID, p.Amount, p.Baseline, p.StartedAt)
	return err
}
func (r *SettingsRepo) PaymentCommand(order string, id int64) error {
	_, err := r.db.Exec(`UPDATE vendista_payments SET command_id=? WHERE order_id=?`, id, order)
	return err
}
func (r *SettingsRepo) FinishPayment(order, state string, id int64) error {
	var tx any
	if id != 0 {
		tx = id
	}
	_, err := r.db.Exec(`UPDATE vendista_payments SET state=?,transaction_id=? WHERE order_id=?`, state, tx, order)
	return err
}
func IsNoPayment(err error) bool { return err == sql.ErrNoRows }
