package kiosk

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"math"
	"print-kiosk/internal/paymaster"
	"print-kiosk/internal/storage"
	"strings"
	"sync"
	"time"
)

var sbpMu sync.Mutex

// All kiosk payment providers share a gate, so changing methods cannot charge twice.
var kioskPaymentMu sync.Mutex

func (h *Handler) paySBP(ctx context.Context, attempt string, amount float64, order string, v map[string]string) (err error) {
	if !storage.SettingEnabled(v, storage.SettingPaymentQREnabled, false) {
		return fmt.Errorf("оплата СБП отключена")
	}
	if _, e := uuid.Parse(attempt); e != nil {
		return fmt.Errorf("некорректный запрос оплаты")
	}
	if !sbpMu.TryLock() {
		return fmt.Errorf("уже выполняется оплата СБП")
	}
	defer sbpMu.Unlock()
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return fmt.Errorf("некорректная сумма")
	}
	test := storage.SettingEnabled(v, storage.SettingTestPaymentMode, true)
	var client *paymaster.Client
	if !test {
		client, err = paymaster.FromSettings(v)
		if err != nil {
			return err
		}
	}
	p, e := h.settings.SBPOrder(order)
	if e == nil {
		if p.Amount != int64(math.Round(amount*100)) || p.Test != test {
			return fmt.Errorf("параметры заказа изменились; проверьте предыдущую оплату")
		}
		if p.State == "paid" && !p.Orphan {
			return nil
		}
		return fmt.Errorf("по заказу уже есть платёж; дождитесь сверки или возврата")
	}
	if !storage.IsNoPayment(e) {
		return e
	}
	if _, e = h.settings.SBP(attempt); !storage.IsNoPayment(e) {
		return fmt.Errorf("этот запрос оплаты уже использован")
	}
	if previous, e := h.settings.Payment(order); e == nil && previous.State != "cancelled" && previous.State != "failed" {
		return fmt.Errorf("по заказу уже есть оплата картой")
	} else if e != nil && !storage.IsNoPayment(e) {
		return e
	}
	if pending, e := h.settings.PendingPayment(); e == nil && pending.State == "pending" {
		return fmt.Errorf("дождитесь сверки оплаты картой")
	} else if e != nil && !storage.IsNoPayment(e) {
		return e
	}
	p = storage.SBPPayment{Attempt: attempt, OrderID: order, Amount: int64(math.Round(amount * 100)), Test: test, Expires: time.Now().Add(2 * time.Minute).Unix(), State: "pending"}
	if client != nil {
		p.Merchant = client.Merchant
	}
	if e = h.settings.CreateSBP(p); e != nil {
		return fmt.Errorf("предыдущий платёж ещё проверяется")
	}
	defer func() {
		if err != nil {
			_ = h.settings.OrphanSBP(attempt)
		}
	}()
	if test {
		p.QRURL = "PRINTUS-TEST-SBP:" + attempt
		if e = h.settings.SBPDetails(attempt, "test-"+attempt, p.QRURL); e != nil {
			return e
		}
	} else {
		callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, e := client.Create(callCtx, p)
		cancel()
		if e != nil {
			state := "unknown"
			if paymaster.DefinitiveRejection(e) {
				state = "cancelled"
			}
			_ = h.settings.SBPState(attempt, state, e.Error())
			return e
		}
		if e = h.settings.SBPDetails(attempt, out.ID, out.Confirmation.URL); e != nil {
			return e
		}
		p.PaymentID = out.ID
		p.QRURL = out.Confirmation.URL
		if out.Status == "Settled" {
			return h.settings.SBPState(attempt, "paid", "")
		}
		if !paymaster.PaymentURL(p.QRURL) {
			_ = h.settings.CancelSBP(attempt)
			return fmt.Errorf("PayMaster не вернул ссылку СБП; платёж будет отменён")
		}
	}
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		p, e = h.settings.SBP(attempt)
		if e != nil {
			return e
		}
		if p.Test && p.State == "paid" {
			return nil
		}
		if p.Test && (p.Cancel || time.Now().Unix() >= p.Expires) {
			_ = h.settings.SBPState(attempt, "cancelled", "")
			return fmt.Errorf("тестовая оплата отменена")
		}
		if !p.Test {
			callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			out, e := client.Get(callCtx, p)
			if e == nil && (p.Cancel || time.Now().Unix() >= p.Expires) && out.Status != "Settled" && out.Status != "Cancelled" && out.Status != "Rejected" {
				_ = client.Cancel(callCtx, p)
				out, e = client.Get(callCtx, p)
			}
			cancel()
			if e == nil {
				switch out.Status {
				case "Settled":
					return h.settings.SBPState(attempt, "paid", "")
				case "Cancelled", "Rejected":
					_ = h.settings.SBPState(attempt, "cancelled", "")
					return fmt.Errorf("оплата СБП отменена или отклонена")
				}
			}
			if time.Now().Unix() > p.Expires+20 {
				_ = h.settings.SBPState(attempt, "unknown", "Результат уточняется")
				return fmt.Errorf("результат оплаты уточняется. Не оплачивайте повторно; при позднем списании будет оформлен возврат")
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("соединение прервано; результат оплаты будет проверен автоматически")
		case <-ticker.C:
		}
	}
}

func (h *Handler) SBPStatus(c *gin.Context) {
	p, e := h.settings.SBP(c.Param("attempt"))
	if e != nil {
		c.JSON(404, gin.H{"error": "Платёж ещё создаётся"})
		return
	}
	c.Header("Cache-Control", "no-store")
	qr := ""
	if p.QRURL != "" && p.State == "pending" {
		png, e := qrcode.Encode(p.QRURL, qrcode.Medium, 768)
		if e == nil {
			qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
	}
	c.JSON(200, gin.H{"qr": qr, "state": p.State, "test": p.Test, "expires": p.Expires, "amount": float64(p.Amount) / 100, "cancel_requested": p.Cancel})
}
func (h *Handler) SBPCancel(c *gin.Context) {
	if e := h.settings.CancelSBP(c.Param("attempt")); e != nil {
		c.JSON(500, gin.H{"error": "Не удалось запросить отмену"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (h *Handler) SBPTestConfirm(c *gin.Context) {
	attempt := c.Param("attempt")
	p, e := h.settings.SBP(attempt)
	if e != nil || !p.Test || p.State != "pending" || p.Cancel || time.Now().Unix() >= p.Expires {
		c.JSON(409, gin.H{"error": "Тестовый платёж недоступен"})
		return
	}
	if e = h.settings.SBPTestPaid(p.Attempt); e != nil {
		c.JSON(409, gin.H{"error": "Тестовый платёж недоступен"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// Reconcile interrupted requests and process durable refunds without depending on browser callbacks.
func (h *Handler) processSBPWork() {
	if !sbpMu.TryLock() {
		return
	}
	defer sbpMu.Unlock()
	rows, e := h.settings.SBPWork()
	if e != nil {
		return
	}
	v, e := h.settings.GetAll()
	if e != nil {
		return
	}
	client, _ := paymaster.FromSettings(v)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	for _, p := range rows {
		if ctx.Err() != nil {
			return
		}
		if p.Test {
			if p.State == "refund_pending" {
				_ = h.settings.ConfirmSBPRefund(p.Attempt)
			} else if p.State == "pending" || p.State == "unknown" {
				_ = h.settings.SBPState(p.Attempt, "cancelled", "")
			}
			continue
		}
		if client == nil || client.Merchant != p.Merchant {
			continue
		}
		if p.State == "pending" || p.State == "unknown" {
			// Recover lost responses through a read-only lookup, never another creation.
			if p.PaymentID == "" {
				out, found, e := client.Find(ctx, p)
				if e != nil || !found {
					continue
				}
				if e = h.settings.SBPDetails(p.Attempt, out.ID, out.Confirmation.URL); e != nil {
					continue
				}
				p.PaymentID = out.ID
			}
			out, e := client.Get(ctx, p)
			if e != nil {
				continue
			}
			if out.Status != "Settled" && out.Status != "Cancelled" && out.Status != "Rejected" {
				_ = client.Cancel(ctx, p)
				out, e = client.Get(ctx, p)
				if e != nil {
					continue
				}
			}
			switch out.Status {
			case "Settled":
				_ = h.settings.OrphanSBP(p.Attempt)
				if e = h.settings.SBPState(p.Attempt, "paid", ""); e != nil {
					continue
				}
				p.State = "paid"
				p.Orphan = true
			case "Cancelled", "Rejected":
				_ = h.settings.SBPState(p.Attempt, "cancelled", "")
				continue
			default:
				continue
			}
		}
		if p.State == "paid" && p.Orphan {
			if e = h.settings.QueueSBPRefund(p.OrderID); e != nil {
				continue
			}
			p.State = "refund_pending"
		}
		if p.State == "refund_pending" && p.RefundState != "rejected" {
			out, e := client.Refund(ctx, p)
			if e != nil {
				continue
			}
			state := "pending"
			if out.Status == "Rejected" {
				state = "rejected"
			}
			if e = h.settings.SBPRefund(p.Attempt, out.ID, state); e != nil {
				continue
			}
			if out.Status == "Success" {
				_ = h.settings.ConfirmSBPRefund(p.Attempt)
			}
		}
	}
}
func paymentMethod(method string) string {
	if strings.HasPrefix(method, "qr:") {
		return "qr"
	}
	return "terminal"
}
