package ophistory_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"print-kiosk/internal/ophistory"
)

func testRepo(t *testing.T) *ophistory.Repo {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE operation_history (id INTEGER PRIMARY KEY AUTOINCREMENT, created_at INTEGER NOT NULL, operation TEXT NOT NULL, job_id TEXT NOT NULL DEFAULT '', pages INTEGER NOT NULL DEFAULT 0, sheets INTEGER NOT NULL DEFAULT 0, amount REAL NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error_text TEXT NOT NULL DEFAULT ''); CREATE INDEX idx_operation_history_created_at ON operation_history(created_at);`)
	if err != nil {
		t.Fatal(err)
	}
	return ophistory.NewRepo(db)
}

func TestListDaysAndRetention(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.Local)
	for _, e := range []ophistory.Entry{
		{CreatedAt: now, Operation: "print", Pages: 2, Sheets: 1, Amount: 20, Success: true},
		{CreatedAt: now.AddDate(0, 0, -6), Operation: "copy", Success: false, ErrorText: "printer offline"},
		{CreatedAt: now.AddDate(0, 0, -31), Operation: "scan", Success: true},
	} {
		if err := r.Add(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	items, err := r.ListDays(ctx, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d entries, want 2", len(items))
	}
	deleted, err := r.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d entries, want 1", deleted)
	}
}

func TestListDaysRejectsInvalidRange(t *testing.T) {
	r := testRepo(t)
	if _, err := r.ListDays(context.Background(), 31, time.Now()); err == nil {
		t.Fatal("expected validation error")
	}
}
