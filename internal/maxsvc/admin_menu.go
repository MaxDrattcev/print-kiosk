package maxsvc

import (
	"context"
	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"print-kiosk/internal/storage"
	"strconv"
	"strings"
	"time"
)

const adminStatusPayload = "admin:status"

func adminAction(u model.Update) (int64, string) {
	if u.UpdateType == model.UpdateMessageCallback && u.Callback != nil && u.Callback.Payload == adminStatusPayload {
		return u.Callback.User.UserID, "status"
	}
	if u.UpdateType == model.UpdateMessageCallback && u.Callback != nil {
		payload := u.Callback.Payload
		if payload == "admin:history" {
			return u.Callback.User.UserID, "history"
		}
		for days := 1; days <= 5; days++ {
			if payload == "admin:history:"+strconv.Itoa(days) {
				return u.Callback.User.UserID, "history:" + strconv.Itoa(days)
			}
		}
	}
	if u.UpdateType == model.UpdateMessageCreated && u.Message != nil {
		text := strings.ToLower(strings.TrimSpace(u.Message.Body.Text))
		switch text {
		case "admin", "/admin":
			return u.Message.Sender.UserID, "admin"
		case "status", "/status":
			return u.Message.Sender.UserID, "status"
		}
	}
	return 0, ""
}

func (s *Service) handleAdminCommand(ctx context.Context, u model.Update) bool {
	sender, action := adminAction(u)
	if action == "" {
		return false
	}
	values, err := s.settings.GetAll()
	// Always authenticate the current sender, including clicks on forwarded/old menus.
	if err != nil || sender == 0 || sender != storage.MaxAdminID(values) {
		return true
	}
	if u.Callback != nil {
		if api, e := s.api(); e == nil {
			note := "Запрос принят"
			_, _ = api.Messages.AnswerOnCallback(ctx, u.Callback.CallbackID, model.CallbackAnswer{Notification: &note})
		}
	}
	if action == "admin" || action == "history" {
		api, e := s.api()
		if e != nil {
			return true
		}
		keyboard := model.NewKeyboard()
		text := "Управление киоском\nВыберите действие:"
		if action == "admin" {
			keyboard.AddRow().AddCallBack("📋 Статус", adminStatusPayload)
			keyboard.AddRow().AddCallBack("Выгрузить историю", "admin:history")
		} else {
			text = "За сколько дней выгрузить историю?\nПериод включает сегодняшний день."
			labels := []string{"Один день", "Два дня", "Три дня", "Четыре дня", "Пять дней"}
			for i, label := range labels {
				keyboard.AddRow().AddCallBack(label, "admin:history:"+strconv.Itoa(i+1))
			}
		}
		_, _ = api.Messages.Send(ctx, maxbot.NewMessage().SetUser(sender).SetText(text).AddKeyboard(keyboard))
		return true
	}
	if strings.HasPrefix(action, "history:") {
		days, _ := strconv.Atoi(strings.TrimPrefix(action, "history:"))
		_ = s.sendUserText(ctx, sender, "Формируем PDF с историей операций…")
		s.mu.RLock()
		exporter := s.historyExporter
		s.mu.RUnlock()
		if exporter == nil {
			_ = s.sendUserText(ctx, sender, "Выгрузка истории недоступна")
			return true
		}
		exportCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := exporter(exportCtx, sender, days); err != nil {
			_ = s.sendUserText(ctx, sender, "Не удалось выгрузить историю: "+err.Error())
		}
		return true
	}
	_ = s.sendUserText(ctx, sender, "Проверяем оборудование и подключения…")
	s.mu.RLock()
	reporter := s.statusReporter
	s.mu.RUnlock()
	report := "Проверка оборудования недоступна"
	if reporter != nil {
		report = reporter(ctx)
	}
	_ = s.sendUserText(ctx, sender, report)
	return true
}
