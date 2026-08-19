package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"

	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/mailout"
	"print-kiosk/internal/storage"
)

const secretMask = "********"

type emailTestRequest struct {
	Address  string `json:"email_address"`
	Login    string `json:"email_login"`
	Password string `json:"email_password"`
}

func (h *Handler) TestEmail(c *gin.Context) {
	var req emailTestRequest
	_ = c.ShouldBindJSON(&req)

	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}

	addr := strings.TrimSpace(req.Address)
	if addr == "" {
		addr = strings.TrimSpace(values[storage.SettingEmailAddress])
	}
	login := strings.TrimSpace(req.Login)
	if login == "" {
		login = strings.TrimSpace(values[storage.SettingEmailLogin])
	}
	pass := req.Password
	if pass == "" || pass == secretMask {
		pass = values[storage.SettingEmailPassword]
	}
	if addr == "" || pass == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Укажите адрес и пароль приложения"})
		return
	}
	if login == "" {
		login = addr
	}

	imapHost, imapPort := mailinbox.ResolveHost(addr)
	if err := mailinbox.Test(mailinbox.Credentials{
		Address: addr, Login: login, Password: pass, Host: imapHost, Port: imapPort,
	}); err != nil {
		slog.Warn("admin imap test failed", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": classifyMailError("IMAP", err)})
		return
	}

	smtpHost, smtpPort := mailout.ResolveHost(addr)
	if err := mailout.Test(mailout.Credentials{
		From: addr, Login: login, Password: pass, Host: smtpHost, Port: smtpPort,
	}); err != nil {
		slog.Warn("admin smtp test failed", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": classifyMailError("SMTP", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Подключение успешно"})
}

type maxTestRequest struct {
	Token       string `json:"max_bot_token"`
	AdminID     string `json:"max_admin_id"`
	SendMessage bool   `json:"send_message"`
}

func (h *Handler) TestMAX(c *gin.Context) {
	var req maxTestRequest
	_ = c.ShouldBindJSON(&req)

	values, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" || token == secretMask {
		token = strings.TrimSpace(values[storage.SettingMaxBotToken])
	}
	adminRaw := strings.TrimSpace(req.AdminID)
	if adminRaw == "" {
		adminRaw = strings.TrimSpace(values[storage.SettingMaxAdminID])
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MAX не настроен: нет токена"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	api, err := maxbot.NewApi(token)
	if err != nil {
		slog.Warn("admin max test api", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Не удалось проверить MAX"})
		return
	}
	info, err := api.Bots.GetMyInfo(ctx)
	if err != nil {
		slog.Warn("admin max test getme", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Не удалось проверить MAX"})
		return
	}
	username := strings.TrimPrefix(info.Username, "@")

	if !req.SendMessage {
		c.JSON(http.StatusOK, gin.H{
			"ok":           true,
			"message":      "Подключение успешно",
			"bot_username": username,
		})
		return
	}

	adminID := storage.MaxAdminID(map[string]string{storage.SettingMaxAdminID: adminRaw})
	if adminID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Укажите ID админа, чтобы отправить тестовое сообщение"})
		return
	}
	msg := maxbot.NewMessage().SetUser(adminID).SetText("Тестовое сообщение PrintStart. Бот отвечает.")
	if _, err := api.Messages.Send(ctx, msg); err != nil {
		slog.Warn("admin max test send", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "API доступен, но не удалось отправить сообщение админу"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"message":      "Тестовое сообщение отправлено",
		"bot_username": username,
	})
}

func classifyMailError(kind string, err error) string {
	if err == nil {
		return "Не удалось проверить " + kind
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "вход"),
		strings.Contains(msg, "login"),
		strings.Contains(msg, "auth"),
		strings.Contains(msg, "authentication"),
		strings.Contains(msg, "invalid credentials"),
		strings.Contains(msg, "authenticationfailed"):
		return "Не удалось авторизоваться в " + kind
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "Превышено время ожидания " + kind
	default:
		return "Не удалось подключиться к " + kind
	}
}
