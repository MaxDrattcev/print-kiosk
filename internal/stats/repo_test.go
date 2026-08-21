package stats_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
)

func testRepo(t *testing.T) *stats.Repo {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return stats.NewRepo(db)
}

func TestDailyAndTotalStatisticsResetIndependently(t *testing.T) {
	r := testRepo(t)
	if err := r.AddPrint(30, 3, false, 2); err != nil {
		t.Fatal(err)
	}
	if err := r.AddCopy(20, 2, 2, true); err != nil {
		t.Fatal(err)
	}
	if err := r.AddScan(10); err != nil {
		t.Fatal(err)
	}

	today, err := r.GetDay("")
	if err != nil {
		t.Fatal(err)
	}
	total, err := r.GetTotal()
	if err != nil {
		t.Fatal(err)
	}
	if today.Revenue != 60 || today.PagesBW != 3 || today.PagesColor != 2 || today.Copies != 2 || today.Scans != 1 || today.SheetsUsed != 4 {
		t.Fatalf("unexpected today: %+v", today)
	}
	if total.Revenue != today.Revenue || total.PagesBW != today.PagesBW || total.PagesColor != today.PagesColor || total.Scans != today.Scans {
		t.Fatalf("total does not match increments: %+v / %+v", total, today)
	}

	if err := r.Reset("today"); err != nil {
		t.Fatal(err)
	}
	today, _ = r.GetDay("")
	total, _ = r.GetTotal()
	if today.Revenue != 0 || today.PagesBW != 0 || today.Scans != 0 {
		t.Fatalf("today was not reset: %+v", today)
	}
	if total.Revenue != 60 || total.PagesBW != 3 || total.Scans != 1 {
		t.Fatalf("reset today changed total: %+v", total)
	}

	if err := r.AddScan(5); err != nil {
		t.Fatal(err)
	}
	if err := r.Reset("total"); err != nil {
		t.Fatal(err)
	}
	today, _ = r.GetDay("")
	total, _ = r.GetTotal()
	if today.Revenue != 5 || today.Scans != 1 {
		t.Fatalf("reset total changed today: %+v", today)
	}
	if total.Revenue != 0 || total.Scans != 0 {
		t.Fatalf("total was not reset: %+v", total)
	}
}

func TestTrackUptimePersists(t *testing.T) {
	r := testRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.TrackUptime(ctx)
		close(done)
	}()
	time.Sleep(1100 * time.Millisecond)
	cancel()
	<-done
	today, err := r.GetDay("")
	if err != nil {
		t.Fatal(err)
	}
	total, err := r.GetTotal()
	if err != nil {
		t.Fatal(err)
	}
	if today.UptimeSeconds < 1 || total.UptimeSeconds < 1 {
		t.Fatalf("uptime was not persisted: today=%d total=%d", today.UptimeSeconds, total.UptimeSeconds)
	}
}

func TestTestModeMetricsIncrementWithZeroRevenue(t *testing.T) {
	r := testRepo(t)
	if err := r.AddPrint(0, 4, false, 2); err != nil {
		t.Fatal(err)
	}
	if err := r.AddCopy(0, 3, 3, true); err != nil {
		t.Fatal(err)
	}
	if err := r.AddScan(0); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"today", "total"} {
		var d stats.Day
		var err error
		if scope == "today" {
			d, err = r.GetDay("")
		} else {
			d, err = r.GetTotal()
		}
		if err != nil {
			t.Fatal(err)
		}
		if d.Revenue != 0 || d.PagesBW != 4 || d.PagesColor != 3 || d.Copies != 3 || d.Scans != 1 || d.SheetsUsed != 5 {
			t.Fatalf("unexpected %s test metrics: %+v", scope, d)
		}
	}
}
