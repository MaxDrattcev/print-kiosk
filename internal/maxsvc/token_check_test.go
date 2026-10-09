package maxsvc

import (
	"path/filepath"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
	"strings"
	"testing"
)

func TestTokenCheckTracksExactTokenAndPersists(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "checks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := stats.NewRepo(db)
	s := &Service{stats: repo}
	const token = "fixture-secret-token"
	if s.TokenChecked(token) {
		t.Fatal("unverified token accepted")
	}
	s.RecordTokenCheck(token, true)
	if !s.TokenChecked(token) || s.TokenChecked("replacement") {
		t.Fatal("check must match exact token")
	}
	reloaded := &Service{stats: stats.NewRepo(db)}
	if !reloaded.TokenChecked(token) {
		t.Fatal("check lost on restart")
	}
	value, _ := repo.GetKV("max_token_check")
	if strings.Contains(value, token) {
		t.Fatal("raw token stored in check")
	}
	s.RecordTokenCheck(token, false)
	if s.TokenChecked(token) {
		t.Fatal("failed check left success status")
	}
}
