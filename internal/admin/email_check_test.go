package admin

import (
	"path/filepath"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
	"strings"
	"testing"
)

func TestEmailCheckTracksCredentialsAndPersists(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "checks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	h := &Handler{stats: stats.NewRepo(db)}
	values := map[string]string{storage.SettingEmailAddress: "kiosk@example.org", storage.SettingEmailPassword: "fixture-password"}
	if h.emailChecked(values) {
		t.Fatal("unchecked settings accepted")
	}
	h.recordEmailCheck("kiosk@example.org", "kiosk@example.org", "fixture-password", true)
	if !h.emailChecked(values) {
		t.Fatal("default login does not match")
	}
	reloaded := &Handler{stats: stats.NewRepo(db)}
	if !reloaded.emailChecked(values) {
		t.Fatal("check lost on restart")
	}
	for _, key := range []string{storage.SettingEmailAddress, storage.SettingEmailLogin, storage.SettingEmailPassword} {
		old := values[key]
		values[key] = "changed"
		if h.emailChecked(values) {
			t.Fatalf("change to %s retained checked status", key)
		}
		values[key] = old
	}
	stored, _ := h.stats.GetKV("email_connection_check")
	if strings.Contains(stored, "fixture-password") {
		t.Fatal("password stored in check")
	}
	h.recordEmailCheck("kiosk@example.org", "", "fixture-password", false)
	if h.emailChecked(values) {
		t.Fatal("failed check retained success")
	}
}
