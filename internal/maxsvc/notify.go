package maxsvc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"

	"print-kiosk/internal/storage"
)

func (s *Service) dailyLoop(ctx context.Context) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 22, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.sendDailyReport(ctx)
		}
	}
}

func (s *Service) sendDailyReport(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	day := time.Now().Format("2006-01-02")
	key := "max_daily_sent_" + day
	if _, ok := s.stats.GetKV(key); ok {
		return
	}
	d, err := s.stats.GetDay(day)
	if err != nil {
		s.NotifyError(ctx, fmt.Errorf("статистика за день: %w", err))
		return
	}
	paper, _ := s.settings.PaperRemaining()
	text := fmt.Sprintf(
		"📊 Ежедневная статистика киоска (%s)\n\n"+
			"Статус принтера: Активен\n"+
			"Заработано: %.2f ₽\n"+
			"Напечатано ч/б страниц: %d\n"+
			"Напечатано цветных страниц: %d\n"+
			"Сканов: %d\n"+
			"Копий: %d\n"+
			"Израсходовано листов: %d\n"+
			"Осталось листов: %d",
		day, d.Revenue, d.PagesBW, d.PagesColor, d.Scans, d.Copies, d.SheetsUsed, paper,
	)
	if err := s.sendAdmin(ctx, text); err != nil {
		slog.Warn("max daily report failed", "error", err)
		return
	}
	_ = s.stats.SetKV(key, time.Now().Format(time.RFC3339))
}

func (s *Service) NotifyStartup(ctx context.Context) {
	_ = s.sendAdmin(ctx, "🟢 Киоск запущен\nПриложение стартовало.")
}

func (s *Service) NotifyShutdown(ctx context.Context) {
	_ = s.sendAdmin(ctx, "🔴 Киоск выключен\nПриложение остановлено.")
}

func (s *Service) NotifyError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	_ = s.sendAdmin(ctx, "⚠️ Ошибка киоска\n"+err.Error())
}

func (s *Service) CheckPaperAlert(ctx context.Context) {
	values, err := s.settings.GetAll()
	if err != nil {
		return
	}
	remaining, _ := strconv.Atoi(values[storage.SettingPaperRemaining])
	threshold, _ := strconv.Atoi(values[storage.SettingPaperAlertThreshold])
	if threshold < 0 {
		threshold = 0
	}
	s.mu.Lock()
	if remaining > threshold {
		s.paperAlerted = false
		s.lastPaperN = remaining
		s.mu.Unlock()
		return
	}
	if s.paperAlerted && remaining >= s.lastPaperN {
		s.lastPaperN = remaining
		s.mu.Unlock()
		return
	}
	s.paperAlerted = true
	s.lastPaperN = remaining
	s.mu.Unlock()

	_ = s.sendAdmin(ctx, fmt.Sprintf(
		"📄 Заканчивается бумага\nОсталось: %d лист(ов)\nПорог: %d",
		remaining, threshold,
	))
}

func (s *Service) NotifyInkLow(ctx context.Context) {
	values, err := s.settings.GetAll()
	if err != nil || values[storage.SettingMaxInkAlerts] != "true" {
		return
	}
	_ = s.sendAdmin(ctx, "🖨 Заканчиваются чернила / тонер\nПроверьте картридж принтера.")
}

func (s *Service) sendAdmin(ctx context.Context, text string) error {
	token, adminID, ok := s.creds()
	if !ok {
		return fmt.Errorf("max disabled")
	}
	api, err := maxbot.NewApi(token)
	if err != nil {
		return err
	}
	msg := maxbot.NewMessage().SetUser(adminID).SetText(text)
	_, err = api.Messages.Send(ctx, msg)
	if err != nil {
		slog.Warn("max send admin failed", "error", err)
	}
	return err
}

func (s *Service) SendFileToUser(ctx context.Context, userID int64, filePath, fileName, caption string) error {
	api, err := s.api()
	if err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if fileName == "" {
		fileName = st.Name()
	}
	token, err := api.Upload.Upload(ctx, model.UploadFile, f, fileName, st.Size())
	if err != nil {
		return fmt.Errorf("upload to MAX: %w", err)
	}
	time.Sleep(1500 * time.Millisecond)
	msg := maxbot.NewMessage().
		SetUser(userID).
		SetText(caption).
		AddAttachByToken(token, model.AttachFile)
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		_, lastErr = api.Messages.Send(ctx, msg)
		if lastErr == nil {
			return nil
		}
		if !strings.Contains(lastErr.Error(), "not.ready") && !strings.Contains(lastErr.Error(), "not processed") {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
	return lastErr
}

func (s *Service) sendUserText(ctx context.Context, userID int64, text string) error {
	api, err := s.api()
	if err != nil {
		return err
	}
	msg := maxbot.NewMessage().SetUser(userID).SetText(text)
	_, err = api.Messages.Send(ctx, msg)
	return err
}
