package scanjob

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryCancellationGuard(t *testing.T) {
	s, e := NewService(t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.Create(16)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BeginCancelDelivery(job.ID); e == nil {
		t.Fatal("cancel allowed without delivery error")
	}
	if e = s.BeginDelivery(job.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.BeginCancelDelivery(job.ID); e == nil {
		t.Fatal("cancel raced with sending")
	}
	s.EndDelivery(job.ID, false)
	if e = s.BeginCancelDelivery(job.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.BeginDelivery(job.ID); e == nil {
		t.Fatal("sending raced with cancellation")
	}
	s.EndCancelDelivery(job.ID, true)
	if e = s.BeginDelivery(job.ID); e == nil {
		t.Fatal("cancelled scan delivered")
	}
}
func TestSuccessfulDeliveryCannotBeRefunded(t *testing.T) {
	s, e := NewService(t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.Create(16)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BeginDelivery(job.ID); e != nil {
		t.Fatal(e)
	}
	s.EndDelivery(job.ID, true)
	s.NoteDeliveryFailure(job.ID)
	if e = s.BeginCancelDelivery(job.ID); e == nil {
		t.Fatal("received document cancelled")
	}
}

func TestScanCannotStartDuringCancellation(t *testing.T) {
	s, e := NewService(t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.Create(16)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkPaid(job.ID); e != nil {
		t.Fatal(e)
	}
	s.NoteDeliveryFailure(job.ID)
	if e = s.BeginCancelDelivery(job.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ScanPage(job.ID, -1, true); e == nil {
		t.Fatal("scan raced with refund cancellation")
	}
	s.EndCancelDelivery(job.ID, true)
	if _, e = s.ReservePayment(job.ID); e == nil {
		t.Fatal("cancelled job accepted payment")
	}
}

func TestFirstScanFailureRequiresRefund(t *testing.T) {
	dir := t.TempDir()
	s, e := NewService(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.Create(16)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkPaid(job.ID); e != nil {
		t.Fatal(e)
	}
	// A file in place of the output directory causes a real acquisition setup failure.
	if e = os.WriteFile(filepath.Join(dir, job.ID, "pages"), []byte("blocked"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ScanPage(job.ID, -1, true); e == nil {
		t.Fatal("expected scan failure")
	}
	if !job.RefundRequired || job.Paid {
		t.Fatal("failed prepaid scan was not marked for refund")
	}
	if _, e = s.ScanPage(job.ID, -1, true); e == nil {
		t.Fatal("refunding scan allowed retry")
	}
}
func TestInvalidScanRequestDoesNotRefund(t *testing.T) {
	s, e := NewService(t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.Create(16)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkPaid(job.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ScanPage(job.ID, 10, true); e == nil {
		t.Fatal("invalid page accepted")
	}
	if job.RefundRequired {
		t.Fatal("invalid request triggered refund")
	}
}
