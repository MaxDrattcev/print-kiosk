package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type Day struct {
	Day           string
	Revenue       float64
	PagesBW       int
	PagesColor    int
	Scans         int
	Copies        int
	SheetsUsed    int
	UptimeSeconds int64
}

type Repo struct {
	db         *sql.DB
	mu         sync.Mutex
	uptimeLast time.Time
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }
func today() string            { return time.Now().Format("2006-01-02") }

func ensureDay(tx *sql.Tx, day string) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO daily_stats
		(day, revenue, pages_bw, pages_color, scans, copies, sheets_used, uptime_seconds)
		VALUES (?, 0, 0, 0, 0, 0, 0, 0)`, day)
	return err
}

func ensureTotal(tx *sql.Tx) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO total_stats (id) VALUES (1)`)
	return err
}

func (r *Repo) add(revenue float64, bw, color, scans, copies, sheets int, uptime int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addLocked(today(), revenue, bw, color, scans, copies, sheets, uptime)
}

func (r *Repo) addLocked(day string, revenue float64, bw, color, scans, copies, sheets int, uptime int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureDay(tx, day); err != nil {
		return err
	}
	if err := ensureTotal(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE daily_stats SET
		revenue=revenue+?, pages_bw=pages_bw+?, pages_color=pages_color+?,
		scans=scans+?, copies=copies+?, sheets_used=sheets_used+?, uptime_seconds=uptime_seconds+?
		WHERE day=?`, revenue, bw, color, scans, copies, sheets, uptime, day); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE total_stats SET
		revenue=revenue+?, pages_bw=pages_bw+?, pages_color=pages_color+?,
		scans=scans+?, copies=copies+?, sheets_used=sheets_used+?, uptime_seconds=uptime_seconds+?
		WHERE id=1`, revenue, bw, color, scans, copies, sheets, uptime); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repo) AddPrint(revenue float64, pages int, color bool, sheets int) error {
	bw, col := pages, 0
	if color {
		bw, col = 0, pages
	}
	return r.add(revenue, bw, col, 0, 0, sheets, 0)
}

func (r *Repo) AddScan(revenue float64) error { return r.add(revenue, 0, 0, 1, 0, 0, 0) }

// AddRevenue records a payment that is not tied to a newly completed device
// operation, for example the final surcharge for extra scan pages.
func (r *Repo) AddRevenue(revenue float64) error { return r.add(revenue, 0, 0, 0, 0, 0, 0) }

func (r *Repo) AddCopy(revenue float64, copies, sheets int, color bool) error {
	bw, col := copies, 0
	if color {
		bw, col = 0, copies
	}
	return r.add(revenue, bw, col, 0, copies, sheets, 0)
}

func (r *Repo) GetDay(day string) (Day, error) {
	if day == "" {
		day = today()
	}
	var d Day
	err := r.db.QueryRow(`SELECT day, revenue, pages_bw, pages_color, scans, copies, sheets_used, uptime_seconds
		FROM daily_stats WHERE day=?`, day).Scan(
		&d.Day, &d.Revenue, &d.PagesBW, &d.PagesColor, &d.Scans, &d.Copies, &d.SheetsUsed, &d.UptimeSeconds,
	)
	if err == sql.ErrNoRows {
		return Day{Day: day}, nil
	}
	if err != nil {
		return Day{}, fmt.Errorf("get daily stats: %w", err)
	}
	return d, nil
}

func (r *Repo) GetTotal() (Day, error) {
	d := Day{Day: "total"}
	err := r.db.QueryRow(`SELECT revenue, pages_bw, pages_color, scans, copies, sheets_used, uptime_seconds
		FROM total_stats WHERE id=1`).Scan(
		&d.Revenue, &d.PagesBW, &d.PagesColor, &d.Scans, &d.Copies, &d.SheetsUsed, &d.UptimeSeconds,
	)
	if err == sql.ErrNoRows {
		return d, nil
	}
	if err != nil {
		return Day{}, fmt.Errorf("get total stats: %w", err)
	}
	return d, nil
}

func (r *Repo) Reset(scope string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uptimeLast = time.Now()
	switch scope {
	case "today":
		_, err := r.db.Exec(`INSERT INTO daily_stats
			(day, revenue, pages_bw, pages_color, scans, copies, sheets_used, uptime_seconds)
			VALUES (?, 0, 0, 0, 0, 0, 0, 0)
			ON CONFLICT(day) DO UPDATE SET revenue=0, pages_bw=0, pages_color=0,
				scans=0, copies=0, sheets_used=0, uptime_seconds=0`, today())
		return err
	case "total":
		_, err := r.db.Exec(`INSERT INTO total_stats (id) VALUES (1)
			ON CONFLICT(id) DO UPDATE SET revenue=0, pages_bw=0, pages_color=0,
				scans=0, copies=0, sheets_used=0, uptime_seconds=0`)
		return err
	default:
		return fmt.Errorf("unknown statistics scope %q", scope)
	}
}

// TrackUptime persists runtime across restarts and naturally switches the
// daily counter when the local calendar date changes at midnight.
func (r *Repo) TrackUptime(ctx context.Context) {
	r.mu.Lock()
	r.uptimeLast = time.Now()
	r.mu.Unlock()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	flush := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		now := time.Now()
		cursor := r.uptimeLast
		for cursor.Before(now) {
			nextMidnight := time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, cursor.Location())
			end := now
			if nextMidnight.Before(end) {
				end = nextMidnight
			}
			seconds := int64(end.Sub(cursor) / time.Second)
			if seconds > 0 {
				_ = r.addLocked(cursor.Format("2006-01-02"), 0, 0, 0, 0, 0, 0, seconds)
				cursor = cursor.Add(time.Duration(seconds) * time.Second)
			} else {
				break
			}
		}
		r.uptimeLast = cursor
	}
	for {
		select {
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			flush()
			return
		}
	}
}

func (r *Repo) GetKV(key string) (string, bool) {
	var v string
	err := r.db.QueryRow(`SELECT value FROM kv_state WHERE key=?`, key).Scan(&v)
	return v, err == nil
}

func (r *Repo) SetKV(key, value string) error {
	_, err := r.db.Exec(`INSERT INTO kv_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
