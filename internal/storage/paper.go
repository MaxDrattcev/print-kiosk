package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrInsufficientPaper = errors.New("insufficient paper")

// PaperRemaining returns the configured sheet count (0 if missing/invalid).
func (r *SettingsRepo) PaperRemaining() (int, error) {
	value, err := r.Get(SettingPaperRemaining)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, nil
	}
	return n, nil
}

// ConsumePaper atomically subtracts sheets if enough paper remains.
// Returns the new remaining count.
func (r *SettingsRepo) ConsumePaper(sheets int) (int, error) {
	if sheets <= 0 {
		return r.PaperRemaining()
	}

	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin paper consume: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := readPaperTx(tx)
	if err != nil {
		return 0, err
	}
	if current < sheets {
		return current, ErrInsufficientPaper
	}
	next := current - sheets
	if err := writePaperTx(tx, next); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit paper consume: %w", err)
	}
	return next, nil
}

// RefundPaper adds sheets back (e.g. after a failed print that already consumed).
func (r *SettingsRepo) RefundPaper(sheets int) (int, error) {
	if sheets <= 0 {
		return r.PaperRemaining()
	}

	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin paper refund: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := readPaperTx(tx)
	if err != nil {
		return 0, err
	}
	next := current + sheets
	if err := writePaperTx(tx, next); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit paper refund: %w", err)
	}
	return next, nil
}

func readPaperTx(tx *sql.Tx) (int, error) {
	var value string
	err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, SettingPaperRemaining).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read paper: %w", err)
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, nil
	}
	return n, nil
}

func writePaperTx(tx *sql.Tx, n int) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := tx.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, SettingPaperRemaining, strconv.Itoa(n), now)
	if err != nil {
		return fmt.Errorf("write paper: %w", err)
	}
	return nil
}
