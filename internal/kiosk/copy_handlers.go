package kiosk

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/copyjob"
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
	quote, err := copyjob.QuotePrice(copyjob.Options{Color: in.Color, Copies: in.Copies}, priceBW, priceColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, quote)
}

type copyPayRequest struct {
	Color  bool   `json:"color"`
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
	quote, err := copyjob.QuotePrice(copyjob.Options{Color: in.Color, Copies: in.Copies}, priceBW, priceColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sheetsNeeded := in.Copies
	if sheetsNeeded < 1 {
		sheetsNeeded = 1
	}
	if err := h.ensurePaper(sheetsNeeded); err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":           err.Error(),
			"code":            "paper_insufficient",
			"sheets_required": sheetsNeeded,
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

	job, err := h.copies.MarkPaid(c.Param("id"), copyjob.Options{Color: in.Color, Copies: in.Copies})
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
	sheetsNeeded := job.Copies
	if sheetsNeeded < 1 {
		sheetsNeeded = 1
	}
	if _, err := h.settings.ConsumePaper(sheetsNeeded); err != nil {
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
	job, sheets, err := h.copies.Execute(c.Param("id"))
	if err != nil {
		_, _ = h.settings.RefundPaper(sheetsNeeded)
		h.notifyErr(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.stats != nil {
		priceBW, priceColor, _, _ := h.copyPrices()
		price := priceBW
		if job.Color {
			price = priceColor
		}
		_ = h.stats.AddCopy(price*float64(job.Copies), job.Copies, sheets, job.Color)
	}
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
	// Color copies use the color print price when available.
	color, err = strconv.ParseFloat(values[storage.SettingPriceColor], 64)
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
		"copies": job.Copies,
	}
}
