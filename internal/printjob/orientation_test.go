package printjob

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestOrientationFromDimensions(t *testing.T) {
	tests := []struct {
		name string
		dims []types.Dim
		want string
	}{
		{name: "portrait", dims: []types.Dim{{Width: 595, Height: 842}}, want: "portrait"},
		{name: "landscape", dims: []types.Dim{{Width: 842, Height: 595}}, want: "landscape"},
		{name: "landscape majority", dims: []types.Dim{{Width: 595, Height: 842}, {Width: 842, Height: 595}, {Width: 842, Height: 595}}, want: "landscape"},
		{name: "tie follows first", dims: []types.Dim{{Width: 842, Height: 595}, {Width: 595, Height: 842}}, want: "landscape"},
		{name: "empty defaults portrait", want: "portrait"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orientationFromDimensions(tt.dims); got != tt.want {
				t.Fatalf("orientationFromDimensions() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePageRange(t *testing.T) {
	tests := []struct {
		value     string
		maxPages  int
		wantPages int
		wantRange string
		wantErr   bool
	}{
		{value: "", maxPages: 5, wantPages: 5, wantRange: ""},
		{value: "1-3, 6, 9-12", maxPages: 12, wantPages: 8, wantRange: "1-3,6,9-12"},
		{value: "1,2,2,3", maxPages: 3, wantPages: 3, wantRange: "1-3"},
		{value: "3–5", maxPages: 5, wantPages: 3, wantRange: "3-5"},
		{value: "0-2", maxPages: 5, wantErr: true},
		{value: "4-2", maxPages: 5, wantErr: true},
		{value: "1-6", maxPages: 5, wantErr: true},
	}
	for _, tt := range tests {
		pages, normalized, err := ParsePageRange(tt.value, tt.maxPages)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ParsePageRange(%q, %d) expected error", tt.value, tt.maxPages)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParsePageRange(%q, %d): %v", tt.value, tt.maxPages, err)
		}
		if len(pages) != tt.wantPages || normalized != tt.wantRange {
			t.Fatalf("ParsePageRange(%q, %d) = %d pages, %q; want %d pages, %q",
				tt.value, tt.maxPages, len(pages), normalized, tt.wantPages, tt.wantRange)
		}
	}
}
