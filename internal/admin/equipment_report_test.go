package admin

import (
	"context"
	"path/filepath"
	"print-kiosk/internal/config"
	"print-kiosk/internal/storage"
	"strings"
	"testing"
)

func TestEquipmentReportIncludesAllComponentsWithoutConfiguredServices(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "report.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSettingsRepo(db)
	if err = repo.SetMany(map[string]string{storage.SettingEmailAddress: "", storage.SettingEmailPassword: "", storage.SettingMaxBotToken: "", storage.SettingVendistaToken: "", storage.SettingVendistaTerminalID: "", storage.SettingTestDeviceMode: "true", storage.SettingTestPaymentMode: "true"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: &config.Config{}, settings: repo}
	report := h.EquipmentReport(context.Background())
	for _, want := range []string{"Принтер:", "Сканер:", "Почта IMAP:", "Почта SMTP:", "MAX:", "Терминал Vendista:", "Бумага:", "тестовый режим — деньги не списываются"} {
		if !strings.Contains(report, want) {
			t.Errorf("missing %q in %s", want, report)
		}
	}
	if strings.Contains(report, "подключение работает") {
		t.Fatal("unconfigured service reported as working")
	}
}
