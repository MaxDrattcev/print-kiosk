package admin

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pdfcpu/pdfcpu/pkg/api"

	"print-kiosk/internal/libreoffice"
	"print-kiosk/internal/mailout"
	"print-kiosk/internal/maxsvc"
	"print-kiosk/internal/ophistory"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/storage"
	"print-kiosk/internal/usb"
)

func historyDays(c *gin.Context) (int, error) {
	days, err := strconv.Atoi(c.Query("days"))
	if err != nil || days < 1 || days > ophistory.RetentionDays {
		return 0, fmt.Errorf("укажите период от 1 до 30 дней")
	}
	return days, nil
}

func (h *Handler) OperationHistory(c *gin.Context) {
	days, err := historyDays(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := h.history.ListDays(c.Request.Context(), days, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить историю"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"days": days, "items": items})
}

type historyReportRequest struct {
	Days int `json:"days"`
}
type historyPrintRequest struct {
	ReportID string `json:"report_id"`
	FromPage int    `json:"from_page"`
	ToPage   int    `json:"to_page"`
}

func (h *Handler) reportsDir() string { return filepath.Join(h.cfg.DataRoot(), "admin-reports") }

func (h *Handler) CreateHistoryReport(c *gin.Context) {
	var in historyReportRequest
	if c.ShouldBindJSON(&in) != nil || in.Days < 1 || in.Days > 30 {
		c.JSON(400, gin.H{"error": "Укажите период от 1 до 30 дней"})
		return
	}
	items, err := h.history.ListDays(c.Request.Context(), in.Days, time.Now())
	if err != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить историю"})
		return
	}
	id := uuid.NewString()
	if err := os.MkdirAll(h.reportsDir(), 0o755); err != nil {
		c.JSON(500, gin.H{"error": "Не удалось создать отчёт"})
		return
	}
	htmlPath := filepath.Join(h.reportsDir(), id+".html")
	pdfPath := filepath.Join(h.reportsDir(), id+".pdf")
	if err := writeHistoryHTML(htmlPath, in.Days, items); err != nil {
		c.JSON(500, gin.H{"error": "Не удалось сформировать отчёт"})
		return
	}
	soffice, err := libreoffice.Find(h.cfg.Paths.LibreOffice)
	if err == nil {
		cmd := exec.CommandContext(c.Request.Context(), soffice, "--headless", "--convert-to", "pdf", "--outdir", h.reportsDir(), htmlPath)
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			err = fmt.Errorf("%w: %s", runErr, strings.TrimSpace(string(out)))
		}
	}
	_ = os.Remove(htmlPath)
	if err != nil {
		c.JSON(500, gin.H{"error": "Не удалось создать PDF. Проверьте LibreOffice"})
		return
	}
	pages, err := api.PageCountFile(pdfPath)
	if err != nil {
		c.JSON(500, gin.H{"error": "Не удалось прочитать готовый PDF"})
		return
	}
	values, _ := h.settings.GetAll()
	c.JSON(200, gin.H{"report_id": id, "pages": pages, "default_name": "Отчет_" + time.Now().Format("02.01.2006") + ".pdf", "delivery": gin.H{
		"usb":   storage.SettingEnabled(values, storage.SettingSourceUSBEnabled, true),
		"email": storage.SettingEnabled(values, storage.SettingSourceEmailEnabled, true) && storage.EmailReady(values),
		"max":   storage.MaxKioskReady(values),
	}})
}

func (h *Handler) PrintHistoryReport(c *gin.Context) {
	var in historyPrintRequest
	if c.ShouldBindJSON(&in) != nil || in.ReportID == "" {
		c.JSON(400, gin.H{"error": "Некорректные параметры печати"})
		return
	}
	base := filepath.Join(h.reportsDir(), filepath.Base(in.ReportID)+".pdf")
	pages, err := api.PageCountFile(base)
	if err != nil {
		c.JSON(404, gin.H{"error": "Отчёт не найден"})
		return
	}
	if in.FromPage < 1 || in.ToPage < in.FromPage || in.ToPage > pages {
		c.JSON(400, gin.H{"error": fmt.Sprintf("Укажите страницы от 1 до %d", pages)})
		return
	}
	printPath := base
	if in.FromPage != 1 || in.ToPage != pages {
		printPath = filepath.Join(h.reportsDir(), in.ReportID+"-selection.pdf")
		if err := api.TrimFile(base, printPath, []string{fmt.Sprintf("%d-%d", in.FromPage, in.ToPage)}, nil); err != nil {
			c.JSON(500, gin.H{"error": "Не удалось подготовить выбранные страницы"})
			return
		}
	}
	values, _ := h.settings.GetAll()
	if storage.SettingEnabled(values, storage.SettingTestDeviceMode, true) || h.cfg.Printer.DryRun {
		time.Sleep(2 * time.Second)
	} else if err := h.printer.PrintFile(printPath, printjob.PrintOptions{Copies: 1}); err != nil {
		_ = h.history.Add(c.Request.Context(), ophistory.Entry{Operation: "report", Pages: in.ToPage - in.FromPage + 1, Success: false, ErrorText: err.Error()})
		c.JSON(500, gin.H{"error": "Не удалось напечатать отчёт"})
		return
	}
	count := in.ToPage - in.FromPage + 1
	_ = h.history.Add(c.Request.Context(), ophistory.Entry{Operation: "report", Pages: count, Sheets: count, Success: true})
	c.JSON(200, gin.H{"ok": true, "message": "Отчёт отправлен на бесплатную печать"})
}

func (h *Handler) PreviewHistoryReport(c *gin.Context) {
	id := filepath.Base(strings.TrimSpace(c.Param("id")))
	if id == "" || id != c.Param("id") {
		c.Status(http.StatusNotFound)
		return
	}
	path := filepath.Join(h.reportsDir(), id+".pdf")
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", `inline; filename="report.pdf"`)
	c.File(path)
}

type historyDeliveryRequest struct {
	ReportID  string `json:"report_id"`
	FileName  string `json:"file_name"`
	DrivePath string `json:"drive_path"`
	Email     string `json:"email"`
}

func (h *Handler) reportForDelivery(reportID, requestedName string) (string, string, error) {
	id := filepath.Base(strings.TrimSpace(reportID))
	if id == "" || id != reportID {
		return "", "", fmt.Errorf("отчёт не найден")
	}
	path := filepath.Join(h.reportsDir(), id+".pdf")
	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("отчёт не найден")
	}
	name := strings.TrimSpace(requestedName)
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\\|?*`, r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "", "", fmt.Errorf("укажите название файла")
	}
	if !strings.EqualFold(filepath.Ext(name), ".pdf") {
		name += ".pdf"
	}
	if len([]rune(name)) > 100 {
		return "", "", fmt.Errorf("название файла слишком длинное")
	}
	return path, name, nil
}

func (h *Handler) SaveHistoryReportUSB(c *gin.Context) {
	var in historyDeliveryRequest
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.DrivePath) == "" {
		c.JSON(400, gin.H{"error": "Выберите флешку"})
		return
	}
	path, name, err := h.reportForDelivery(in.ReportID, in.FileName)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	saved, err := usb.SaveFileToDrive(in.DrivePath, name, path)
	if err != nil {
		c.JSON(400, gin.H{"error": "Не удалось сохранить отчёт: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "Отчёт сохранён на флешку", "file_name": name, "saved_path": saved})
}

func (h *Handler) SendHistoryReportEmail(c *gin.Context) {
	var in historyDeliveryRequest
	if c.ShouldBindJSON(&in) != nil || !strings.Contains(strings.TrimSpace(in.Email), "@") {
		c.JSON(400, gin.H{"error": "Укажите корректный Email"})
		return
	}
	path, name, err := h.reportForDelivery(in.ReportID, in.FileName)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	deliveryKey := "email:" + in.ReportID + ":" + strings.ToLower(strings.TrimSpace(in.Email)) + ":" + strings.ToLower(name)
	h.deliveryMu.Lock()
	if h.delivered[deliveryKey] {
		h.deliveryMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"ok": true, "already_sent": true, "message": "Отчёт уже отправлен на этот Email", "file_name": name})
		return
	}
	if h.delivering[deliveryKey] {
		h.deliveryMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "Отчёт уже отправляется. Пожалуйста, подождите"})
		return
	}
	h.delivering[deliveryKey] = true
	h.deliveryMu.Unlock()
	deliverySucceeded := false
	defer func() {
		h.deliveryMu.Lock()
		delete(h.delivering, deliveryKey)
		if deliverySucceeded {
			h.delivered[deliveryKey] = true
		}
		h.deliveryMu.Unlock()
	}()
	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить настройки Email"})
		return
	}
	address := strings.TrimSpace(values[storage.SettingEmailAddress])
	login := strings.TrimSpace(values[storage.SettingEmailLogin])
	if login == "" {
		login = address
	}
	password := values[storage.SettingEmailPassword]
	if address == "" || password == "" || !storage.SettingEnabled(values, storage.SettingSourceEmailEnabled, true) {
		c.JSON(503, gin.H{"error": "Email не настроен"})
		return
	}
	host, port := mailout.ResolveHost(address)
	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(500, gin.H{"error": "Не удалось прочитать отчёт"})
		return
	}
	err = mailout.SendMail(mailout.Credentials{From: address, Login: login, Password: password, Host: host, Port: port}, strings.TrimSpace(in.Email), "Отчёт PRINTUS: "+name, "Здравствуйте!\r\n\r\nВо вложении отчёт об операциях терминала PRINTUS.\r\n", mailout.Attachment{Name: name, Data: data})
	if err != nil {
		c.JSON(502, gin.H{"error": "Не удалось отправить Email: " + err.Error()})
		return
	}
	deliverySucceeded = true
	c.JSON(200, gin.H{"ok": true, "message": "Отчёт отправлен на Email", "file_name": name})
}

func (h *Handler) StartHistoryReportMAX(c *gin.Context) {
	var in historyDeliveryRequest
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "Некорректный запрос"})
		return
	}
	_, name, err := h.reportForDelivery(in.ReportID, in.FileName)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if h.max == nil {
		c.JSON(503, gin.H{"error": "MAX недоступен"})
		return
	}
	sess, err := h.max.StartScanSession(in.ReportID, 2*time.Minute)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	username := h.max.BotUsername()
	if username == "" {
		if u, _, e := h.max.Info(c.Request.Context()); e == nil {
			username = u
		}
	}
	c.JSON(200, gin.H{"session": maxsvc.ScanSessionJSON(sess), "bot_username": username, "bot_link": "https://max.ru/" + username, "file_name": name})
}

func (h *Handler) GetHistoryReportMAX(c *gin.Context) {
	if h.max == nil {
		c.JSON(503, gin.H{"error": "MAX недоступен"})
		return
	}
	sess, ok := h.max.GetScan(c.Param("sid"))
	if !ok {
		c.JSON(404, gin.H{"error": "Сессия не найдена"})
		return
	}
	c.JSON(200, gin.H{"session": maxsvc.ScanSessionJSON(sess)})
}

func (h *Handler) CompleteHistoryReportMAX(c *gin.Context) {
	var in historyDeliveryRequest
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "Некорректный запрос"})
		return
	}
	sess, ok := h.max.GetScan(c.Param("sid"))
	if !ok || sess.JobID != in.ReportID {
		c.JSON(404, gin.H{"error": "Сессия не найдена"})
		return
	}
	if sess.Status != maxsvc.StatusFound || sess.UserID == 0 {
		c.JSON(400, gin.H{"error": "Пользователь ещё не подтвердил код"})
		return
	}
	path, name, err := h.reportForDelivery(in.ReportID, in.FileName)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.max.SendFileToUser(c.Request.Context(), sess.UserID, path, name, "Ваш отчёт PRINTUS: "+name); err != nil {
		c.JSON(502, gin.H{"error": "Не удалось отправить отчёт в MAX"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "Отчёт отправлен в MAX", "file_name": name})
}

func writeHistoryHTML(path string, days int, items []ophistory.Entry) error {
	const source = `<!doctype html><html><head><meta charset="utf-8"><style>@page{size:A4 landscape;margin:12mm}body{font-family:Arial,sans-serif;color:#172b55}h1{font-size:22pt}p{color:#60708c}table{width:100%;border-collapse:collapse;font-size:9pt}th{background:#eaf2ff;text-align:left}th,td{padding:7px;border:1px solid #cbd8eb}tr.error{background:#fff1f3}.ok{color:#118655;font-weight:bold}.bad{color:#c43d50;font-weight:bold}</style></head><body><h1>PRINTUS · История операций</h1><p>Последние {{.Days}} дней · сформировано {{.Now}}</p><table><thead><tr><th>Дата и время</th><th>Операция</th><th>Страниц</th><th>Листов</th><th>Оплачено</th><th>Результат</th><th>Ошибка</th></tr></thead><tbody>{{range .Items}}<tr class="{{if not .Success}}error{{end}}"><td>{{date .CreatedAt}}</td><td>{{op .Operation}}</td><td>{{.Pages}}</td><td>{{.Sheets}}</td><td>{{money .Amount}}</td><td class="{{if .Success}}ok{{else}}bad{{end}}">{{if .Success}}Успешно{{else}}Ошибка{{end}}</td><td>{{.ErrorText}}</td></tr>{{else}}<tr><td colspan="7">За выбранный период операций нет</td></tr>{{end}}</tbody></table></body></html>`
	t := template.Must(template.New("report").Funcs(template.FuncMap{"date": func(v time.Time) string { return v.Local().Format("02.01.2006 15:04:05") }, "op": operationLabel, "money": func(v float64) string { return fmt.Sprintf("%.2f ₽", v) }}).Parse(source))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, struct {
		Days  int
		Now   string
		Items []ophistory.Entry
	}{days, time.Now().Format("02.01.2006 15:04"), items})
}

func operationLabel(v string) string {
	switch v {
	case "print":
		return "Печать"
	case "copy":
		return "Копирование"
	case "scan":
		return "Сканирование"
	case "report":
		return "Печать отчёта"
	}
	return v
}
