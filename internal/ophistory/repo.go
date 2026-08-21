package ophistory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const RetentionDays = 30

type Entry struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Operation string    `json:"operation"`
	JobID     string    `json:"job_id,omitempty"`
	Pages     int       `json:"pages"`
	Sheets    int       `json:"sheets"`
	Amount    float64   `json:"amount"`
	Success   bool      `json:"success"`
	ErrorText string    `json:"error_text,omitempty"`
}

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Add(ctx context.Context, e Entry) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO operation_history
		(created_at, operation, job_id, pages, sheets, amount, success, error_text)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.CreatedAt.Unix(), e.Operation, e.JobID,
		e.Pages, e.Sheets, e.Amount, e.Success, e.ErrorText)
	return err
}

func (r *Repo) ListDays(ctx context.Context, days int, now time.Time) ([]Entry, error) {
	if days < 1 || days > RetentionDays {
		return nil, fmt.Errorf("число дней должно быть от 1 до %d", RetentionDays)
	}
	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).AddDate(0, 0, -(days - 1))
	rows, err := r.db.QueryContext(ctx, `SELECT id, created_at, operation, job_id, pages, sheets, amount, success, error_text
		FROM operation_history WHERE created_at >= ? ORDER BY created_at DESC, id DESC`, start.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var unix int64
		if err := rows.Scan(&e.ID, &unix, &e.Operation, &e.JobID, &e.Pages, &e.Sheets, &e.Amount, &e.Success, &e.ErrorText); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(unix, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.AddDate(0, 0, -RetentionDays).Unix()
	res, err := r.db.ExecContext(ctx, `DELETE FROM operation_history WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *Repo) RunRetention(ctx context.Context) {
	_, _ = r.DeleteExpired(ctx, time.Now())
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
			_, _ = r.DeleteExpired(ctx, time.Now())
		}
	}
}
