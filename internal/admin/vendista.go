package admin

import (
	"context"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"print-kiosk/internal/storage"
	"print-kiosk/internal/vendista"
	"strconv"
	"time"
)

func (h *Handler) paymentStatus(ctx context.Context, v map[string]string) gin.H {
	test := storage.SettingEnabled(v, storage.SettingTestPaymentMode, true)
	result := gin.H{"status": "warn", "label": "Vendista · не настроена", "stub": test}
	client, e := vendista.FromSettings(v)
	if e == nil {
		checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		for k, value := range client.Check(checkCtx) {
			result[k] = value
		}
	}
	if test {
		result["label"] = "Тестовый режим · " + result["label"].(string)
		result["status"] = "warn"
	}
	if p, e := h.settings.PendingPayment(); e == nil {
		result["status"] = "warn"
		result["label"] = "Требуется сверка оплаты · " + p.OrderID
		result["pending_order"] = p.OrderID
	}
	if refunds, e := h.settings.RecentRefunds(); e == nil {
		result["refunds"] = refunds
		count := 0
		for _, refund := range refunds {
			if refund.State != "confirmed" {
				count++
			}
		}
		if count > 0 {
			result["status"] = "warn"
			result["label"] = result["label"].(string) + " · Возвратов ожидают проверки: " + strconv.Itoa(count)
		}
	}
	return result
}
func (h *Handler) TestVendista(c *gin.Context) {
	values, e := h.settings.GetAll()
	if e != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}
	result := h.paymentStatus(c.Request.Context(), values)
	if client, err := vendista.FromSettings(values); err == nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
		defer cancel()
		for k, value := range client.Diagnose(ctx) {
			result[k] = value
		}
	} else {
		result["label"] = err.Error()
	}
	c.JSON(http.StatusOK, result)
}
func (h *Handler) ReconcileVendista(c *gin.Context) {
	values, e := h.settings.GetAll()
	if e != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}
	client, e := vendista.FromSettings(values)
	if e == nil {
		e = client.ReconcilePending(c.Request.Context(), h.settings)
		if e == nil {
			e = client.ProcessRefunds(c.Request.Context(), h.settings)
		}
	}
	if e != nil {
		c.JSON(http.StatusConflict, gin.H{"error": e.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Сверка завершена. Незавершённых платежей нет"})
}

func (h *Handler) ResolveVendista(c *gin.Context) {
	var in struct {
		Confirmed bool `json:"confirmed_no_charge"`
	}
	if c.ShouldBindJSON(&in) != nil || !in.Confirmed {
		c.JSON(400, gin.H{"error": "Подтвердите, что проверили отсутствие списания в кабинете эквайринга"})
		return
	}
	values, e := h.settings.GetAll()
	if e != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}
	client, e := vendista.FromSettings(values)
	if e == nil {
		e = client.ResolveNoCharge(c.Request.Context(), h.settings)
	}
	if e != nil {
		c.JSON(409, gin.H{"error": e.Error()})
		return
	}
	user, _ := c.Get("admin_username")
	slog.Warn("vendista unresolved payment marked unpaid by administrator", "username", user)
	c.JSON(200, gin.H{"message": "Оплата отмечена как несостоявшаяся. Можно создать новый заказ"})
}
