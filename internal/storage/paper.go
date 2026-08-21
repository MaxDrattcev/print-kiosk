package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// PaperRefillSheets is the amount written by “Бумага загружена”.
const PaperRefillSheets = 500

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
	return r.withPaperLock(func(current int) (int, error) {
		if current < sheets {
			return current, ErrInsufficientPaper
		}
		return current - sheets, nil
	})
}

// RefundPaper adds sheets back (e.g. after a failed print that already consumed).
func (r *SettingsRepo) RefundPaper(sheets int) (int, error) {
	if sheets <= 0 {
		return r.PaperRemaining()
	}
	return r.withPaperLock(func(current int) (int, error) {
		return current + sheets, nil
	})
}

// SetPaperRemaining replaces the stored paper count. It is used when the
// printer itself reports an empty tray, which is more authoritative than the
// calculated software balance.
func (r *SettingsRepo) SetPaperRemaining(sheets int) (int, error) {
	if sheets < 0 {
		sheets = 0
	}
	return r.withPaperLock(func(int) (int, error) {
		return sheets, nil
	})
}

func (r *SettingsRepo) withPaperLock(nextFn func(current int) (int, error)) (int, error) {
	ctx := context.Background()
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("paper conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, fmt.Errorf("begin paper tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	current, err := readPaperConn(ctx, conn)
	if err != nil {
		return 0, err
	}
	next, err := nextFn(current)
	if err != nil {
		return current, err
	}
	if err := writePaperConn(ctx, conn, next); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, fmt.Errorf("commit paper tx: %w", err)
	}
	committed = true
	return next, nil
}

func readPaperConn(ctx context.Context, conn *sql.Conn) (int, error) {
	var value string
	err := conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, SettingPaperRemaining).Scan(&value)
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

func writePaperConn(ctx context.Context, conn *sql.Conn, n int) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := conn.ExecContext(ctx, `
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
