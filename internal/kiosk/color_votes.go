package kiosk

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *Handler) VoteColorPrint(c *gin.Context) {
	var in struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "Некорректный голос"})
		return
	}
	if _, err := uuid.Parse(in.SessionID); err != nil || (in.Source != "print" && in.Source != "copy") {
		c.JSON(400, gin.H{"error": "Некорректный голос"})
		return
	}
	if h.stats == nil {
		c.JSON(503, gin.H{"error": "Опрос временно недоступен"})
		return
	}
	if err := h.stats.VoteForColorPrint(in.SessionID, in.Source); err != nil {
		c.JSON(500, gin.H{"error": "Не удалось сохранить голос. Попробуйте ещё раз."})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
