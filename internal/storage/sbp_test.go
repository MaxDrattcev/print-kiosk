package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSBPRefundIsDurableAndRevenueAdjustedOnce(t *testing.T) {
	db, e := Open(filepath.Join(t.TempDir(), "sbp.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = Migrate(db); e != nil {
		t.Fatal(e)
	}
	r := NewSettingsRepo(db)
	p := SBPPayment{Attempt: "a", OrderID: "job:scan:0", Merchant: "shop", Amount: 2100, Expires: time.Now().Add(time.Minute).Unix()}
	if e = r.CreateSBP(p); e != nil {
		t.Fatal(e)
	}
	if e = r.CreateSBP(SBPPayment{Attempt: "b", OrderID: "other", Amount: 100}); e == nil {
		t.Fatal("parallel pending allowed")
	}
	if e = r.SBPState("a", "paid", ""); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT OR IGNORE INTO daily_stats(day) VALUES(date('now','localtime'))`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`UPDATE daily_stats SET revenue=21 WHERE day=date('now','localtime')`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`UPDATE total_stats SET revenue=21 WHERE id=1`)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.AccountSBP(p.OrderID); e != nil {
		t.Fatal(e)
	}
	if e = r.QueueJobRefunds("job"); e != nil {
		t.Fatal(e)
	}
	work, e := r.SBPWork()
	if e != nil || len(work) != 1 || work[0].State != "refund_pending" {
		t.Fatalf("%+v %v", work, e)
	}
	for i := 0; i < 2; i++ {
		if e = r.ConfirmSBPRefund("a"); e != nil {
			t.Fatal(e)
		}
	}
	var revenue float64
	if e = db.QueryRow(`SELECT revenue FROM total_stats WHERE id=1`).Scan(&revenue); e != nil {
		t.Fatal(e)
	}
	if revenue != 0 {
		t.Fatal(revenue)
	}
	p, e = r.SBP("a")
	if e != nil || p.State != "refunded" {
		t.Fatal(p, e)
	}
}
