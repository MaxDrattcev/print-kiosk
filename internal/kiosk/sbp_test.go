package kiosk

import (
	"context"
	"github.com/google/uuid"
	"path/filepath"
	"print-kiosk/internal/storage"
	"testing"
	"time"
)

func sbpTestHandler(t *testing.T) *Handler {
	t.Helper()
	db, e := storage.Open(filepath.Join(t.TempDir(), "sbp.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	if e = storage.Migrate(db); e != nil {
		t.Fatal(e)
	}
	r := storage.NewSettingsRepo(db)
	if e = r.SetMany(map[string]string{storage.SettingPaymentQREnabled: "true", storage.SettingTestPaymentMode: "true"}); e != nil {
		t.Fatal(e)
	}
	return &Handler{settings: r}
}
func waitSBP(t *testing.T, h *Handler, attempt string) storage.SBPPayment {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, e := h.settings.SBP(attempt)
		if e == nil && p.QRURL != "" {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("QR not prepared")
	return storage.SBPPayment{}
}
func TestSBPTestConfirmDoesNotChargeAndCannotPayTwice(t *testing.T) {
	h := sbpTestHandler(t)
	attempt := uuid.NewString()
	done := make(chan error, 1)
	go func() { done <- h.processPayment(context.Background(), "qr:"+attempt, 21, "scan:0") }()
	p := waitSBP(t, h, attempt)
	if !p.Test {
		t.Fatal("not test")
	}
	if e := h.settings.SBPTestPaid(attempt); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := h.processPayment(context.Background(), "terminal", 21, "scan:0"); e == nil {
		t.Fatal("card accepted already paid order")
	}
	if e := h.processPayment(context.Background(), "qr:"+uuid.NewString(), 22, "scan:0"); e == nil {
		t.Fatal("changed amount accepted")
	}
}
func TestSBPCancelAndRetry(t *testing.T) {
	h := sbpTestHandler(t)
	attempt := uuid.NewString()
	done := make(chan error, 1)
	go func() { done <- h.processPayment(context.Background(), "qr:"+attempt, 10, "order") }()
	waitSBP(t, h, attempt)
	if e := h.settings.CancelSBP(attempt); e != nil {
		t.Fatal(e)
	}
	if e := h.settings.SBPTestPaid(attempt); e == nil {
		t.Fatal("confirmed after cancellation")
	}
	if e := <-done; e == nil {
		t.Fatal("cancel returned success")
	}
	p, e := h.settings.SBP(attempt)
	if e != nil || p.State != "cancelled" {
		t.Fatalf("%+v %v", p, e)
	}
	attempt = uuid.NewString()
	go func() { done <- h.processPayment(context.Background(), "qr:"+attempt, 10, "order") }()
	waitSBP(t, h, attempt)
	_ = h.settings.SBPTestPaid(attempt)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
func TestSBPInterruptedRequestIsRecovered(t *testing.T) {
	h := sbpTestHandler(t)
	attempt := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.processPayment(ctx, "qr:"+attempt, 10, "order") }()
	waitSBP(t, h, attempt)
	cancel()
	if e := <-done; e == nil {
		t.Fatal("disconnected returned success")
	}
	p, _ := h.settings.SBP(attempt)
	if !p.Orphan {
		t.Fatal("lost request not recorded")
	}
	h.processSBPWork()
	p, _ = h.settings.SBP(attempt)
	if p.State != "cancelled" {
		t.Fatal(p.State)
	}
}
