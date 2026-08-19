package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
	"print-kiosk/internal/storage"
)

type Handler struct {
	cfg      *config.Config
	sessions *SessionStore
	settings *storage.SettingsRepo
}

func NewHandler(cfg *config.Config, sessions *SessionStore, settings *storage.SettingsRepo) *Handler {
	return &Handler{
		cfg:      cfg,
		sessions: sessions,
		settings: settings,
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить настройки"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось сохранить настройки"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
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
	case storage.SettingTelegramCartridgeAlerts, storage.SettingPaymentEnabled:
		if value != "true" && value != "false" {
			return errInvalid(key, "ожидается true или false")
		}
	case storage.SettingTelegramHeartbeatInterval:
		if value == "" {
			return errInvalid(key, "значение не может быть пустым")
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
