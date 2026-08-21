package kiosk

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/storage"
)

func (h *Handler) EmailInfo(c *gin.Context) {
	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить настройки"})
		return
	}
	addr := strings.TrimSpace(values[storage.SettingEmailAddress])
	if addr == "" || !storage.SettingEnabled(values, storage.SettingSourceEmailEnabled, true) || !storage.EmailReady(values) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Печать по Email временно недоступна"})
		return
	}
	maxMB := values[storage.SettingEmailMaxFileSizeMB]
	c.JSON(http.StatusOK, gin.H{
		"email_address":     addr,
		"max_file_size_mb":  maxMB,
		"poll_interval_sec": values[storage.SettingEmailPollIntervalSec],
		"supported_hint":    "PDF, Word, Excel, PowerPoint, изображения",
	})
}

func (h *Handler) StartEmailSession(c *gin.Context) {
	cfg, pollEvery, err := h.emailCredentials()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	sess, err := h.mail.Start(cfg, pollEvery, 2*time.Minute)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"session": emailSessionJSON(sess),
	})
}

func (h *Handler) GetEmailSession(c *gin.Context) {
	sess, ok := h.mail.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "сессия не найдена"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": emailSessionJSON(sess)})
}

func (h *Handler) ConfirmEmailSession(c *gin.Context) {
	sess, err := h.mail.Confirm(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": emailSessionJSON(sess)})
}

func (h *Handler) RejectEmailSession(c *gin.Context) {
	sess, err := h.mail.Reject(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": emailSessionJSON(sess)})
}

type emailPrintedRequest struct {
	FileID string `json:"file_id"`
}

func (h *Handler) MarkEmailPrinted(c *gin.Context) {
	var req emailPrintedRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.FileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите файл"})
		return
	}
	sess, err := h.mail.MarkPrinted(c.Param("id"), req.FileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Fallback cleanup if deletion right after confirm failed.
	if uid, ok := h.mail.TakeDeleteUID(c.Param("id")); ok {
		cfg, _, credErr := h.emailCredentials()
		if credErr == nil {
			if delErr := mailinbox.DeleteUIDs(cfg, []uint32{uid}); delErr != nil {
				// Allow retry next time.
				h.mail.ResetDeleteFlag(c.Param("id"), uid)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"session": emailSessionJSON(sess)})
}

type emailPrepareRequest struct {
	SessionID string `json:"session_id"`
	FileID    string `json:"file_id"`
}

func (h *Handler) PrepareEmailPrint(c *gin.Context) {
	var req emailPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.SessionID == "" || req.FileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите файл"})
		return
	}
	_, file, err := h.mail.GetFile(req.SessionID, req.FileID)
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

func (h *Handler) emailCredentials() (mailinbox.Credentials, time.Duration, error) {
	values, err := h.settings.GetAll()
	if err != nil {
		return mailinbox.Credentials{}, 0, err
	}
	addr := strings.TrimSpace(values[storage.SettingEmailAddress])
	login := strings.TrimSpace(values[storage.SettingEmailLogin])
	pass := values[storage.SettingEmailPassword]
	if addr == "" || pass == "" || !storage.SettingEnabled(values, storage.SettingSourceEmailEnabled, true) {
		return mailinbox.Credentials{}, 0, errEmailNotConfigured
	}
	if login == "" {
		login = addr
	}
	host, port := mailinbox.ResolveHost(addr)
	maxMB, _ := strconv.Atoi(values[storage.SettingEmailMaxFileSizeMB])
	if maxMB < 1 {
		maxMB = 20
	}
	pollSec, _ := strconv.Atoi(values[storage.SettingEmailPollIntervalSec])
	pollEvery := time.Duration(pollSec) * time.Second
	if pollEvery < 5*time.Second {
		pollEvery = 5 * time.Second
	}
	return mailinbox.Credentials{
		Address:  addr,
		Login:    login,
		Password: pass,
		Host:     host,
		Port:     port,
		MaxBytes: int64(maxMB) * 1024 * 1024,
	}, pollEvery, nil
}

var errEmailNotConfigured = errString("Печать по Email временно недоступна")

func emailSessionJSON(sess *mailinbox.Session) gin.H {
	files := make([]gin.H, 0, len(sess.Files))
	remaining := 0
	for _, f := range sess.Files {
		if !f.Printed {
			remaining++
		}
		files = append(files, gin.H{
			"id":      f.ID,
			"name":    f.Name,
			"size":    f.Size,
			"printed": f.Printed,
		})
	}
	deadlineMS := int64(0)
	if !sess.Deadline.IsZero() {
		deadlineMS = sess.Deadline.UnixMilli()
	}
	return gin.H{
		"id":          sess.ID,
		"status":      sess.Status,
		"error":       sess.Error,
		"from":        sess.From,
		"files":       files,
		"remaining":   remaining,
		"deadline_ms": deadlineMS,
	}
}
