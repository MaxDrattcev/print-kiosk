package kiosk

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/copyjob"
	"print-kiosk/internal/ophistory"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/storage"
)

func (h *Handler) CreateCopyJob(c *gin.Context) {
	job := h.copies.Create()
	priceBW, priceColor, paper, err := h.copyPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"job": copyJobJSON(job),
		"prices": gin.H{
			"bw":              priceBW,
			"color":           priceColor,
			"paper_remaining": paper,
		},
	})
}

func (h *Handler) GetCopyJob(c *gin.Context) {
	job, ok := h.copies.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	priceBW, priceColor, paper, err := h.copyPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"job": copyJobJSON(job),
		"prices": gin.H{
			"bw":              priceBW,
			"color":           priceColor,
			"paper_remaining": paper,
		},
	})
}

type copyQuoteRequest struct {
	Color  bool `json:"color"`
	Duplex bool `json:"duplex"`
	Copies int  `json:"copies"`
}

func (h *Handler) QuoteCopyJob(c *gin.Context) {
	if _, ok := h.copies.Get(c.Param("id")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	var in copyQuoteRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректные параметры"})
		return
	}
	priceBW, priceColor, _, err := h.copyPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	quote, err := copyjob.QuotePrice(copyjob.Options{Color: in.Color, Duplex: in.Duplex, Copies: in.Copies}, priceBW, priceColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, quote)
}

type copyPayRequest struct {
	Color  bool   `json:"color"`
	Duplex bool   `json:"duplex"`
	Copies int    `json:"copies"`
	Method string `json:"method"`
}

func (h *Handler) PayCopyJob(c *gin.Context) {
	if _, ok := h.copies.Get(c.Param("id")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	var in copyPayRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректные параметры"})
		return
	}

	priceBW, priceColor, _, err := h.copyPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цены"})
		return
	}
	quote, err := copyjob.QuotePrice(copyjob.Options{Color: in.Color, Duplex: in.Duplex, Copies: in.Copies}, priceBW, priceColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sheetsNeeded := quote.Sheets
	if err := h.ensurePaper(sheetsNeeded); err != nil {
		code := "paper_insufficient"
		if errors.Is(err, errPrinterFaultBlocked) {
			code = "printer_blocked"
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":           err.Error(),
			"code":            code,
			"sheets_required": sheetsNeeded,
			"paper_remaining": h.paperRemaining(),
		})
		return
	}

	if err := h.processPayment(c.Request.Context(), in.Method, quote.Total, c.Param("id")); err != nil {
		if errors.Is(err, errPaymentQR) {
			c.JSON(http.StatusNotImplemented, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	job, err := h.copies.MarkPaid(c.Param("id"), copyjob.Options{Color: in.Color, Duplex: quote.Duplex, Copies: in.Copies})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"paid":    true,
		"method":  "terminal",
		"message": "Оплата прошла успешно",
		"quote":   quote,
		"job":     copyJobJSON(job),
	})
}

func (h *Handler) ExecuteCopyJob(c *gin.Context) {
	job, ok := h.copies.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	sheetsNeeded := printjob.PaperSheets(1, job.Copies, job.Duplex)
	deviceTest := h.testDeviceMode()
	priceBW, priceColor, _, _ := h.copyPrices()
	unitPrice := priceBW
	if job.Color {
		unitPrice = priceColor
	}
	paidAmount := unitPrice * float64(job.Copies)
	if h.testPaymentMode() {
		paidAmount = 0
	}
	jobID := job.ID
	requestedCopies := job.Copies
	if _, err := h.settings.ConsumePaper(sheetsNeeded); err != nil {
		h.recordOperation(c.Request.Context(), ophistory.Entry{Operation: "copy", JobID: job.ID, Pages: job.Copies, Amount: paidAmount, Success: false, ErrorText: err.Error()})
		if errors.Is(err, storage.ErrInsufficientPaper) {
			c.JSON(http.StatusConflict, gin.H{
				"error":           "В принтере недостаточно бумаги для печати",
				"code":            "paper_insufficient",
				"sheets_required": sheetsNeeded,
				"paper_remaining": h.paperRemaining(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось обновить счётчик бумаги"})
		return
	}
	var sheets int
	var err error
	if deviceTest {
		time.Sleep(10 * time.Second)
		job, sheets, err = h.copies.CompleteTest(c.Param("id"))
	} else {
		job, sheets, err = h.copies.Execute(c.Param("id"))
	}
	if err != nil {
		paperOut := printjob.IsPaperOut(err)
		paperJam := printjob.IsPaperJam(err)
		if paperOut {
			h.markPaperEmpty(jobID)
		} else {
			_, _ = h.settings.RefundPaper(sheetsNeeded)
		}
		if paperJam {
			h.markPrinterFault(jobID, "Замятие бумаги")
		}
		h.notifyErr(err)
		h.recordOperation(c.Request.Context(), ophistory.Entry{Operation: "copy", JobID: jobID, Pages: requestedCopies, Amount: paidAmount, Success: false, ErrorText: err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.stats != nil {
		price := priceBW
		if job.Color {
			price = priceColor
		}
		revenue := price * float64(job.Copies)
		if h.testPaymentMode() {
			revenue = 0
		}
		if err := h.stats.AddCopy(revenue, job.Copies, sheets, job.Color); err != nil {
			slog.Error("copy statistics update failed", "job_id", job.ID, "error", err)
		}
	}
	h.recordOperation(c.Request.Context(), ophistory.Entry{Operation: "copy", JobID: job.ID, Pages: job.Copies, Sheets: sheets, Amount: paidAmount, Success: true})
	if h.max != nil {
		h.max.CheckPaperAlert(context.Background())
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Документ отправлен на копирование",
		"sheets":  sheets,
		"job":     copyJobJSON(job),
	})
}

func (h *Handler) copyPrices() (bw, color float64, paper string, err error) {
	values, err := h.settings.GetAll()
	if err != nil {
		return 0, 0, "", err
	}
	bw, err = strconv.ParseFloat(values[storage.SettingPriceCopy], 64)
	if err != nil {
		return 0, 0, "", err
	}
	color, err = strconv.ParseFloat(values[storage.SettingPriceCopyColor], 64)
	if err != nil {
		color = bw
		err = nil
	}
	paper = values[storage.SettingPaperRemaining]
	return bw, color, paper, nil
}

func copyJobJSON(job *copyjob.Job) gin.H {
	return gin.H{
		"id":     job.ID,
		"status": job.Status,
		"paid":   job.Paid,
		"color":  job.Color,
		"duplex": job.Duplex,
		"copies": job.Copies,
	}
}
