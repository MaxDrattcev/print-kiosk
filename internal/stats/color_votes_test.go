package stats

import (
	"path/filepath"
	"print-kiosk/internal/storage"
	"testing"
)

func TestColorVotesPersistAndDoNotDuplicate(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "votes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewRepo(db)
	for _, vote := range []struct{ id, source string }{{"one", "print"}, {"one", "print"}, {"one", "copy"}, {"two", "copy"}} {
		if err := repo.VoteForColorPrint(vote.id, vote.source); err != nil {
			t.Fatal(err)
		}
	}
	reloaded := NewRepo(db)
	count, err := reloaded.ColorPrintVotes()
	if err != nil || count != 2 {
		t.Fatalf("votes=%d err=%v", count, err)
	}
	if err := repo.VoteForColorPrint("bad", "unknown"); err == nil {
		t.Fatal("invalid source accepted")
	}
}
