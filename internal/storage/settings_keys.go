package storage

// Setting keys stored in the settings table.
const (
	SettingPriceBW                   = "price_bw"
	SettingPriceColor                = "price_color"
	SettingPriceCopy                 = "price_copy"
	SettingPriceScan                 = "price_scan"
	SettingPaperRemaining            = "paper_remaining"
	SettingPaperAlertThreshold       = "paper_alert_threshold"
	SettingTelegramHeartbeatInterval = "telegram_heartbeat_interval" // legacy, unused
	SettingTelegramCartridgeAlerts   = "telegram_cartridge_alerts"   // legacy
	SettingMaxEnabled                = "max_enabled"
	SettingMaxBotToken               = "max_bot_token"
	SettingMaxAdminID                = "max_admin_id"
	SettingMaxInkAlerts              = "max_ink_alerts"
	SettingEmailAddress              = "email_address"
	SettingEmailLogin                = "email_login"
	SettingEmailPassword             = "email_password"
	SettingEmailPollIntervalSec      = "email_poll_interval_sec"
	SettingEmailMaxFileSizeMB        = "email_max_file_size_mb"
	SettingPaymentEnabled            = "payment_enabled"
	SettingPaymentQREnabled          = "payment_qr_enabled"
	SettingTestDeviceMode            = "test_device_mode"
	SettingTestPaymentMode           = "test_payment_mode"
	SettingSessionTimeoutSec         = "session_timeout_sec"
	SettingSupportText               = "support_text"
	SettingServicePrintEnabled       = "service_print_enabled"
	SettingServiceCopyEnabled        = "service_copy_enabled"
	SettingServiceScanEnabled        = "service_scan_enabled"
	SettingSourceUSBEnabled          = "source_usb_enabled"
	SettingSourceEmailEnabled        = "source_email_enabled"
	SettingKioskName                 = "kiosk_name"
	SettingKioskID                   = "kiosk_id"
	SettingKioskLocation             = "kiosk_location"
)
