package scanjob

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"print-kiosk/internal/usb"
)

type Status string

const (
	StatusCreated   Status = "created"
	StatusPaid      Status = "paid"
	StatusScanned   Status = "scanned"
	StatusNamed     Status = "named"
	StatusSavedUSB  Status = "saved_usb"
	StatusSentEmail Status = "sent_email"
)

type Job struct {
	ID           string  `json:"id"`
	Status       Status  `json:"status"`
	Paid         bool    `json:"paid"`
	FileName     string  `json:"file_name"`
	ScanPath     string  `json:"-"`
	SavedPath    string  `json:"saved_path,omitempty"`
	SentToEmail  string  `json:"sent_to_email,omitempty"`
	PricePerScan float64 `json:"price"`
	CreatedAt    time.Time
}

type Service struct {
	jobsDir          string
	dryRun           bool
	mu               sync.RWMutex
	jobs             map[string]*Job
	fileNameCounter  int
	counterStartedAt time.Time
	now              func() time.Time
}

func NewService(jobsDir string, dryRun bool) (*Service, error) {
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		return nil, err
	}
	return &Service{
		jobsDir: jobsDir,
		dryRun:  dryRun,
		jobs:    make(map[string]*Job),
		now:     time.Now,
	}, nil
}

func (s *Service) Create(price float64) (*Job, error) {
	id := uuid.NewString()
	dir := filepath.Join(s.jobsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	job := &Job{
		ID:           id,
		Status:       StatusCreated,
		FileName:     s.nextDefaultFileName(now),
		PricePerScan: price,
		CreatedAt:    now,
	}
	s.jobs[id] = job
	return job, nil
}

const fileNameCounterWindow = 10 * time.Minute

// nextDefaultFileName numbers scans within a ten-minute series. The next
// series starts again at File_1, making the suggested name short and familiar.
// The caller must hold s.mu.
func (s *Service) nextDefaultFileName(now time.Time) string {
	if s.counterStartedAt.IsZero() || now.Before(s.counterStartedAt) || now.Sub(s.counterStartedAt) >= fileNameCounterWindow {
		s.counterStartedAt = now
		s.fileNameCounter = 0
	}
	s.fileNameCounter++
	return fmt.Sprintf("Файл_%d.pdf", s.fileNameCounter)
}

func (s *Service) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

func (s *Service) MarkPaid(id string) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Paid = true
	job.Status = StatusPaid
	return job, nil
}

func (s *Service) Scan(id string) (*Job, error) {
	return s.scan(id, s.dryRun)
}

func (s *Service) ScanTest(id string) (*Job, error) {
	return s.scan(id, true)
}

func (s *Service) scan(id string, dryRun bool) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if !job.Paid {
		return nil, fmt.Errorf("сначала оплатите сканирование")
	}

	dir := filepath.Join(s.jobsDir, id)
	out := filepath.Join(dir, "scan.pdf")
	if err := performScan(out, dryRun); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	job.ScanPath = out
	job.Status = StatusScanned
	return job, nil
}

func (s *Service) SetName(id, name string) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if job.Status != StatusScanned && job.Status != StatusNamed {
		return nil, fmt.Errorf("сначала выполните сканирование")
	}
	clean, err := sanitizeFileName(name)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job.FileName = clean
	job.Status = StatusNamed
	return job, nil
}

func (s *Service) SaveToUSB(id, drivePath string) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if job.ScanPath == "" {
		return nil, fmt.Errorf("файл скана ещё не готов")
	}
	if job.FileName == "" {
		return nil, fmt.Errorf("укажите имя файла")
	}

	saved, err := usb.SaveFileToDrive(drivePath, job.FileName, job.ScanPath)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	job.SavedPath = saved
	job.Status = StatusSavedUSB
	s.mu.Unlock()

	s.CleanupFiles(id)
	return job, nil
}

// MarkDelivered marks the job as sent (email or MAX) and deletes the local PDF.
func (s *Service) MarkDelivered(id, dest string) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if job.ScanPath == "" {
		return nil, fmt.Errorf("файл скана ещё не готов")
	}
	if job.FileName == "" {
		return nil, fmt.Errorf("укажите имя файла")
	}
	s.mu.Lock()
	job.SentToEmail = strings.TrimSpace(dest)
	job.Status = StatusSentEmail
	s.mu.Unlock()

	s.CleanupFiles(id)
	return job, nil
}

// MarkSentEmail is MarkDelivered for SMTP.
func (s *Service) MarkSentEmail(id, toAddr string) (*Job, error) {
	return s.MarkDelivered(id, toAddr)
}

// CleanupFiles removes the local scan PDF after successful delivery to the user.
func (s *Service) CleanupFiles(id string) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	dir := filepath.Join(s.jobsDir, id)
	if ok && job.ScanPath != "" {
		dir = filepath.Dir(job.ScanPath)
		job.ScanPath = ""
	}
	s.mu.Unlock()
	_ = os.RemoveAll(dir)
}

// Abandon removes the job and its files so the next visitor cannot open them.
func (s *Service) Abandon(id string) {
	s.CleanupFiles(id)
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
}

// ReadyForDelivery returns scan path and file name if the job can be delivered.
func (s *Service) ReadyForDelivery(id string) (scanPath, fileName string, err error) {
	job, err := s.mustGet(id)
	if err != nil {
		return "", "", err
	}
	if job.ScanPath == "" {
		return "", "", fmt.Errorf("файл скана ещё не готов")
	}
	if strings.TrimSpace(job.FileName) == "" {
		return "", "", fmt.Errorf("укажите имя файла")
	}
	return job.ScanPath, job.FileName, nil
}

func (s *Service) PreviewPath(id string) (string, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return "", err
	}
	if job.ScanPath == "" {
		return "", fmt.Errorf("превью ещё не готово")
	}
	return job.ScanPath, nil
}

func (s *Service) mustGet(id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("заказ не найден")
	}
	return job, nil
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizeFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	name = unsafeName.ReplaceAllString(name, "")
	name = strings.Trim(name, " .")
	if name == "" {
		return "", fmt.Errorf("введите имя файла")
	}

	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(b.String())
	if name == "" {
		return "", fmt.Errorf("имя файла содержит недопустимые символы")
	}

	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		name += ".pdf"
	} else if ext != ".pdf" {
		name = strings.TrimSuffix(name, ext) + ".pdf"
	}
	if len(name) > 80 {
		base := strings.TrimSuffix(name, ".pdf")
		if len(base) > 76 {
			base = base[:76]
		}
		name = base + ".pdf"
	}
	return name, nil
}
