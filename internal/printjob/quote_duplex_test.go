package printjob

import "testing"

func TestQuoteDisablesDuplexForOneSelectedPage(t *testing.T) {
	service := &Service{}
	job := &Job{Pages: 5}

	quote, err := service.Quote(job, QuoteInput{Duplex: true, Copies: 1, PageRange: "3"}, 7, 15)
	if err != nil {
		t.Fatal(err)
	}
	if quote.Duplex {
		t.Fatal("duplex must be disabled when only one page is selected")
	}
	if quote.Sheets != 1 {
		t.Fatalf("one impression must use one sheet, got %d", quote.Sheets)
	}
}

func TestQuoteAllowsDuplexForTwoCopiesOfOnePage(t *testing.T) {
	service := &Service{}
	job := &Job{Pages: 1}

	quote, err := service.Quote(job, QuoteInput{Duplex: true, Copies: 2}, 7, 15)
	if err != nil {
		t.Fatal(err)
	}
	if !quote.Duplex {
		t.Fatal("duplex must be available for two copies of one page")
	}
	if quote.Sheets != 1 {
		t.Fatalf("two duplex impressions must use one sheet, got %d", quote.Sheets)
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
