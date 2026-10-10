package kiosk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"print-kiosk/internal/storage"
	"print-kiosk/internal/vendista"
)

func (h *Handler) processPayment(ctx context.Context, method string, amount float64, orderID string) error {
	if !kioskPaymentMu.TryLock() {
		return fmt.Errorf("уже выполняется оплата")
	}
	defer kioskPaymentMu.Unlock()
	values, err := h.settings.GetAll()
	if err != nil {
		return fmt.Errorf("настройки оплаты: %w", err)
	}
	if strings.HasPrefix(method, "qr:") {
		return h.paySBP(ctx, strings.TrimPrefix(method, "qr:"), amount, orderID, values)
	}
	if method != "terminal" {
		return fmt.Errorf("неизвестный способ оплаты")
	}
	if blocked, e := h.settings.SBPBlocked(); e != nil || blocked {
		return fmt.Errorf("результат предыдущей оплаты СБП уточняется")
	}
	if p, e := h.settings.SBPOrder(orderID); e == nil && p.State != "cancelled" {
		return fmt.Errorf("по этому заказу уже есть платёж СБП")
	} else if e != nil && !storage.IsNoPayment(e) {
		return e
	}
	if storage.SettingEnabled(values, storage.SettingTestPaymentMode, true) {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	client, err := vendista.FromSettings(values)
	if err != nil {
		return err
	}
	return client.Pay(ctx, h.settings, orderID, amount)
}

func (h *Handler) testPaymentMode() bool {
	values, err := h.settings.GetAll()
	if err != nil {
		return true
	}
	return storage.SettingEnabled(values, storage.SettingTestPaymentMode, true)
}

func (h *Handler) testDeviceMode() bool {
	values, err := h.settings.GetAll()
	if err != nil {
		return h.cfg.Printer.DryRun
	}
	return h.cfg.Printer.DryRun || storage.SettingEnabled(values, storage.SettingTestDeviceMode, true)
}

var errPaymentQR = errString("Оплата через QR пока не подключена")
