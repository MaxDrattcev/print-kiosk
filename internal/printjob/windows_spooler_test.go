package printjob

import (
	"errors"
	"fmt"
	"testing"
)

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
	if !windowsPaperOut(normalizeWindowsJobStatus("Error, PaperOut")) {
		t.Fatal("paper-out status was not recognized")
	}
	if windowsPaperOut(normalizeWindowsJobStatus("Printing")) {
		t.Fatal("printing status was recognized as paper-out")
	}
	if !IsPaperOut(fmt.Errorf("wrapped: %w", ErrPaperOut)) {
		t.Fatal("wrapped paper-out error was not recognized")
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", ErrPaperOut), ErrPaperOut) {
		t.Fatal("paper-out sentinel cannot be unwrapped")
	}
	if !windowsPaperJam(normalizeWindowsJobStatus("Error, PaperJam")) {
		t.Fatal("paper-jam status was not recognized")
	}
	if windowsPaperJam(normalizeWindowsJobStatus("Printing")) {
		t.Fatal("printing status was recognized as paper-jam")
	}
	if !IsPaperJam(fmt.Errorf("wrapped: %w", ErrPaperJam)) {
		t.Fatal("wrapped paper-jam error was not recognized")
	}
}

func TestDeviceTrackerRequiresBusyThenStableIdle(t *testing.T) {
	var tracker windowsDeviceTracker
	idle := windowsDeviceState{Status: 3, Extended: 3, Error: 2}
	tracker.observe(idle)
	tracker.observe(idle)
	if tracker.confirmed() {
		t.Fatal("idle without activity is not confirmation")
	}
	tracker.observe(windowsDeviceState{Status: 4, Extended: 4, Error: 2})
	tracker.observe(idle)
	if tracker.confirmed() {
		t.Fatal("one idle sample is insufficient")
	}
	tracker.observe(idle)
	if !tracker.confirmed() {
		t.Fatal("busy then stable idle was not detected")
	}
	tracker.observe(windowsDeviceState{Status: 2, Extended: 2})
	if tracker.confirmed() {
		t.Fatal("unknown state must invalidate idle samples")
	}
}
func TestDeviceErrorsCannotConfirmIdle(t *testing.T) {
	for _, state := range []windowsDeviceState{
		{Status: 3, Offline: true}, {Status: 3, Error: 4},
		{Status: 3, Error: 8}, {Status: 3, Extended: 4},
	} {
		if state.idle() {
			t.Fatalf("unavailable or busy printer reported idle: %+v", state)
		}
	}
}
