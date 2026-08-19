package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type SettingsRepo struct {
	db *sql.DB
}

func NewSettingsRepo(db *sql.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

func (r *SettingsRepo) GetAll() (map[string]string, error) {
	rows, err := r.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return out, nil
}

func (r *SettingsRepo) Get(key string) (string, error) {
	var value string
	err := r.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("setting %q not found", key)
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

func (r *SettingsRepo) SetMany(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin settings update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare settings update: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for key, value := range values {
		if !IsKnownSetting(key) {
			return fmt.Errorf("unknown setting key %q", key)
		}
		if _, err := stmt.Exec(key, value, now); err != nil {
			return fmt.Errorf("upsert setting %q: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings update: %w", err)
	}
	return nil
}

func IsKnownSetting(key string) bool {
	switch key {
	case SettingPriceBW,
		SettingPriceColor,
		SettingPriceCopy,
		SettingPriceScan,
		SettingPaperRemaining,
		SettingPaperAlertThreshold,
		SettingTelegramHeartbeatInterval,
		SettingTelegramCartridgeAlerts,
		SettingEmailAddress,
		SettingEmailLogin,
		SettingEmailPassword,
		SettingEmailPollIntervalSec,
		SettingEmailMaxFileSizeMB,
		SettingPaymentEnabled,
		SettingSessionTimeoutSec,
		SettingSupportText:
		return true
	default:
		return false
	}
}

// SensitiveSettings are masked in API responses.
func IsSensitiveSetting(key string) bool {
	return key == SettingEmailPassword
}
