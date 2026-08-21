package scanjob

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestMultiPageScanPaymentDeleteAndReplace(t *testing.T) {
	svc, err := NewService(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	job, err := svc.Create(12)
	if err != nil {
		t.Fatal(err)
	}
	firstPayment, err := svc.ReservePayment(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstPayment.Pages != 1 || firstPayment.Amount != 12 {
		t.Fatalf("first payment = %+v", firstPayment)
	}
	if _, err := svc.CommitPayment(job.ID, firstPayment.Pages); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := svc.ScanTest(job.ID); err != nil {
			t.Fatal(err)
		}
	}
	job, _ = svc.Get(job.ID)
	if len(job.Pages) != 3 || job.PaidPages != 1 {
		t.Fatalf("pages=%d paid=%d", len(job.Pages), job.PaidPages)
	}
	if _, _, err := svc.ReadyForDelivery(job.ID); err == nil {
		t.Fatal("delivery unexpectedly allowed before surcharge")
	}
	originalSecondID := job.Pages[1].ID
	if _, err := svc.ScanPage(job.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	job, _ = svc.Get(job.ID)
	if len(job.Pages) != 3 || job.Pages[1].ID == originalSecondID {
		t.Fatal("replacement did not preserve page count or page id")
	}
	if _, err := svc.DeletePage(job.ID, 0); err != nil {
		t.Fatal(err)
	}
	job, _ = svc.Get(job.ID)
	if len(job.Pages) != 2 {
		t.Fatalf("pages after delete=%d", len(job.Pages))
	}
	extraPayment, err := svc.ReservePayment(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if extraPayment.Pages != 1 || extraPayment.Amount != 12 {
		t.Fatalf("extra payment = %+v", extraPayment)
	}
	if _, err := svc.CommitPayment(job.ID, extraPayment.Pages); err != nil {
		t.Fatal(err)
	}
	path, _, err := svc.ReadyForDelivery(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	pageCount, err := api.PageCountFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pageCount != 2 {
		t.Fatalf("merged PDF pages=%d", pageCount)
	}
}
