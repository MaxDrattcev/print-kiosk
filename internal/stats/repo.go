package stats

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type Day struct {
	Day        string
	Revenue    float64
	PagesBW    int
	PagesColor int
	Scans      int
	Copies     int
	SheetsUsed int
}

type Repo struct {
	db *sql.DB
	mu sync.Mutex
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func today() string {
	return time.Now().Format("2006-01-02")
}

func (r *Repo) ensureDay(day string) error {
	_, err := r.db.Exec(`
		INSERT OR IGNORE INTO daily_stats (day, revenue, pages_bw, pages_color, scans, copies, sheets_used)
		VALUES (?, 0, 0, 0, 0, 0, 0)
	`, day)
	return err
}

func (r *Repo) AddPrint(revenue float64, pages int, color bool, sheets int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	day := today()
	if err := r.ensureDay(day); err != nil {
		return err
	}
	bw, col := 0, 0
	if color {
		col = pages
	} else {
		bw = pages
	}
	_, err := r.db.Exec(`
		UPDATE daily_stats SET
			revenue = revenue + ?,
			pages_bw = pages_bw + ?,
			pages_color = pages_color + ?,
			sheets_used = sheets_used + ?
		WHERE day = ?
	`, revenue, bw, col, sheets, day)
	return err
}

func (r *Repo) AddScan(revenue float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	day := today()
	if err := r.ensureDay(day); err != nil {
		return err
	}
	_, err := r.db.Exec(`
		UPDATE daily_stats SET revenue = revenue + ?, scans = scans + 1 WHERE day = ?
	`, revenue, day)
	return err
}

func (r *Repo) AddCopy(revenue float64, copies, sheets int, color bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	day := today()
	if err := r.ensureDay(day); err != nil {
		return err
	}
	bw, col := 0, 0
	if color {
		col = copies
	} else {
		bw = copies
	}
	_, err := r.db.Exec(`
		UPDATE daily_stats SET
			revenue = revenue + ?,
			pages_bw = pages_bw + ?,
			pages_color = pages_color + ?,
			copies = copies + ?,
			sheets_used = sheets_used + ?
		WHERE day = ?
	`, revenue, bw, col, copies, sheets, day)
	return err
}

func (r *Repo) GetDay(day string) (Day, error) {
	if day == "" {
		day = today()
	}
	var d Day
	err := r.db.QueryRow(`
		SELECT day, revenue, pages_bw, pages_color, scans, copies, sheets_used
		FROM daily_stats WHERE day = ?
	`, day).Scan(&d.Day, &d.Revenue, &d.PagesBW, &d.PagesColor, &d.Scans, &d.Copies, &d.SheetsUsed)
	if err == sql.ErrNoRows {
		return Day{Day: day}, nil
	}
	if err != nil {
		return Day{}, fmt.Errorf("get daily stats: %w", err)
	}
	return d, nil
}

func (r *Repo) GetKV(key string) (string, bool) {
	var v string
	err := r.db.QueryRow(`SELECT value FROM kv_state WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

func (r *Repo) SetKV(key, value string) error {
	_, err := r.db.Exec(`
		INSERT INTO kv_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}
