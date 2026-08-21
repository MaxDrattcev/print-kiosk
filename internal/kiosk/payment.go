package kiosk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"print-kiosk/internal/storage"
)

func (h *Handler) processPayment(ctx context.Context, method string, amount float64, orderID string) error {
	if strings.EqualFold(strings.TrimSpace(method), "qr") {
		return errPaymentQR
	}
	if h.testPaymentMode() {
		time.Sleep(5 * time.Second)
		return nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(h.cfg.Payment.DriverURL), "/") + "/pay"
	payload, _ := json.Marshal(map[string]any{"amount": amount, "currency": "RUB", "order_id": orderID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("запрос оплаты: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("платёжный терминал: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("платёжный терминал вернул HTTP %d", res.StatusCode)
	}
	return nil
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
