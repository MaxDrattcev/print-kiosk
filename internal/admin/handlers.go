package admin

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
	"print-kiosk/internal/libreoffice"
	"print-kiosk/internal/mailcfg"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
	"print-kiosk/internal/usb"
)

type Handler struct {
	cfg       *config.Config
	sessions  *SessionStore
	settings  *storage.SettingsRepo
	stats     *stats.Repo
	startedAt time.Time
}

func NewHandler(cfg *config.Config, sessions *SessionStore, settings *storage.SettingsRepo, st *stats.Repo) *Handler {
	return &Handler{
		cfg:       cfg,
		sessions:  sessions,
		settings:  settings,
		stats:     st,
		startedAt: time.Now(),
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный запрос"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите логин и пароль"})
		return
	}

	if !credentialsMatch(h.cfg.Admin, req.Username, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "неверный логин или пароль"})
		return
	}

	token, err := h.sessions.Create(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось создать сессию"})
		return
	}

	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "username": req.Username})
}

func (h *Handler) Logout(c *gin.Context) {
	if token := sessionTokenFromRequest(c); token != "" {
		h.sessions.Delete(token)
	}
	clearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) Me(c *gin.Context) {
	username, ok := c.Get("admin_username")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "не авторизован"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"username": username})
}

func (h *Handler) GetSettings(c *gin.Context) {
	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}

	for key := range values {
		if storage.IsSensitiveSetting(key) && values[key] != "" {
			values[key] = "********"
		}
	}

	c.JSON(http.StatusOK, gin.H{"settings": values})
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	var payload map[string]string
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный JSON"})
		return
	}

	updates := make(map[string]string, len(payload))
	for key, value := range payload {
		if !storage.IsKnownSetting(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "неизвестный параметр: " + key})
			return
		}
		if storage.IsSensitiveSetting(key) && (value == "" || value == "********") {
			continue
		}
		if err := validateSetting(key, value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updates[key] = strings.TrimSpace(value)
	}

	if err := h.settings.SetMany(updates); err != nil {
		slog.Error("admin settings save", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сохранить настройки"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) Overview(c *gin.Context) {
	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}

	paper, _ := strconv.Atoi(values[storage.SettingPaperRemaining])
	threshold, _ := strconv.Atoi(values[storage.SettingPaperAlertThreshold])

	usbCard := gin.H{"known": true, "available": false, "count": 0}
	if drives, err := usb.ListRemovableDrives(); err == nil {
		usbCard["available"] = len(drives) > 0
		usbCard["count"] = len(drives)
	}

	var today gin.H
	if h.stats != nil {
		if day, err := h.stats.GetDay(""); err == nil {
			today = gin.H{
				"revenue":     day.Revenue,
				"pages_bw":    day.PagesBW,
				"pages_color": day.PagesColor,
				"scans":       day.Scans,
				"copies":      day.Copies,
				"sheets_used": day.SheetsUsed,
			}
		}
	}

	username, _ := c.Get("admin_username")

	emailReady := storage.EmailReady(values)
	emailOn := storage.SettingEnabled(values, storage.SettingSourceEmailEnabled, true)
	emailStatus := "off"
	emailLabel := "Выключен"
	if !emailOn {
		emailStatus, emailLabel = "off", "Выключен"
	} else if !emailReady {
		emailStatus, emailLabel = "warn", "Не настроен"
	} else {
		emailStatus, emailLabel = "ok", "Настроен"
	}

	maxOn := storage.SettingEnabled(values, storage.SettingMaxEnabled, false)
	maxToken := storage.MaxTokenSet(values)
	maxAdmin := storage.MaxAdminID(values) != 0
	maxStatus, maxLabel := "off", "Выключен"
	switch {
	case !maxOn:
		maxStatus, maxLabel = "off", "Выключен"
	case !maxToken:
		maxStatus, maxLabel = "warn", "Не настроен"
	default:
		maxStatus, maxLabel = "ok", "Активирован"
	}

	printerName := strings.TrimSpace(h.cfg.Printer.Name)
	if printerName == "" {
		printerName = "принтер по умолчанию"
	}
	printer := gin.H{
		"known":   true,
		"dry_run": h.cfg.Printer.DryRun,
		"name":    printerName,
		"status":  "ok",
		"label":   "Реальная печать",
		"source":  "configs/config.yaml",
	}
	if h.cfg.Printer.DryRun {
		printer["status"] = "warn"
		printer["label"] = "Режим тестирования"
	}

	scanner := gin.H{
		"known":  true,
		"stub":   false,
		"status": "ok",
		"label":  "Сканер МФУ",
	}
	copyDev := gin.H{
		"known":  true,
		"stub":   false,
		"status": "ok",
		"label":  "Скан и печать",
	}
	if h.cfg.Printer.DryRun {
		scanner["status"] = "warn"
		scanner["label"] = "Режим тестирования"
		copyDev["status"] = "warn"
		copyDev["label"] = "Режим тестирования"
	}

	_, loErr := libreoffice.Find(h.cfg.Paths.LibreOffice)
	sumatraPath := printjob.FindSumatra(h.cfg.Printer.Sumatra)
	imapHost, imapPort := "", 0
	smtpHost, smtpPort := "", 0
	if strings.TrimSpace(values[storage.SettingEmailAddress]) != "" {
		imapHost, imapPort = mailcfg.IMAPHost(values[storage.SettingEmailAddress])
		smtpHost, smtpPort = mailcfg.SMTPHost(values[storage.SettingEmailAddress])
	}

	c.JSON(http.StatusOK, gin.H{
		"backend": gin.H{"known": true, "ok": true},
		"usb":     usbCard,
		"paper": gin.H{
			"known":     true,
			"remaining": paper,
			"capacity":  storage.PaperRefillSheets,
			"threshold": threshold,
			"low":       threshold > 0 && paper < threshold,
		},
		"email": gin.H{
			"enabled":    emailOn,
			"configured": emailReady,
			"status":     emailStatus,
			"label":      emailLabel,
			"imap_host":  imapHost,
			"imap_port":  imapPort,
			"smtp_host":  smtpHost,
			"smtp_port":  smtpPort,
		},
		"max": gin.H{
			"enabled":    maxOn,
			"token_set":  maxToken,
			"admin_set":  maxAdmin,
			"configured": storage.MaxKioskReady(values),
			"status":     maxStatus,
			"label":      maxLabel,
		},
		"printer":           printer,
		"sumatra_found":     sumatraPath != "",
		"libreoffice_found": loErr == nil,
		"scanner":           scanner,
		"copy":              copyDev,
		"payment": gin.H{
			"known":     true,
			"stub":      true,
			"delay_sec": 5,
			"status":    "warn",
			"label":     "Тестовый режим",
		},
		"kiosk_name":     values[storage.SettingKioskName],
		"kiosk_id":       values[storage.SettingKioskID],
		"kiosk_location": values[storage.SettingKioskLocation],
		"listen_addr":    h.cfg.Server.Addr,
		"log_path":       h.cfg.Logging.Path,
		"uptime_sec":     int(time.Since(h.startedAt).Seconds()),
		"username":       username,
		"today":          today,
	})
}

func (h *Handler) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := sessionTokenFromRequest(c)
		username, ok := h.sessions.Get(token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "не авторизован"})
			return
		}
		// Refresh cookie so browser TTL matches sliding server session.
		setSessionCookie(c, token)
		c.Set("admin_username", username)
		c.Next()
	}
}

func validateSetting(key, value string) error {
	value = strings.TrimSpace(value)

	switch key {
	case storage.SettingPriceBW, storage.SettingPriceColor, storage.SettingPriceCopy, storage.SettingPriceScan:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return errInvalid(key, "ожидается число")
		}
	case storage.SettingPaperRemaining,
		storage.SettingPaperAlertThreshold,
		storage.SettingEmailPollIntervalSec,
		storage.SettingEmailMaxFileSizeMB,
		storage.SettingSessionTimeoutSec:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return errInvalid(key, "ожидается целое число ≥ 0")
		}
	case storage.SettingTelegramCartridgeAlerts, storage.SettingPaymentEnabled,
		storage.SettingPaymentQREnabled,
		storage.SettingMaxEnabled, storage.SettingMaxInkAlerts,
		storage.SettingServicePrintEnabled, storage.SettingServiceCopyEnabled,
		storage.SettingServiceScanEnabled, storage.SettingSourceUSBEnabled,
		storage.SettingSourceEmailEnabled:
		if value != "true" && value != "false" {
			return errInvalid(key, "ожидается true или false")
		}
	case storage.SettingTelegramHeartbeatInterval:
		if value == "" {
			return errInvalid(key, "значение не может быть пустым")
		}
	case storage.SettingMaxAdminID:
		if value == "" {
			return nil
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return errInvalid(key, "ожидается числовой user_id")
		}
	case storage.SettingSupportText:
		if value == "" {
			return errInvalid(key, "текст поддержки не может быть пустым")
		}
	}

	return nil
}

type settingError struct {
	msg string
}

func (e settingError) Error() string { return e.msg }

func errInvalid(key, reason string) error {
	return settingError{msg: key + ": " + reason}
}
