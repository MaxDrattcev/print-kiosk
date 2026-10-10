package vendista

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"print-kiosk/internal/storage"
	"sync/atomic"
	"testing"
	"time"
)

func testRepo(t *testing.T) *storage.SettingsRepo {
	t.Helper()
	db, e := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	if e = storage.Migrate(db); e != nil {
		t.Fatal(e)
	}
	return storage.NewSettingsRepo(db)
}
func TestPaymentAndPersistentRetry(t *testing.T) {
	repo := testRepo(t)
	var commands atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "secret" {
			t.Error("missing token")
		}
		switch r.URL.Path {
		case "/transactions":
			items := []Transaction{{ID: 10, Terminal: 293416, Amount: 1600, Status: 1, Time: time.Now().Add(-time.Hour).Format(time.RFC3339)}}
			if commands.Load() > 0 {
				items = append(items, Transaction{ID: 11, Terminal: 293416, Amount: 1600, Status: 1, Time: time.Now().Format(time.RFC3339Nano)})
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "items": items})
		case "/terminals/293416/commands":
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if in["command_id"] != float64(32) || in["parameter1"] != float64(1600) {
				t.Errorf("wrong request: %v", in)
			}
			commands.Add(1)
			w.Write([]byte(`{"success":true,"item":{"id":42}}`))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client(), Timeout: time.Second, Poll: time.Millisecond}
	for i := 0; i < 2; i++ {
		if e := c.Pay(context.Background(), repo, "order", 16); e != nil {
			t.Fatal(e)
		}
	}
	if commands.Load() != 1 {
		t.Fatal("duplicate payment command")
	}
	p, e := repo.Payment("order")
	if e != nil || p.TransactionID != 11 || p.State != "paid" || p.CommandID != 42 {
		t.Fatalf("journal: %+v %v", p, e)
	}
	if e = c.Pay(context.Background(), repo, "order", 17); e == nil {
		t.Fatal("changed amount accepted")
	}
}
func TestUncertainPaymentNeverResubmitted(t *testing.T) {
	repo := testRepo(t)
	var commands atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			commands.Add(1)
			w.WriteHeader(502)
			return
		}
		w.Write([]byte(`{"success":true,"items":[]}`))
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client(), Timeout: time.Hour, Poll: time.Millisecond}
	if e := c.Pay(context.Background(), repo, "order", 16); !errors.Is(e, ErrUncertain) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := c.Pay(ctx, repo, "order", 16); !errors.Is(e, ErrUncertain) {
		t.Fatal(e)
	}
	if commands.Load() != 1 {
		t.Fatal("uncertain command resubmitted")
	}
	if e := c.Pay(context.Background(), repo, "other", 16); e == nil {
		t.Fatal("other order accepted while unresolved")
	}
}
func TestReconcileRejectsWrongTransactions(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []Transaction
		want string
	}{
		{"wrong amount", []Transaction{{ID: 11, Terminal: 293416, Amount: 1700, Status: 1}}, "pending"},
		{"wrong terminal", []Transaction{{ID: 11, Terminal: 9, Amount: 1600, Status: 1}}, "pending"},
		{"old ID", []Transaction{{ID: 10, Terminal: 293416, Amount: 1600, Status: 1}}, "pending"},
		{"declined", []Transaction{{ID: 11, Terminal: 293416, Amount: 1600, Status: 3}}, "failed"},
		{"refunded", []Transaction{{ID: 11, Terminal: 293416, Amount: 1600, Status: 2}}, "failed"},
		{"ambiguous", []Transaction{{ID: 11, Terminal: 293416, Amount: 1600, Status: 1}, {ID: 12, Terminal: 293416, Amount: 1600, Status: 1}}, "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepo(t)
			now := time.Now().UTC()
			p := storage.Payment{OrderID: "order", TerminalID: "293416", Amount: 1600, Baseline: 10, StartedAt: now.Format(time.RFC3339Nano)}
			if e := repo.CreatePayment(p); e != nil {
				t.Fatal(e)
			}
			for i := range tc.rows {
				tc.rows[i].Time = now.Format(time.RFC3339Nano)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"success": true, "items": tc.rows})
			}))
			defer server.Close()
			c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client()}
			done, _ := c.reconcile(context.Background(), repo, p)
			if done {
				t.Fatal("invalid payment accepted")
			}
			got, _ := repo.Payment("order")
			if got.State != tc.want {
				t.Fatalf("state %s", got.State)
			}
		})
	}
}
func TestBusinessFailureAndTokenRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"error":"token=secret"}`))
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client()}
	_, e := c.transactions(context.Background(), "")
	if e == nil {
		t.Fatal("business failure ignored")
	}
	if e.Error() == "token=secret" {
		t.Fatal("secret exposed")
	}
}
func TestTerminalStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "item": map[string]any{"id": 293416, "last_online_time": time.Now().Format(time.RFC3339Nano)}})
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client()}
	if s := c.Check(context.Background()); s["status"] != "ok" {
		t.Fatal(s)
	}
}

func TestTimeoutSendsStandbyWithoutRecharging(t *testing.T) {
	repo := testRepo(t)
	var pay, standby atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var in struct {
				Command int `json:"command_id"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.Command == 32 {
				pay.Add(1)
			} else if in.Command == 78 {
				standby.Add(1)
			} else {
				t.Error(in.Command)
			}
			w.Write([]byte(`{"success":true,"item":{"id":5}}`))
			return
		}
		w.Write([]byte(`{"success":true,"items":[]}`))
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Terminal: "293416", HTTP: server.Client(), Timeout: 5 * time.Millisecond, Poll: time.Millisecond}
	for i := 0; i < 2; i++ {
		if e := c.Pay(context.Background(), repo, "order", 16); !errors.Is(e, ErrUncertain) {
			t.Fatal(e)
		}
	}
	if pay.Load() != 1 || standby.Load() != 2 {
		t.Fatalf("pay=%d standby=%d", pay.Load(), standby.Load())
	}
	p, _ := repo.Payment("order")
	if p.State != "pending" {
		t.Fatal("unknown payment cleared automatically")
	}
}
func TestJournalRejectsReusedTransaction(t *testing.T) {
	repo := testRepo(t)
	for _, order := range []string{"one", "two"} {
		if e := repo.CreatePayment(storage.Payment{OrderID: order, TerminalID: "293416", Amount: 1600, StartedAt: time.Now().Format(time.RFC3339)}); e != nil {
			t.Fatal(e)
		}
		e := repo.FinishPayment(order, "paid", 123)
		if order == "one" && e != nil {
			t.Fatal(e)
		}
		if order == "two" && e == nil {
			t.Fatal("same transaction accepted twice")
		}
	}
}
