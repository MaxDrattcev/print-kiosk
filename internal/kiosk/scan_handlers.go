package kiosk

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/mailout"
	"print-kiosk/internal/scanjob"
	"print-kiosk/internal/storage"
)

func (h *Handler) CreateScanJob(c *gin.Context) {
	price, err := h.scanPrice()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить цену сканирования"})
		return
	}
	job, err := h.scans.Create(price)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось создать заказ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"job":   scanJobJSON(job),
		"price": price,
	})
}

func (h *Handler) GetScanJob(c *gin.Context) {
	job, ok := h.scans.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	price, _ := h.scanPrice()
	c.JSON(http.StatusOK, gin.H{
		"job":   scanJobJSON(job),
		"price": price,
	})
}

func (h *Handler) PreviewScanJob(c *gin.Context) {
	path, err := h.scans.PreviewPath(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.File(path)
}

type scanPayRequest struct {
	Method string `json:"method"`
}

func (h *Handler) PayScanJob(c *gin.Context) {
	if _, ok := h.scans.Get(c.Param("id")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}

	var in scanPayRequest
	_ = c.ShouldBindJSON(&in)

	if err := simulatePayment(in.Method); err != nil {
		if errors.Is(err, errPaymentQR) {
			c.JSON(http.StatusNotImplemented, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	job, err := h.scans.MarkPaid(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"paid":    true,
		"method":  "terminal",
		"message": "Оплата прошла успешно",
		"job":     scanJobJSON(job),
	})
}

func (h *Handler) ExecuteScanJob(c *gin.Context) {
	job, err := h.scans.Scan(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Документ отсканирован",
		"job":     scanJobJSON(job),
	})
}

type scanNameRequest struct {
	Name string `json:"name"`
}

func (h *Handler) NameScanJob(c *gin.Context) {
	var in scanNameRequest
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите имя файла"})
		return
	}
	job, err := h.scans.SetName(c.Param("id"), in.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "job": scanJobJSON(job)})
}

type scanSaveUSBRequest struct {
	DrivePath string `json:"drive_path"`
}

func (h *Handler) SaveScanToUSB(c *gin.Context) {
	var in scanSaveUSBRequest
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.DrivePath) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите флешку"})
		return
	}
	job, err := h.scans.SaveToUSB(c.Param("id"), in.DrivePath)
	if err != nil {
		msg := err.Error()
		status := http.StatusBadRequest
		if strings.Contains(msg, "флешка не найдена") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"message":    "Скан сохранён на флешку",
		"saved_path": job.SavedPath,
		"job":        scanJobJSON(job),
	})
}

type scanSendEmailRequest struct {
	Email string `json:"email"`
}

func (h *Handler) SendScanEmail(c *gin.Context) {
	var in scanSendEmailRequest
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.Email) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите email получателя"})
		return
	}

	scanPath, fileName, err := h.scans.ReadyForDelivery(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, err := os.ReadFile(scanPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось прочитать файл скана"})
		return
	}

	cfg, err := h.smtpCredentials()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	subject := "Ваш скан: " + fileName
	body := "Здравствуйте!\r\n\r\nВо вложении отсканированный документ с киоска печати.\r\n\r\nФайл: " + fileName + "\r\n"
	if err := mailout.SendMail(cfg, in.Email, subject, body, mailout.Attachment{
		Name: fileName,
		Data: data,
	}); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "не удалось отправить письмо: " + err.Error()})
		return
	}

	job, err := h.scans.MarkSentEmail(c.Param("id"), in.Email)
	if err != nil {
		// Letter already left the kiosk — still drop the local copy.
		h.scans.CleanupFiles(c.Param("id"))
		c.JSON(http.StatusOK, gin.H{
			"ok":      true,
			"message": "Письмо отправлено",
			"email":   strings.TrimSpace(in.Email),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Скан отправлен на email",
		"email":   job.SentToEmail,
		"job":     scanJobJSON(job),
	})
}

func (h *Handler) smtpCredentials() (mailout.Credentials, error) {
	values, err := h.settings.GetAll()
	if err != nil {
		return mailout.Credentials{}, err
	}
	addr := strings.TrimSpace(values[storage.SettingEmailAddress])
	login := strings.TrimSpace(values[storage.SettingEmailLogin])
	pass := values[storage.SettingEmailPassword]
	if addr == "" || pass == "" {
		return mailout.Credentials{}, errEmailNotConfigured
	}
	if login == "" {
		login = addr
	}
	host, port := mailout.ResolveHost(addr)
	return mailout.Credentials{
		From:     addr,
		Login:    login,
		Password: pass,
		Host:     host,
		Port:     port,
	}, nil
}

func (h *Handler) scanPrice() (float64, error) {
	values, err := h.settings.GetAll()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(values[storage.SettingPriceScan], 64)
}

func scanJobJSON(job *scanjob.Job) gin.H {
	preview := ""
	if job.ScanPath != "" {
		preview = "/api/kiosk/scan/jobs/" + job.ID + "/preview"
	}
	return gin.H{
		"id":            job.ID,
		"status":        job.Status,
		"paid":          job.Paid,
		"file_name":     job.FileName,
		"price":         job.PricePerScan,
		"preview_url":   preview,
		"saved_path":    job.SavedPath,
		"sent_to_email": job.SentToEmail,
	}
}
