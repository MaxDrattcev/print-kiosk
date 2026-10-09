package admin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"print-kiosk/internal/storage"
	"strings"
)

func emailCheckFingerprint(address, login, password string) string {
	address = strings.TrimSpace(address)
	login = strings.TrimSpace(login)
	if login == "" {
		login = address
	}
	data, _ := json.Marshal([]string{address, login, password})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (h *Handler) recordEmailCheck(address, login, password string, success bool) {
	if h.stats == nil {
		return
	}
	state := "failed:"
	if success {
		state = "ok:"
	}
	if err := h.stats.SetKV("email_connection_check", state+emailCheckFingerprint(address, login, password)); err != nil {
		slog.Warn("save email connection check", "error", err)
	}
}
func (h *Handler) emailChecked(values map[string]string) bool {
	if h.stats == nil || !storage.EmailReady(values) {
		return false
	}
	value, ok := h.stats.GetKV("email_connection_check")
	return ok && value == "ok:"+emailCheckFingerprint(values[storage.SettingEmailAddress], values[storage.SettingEmailLogin], values[storage.SettingEmailPassword])
}
