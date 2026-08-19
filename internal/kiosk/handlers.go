package kiosk

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
	"print-kiosk/internal/copyjob"
	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/scanjob"
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
}

func NewHandler(cfg *config.Config, settings *storage.SettingsRepo, jobs *printjob.Service, scans *scanjob.Service, copies *copyjob.Service, mail *mailinbox.Service) *Handler {
	return &Handler{cfg: cfg, settings: settings, jobs: jobs, scans: scans, copies: copies, mail: mail}
}

func (h *Handler) Info(c *gin.Context) {
	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить данные"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"price_bw":     values[storage.SettingPriceBW],
		"price_color":  values[storage.SettingPriceColor],
		"price_copy":   values[storage.SettingPriceCopy],
		"price_scan":   values[storage.SettingPriceScan],
		"support_text": values[storage.SettingSupportText],
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
			"id":           job.ID,
			"file_name":    job.FileName,
			"pages":        job.Pages,
			"preview_kind": job.PreviewKind,
			"preview_url":  "/api/kiosk/print/jobs/" + job.ID + "/preview",
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
			"id":           job.ID,
			"file_name":    job.FileName,
			"pages":        job.Pages,
			"preview_kind": job.PreviewKind,
			"preview_url":  "/api/kiosk/print/jobs/" + job.ID + "/preview",
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
		c.JSON(http.StatusConflict, gin.H{
			"error":           err.Error(),
			"code":            "paper_insufficient",
			"sheets_required": quote.Sheets,
			"paper_remaining": h.paperRemaining(),
		})
		return
	}

	if err := simulatePayment(in.Method); err != nil {
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

	sheets := printjob.PaperSheets(job.Pages, in.Copies, in.Duplex)
	if _, err := h.settings.ConsumePaper(sheets); err != nil {
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

	if err := h.jobs.Print(job, printjob.PrintOptions{
		Color:  in.Color,
		Duplex: in.Duplex,
		Copies: in.Copies,
	}); err != nil {
		_, _ = h.settings.RefundPaper(sheets)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось отправить на печать: " + err.Error()})
		return
	}

	h.jobs.Cleanup(job.ID)

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Документ отправлен на печать",
		"sheets":  sheets,
		"pages":   job.Pages,
		"duplex":  in.Duplex,
		"copies":  in.Copies,
	})
}

func (h *Handler) paperRemaining() int {
	n, err := h.settings.PaperRemaining()
	if err != nil {
		return 0
	}
	return n
}

func (h *Handler) ensurePaper(sheets int) error {
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
