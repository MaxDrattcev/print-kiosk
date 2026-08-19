package kiosk

import (
	"fmt"
	"strings"
	"time"
)

// simulatePayment is the shared terminal/QR stub used by print, scan, and copy.
// Replace with a real driver (config.Payment.DriverURL) later.
func simulatePayment(method string) error {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "qr":
		return errPaymentQR
	case "terminal", "":
		time.Sleep(5 * time.Second)
		return nil
	default:
		return fmt.Errorf("неизвестный способ оплаты")
	}
}

var errPaymentQR = errString("Оплата через QR пока не подключена")
