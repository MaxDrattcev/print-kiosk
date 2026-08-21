package kiosk

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/maxsvc"
	"print-kiosk/internal/storage"
)

func (h *Handler) requireMax(c *gin.Context, needEnabled bool) bool {
	if h.max == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MAX не подключён", "enabled": false})
		return false
	}
	if needEnabled && !h.max.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MAX не активирован в настройках"})
		return false
	}
	return true
}

func (h *Handler) MaxInfo(c *gin.Context) {
	maxMB := "20"
	if h.settings != nil {
		if values, err := h.settings.GetAll(); err == nil {
			if v := strings.TrimSpace(values[storage.SettingEmailMaxFileSizeMB]); v != "" {
				maxMB = v
			}
		}
	}
	out := gin.H{
		"enabled":          false,
		"bot_username":     "",
		"bot_link":         "",
		"max_file_size_mb": maxMB,
	}
	if h.max == nil {
		out["error"] = "MAX не подключён"
		c.JSON(http.StatusOK, out)
		return
	}
	if !h.max.Enabled() {
		c.JSON(http.StatusOK, out)
		return
	}
	username, enabled, err := h.max.Info(c.Request.Context())
	out["enabled"] = enabled
	out["bot_username"] = username
	if username != "" {
		out["bot_link"] = "https://max.ru/" + username
	}
	if err != nil {
		out["error"] = err.Error()
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) StartMaxPrintSession(c *gin.Context) {
	if !h.requireMax(c, true) {
		return
	}
	sess, err := h.max.StartPrintSession(2 * time.Minute)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": maxsvc.PrintSessionJSON(sess)})
}

func (h *Handler) GetMaxPrintSession(c *gin.Context) {
	if !h.requireMax(c, false) {
		return
	}
	sess, ok := h.max.GetPrint(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "сессия не найдена"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": maxsvc.PrintSessionJSON(sess)})
}

func (h *Handler) ConfirmMaxPrintSession(c *gin.Context) {
	sess, err := h.max.ConfirmPrint(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": maxsvc.PrintSessionJSON(sess)})
}

func (h *Handler) RejectMaxPrintSession(c *gin.Context) {
	sess, err := h.max.RejectPrint(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": maxsvc.PrintSessionJSON(sess)})
}

func (h *Handler) MarkMaxPrinted(c *gin.Context) {
	var req struct {
		FileID string `json:"file_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.FileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите файл"})
		return
	}
	sess, err := h.max.MarkPrintPrinted(c.Param("id"), req.FileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": maxsvc.PrintSessionJSON(sess)})
}

func (h *Handler) PrepareMaxPrint(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id"`
		FileID    string `json:"file_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.SessionID == "" || req.FileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите файл"})
		return
	}
	_, file, err := h.max.GetPrintFile(req.SessionID, req.FileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	job, err := h.jobs.PrepareFromLocal(file.Path, file.Name)
	if err != nil {
		msg := err.Error()
		status := http.StatusBadRequest
		if strings.Contains(msg, "LibreOffice") {
			status = http.StatusServiceUnavailable
		}
		h.notifyErr(fmt.Errorf("подготовка печати MAX: %w", err))
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
			"preview_url":         "/api/kiosk/print/jobs/" + job.ID + "/preview",
		},
		"prices": prices,
	})
}

func (h *Handler) StartMaxScanSession(c *gin.Context) {
	if !h.requireMax(c, true) {
		return
	}
	jobID := c.Param("id")
	if _, ok := h.scans.Get(jobID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "заказ не найден"})
		return
	}
	if _, _, err := h.scans.ReadyForDelivery(jobID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sess, err := h.max.StartScanSession(jobID, 2*time.Minute)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	username := h.max.BotUsername()
	if username == "" {
		if u, _, e := h.max.Info(c.Request.Context()); e == nil {
			username = u
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"session":      maxsvc.ScanSessionJSON(sess),
		"bot_username": username,
		"bot_link":     "https://max.ru/" + username,
	})
}

func (h *Handler) GetMaxScanSession(c *gin.Context) {
	if !h.requireMax(c, false) {
		return
	}
	sess, ok := h.max.GetScan(c.Param("sid"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "сессия не найдена"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": maxsvc.ScanSessionJSON(sess)})
}

func (h *Handler) CompleteMaxScanSession(c *gin.Context) {
	if !h.requireMax(c, true) {
		return
	}
	sess, ok := h.max.GetScan(c.Param("sid"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "сессия не найдена"})
		return
	}
	if sess.Status != maxsvc.StatusFound || sess.UserID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "пользователь ещё не подтвердил код"})
		return
	}
	scanPath, fileName, err := h.scans.ReadyForDelivery(sess.JobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	caption := "Ваш скан с киоска печати: " + fileName
	if err := h.max.SendFileToUser(c.Request.Context(), sess.UserID, scanPath, fileName, caption); err != nil {
		h.notifyErr(fmt.Errorf("отправка скана в MAX: %w", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "не удалось отправить в MAX: " + err.Error()})
		return
	}
	job, err := h.scans.MarkDelivered(sess.JobID, "max:"+strconv.FormatInt(sess.UserID, 10))
	if err != nil {
		h.scans.CleanupFiles(sess.JobID)
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Скан отправлен в MAX"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Скан отправлен в MAX",
		"job":     scanJobJSON(job),
	})
}

func (h *Handler) notifyErr(err error) {
	if h.max == nil || err == nil {
		return
	}
	h.max.NotifyError(context.Background(), err)
}
