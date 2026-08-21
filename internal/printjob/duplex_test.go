package printjob

import "testing"

func TestDuplexBindingSettings(t *testing.T) {
	tests := []struct {
		orientation string
		cups        string
		canon       string
		sumatra     string
	}{
		{"portrait", "two-sided-long-edge", "DuplexFront", "duplexlong"},
		{"landscape", "two-sided-short-edge", "DuplexTop", "duplexshort"},
		{"", "two-sided-long-edge", "DuplexFront", "duplexlong"},
	}
	for _, tt := range tests {
		cups, canon, sumatra := duplexBindingSettings(tt.orientation)
		if cups != tt.cups || canon != tt.canon || sumatra != tt.sumatra {
			t.Fatalf("duplexBindingSettings(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tt.orientation, cups, canon, sumatra, tt.cups, tt.canon, tt.sumatra)
		}
	}
}
