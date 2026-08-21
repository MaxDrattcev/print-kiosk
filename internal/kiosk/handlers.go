package kiosk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
	"print-kiosk/internal/copyjob"
	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/maxsvc"
	"print-kiosk/internal/ophistory"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/scanjob"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
	"print-kiosk/internal/usb"
)

type Handler struct {
	cfg      *config.Config
	settings *storage.SettingsRepo
	jobs     *printjob.Service
	scans    *scanjob.Service
	copies   *copyjob.Service
	mail     *mailinbox.Service
	max      *maxsvc.Service
	stats    *stats.Repo
	history  *ophistory.Repo
}

var errPrinterFaultBlocked = errors.New("Извините, принтер временно недоступен. Мы уже сообщили специалисту — он скоро всё исправит.")

func NewHandler(
	cfg *config.Config,
	settings *storage.SettingsRepo,
	jobs *printjob.Service,
	scans *scanjob.Service,
	copies *copyjob.Service,
	mail *mailinbox.Service,
	max *maxsvc.Service,
	st *stats.Repo,
	history *ophistory.Repo,
) *Handler {
	return &Handler{
		cfg: cfg, settings: settings, jobs: jobs, scans: scans,
		copies: copies, mail: mail, max: max, stats: st, history: history,
	}
}

func (h *Handler) recordOperation(ctx context.Context, e ophistory.Entry) {
	if h.history == nil {
		return
	}
	if err := h.history.Add(ctx, e); err != nil {
		slog.Error("operation history write failed", "operation", e.Operation, "job_id", e.JobID, "error", err)
	}
}

func (h *Handler) Info(c *gin.Context) {
	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить данные"})
		return
	}

	emailReady := storage.EmailReady(values)
	emailOn := storage.SettingEnabled(values, storage.SettingSourceEmailEnabled, true) && emailReady
	timeoutSec, _ := strconv.Atoi(values[storage.SettingSessionTimeoutSec])
	if timeoutSec < 0 {
		timeoutSec = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"price_bw":             values[storage.SettingPriceBW],
		"price_color":          values[storage.SettingPriceColor],
		"price_copy":           values[storage.SettingPriceCopy],
		"price_copy_color":     values[storage.SettingPriceCopyColor],
		"price_scan":           values[storage.SettingPriceScan],
		"support_text":         values[storage.SettingSupportText],
		"service_print":        storage.SettingEnabled(values, storage.SettingServicePrintEnabled, true),
		"service_copy":         storage.SettingEnabled(values, storage.SettingServiceCopyEnabled, true),
		"service_scan":         storage.SettingEnabled(values, storage.SettingServiceScanEnabled, true),
		"source_usb":           storage.SettingEnabled(values, storage.SettingSourceUSBEnabled, true),
		"source_email":         emailOn,
		"source_max":           storage.MaxKioskReady(values),
		"payment_qr":           false,
		"session_timeout_sec":  timeoutSec,
		"paper_remaining":      values[storage.SettingPaperRemaining],
		"printer_blocked":      storage.SettingEnabled(values, storage.SettingPrinterFaultBlocked, false),
		"printer_block_reason": values[storage.SettingPrinterFaultReason],
	})
}

func (h *Handler) ListUSBDrives(c *gin.Context) {
	drives, err := usb.ListRemovableDrives()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось проверить USB"})
		return
	}
	if drives == nil {
		drives = []usb.Drive{}
	}
	c.JSON(http.StatusOK, gin.H{"drives": drives})
}

func (h *Handler) BrowseUSB(c *gin.Context) {
	// Do not TrimSpace: volume names can end with spaces (e.g. "/Volumes/N ").
	path := c.Query("path")
	if path == "" {
		drives, err := usb.ListRemovableDrives()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось проверить USB"})
			return
		}
		if len(drives) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "флешка не найдена. Вставьте USB-накопитель и попробуйте снова"})
			return
		}
		if len(drives) == 1 {
			path = drives[0].Path
		} else {
			c.JSON(http.StatusOK, gin.H{"drives": drives})
			return
		}
	}

	listing, err := usb.ListDir(path)
	if err != nil {
		status := http.StatusBadRequest
		msg := err.Error()
		switch {
		case errors.Is(err, os.ErrNotExist), strings.Contains(msg, "usb drive not found"):
			status = http.StatusNotFound
			msg = "флешка не найдена. Вставьте USB-накопитель и попробуйте снова"
		case strings.Contains(msg, "outside usb"):
			status = http.StatusForbidden
			msg = "доступ запрещён"
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, listing)
}

type prepareRequest struct {
	Path string `json:"path"`
}

func (h *Handler) PreparePrint(c *gin.Context) {
	var req prepareRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите путь к файлу"})
		return
	}

	job, err := h.jobs.PrepareFromUSB(req.Path)
	if err != nil {
		msg := err.Error()
		status := http.StatusBadRequest
		switch {
		case strings.Contains(msg, "outside usb"):
			status = http.StatusForbidden
			msg = "доступ запрещён"
		case strings.Contains(msg, "LibreOffice"):
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	prices, err := h.pricePair()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job": gin.H{
			"id":                  job.ID,
			"file_name":           job.FileName,
			"pages":               job.Pages,
			"preview_kind":        job.PreviewKind,
			"natural_orientation": job.NaturalOrientation,
			"suggested_color":     job.SuggestedColor,
			"preview_url":         "/api/kiosk/print/jobs/" + job.ID + "/preview",
		},
		"prices": prices,
	})
}

func (h *Handler) GetPrintJob(c *gin.Context) {
	job, ok := h.jobs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	prices, err := h.pricePair()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"job": gin.H{
			"id":                  job.ID,
			"file_name":           job.FileName,
			"pages":               job.Pages,
			"preview_kind":        job.PreviewKind,
			"natural_orientation": job.NaturalOrientation,
			"suggested_color":     job.SuggestedColor,
			"preview_url":         "/api/kiosk/print/jobs/" + job.ID + "/preview",
		},
		"prices": prices,
	})
}

func (h *Handler) PreviewPrintJob(c *gin.Context) {
	job, ok := h.jobs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	if job.PreviewKind == printjob.PreviewImage && job.SourcePath != "" {
		c.File(job.SourcePath)
		return
	}
	c.File(job.PreviewPath)
}

func (h *Handler) QuotePrintJob(c *gin.Context) {
	job, ok := h.jobs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}

	var in printjob.QuoteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректные параметры"})
		return
	}

	bw, color, err := h.priceValues()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	quote, err := h.jobs.Quote(job, in, bw, color)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, quote)
}

type payRequest struct {
	printjob.QuoteInput
	Method string `json:"method"`
}

func (h *Handler) PayPrintJob(c *gin.Context) {
	job, ok := h.jobs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}

	var in payRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректные параметры"})
		return
	}

	bw, color, err := h.priceValues()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	quote, err := h.jobs.Quote(job, in.QuoteInput, bw, color)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.ensurePaper(quote.Sheets); err != nil {
		code := "paper_insufficient"
		if errors.Is(err, errPrinterFaultBlocked) {
			code = "printer_blocked"
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":           err.Error(),
			"code":            code,
			"sheets_required": quote.Sheets,
			"paper_remaining": h.paperRemaining(),
		})
		return
	}

	if err := h.processPayment(c.Request.Context(), in.Method, quote.Total, job.ID); err != nil {
		if errors.Is(err, errPaymentQR) {
			c.JSON(http.StatusNotImplemented, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.jobs.LockOptions(job, in.QuoteInput)

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"paid":    true,
		"method":  "terminal",
		"message": "Оплата прошла успешно",
		"quote":   quote,
		"job_id":  job.ID,
	})
}

func (h *Handler) ExecutePrintJob(c *gin.Context) {
	job, ok := h.jobs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	if !job.Paid {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": "сначала оплатите печать"})
		return
	}

	var in printjob.QuoteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректные параметры"})
		return
	}
	if locked, ok := h.jobs.LockedOptions(job); ok {
		in = locked
	}
	if in.Copies < 1 {
		in.Copies = 1
	}

	bw, colorPrice, _ := h.priceValues()
	quote, err := h.jobs.Quote(job, in, bw, colorPrice)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sheets := quote.Sheets
	pages := quote.Pages * in.Copies
	price := bw
	if in.Color {
		price = colorPrice
	}
	revenue := price * float64(pages)
	paidAmount := revenue
	if h.testPaymentMode() {
		paidAmount = 0
	}
	deviceTest := h.testDeviceMode()
	if _, err := h.settings.ConsumePaper(sheets); err != nil {
		h.recordOperation(c.Request.Context(), ophistory.Entry{Operation: "print", JobID: job.ID, Pages: pages, Amount: paidAmount, Success: false, ErrorText: err.Error()})
		if errors.Is(err, storage.ErrInsufficientPaper) {
			c.JSON(http.StatusConflict, gin.H{
				"error":           "В принтере недостаточно бумаги для печати",
				"code":            "paper_insufficient",
				"sheets_required": sheets,
				"paper_remaining": h.paperRemaining(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось обновить счётчик бумаги"})
		return
	}

	var printErr error
	if deviceTest {
		time.Sleep(10 * time.Second)
		slog.Info("test print completed", "job_id", job.ID)
	} else {
		if err := h.jobs.ApplyImageOrientation(job, in.Orientation); err != nil {
			printErr = fmt.Errorf("подготовка ориентации изображения: %w", err)
		} else {
			printErr = h.jobs.Print(job, printjob.PrintOptions{
				Color: in.Color, Duplex: in.Duplex, Copies: in.Copies,
				Orientation: in.Orientation, PageRange: quote.PageRange, Scale: quote.Scale,
			})
		}
	}
	if printErr != nil {
		paperOut := printjob.IsPaperOut(printErr)
		paperJam := printjob.IsPaperJam(printErr)
		if paperOut {
			h.markPaperEmpty(job.ID)
		} else {
			_, _ = h.settings.RefundPaper(sheets)
		}
		if paperJam {
			h.markPrinterFault(job.ID, "Замятие бумаги")
		}
		technicalErr := fmt.Errorf("печать задания %s на принтере %q: %w", job.ID, h.cfg.Printer.Name, printErr)
		slog.Error("print job failed", "job_id", job.ID, "printer", h.cfg.Printer.Name, "error", printErr)
		h.notifyErr(technicalErr)
		h.recordOperation(c.Request.Context(), ophistory.Entry{Operation: "print", JobID: job.ID, Pages: pages, Amount: paidAmount, Success: false, ErrorText: technicalErr.Error()})
		if paperOut {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":           "Извините, в принтере закончилась бумага. Мы уже сообщили специалисту — он скоро всё исправит.",
				"code":            "paper_empty",
				"paper_remaining": 0,
			})
			return
		}
		if paperJam {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":  errPrinterFaultBlocked.Error(),
				"code":   "printer_blocked",
				"reason": "Замятие бумаги",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось отправить на печать: " + printErr.Error()})
		return
	}

	if h.testPaymentMode() {
		revenue = 0
	}
	if h.stats != nil {
		if err := h.stats.AddPrint(revenue, pages, in.Color, sheets); err != nil {
			slog.Error("print statistics update failed", "job_id", job.ID, "error", err)
		}
	}
	if h.max != nil {
		h.max.CheckPaperAlert(context.Background())
	}
	h.recordOperation(c.Request.Context(), ophistory.Entry{Operation: "print", JobID: job.ID, Pages: pages, Sheets: sheets, Amount: paidAmount, Success: true})

	h.jobs.Cleanup(job.ID)

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Печать документа завершена",
		"sheets":  sheets,
		"pages":   quote.Pages,
		"duplex":  in.Duplex,
		"copies":  in.Copies,
	})
}

func (h *Handler) markPaperEmpty(jobID string) {
	if _, err := h.settings.SetPaperRemaining(0); err != nil {
		slog.Error("failed to mark printer paper as empty", "job_id", jobID, "error", err)
		return
	}
	slog.Warn("printer reported paper out; paper balance set to zero", "job_id", jobID)
}

func (h *Handler) markPrinterFault(jobID, reason string) {
	if err := h.settings.SetMany(map[string]string{
		storage.SettingPrinterFaultBlocked: "true",
		storage.SettingPrinterFaultReason:  reason,
	}); err != nil {
		slog.Error("failed to block printer after device fault", "job_id", jobID, "reason", reason, "error", err)
		return
	}
	slog.Warn("printer blocked after device fault", "job_id", jobID, "reason", reason)
}

func (h *Handler) printerFaultBlocked() bool {
	values, err := h.settings.GetAll()
	if err != nil {
		return false
	}
	return storage.SettingEnabled(values, storage.SettingPrinterFaultBlocked, false)
}

func (h *Handler) paperRemaining() int {
	n, err := h.settings.PaperRemaining()
	if err != nil {
		return 0
	}
	return n
}

func (h *Handler) ensurePaper(sheets int) error {
	if h.printerFaultBlocked() {
		return errPrinterFaultBlocked
	}
	if sheets <= 0 {
		return nil
	}
	remaining := h.paperRemaining()
	if remaining < sheets {
		return errString("В принтере недостаточно бумаги для печати")
	}
	return nil
}

func (h *Handler) pricePair() (gin.H, error) {
	values, err := h.settings.GetAll()
	if err != nil {
		return nil, err
	}
	bw, err := strconv.ParseFloat(values[storage.SettingPriceBW], 64)
	if err != nil {
		return nil, err
	}
	color, err := strconv.ParseFloat(values[storage.SettingPriceColor], 64)
	if err != nil {
		return nil, err
	}
	paper := values[storage.SettingPaperRemaining]
	return gin.H{
		"bw":              bw,
		"color":           color,
		"paper_remaining": paper,
	}, nil
}

func (h *Handler) priceValues() (float64, float64, error) {
	values, err := h.settings.GetAll()
	if err != nil {
		return 0, 0, err
	}
	bw, err := strconv.ParseFloat(values[storage.SettingPriceBW], 64)
	if err != nil {
		return 0, 0, err
	}
	color, err := strconv.ParseFloat(values[storage.SettingPriceColor], 64)
	if err != nil {
		return 0, 0, err
	}
	return bw, color, nil
}
