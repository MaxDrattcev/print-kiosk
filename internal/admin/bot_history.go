package admin

import (
	"context"
	"fmt"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"print-kiosk/internal/executil"
	"print-kiosk/internal/libreoffice"
	"print-kiosk/internal/storage"
	"strings"
	"time"
)

// SendBotHistory uses the same history query and PDF layout as the cabinet.
func (h *Handler) SendBotHistory(ctx context.Context, userID int64, days int) error {
	if days < 1 || days > 5 {
		return fmt.Errorf("выберите период от 1 до 5 дней")
	}
	values, err := h.settings.GetAll()
	if err != nil {
		return fmt.Errorf("не удалось загрузить настройки")
	}
	if userID == 0 || userID != storage.MaxAdminID(values) {
		return fmt.Errorf("доступ запрещён")
	}
	items, err := h.history.ListDays(ctx, days, time.Now())
	if err != nil {
		return fmt.Errorf("не удалось загрузить историю")
	}
	if err = os.MkdirAll(h.reportsDir(), 0755); err != nil {
		return fmt.Errorf("не удалось создать каталог отчётов")
	}
	dir, err := os.MkdirTemp(h.reportsDir(), "bot-history-")
	if err != nil {
		return fmt.Errorf("не удалось создать отчёт")
	}
	defer os.RemoveAll(dir)
	html := filepath.Join(dir, "history.html")
	pdf := filepath.Join(dir, "history.pdf")
	if err = writeHistoryHTML(html, days, items); err != nil {
		return fmt.Errorf("не удалось сформировать отчёт")
	}
	soffice, err := libreoffice.Find(h.cfg.Paths.LibreOffice)
	if err != nil {
		return fmt.Errorf("проверьте установку LibreOffice")
	}
	profilePath, err := filepath.Abs(filepath.Join(dir, "lo-profile"))
	if err != nil {
		return fmt.Errorf("не удалось подготовить LibreOffice")
	}
	profilePath = filepath.ToSlash(profilePath)
	if !strings.HasPrefix(profilePath, "/") {
		profilePath = "/" + profilePath
	}
	profile := (&url.URL{Scheme: "file", Path: profilePath}).String()
	cmd := exec.CommandContext(ctx, soffice, "-env:UserInstallation="+profile, "--headless", "--convert-to", "pdf", "--outdir", dir, html)
	executil.HideWindow(cmd)
	if _, err = cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("не удалось создать PDF, проверьте LibreOffice")
	}
	if _, err = api.PageCountFile(pdf); err != nil {
		return fmt.Errorf("не удалось прочитать готовый PDF")
	}
	if h.max == nil {
		return fmt.Errorf("MAX недоступен")
	}
	if err = h.max.SendFileToUser(ctx, userID, pdf, fmt.Sprintf("История_%d_дн_%s.pdf", days, time.Now().Format("02.01.2006")), fmt.Sprintf("История операций за %d дн., включая сегодня", days)); err != nil {
		return fmt.Errorf("не удалось отправить PDF в MAX; попробуйте ещё раз")
	}
	return nil
}
