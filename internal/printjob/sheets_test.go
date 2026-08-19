package printjob

import "testing"

func TestPaperSheets(t *testing.T) {
	tests := []struct {
		pages, copies int
		duplex        bool
		want          int
	}{
		{1, 1, false, 1},
		{2, 1, false, 2},
		{3, 1, false, 3},
		{1, 2, false, 2},
		{1, 1, true, 1},
		{2, 1, true, 1},
		{3, 1, true, 2},
		{4, 1, true, 2},
		{5, 1, true, 3},
		{1, 2, true, 1}, // 2 pages total duplex → 1 sheet
		{3, 2, true, 3}, // 6 pages total duplex → 3 sheets
		{2, 3, true, 3}, // 6 pages total duplex → 3 sheets
		{3, 3, true, 5}, // 9 pages total duplex → 5 sheets
	}
	for _, tt := range tests {
		got := PaperSheets(tt.pages, tt.copies, tt.duplex)
		if got != tt.want {
			t.Fatalf("PaperSheets(%d,%d,duplex=%v)=%d want %d", tt.pages, tt.copies, tt.duplex, got, tt.want)
		}
	}
}
