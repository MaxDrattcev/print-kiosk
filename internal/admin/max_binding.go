package admin

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
	"net/url"
	"print-kiosk/internal/storage"
	"strings"
)

func (h *Handler) StartMAXBinding(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.max == nil {
		c.JSON(503, gin.H{"error": "MAX не подключён"})
		return
	}
	link, _ := h.settings.Get(storage.SettingMaxBotLink)
	link = strings.TrimSpace(link)
	if link == "" {
		username, enabled, err := h.max.Info(c.Request.Context())
		if err != nil || !enabled || username == "" {
			c.JSON(400, gin.H{"error": "Не удалось определить бота. Проверьте сохранённый токен и включение MAX."})
			return
		}
		link = "https://max.ru/" + username
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "max.ru" {
		c.JSON(400, gin.H{"error": "Проверьте ссылку на бота MAX"})
		return
	}
	b, err := h.max.StartAdminBinding()
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	query := parsed.Query()
	query.Set("start", "notify_"+b.Token)
	parsed.RawQuery = query.Encode()
	png, err := qrcode.Encode(parsed.String(), qrcode.Medium, 768)
	if err != nil {
		h.max.CancelAdminBinding(b.ID)
		c.JSON(500, gin.H{"error": "Не удалось создать QR-код"})
		return
	}
	c.JSON(200, gin.H{"binding": b, "qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
}
func (h *Handler) GetMAXBinding(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.max != nil {
		if b, ok := h.max.GetAdminBinding(c.Param("id")); ok {
			c.JSON(200, gin.H{"binding": b})
			return
		}
	}
	c.JSON(404, gin.H{"error": "Привязка завершена или отменена"})
}
func (h *Handler) CancelMAXBinding(c *gin.Context) {
	if h.max != nil {
		h.max.CancelAdminBinding(c.Param("id"))
		if b, ok := h.max.GetAdminBinding(c.Param("id")); ok {
			c.JSON(200, gin.H{"ok": true, "binding": b})
			return
		}
	}
	c.JSON(200, gin.H{"ok": true})
}
