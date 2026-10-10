package kiosk

import (
	"context"
	"github.com/gin-gonic/gin"
	"log/slog"
	"print-kiosk/internal/vendista"
	"time"
)

func (h *Handler) processScanRefunds() {
	h.processSBPWork()
	values, e := h.settings.GetAll()
	if e != nil {
		return
	}
	client, e := vendista.FromSettings(values)
	if e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if e := client.ProcessRefunds(ctx, h.settings); e != nil {
		slog.Warn("scan refund processing", "error", e)
	}
}
func (h *Handler) scanRefundWorker() {
	h.processScanRefunds()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		h.processScanRefunds()
	}
}
func (h *Handler) CancelScanWithRefund(c *gin.Context) {
	id := c.Param("id")
	if e := h.scans.BeginCancelDelivery(id); e != nil {
		c.JSON(409, gin.H{"error": e.Error()})
		return
	}
	success := false
	defer func() { h.scans.EndCancelDelivery(id, success) }()
	if e := h.settings.QueueJobRefunds(id); e != nil {
		c.JSON(500, gin.H{"error": "Не удалось сохранить запрос возврата. Попробуйте ещё раз"})
		return
	}
	success = true
	go h.processScanRefunds()
	c.JSON(200, gin.H{"ok": true, "message": "Операция отменена. Запрос возврата сохранён. Срок зачисления зависит от банка; при тестовой оплате деньги не списывались"})
}
