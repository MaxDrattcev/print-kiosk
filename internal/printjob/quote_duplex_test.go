package printjob

import "testing"

func TestQuoteDisablesDuplexForOneSelectedPage(t *testing.T) {
	service := &Service{}
	job := &Job{Pages: 5}

	quote, err := service.Quote(job, QuoteInput{Duplex: true, Copies: 2, PageRange: "3"}, 7, 15)
	if err != nil {
		t.Fatal(err)
	}
	if quote.Duplex {
		t.Fatal("duplex must be disabled when only one page is selected")
	}
	if quote.Sheets != 2 {
		t.Fatalf("one page with two copies must use two sheets, got %d", quote.Sheets)
	}
}

func TestQuoteKeepsDuplexForSeveralSelectedPages(t *testing.T) {
	service := &Service{}
	job := &Job{Pages: 5}

	quote, err := service.Quote(job, QuoteInput{Duplex: true, Copies: 1, PageRange: "2-3"}, 7, 15)
	if err != nil {
		t.Fatal(err)
	}
	if !quote.Duplex {
		t.Fatal("duplex must remain available for several selected pages")
	}
	if quote.Sheets != 1 {
		t.Fatalf("two duplex pages must use one sheet, got %d", quote.Sheets)
	}
}
