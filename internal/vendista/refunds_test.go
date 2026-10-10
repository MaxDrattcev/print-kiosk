package vendista

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefundOnceAndRevenueOnce(t *testing.T) {
	db, e := storage.Open(filepath.Join(t.TempDir(), "refund.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = storage.Migrate(db); e != nil {
		t.Fatal(e)
	}
	repo := storage.NewSettingsRepo(db)
	st := stats.NewRepo(db)
	if e = st.AddRevenue(16); e != nil {
		t.Fatal(e)
	}
	order := "scan-job:scan:0"
	if e = repo.CreatePayment(storage.Payment{OrderID: order, TerminalID: "293416", Amount: 1600, StartedAt: time.Now().Format(time.RFC3339)}); e != nil {
		t.Fatal(e)
	}
	if e = repo.FinishPayment(order, "paid", 123); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = repo.QueueJobRefunds("scan-job"); e != nil {
			t.Fatal(e)
		}
	}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			if r.URL.Path != "/transactions/123/cancel" {
				t.Error(r.URL.Path)
			}
			w.Write([]byte(`{"success":true}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "item": map[string]any{"id": 123, "term_id": 293416, "status": 2, "left_sum": 0}})
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client()}
	for i := 0; i < 2; i++ {
		if e = c.ProcessRefunds(context.Background(), repo); e != nil {
			t.Fatal(e)
		}
	}
	if posts.Load() != 1 {
		t.Fatal("duplicate refund POST")
	}
	p, _ := repo.Payment(order)
	if p.State != "refunded" {
		t.Fatal(p.State)
	}
	total, e := st.GetTotal()
	if e != nil || total.Revenue != 0 {
		t.Fatalf("revenue %+v %v", total, e)
	}
	if e = repo.ConfirmRefund(order); e != nil {
		t.Fatal(e)
	}
	total, _ = st.GetTotal()
	if total.Revenue != 0 {
		t.Fatal("refund counted twice")
	}
	if e = c.Pay(context.Background(), repo, order, 16); e == nil {
		t.Fatal("refunded payment reused")
	}
}
func TestLostRefundReplyDoesNotResend(t *testing.T) {
	repo := testRepo(t)
	order := "scan-job:scan:0"
	if e := repo.CreatePayment(storage.Payment{OrderID: order, TerminalID: "293416", Amount: 1600, StartedAt: time.Now().Format(time.RFC3339)}); e != nil {
		t.Fatal(e)
	}
	if e := repo.FinishPayment(order, "paid", 123); e != nil {
		t.Fatal(e)
	}
	if e := repo.QueueScanRefund(order); e != nil {
		t.Fatal(e)
	}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			w.WriteHeader(502)
			return
		}
		w.Write([]byte(`{"success":true,"item":{"id":123,"term_id":293416,"status":1,"left_sum":1600}}`))
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client()}
	for i := 0; i < 2; i++ {
		if e := c.ProcessRefunds(context.Background(), repo); e != nil {
			t.Fatal(e)
		}
	}
	if posts.Load() != 1 {
		t.Fatal("lost response caused duplicate refund")
	}
	rows, e := repo.ScanRefunds()
	if e != nil || len(rows) != 1 || rows[0].RefundState != "unknown" {
		t.Fatalf("%+v %v", rows, e)
	}
}
func TestPartialRefundIsNotConfirmedAsFull(t *testing.T) {
	repo := testRepo(t)
	order := "scan-job:scan:0"
	repo.CreatePayment(storage.Payment{OrderID: order, TerminalID: "293416", Amount: 1600, StartedAt: time.Now().Format(time.RFC3339)})
	repo.FinishPayment(order, "paid", 123)
	repo.QueueScanRefund(order)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Write([]byte(`{"success":true}`))
			return
		}
		w.Write([]byte(`{"success":true,"item":{"id":123,"term_id":293416,"status":2,"left_sum":800}}`))
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client()}
	if e := c.ProcessRefunds(context.Background(), repo); e != nil {
		t.Fatal(e)
	}
	p, _ := repo.Payment(order)
	if p.State == "refunded" {
		t.Fatal("partial refund accepted as full")
	}
}
