package scanjob

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultFileNameCounterResetsAfterTenMinutes(t *testing.T) {
	s, err := NewService(filepath.Join(t.TempDir(), "scans"), true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }

	first, err := s.Create(10)
	if err != nil {
		t.Fatal(err)
	}
	if first.FileName != "Файл_1.pdf" {
		t.Fatalf("first name = %q", first.FileName)
	}

	now = now.Add(9*time.Minute + 59*time.Second)
	second, err := s.Create(10)
	if err != nil {
		t.Fatal(err)
	}
	if second.FileName != "Файл_2.pdf" {
		t.Fatalf("second name = %q", second.FileName)
	}

	now = now.Add(time.Second)
	third, err := s.Create(10)
	if err != nil {
		t.Fatal(err)
	}
	if third.FileName != "Файл_1.pdf" {
		t.Fatalf("reset name = %q", third.FileName)
	}
}

func TestDefaultFileNameCounterIsSequential(t *testing.T) {
	s, err := NewService(filepath.Join(t.TempDir(), "scans"), true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	for i, want := range []string{"Файл_1.pdf", "Файл_2.pdf", "Файл_3.pdf"} {
		job, err := s.Create(10)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if job.FileName != want {
			t.Fatalf("name %d = %q, want %q", i, job.FileName, want)
		}
	}
}
