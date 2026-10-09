package admin

import (
	"path/filepath"
	"print-kiosk/internal/storage"
	"testing"
)

func TestMAXLinkFollowsVerifiedToken(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSettingsRepo(db)
	if err := repo.SetMany(map[string]string{storage.SettingMaxBotToken: "saved-token", storage.SettingMaxBotLink: "https://max.ru/old_bot"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{settings: repo}
	link, saved, err := h.syncMAXBotLink("saved-token", "current_bot")
	if err != nil || !saved || link != "https://max.ru/current_bot" {
		t.Fatalf("link=%s saved=%v err=%v", link, saved, err)
	}
	actual, _ := repo.Get(storage.SettingMaxBotLink)
	if actual != link {
		t.Fatal("verified saved token did not update stored link")
	}
	link, saved, err = h.syncMAXBotLink("new-unsaved-token", "new_bot")
	if err != nil || saved || link != "https://max.ru/new_bot" {
		t.Fatal("new token link not returned for form")
	}
	actual, _ = repo.Get(storage.SettingMaxBotLink)
	if actual != "https://max.ru/current_bot" {
		t.Fatal("unsaved token overwrote active bot link")
	}
	_, saved, err = h.syncMAXBotLink("saved-token", "")
	if err != nil || saved {
		t.Fatal("missing username must not replace link")
	}
}
