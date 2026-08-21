package printjob

import "testing"

func TestSelectNewWindowsJobPrefersDocument(t *testing.T) {
	jobs := []windowsSpoolJob{{ID: 8, Document: "other.pdf"}, {ID: 9, Document: "document-123.pdf"}}
	got := selectNewWindowsJob(jobs, map[int]struct{}{7: {}}, "document-123")
	if got != 9 {
		t.Fatalf("got %d, want 9", got)
	}
}

func TestWindowsJobStates(t *testing.T) {
	if !windowsJobCompleted(normalizeWindowsJobStatus("Completed")) {
		t.Fatal("completed status was not recognized")
	}
	if !windowsJobFailed(normalizeWindowsJobStatus("Error, PaperOut")) {
		t.Fatal("failure status was not recognized")
	}
	if windowsJobFailed(normalizeWindowsJobStatus("Printing")) {
		t.Fatal("printing status was recognized as failure")
	}
}
