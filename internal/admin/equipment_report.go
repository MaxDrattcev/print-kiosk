package admin

import (
	"context"
	"fmt"
	"print-kiosk/internal/device"
	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/mailout"
	"print-kiosk/internal/maxsvc"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/storage"
	"print-kiosk/internal/vendista"
	"runtime"
	"strings"
	"time"
)

// EquipmentReport probes independently so one unavailable service cannot hide the others.
func (h *Handler) EquipmentReport(parent context.Context) string {
	values, err := h.settings.GetAll()
	if err != nil {
		return "Не удалось загрузить настройки для проверки оборудования"
	}
	ctx, cancel := context.WithTimeout(parent, 40*time.Second)
	defer cancel()
	checks := []struct {
		name string
		run  func() string
	}{
		{"🖨 Принтер", func() string {
			if h.cfg.Printer.DryRun || storage.SettingEnabled(values, storage.SettingTestDeviceMode, true) {
				return "тестовый режим оборудования"
			}
			if runtime.GOOS != "windows" {
				return "проверка очереди доступна на Windows"
			}
			name, state, available, e := printjob.ProbeWindowsPrinter(h.cfg.Printer.Name)
			if e != nil {
				return "не удалось проверить очередь"
			}
			if !available {
				return "недоступен"
			}
			answer := "доступен" + deviceNameSuffix(name) + printerStateSuffix(state)
			if storage.SettingEnabled(values, storage.SettingPrinterFaultBlocked, false) {
				answer += " · печать заблокирована: " + values[storage.SettingPrinterFaultReason]
			}
			return answer
		}},
		{"📠 Сканер", func() string {
			if h.cfg.Printer.DryRun || storage.SettingEnabled(values, storage.SettingTestDeviceMode, true) {
				return "тестовый режим оборудования"
			}
			name, available, e := device.ProbeScanner()
			if e != nil {
				return "не удалось проверить"
			}
			if !available {
				return "не обнаружен"
			}
			return "обнаружен" + deviceNameSuffix(name)
		}},
		{"📥 Почта IMAP", func() string { return checkReportMail(values, false) }},
		{"📤 Почта SMTP", func() string { return checkReportMail(values, true) }},
		{"💬 MAX", func() string {
			api, e := maxsvc.NewAPI(values[storage.SettingMaxBotToken])
			if e != nil {
				return "не настроен"
			}
			_, e = api.Bots.GetMyInfo(ctx)
			if e != nil {
				return "не удалось проверить подключение"
			}
			return "подключение работает"
		}},
		{"💳 Терминал Vendista", func() string {
			client, e := vendista.FromSettings(values)
			if e != nil {
				return e.Error()
			}
			result := client.Diagnose(ctx)
			answer := fmt.Sprint(result["label"])
			if last, ok := result["last_operation_time"].(string); ok {
				if last == "" {
					last = "операций нет"
				}
				answer += "\nПоследняя финансовая операция: " + last
			} else {
				answer += "\nПоследнюю операцию проверить не удалось"
			}
			if last, ok := result["last_online_time"].(string); ok && last != "" {
				answer += "\nПоследняя связь терминала: " + last
			}
			return answer
		}},
	}
	type completed struct {
		index int
		text  string
	}
	results := make(chan completed, len(checks))
	lines := make([]string, len(checks))
	for i, check := range checks {
		lines[i] = check.name + ": время проверки истекло"
		go func(i int, run func() string) { results <- completed{i, run()} }(i, check.run)
	}
	for range checks {
		select {
		case done := <-results:
			lines[done.index] = checks[done.index].name + ": " + done.text
		case <-ctx.Done():
			goto finish
		}
	}
finish:
	paper, e := h.settings.PaperRemaining()
	if e == nil {
		lines = append(lines, fmt.Sprintf("📄 Бумага: %d листов (счётчик приложения)", paper))
	} else {
		lines = append(lines, "📄 Бумага: не удалось прочитать остаток")
	}
	if storage.SettingEnabled(values, storage.SettingTestPaymentMode, true) {
		lines = append(lines, "⚙️ Оплата: тестовый режим — деньги не списываются")
	} else {
		lines = append(lines, "⚙️ Оплата: рабочий режим")
	}
	return "📋 Состояние киоска\n\n" + strings.Join(lines, "\n")
}

func checkReportMail(values map[string]string, smtp bool) string {
	address := strings.TrimSpace(values[storage.SettingEmailAddress])
	password := values[storage.SettingEmailPassword]
	login := strings.TrimSpace(values[storage.SettingEmailLogin])
	if login == "" {
		login = address
	}
	if address == "" || password == "" {
		return "не настроена"
	}
	if smtp {
		host, port := mailout.ResolveHost(address)
		if e := mailout.Test(mailout.Credentials{From: address, Login: login, Password: password, Host: host, Port: port}); e != nil {
			return classifyMailError("SMTP", e)
		}
	} else {
		host, port := mailinbox.ResolveHost(address)
		if e := mailinbox.Test(mailinbox.Credentials{Address: address, Login: login, Password: password, Host: host, Port: port}); e != nil {
			return classifyMailError("IMAP", e)
		}
	}
	return "подключение и авторизация работают"
}
