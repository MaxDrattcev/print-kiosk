package copyjob

import "testing"

func TestQuoteDisablesDuplexForOneCopy(t *testing.T) {
	quote, err := QuotePrice(Options{Duplex: true, Copies: 1}, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if quote.Duplex {
		t.Fatal("duplex must be disabled for one copy")
	}
	if quote.Sheets != 1 {
		t.Fatalf("got %d sheets, want 1", quote.Sheets)
	}
}

func TestQuoteAllowsDuplexForSeveralCopies(t *testing.T) {
	quote, err := QuotePrice(Options{Duplex: true, Copies: 3}, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !quote.Duplex {
		t.Fatal("duplex must remain enabled for several copies")
	}
	if quote.Sheets != 2 {
		t.Fatalf("got %d sheets, want 2", quote.Sheets)
	}
}
