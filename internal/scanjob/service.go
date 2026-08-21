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
	"github.com/pdfcpu/pdfcpu/pkg/api"

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
	ID             string  `json:"id"`
	Status         Status  `json:"status"`
	Paid           bool    `json:"paid"`
	FileName       string  `json:"file_name"`
	ScanPath       string  `json:"-"`
	SavedPath      string  `json:"saved_path,omitempty"`
	SentToEmail    string  `json:"sent_to_email,omitempty"`
	PricePerScan   float64 `json:"price"`
	Pages          []Page  `json:"pages"`
	PaidPages      int     `json:"paid_pages"`
	PaidAmount     float64 `json:"paid_amount"`
	paymentPending int
	CreatedAt      time.Time
}

type Page struct {
	ID   string `json:"id"`
	Path string `json:"-"`
}

type PaymentReservation struct {
	Pages  int
	Amount float64
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
const MaxPages = 30

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
	reservation, err := s.ReservePayment(id)
	if err != nil {
		return nil, err
	}
	return s.CommitPayment(id, reservation.Pages)
}

// ReservePayment atomically reserves only pages that have not been paid for.
// An empty session always reserves the first page before the scanner starts.
func (s *Service) ReservePayment(id string) (PaymentReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return PaymentReservation{}, fmt.Errorf("заказ не найден")
	}
	if job.paymentPending > 0 {
		return PaymentReservation{}, fmt.Errorf("оплата уже выполняется")
	}
	target := len(job.Pages)
	if target < 1 {
		target = 1
	}
	missing := target - job.PaidPages
	if missing <= 0 {
		return PaymentReservation{}, fmt.Errorf("все страницы уже оплачены")
	}
	job.paymentPending = missing
	return PaymentReservation{Pages: missing, Amount: float64(missing) * job.PricePerScan}, nil
}

func (s *Service) CommitPayment(id string, pages int) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("заказ не найден")
	}
	if pages < 1 || job.paymentPending != pages {
		return nil, fmt.Errorf("платёжная сессия устарела")
	}
	job.paymentPending = 0
	job.PaidPages += pages
	job.PaidAmount += float64(pages) * job.PricePerScan
	job.Paid = job.PaidPages > 0
	if len(job.Pages) == 0 {
		job.Status = StatusPaid
	}
	return job, nil
}

func (s *Service) CancelPayment(id string) {
	s.mu.Lock()
	if job, ok := s.jobs[id]; ok {
		job.paymentPending = 0
	}
	s.mu.Unlock()
}

func (s *Service) Scan(id string) (*Job, error) {
	return s.scan(id, -1, s.dryRun)
}

func (s *Service) ScanTest(id string) (*Job, error) {
	return s.scan(id, -1, true)
}

func (s *Service) ScanPage(id string, replaceIndex int, dryRun bool) (*Job, error) {
	return s.scan(id, replaceIndex, dryRun)
}

func (s *Service) scan(id string, replaceIndex int, dryRun bool) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if !job.Paid {
		return nil, fmt.Errorf("сначала оплатите сканирование")
	}
	if replaceIndex >= len(job.Pages) {
		return nil, fmt.Errorf("страница не найдена")
	}
	if replaceIndex < 0 && len(job.Pages) >= MaxPages {
		return nil, fmt.Errorf("в одном документе можно сохранить не более %d страниц", MaxPages)
	}

	dir := filepath.Join(s.jobsDir, id)
	pagesDir := filepath.Join(dir, "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		return nil, err
	}
	pageID := uuid.NewString()
	out := filepath.Join(pagesDir, pageID+".pdf")
	if err := performScan(out, dryRun); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	oldPages := append([]Page(nil), job.Pages...)
	newPage := Page{ID: pageID, Path: out}
	var replacedPath string
	if replaceIndex >= 0 {
		replacedPath = job.Pages[replaceIndex].Path
		job.Pages[replaceIndex] = newPage
	} else {
		job.Pages = append(job.Pages, newPage)
	}
	if err := s.rebuildPDF(job); err != nil {
		job.Pages = oldPages
		_ = os.Remove(out)
		return nil, err
	}
	if replacedPath != "" {
		_ = os.Remove(replacedPath)
	}
	job.Status = StatusScanned
	return job, nil
}

func (s *Service) DeletePage(id string, index int) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("заказ не найден")
	}
	if index < 0 || index >= len(job.Pages) {
		return nil, fmt.Errorf("страница не найдена")
	}
	removed := job.Pages[index]
	oldPages := append([]Page(nil), job.Pages...)
	job.Pages = append(job.Pages[:index:index], job.Pages[index+1:]...)
	if err := s.rebuildPDF(job); err != nil {
		job.Pages = oldPages
		return nil, err
	}
	_ = os.Remove(removed.Path)
	if len(job.Pages) == 0 {
		job.Status = StatusPaid
	} else {
		job.Status = StatusScanned
	}
	return job, nil
}

func (s *Service) rebuildPDF(job *Job) error {
	combined := filepath.Join(s.jobsDir, job.ID, "scan.pdf")
	if len(job.Pages) == 0 {
		_ = os.Remove(combined)
		job.ScanPath = ""
		return nil
	}
	inputs := make([]string, 0, len(job.Pages))
	for _, page := range job.Pages {
		inputs = append(inputs, page.Path)
	}
	tmp := filepath.Join(s.jobsDir, job.ID, "scan-"+uuid.NewString()+".pdf")
	if err := api.MergeCreateFile(inputs, tmp, false, nil); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("собрать PDF: %w", err)
	}
	// Windows does not replace an existing destination with os.Rename.
	_ = os.Remove(combined)
	if err := os.Rename(tmp, combined); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("сохранить PDF: %w", err)
	}
	job.ScanPath = combined
	return nil
}

func (s *Service) SetName(id, name string) (*Job, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if job.Status != StatusScanned && job.Status != StatusNamed {
		return nil, fmt.Errorf("сначала выполните сканирование")
	}
	if job.PaidPages < len(job.Pages) {
		return nil, fmt.Errorf("сначала оплатите дополнительные страницы")
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
	if job.PaidPages < len(job.Pages) {
		return "", "", fmt.Errorf("сначала оплатите дополнительные страницы")
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

func (s *Service) PagePreviewPath(id string, index int) (string, error) {
	job, err := s.mustGet(id)
	if err != nil {
		return "", err
	}
	if index < 0 || index >= len(job.Pages) {
		return "", fmt.Errorf("страница не найдена")
	}
	return job.Pages[index].Path, nil
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
