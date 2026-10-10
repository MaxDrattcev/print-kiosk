package admin

import (
	"context"
	"github.com/gin-gonic/gin"
	"log/slog"
	"print-kiosk/internal/paymaster"
	"print-kiosk/internal/storage"
	"time"
)

func (h *Handler) TestSBP(c *gin.Context) {
	v, e := h.settings.GetAll()
	if e != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}
	test := storage.SettingEnabled(v, storage.SettingTestPaymentMode, true)
	client, e := paymaster.FromSettings(v)
	if e != nil {
		if test {
			c.JSON(200, gin.H{"message": "Тестовый режим СБП: деньги не списываются. Для реальных платежей укажите ID магазина и API-токен PayMaster."})
		} else {
			c.JSON(400, gin.H{"error": e.Error()})
		}
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if e = client.Check(ctx); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	message := "Доступ к API PayMaster подтверждён. Деньги не списывались. Подключение СБП и возвратов проверьте в кабинете PayMaster."
	if test {
		message = "Тестовый режим оплаты. " + message
	}
	if blocked, e := h.settings.SBPBlocked(); e == nil && blocked {
		message += " Есть незавершённый платёж СБП; приложение автоматически уточняет его результат."
	}
	c.JSON(200, gin.H{"message": message})
}

func (h *Handler) ResolveSBP(c *gin.Context) {
	var in struct {
		Confirmed bool `json:"confirmed_no_charge"`
	}
	if c.ShouldBindJSON(&in) != nil || !in.Confirmed {
		c.JSON(400, gin.H{"error": "Проверьте отсутствие списания в кабинете PayMaster и отметьте подтверждение"})
		return
	}
	rows, e := h.settings.SBPWork()
	if e != nil {
		c.JSON(500, gin.H{"error": "Не удалось проверить платежи"})
		return
	}
	v, e := h.settings.GetAll()
	if e != nil {
		c.JSON(500, gin.H{"error": "Не удалось загрузить настройки"})
		return
	}
	client, e := paymaster.FromSettings(v)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	for _, p := range rows {
		if p.State != "pending" && p.State != "unknown" {
			continue
		}
		if p.Expires+180 > time.Now().Unix() {
			c.JSON(409, gin.H{"error": "Дождитесь 3 минут после истечения QR-кода и повторите сверку"})
			return
		}
		if p.Merchant != client.Merchant {
			c.JSON(409, gin.H{"error": "Восстановите реквизиты магазина исходного платежа"})
			return
		}
		if p.PaymentID != "" {
			out, e := client.Get(ctx, p)
			if e != nil {
				c.JSON(409, gin.H{"error": e.Error()})
				return
			}
			if out.Status != "Cancelled" && out.Status != "Rejected" {
				c.JSON(409, gin.H{"error": "Платёж не подтверждён как отменённый; дождитесь автоматической сверки"})
				return
			}
		} else {
			_, found, e := client.Find(ctx, p)
			if e != nil {
				c.JSON(409, gin.H{"error": e.Error()})
				return
			}
			if found {
				c.JSON(409, gin.H{"error": "Платёж найден в PayMaster; дождитесь автоматической сверки"})
				return
			}
		}
		if e = h.settings.ResolveSBPNoCharge(p); e != nil {
			c.JSON(409, gin.H{"error": e.Error()})
			return
		}
		slog.Warn("SBP no-charge resolution by administrator", "order_id", p.OrderID, "username", c.GetString("admin_username"))
		c.JSON(200, gin.H{"message": "Отсутствие списания СБП подтверждено. Можно повторить оплату."})
		return
	}
	c.JSON(200, gin.H{"message": "Неопределённых платежей СБП нет"})
}
