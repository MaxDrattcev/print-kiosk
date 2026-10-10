package paymaster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"print-kiosk/internal/storage"
	"testing"
	"time"
)

func TestSBPProviderCycleAndIdempotency(t *testing.T) {
	p := storage.SBPPayment{Attempt: "d405afcd-3ee6-4c9d-9907-842f1977acb1", OrderID: "scan:0", Merchant: "shop", Amount: 2100, Expires: time.Now().Add(time.Minute).Unix()}
	status := "Confirmation"
	refundStatus := "Pending"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing server-side auth")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v2/payments":
			calls++
			if r.Header.Get("Idempotency-Key") != p.Attempt {
				t.Error("creation key changed")
			}
			var body map[string]any
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Fatal(e)
			}
			if body["paymentData"].(map[string]any)["paymentMethod"] != "sbp" || body["testMode"] != false {
				t.Error("incorrect payment request")
			}
		case "GET /api/v2/payments/p1":
		case "PUT /api/v2/payments/p1/cancel":
			status = "Cancelled"
			return
		case "POST /api/v2/refunds":
			if r.Header.Get("Idempotency-Key") != "refund-"+p.Attempt {
				t.Error("refund key changed")
			}
			json.NewEncoder(w).Encode(Refund{ID: "r1", PaymentID: "p1", Status: refundStatus, Amount: Amount{21, "RUB"}})
			return
		case "GET /api/v2/refunds/r1":
			json.NewEncoder(w).Encode(Refund{ID: "r1", PaymentID: "p1", Status: refundStatus, Amount: Amount{21, "RUB"}})
			return
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		out := Payment{ID: "p1", Merchant: "shop", Status: status, Amount: Amount{21, "RUB"}}
		out.Invoice.Order = p.OrderID
		out.PaymentData.Method = "sbp"
		out.Confirmation.Type = "External"
		out.Confirmation.URL = "https://qr.nspk.ru/example"
		json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Merchant: "shop", HTTP: server.Client()}
	for i := 0; i < 2; i++ {
		out, e := c.Create(context.Background(), p)
		if e != nil || out.ID != "p1" {
			t.Fatalf("create: %v %+v", e, out)
		}
	}
	p.PaymentID = "p1"
	status = "Settled"
	out, e := c.Get(context.Background(), p)
	if e != nil || out.Status != "Settled" {
		t.Fatal(e)
	}
	refund, e := c.Refund(context.Background(), p)
	if e != nil || refund.Status != "Pending" {
		t.Fatal(e)
	}
	p.RefundID = refund.ID
	refundStatus = "Success"
	refund, e = c.Refund(context.Background(), p)
	if e != nil || refund.Status != "Success" {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestRejectMismatchedOrTestPayment(t *testing.T) {
	p := storage.SBPPayment{OrderID: "order", Merchant: "shop", Amount: 100, PaymentID: "p1"}
	base := Payment{ID: "p1", Merchant: "shop", Status: "Settled", Amount: Amount{1, "RUB"}}
	base.Invoice.Order = "order"
	base.PaymentData.Method = "sbp"
	c := &Client{}
	if e := c.Validate(p, base); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Payment){func(p *Payment) { p.ID = "other" }, func(p *Payment) { p.Test = true }, func(p *Payment) { p.Merchant = "other" }, func(p *Payment) { p.Amount.Value = 2 }, func(p *Payment) { p.Amount.Currency = "USD" }, func(p *Payment) { p.Invoice.Order = "other" }, func(p *Payment) { p.PaymentData.Method = "bankcard" }} {
		out := base
		mutate(&out)
		if c.Validate(p, out) == nil {
			t.Fatal("accepted mismatched payment")
		}
	}
}

func TestLostCreateResponseRecoveredWithoutNewCharge(t *testing.T) {
	p := storage.SBPPayment{OrderID: "lost-order", Merchant: "shop", Amount: 100, Expires: time.Now().Unix()}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v2/payments" {
			t.Errorf("recovery attempted mutation: %s %s", r.Method, r.URL.Path)
		}
		out := Payment{ID: "original", Merchant: "shop", Amount: Amount{1, "RUB"}, Status: "Settled"}
		out.Invoice.Order = p.OrderID
		out.PaymentData.Method = "sbp"
		json.NewEncoder(w).Encode(map[string]any{"items": []Payment{out}})
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Token: "secret", Merchant: "shop", HTTP: server.Client()}
	out, found, e := c.Find(context.Background(), p)
	if e != nil || !found || out.ID != "original" || calls != 1 {
		t.Fatalf("%+v %v %v calls=%d", out, found, e, calls)
	}
}
