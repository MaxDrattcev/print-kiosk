package maxsvc

import (
	"path/filepath"
	"print-kiosk/internal/storage"
	"testing"
	"time"
)

func TestAdminBindingLifecycle(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSettingsRepo(db)
	if err := repo.SetMany(map[string]string{storage.SettingMaxBotToken: "fixture", storage.SettingMaxEnabled: "true", storage.SettingMaxAdminID: "7"}); err != nil {
		t.Fatal(err)
	}
	s := &Service{settings: repo}
	first, err := s.StartAdminBinding()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartAdminBinding()
	if err != nil {
		t.Fatal(err)
	}
	if s.claimAdminBinding("notify_"+first.Token, 42, "old", time.Now()) {
		t.Fatal("superseded QR accepted")
	}
	if s.claimAdminBinding("notify_wrong", 42, "wrong", time.Now()) {
		t.Fatal("wrong token accepted")
	}
	if s.claimAdminBinding("notify_"+second.Token, 42, "late", second.Deadline) {
		t.Fatal("expired token accepted")
	}
	value, _ := repo.Get(storage.SettingMaxAdminID)
	if value != "7" {
		t.Fatal("recipient changed before binding")
	}
	if !s.claimAdminBinding("notify_"+second.Token, 42, "Max", time.Now()) {
		t.Fatal("valid token rejected")
	}
	value, _ = repo.Get(storage.SettingMaxAdminID)
	if value != "42" {
		t.Fatal("recipient not persisted")
	}
	if s.claimAdminBinding("notify_"+second.Token, 99, "replay", time.Now()) {
		t.Fatal("replay accepted")
	}
	bound, ok := s.GetAdminBinding(second.ID)
	if !ok || bound.Status != "bound" || bound.Name != "Max" {
		t.Fatalf("binding = %+v", bound)
	}
	third, err := s.StartAdminBinding()
	if err != nil {
		t.Fatal(err)
	}
	s.CancelAdminBinding(third.ID)
	if s.claimAdminBinding("notify_"+third.Token, 99, "cancelled", time.Now()) {
		t.Fatal("cancelled QR accepted")
	}
	value, _ = repo.Get(storage.SettingMaxAdminID)
	if value != "42" {
		t.Fatal("recipient changed on replay/cancel")
	}
}
