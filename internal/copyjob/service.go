package copyjob

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"print-kiosk/internal/device"
	"print-kiosk/internal/printjob"
)

type Status string

const (
	StatusCreated Status = "created"
	StatusPaid    Status = "paid"
	StatusDone    Status = "done"
)

type Options struct {
	Color  bool `json:"color"`
	Copies int  `json:"copies"`
}

type Quote struct {
	Color        bool    `json:"color"`
	Copies       int     `json:"copies"`
	PricePerCopy float64 `json:"price_per_copy"`
	Total        float64 `json:"total"`
	Sheets       int     `json:"sheets"`
}

type Job struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	Paid      bool      `json:"paid"`
	Color     bool      `json:"color"`
	Copies    int       `json:"copies"`
	CreatedAt time.Time `json:"-"`
}

type Service struct {
	dryRun  bool
	jobsDir string
	printer *printjob.Service
	mu      sync.RWMutex
	jobs    map[string]*Job
}

func NewService(jobsDir string, printer *printjob.Service, dryRun bool) (*Service, error) {
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		return nil, err
	}
	return &Service{
		dryRun:  dryRun,
		jobsDir: jobsDir,
		printer: printer,
		jobs:    make(map[string]*Job),
	}, nil
}

func (s *Service) Create() *Job {
	job := &Job{
		ID:        uuid.NewString(),
		Status:    StatusCreated,
		Copies:    1,
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
	return job
}

func (s *Service) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

func QuotePrice(opt Options, priceBW, priceColor float64) (Quote, error) {
	if opt.Copies < 1 {
		opt.Copies = 1
	}
	if opt.Copies > 100 {
		return Quote{}, fmt.Errorf("слишком много копий")
	}
	price := priceBW
	if opt.Color {
		price = priceColor
	}
	return Quote{
		Color:        opt.Color,
		Copies:       opt.Copies,
		PricePerCopy: price,
		Total:        price * float64(opt.Copies),
		Sheets:       opt.Copies,
	}, nil
}

func (s *Service) MarkPaid(id string, opt Options) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("заказ не найден")
	}
	if opt.Copies < 1 {
		opt.Copies = 1
	}
	job.Color = opt.Color
	job.Copies = opt.Copies
	job.Paid = true
	job.Status = StatusPaid
	return job, nil
}

// Execute scans the platen and sends the page to the same printer as print jobs.
func (s *Service) Execute(id string) (*Job, int, error) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return nil, 0, fmt.Errorf("заказ не найден")
	}
	if !job.Paid {
		s.mu.Unlock()
		return nil, 0, fmt.Errorf("сначала оплатите копирование")
	}
	color := job.Color
	copies := job.Copies
	s.mu.Unlock()

	dir := filepath.Join(s.jobsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, fmt.Errorf("каталог копии: %w", err)
	}
	pdfPath := filepath.Join(dir, "copy.pdf")
	defer os.RemoveAll(dir)

	slog.Info("copy started", "job", id, "color", color, "copies", copies, "dry_run", s.dryRun)
	if err := device.ScanToPDF(pdfPath, device.ScanOptions{Color: color, DPI: 200}, s.dryRun); err != nil {
		return nil, 0, err
	}

	if s.printer == nil {
		return nil, 0, fmt.Errorf("принтер не настроен")
	}
	if err := s.printer.PrintFile(pdfPath, printjob.PrintOptions{
		Color:  color,
		Copies: copies,
	}); err != nil {
		return nil, 0, fmt.Errorf("не удалось отправить на печать: %w", err)
	}
	slog.Info("copy finished", "job", id)

	s.mu.Lock()
	defer s.mu.Unlock()
	job.Status = StatusDone
	return job, copies, nil
}

func (s *Service) Delete(id string) {
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
	_ = os.RemoveAll(filepath.Join(s.jobsDir, id))
}
