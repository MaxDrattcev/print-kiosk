package printjob

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"

	"print-kiosk/internal/device"
	"print-kiosk/internal/libreoffice"
	"print-kiosk/internal/usb"
)

type PreviewKind string

const (
	PreviewPDF   PreviewKind = "pdf"
	PreviewImage PreviewKind = "image"
)

type Job struct {
	ID                 string      `json:"id"`
	FileName           string      `json:"file_name"`
	Pages              int         `json:"pages"`
	PreviewKind        PreviewKind `json:"preview_kind"`
	NaturalOrientation string      `json:"natural_orientation"`
	SuggestedColor     bool        `json:"suggested_color"`
	CreatedAt          time.Time   `json:"created_at"`

	Dir         string `json:"-"`
	SourcePath  string `json:"-"`
	PreviewPath string `json:"-"`

	// Options locked after successful payment.
	Paid        bool   `json:"-"`
	Color       bool   `json:"-"`
	Duplex      bool   `json:"-"`
	Copies      int    `json:"-"`
	Orientation string `json:"-"`
	PageRange   string `json:"-"`
	Scale       string `json:"-"`
}

type Service struct {
	jobsDir         string
	libreOfficePath string
	printerName     string
	sumatraPath     string
	dryRun          bool
	mu              sync.RWMutex
	jobs            map[string]*Job
}

type Options struct {
	JobsDir         string
	LibreOfficePath string
	PrinterName     string
	SumatraPath     string
	DryRun          bool
}

func NewService(opt Options) (*Service, error) {
	if err := os.MkdirAll(opt.JobsDir, 0o755); err != nil {
		return nil, err
	}
	return &Service{
		jobsDir:         opt.JobsDir,
		libreOfficePath: opt.LibreOfficePath,
		printerName:     opt.PrinterName,
		sumatraPath:     opt.SumatraPath,
		dryRun:          opt.DryRun,
		jobs:            make(map[string]*Job),
	}, nil
}

func (s *Service) PrepareFromUSB(sourcePath string) (*Job, error) {
	if err := usb.ValidateUSBFile(sourcePath); err != nil {
		return nil, err
	}
	return s.PrepareFromLocal(sourcePath, filepath.Base(sourcePath))
}

// PrepareFromLocal converts a local file into a print job (USB/email/telegram).
func (s *Service) PrepareFromLocal(sourcePath, displayName string) (*Job, error) {
	st, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("файл не найден: %w", err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("путь является папкой")
	}
	if displayName == "" {
		displayName = filepath.Base(sourcePath)
	}

	id := uuid.NewString()
	dir := filepath.Join(s.jobsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create job dir: %w", err)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("resolve job dir: %w", err)
	}
	localSource := filepath.Join(absDir, "source"+strings.ToLower(filepath.Ext(displayName)))
	if err := copyFile(sourcePath, localSource); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("copy source: %w", err)
	}
	_ = os.Chmod(localSource, 0o644)

	previewPath, kind, pages, err := s.buildPreview(localSource, absDir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	naturalOrientation, orientationErr := detectNaturalOrientation(localSource, previewPath, kind)
	if orientationErr != nil {
		slog.Warn("could not detect document orientation", "file", displayName, "error", orientationErr)
		naturalOrientation = "portrait"
	}
	suggestedColor, colorErr := detectDocumentColor(localSource, previewPath, kind)
	if colorErr != nil {
		slog.Warn("could not detect document color", "file", displayName, "error", colorErr)
	}

	job := &Job{
		ID:                 id,
		FileName:           displayName,
		Pages:              pages,
		PreviewKind:        kind,
		NaturalOrientation: naturalOrientation,
		SuggestedColor:     suggestedColor,
		CreatedAt:          time.Now(),
		Dir:                dir,
		SourcePath:         localSource,
		PreviewPath:        previewPath,
	}

	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	return job, nil
}

func detectNaturalOrientation(sourcePath, previewPath string, kind PreviewKind) (string, error) {
	if kind == PreviewImage {
		file, err := os.Open(sourcePath)
		if err != nil {
			return "", err
		}
		defer file.Close()
		cfg, _, err := image.DecodeConfig(file)
		if err != nil {
			return "", err
		}
		if cfg.Width > cfg.Height {
			return "landscape", nil
		}
		return "portrait", nil
	}

	dims, err := api.PageDimsFile(previewPath)
	if err != nil {
		return "", err
	}
	return orientationFromDimensions(dims), nil
}

func orientationFromDimensions(dims []types.Dim) string {
	landscape, portrait := 0, 0
	first := "portrait"
	for i, dim := range dims {
		orientation := "portrait"
		if dim.Width > dim.Height {
			orientation = "landscape"
			landscape++
		} else {
			portrait++
		}
		if i == 0 {
			first = orientation
		}
	}
	if landscape == portrait {
		return first
	}
	if landscape > portrait {
		return "landscape"
	}
	return "portrait"
}

func (s *Service) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

// ApplyImageOrientation rebuilds an image job on an A4 page in the chosen orientation.
func (s *Service) ApplyImageOrientation(job *Job, orientation string) error {
	if job == nil || job.PreviewKind != PreviewImage {
		return nil
	}
	return device.ImageToA4PDFOrientation(job.SourcePath, job.PreviewPath, normalizeOrientation(orientation) == "landscape")
}

// Cleanup removes a finished job and its temporary files from disk.
func (s *Service) Cleanup(id string) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if ok {
		delete(s.jobs, id)
	}
	s.mu.Unlock()
	if ok && job != nil && job.Dir != "" {
		_ = os.RemoveAll(job.Dir)
	}
}

type QuoteInput struct {
	Color       bool   `json:"color"`
	Duplex      bool   `json:"duplex"`
	Copies      int    `json:"copies"`
	Orientation string `json:"orientation,omitempty"`
	PageRange   string `json:"page_range,omitempty"`
	Scale       string `json:"scale,omitempty"`
}

type Quote struct {
	Pages        int     `json:"pages"`
	Sheets       int     `json:"sheets"`
	Copies       int     `json:"copies"`
	Color        bool    `json:"color"`
	Duplex       bool    `json:"duplex"`
	PricePerPage float64 `json:"price_per_page"`
	Total        float64 `json:"total"`
	PageRange    string  `json:"page_range"`
	Scale        string  `json:"scale"`
}

func (s *Service) Quote(job *Job, in QuoteInput, priceBW, priceColor float64) (*Quote, error) {
	if in.Copies < 1 {
		in.Copies = 1
	}
	if in.Copies > 100 {
		return nil, fmt.Errorf("слишком много копий")
	}
	selectedPages, normalizedRange, err := ParsePageRange(in.PageRange, job.Pages)
	if err != nil {
		return nil, err
	}
	scale, err := normalizeScale(in.Scale)
	if err != nil {
		return nil, err
	}
	price := priceBW
	if in.Color {
		price = priceColor
	}
	pageCount := len(selectedPages)
	// Duplex becomes meaningful as soon as the job contains at least two
	// impressions, including two copies of a one-page document.
	effectiveDuplex := in.Duplex && pageCount*in.Copies > 1
	total := float64(pageCount) * price * float64(in.Copies)
	return &Quote{
		Pages:        pageCount,
		Sheets:       PaperSheets(pageCount, in.Copies, effectiveDuplex),
		Copies:       in.Copies,
		Color:        in.Color,
		Duplex:       effectiveDuplex,
		PricePerPage: price,
		Total:        total,
		PageRange:    normalizedRange,
		Scale:        scale,
	}, nil
}

// ParsePageRange validates a user range such as "1-3, 6, 9-12" and returns
// unique page numbers in document order. An empty range means all pages.
func ParsePageRange(value string, maxPages int) ([]int, string, error) {
	if maxPages < 1 {
		return nil, "", fmt.Errorf("в документе нет страниц")
	}
	value = strings.TrimSpace(strings.NewReplacer("–", "-", "—", "-", ";", ",").Replace(value))
	if value == "" {
		pages := make([]int, maxPages)
		for i := range pages {
			pages[i] = i + 1
		}
		return pages, "", nil
	}

	selected := make(map[int]struct{})
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 || strings.TrimSpace(bounds[0]) == "" {
			return nil, "", fmt.Errorf("укажите страницы, например 1-3, 6")
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil {
			return nil, "", fmt.Errorf("укажите страницы, например 1-3, 6")
		}
		end := start
		if len(bounds) == 2 {
			if strings.TrimSpace(bounds[1]) == "" {
				return nil, "", fmt.Errorf("завершите диапазон страниц")
			}
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, "", fmt.Errorf("укажите страницы, например 1-3, 6")
			}
		}
		if start < 1 || end < start || end > maxPages {
			return nil, "", fmt.Errorf("страницы должны быть от 1 до %d", maxPages)
		}
		for page := start; page <= end; page++ {
			selected[page] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return nil, "", fmt.Errorf("выберите хотя бы одну страницу")
	}
	pages := make([]int, 0, len(selected))
	for page := 1; page <= maxPages; page++ {
		if _, ok := selected[page]; ok {
			pages = append(pages, page)
		}
	}
	return pages, compactPageRange(pages), nil
}

func compactPageRange(pages []int) string {
	parts := make([]string, 0, len(pages))
	for i := 0; i < len(pages); {
		start, end := pages[i], pages[i]
		for i+1 < len(pages) && pages[i+1] == end+1 {
			i++
			end = pages[i]
		}
		if start == end {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(end))
		}
		i++
	}
	return strings.Join(parts, ",")
}

// PaperSheets returns physical sheets used.
// Duplex: ceil((pages * copies) / 2); simplex: pages * copies.
func PaperSheets(pages, copies int, duplex bool) int {
	if pages < 1 {
		pages = 1
	}
	if copies < 1 {
		copies = 1
	}
	totalPages := pages * copies
	if duplex {
		return (totalPages + 1) / 2
	}
	return totalPages
}

// LockOptions stores print settings after payment.
func (s *Service) LockOptions(job *Job, in QuoteInput) {
	if job == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copies := in.Copies
	if copies < 1 {
		copies = 1
	}
	job.Paid = true
	job.Color = in.Color
	selectedPages, _, _ := ParsePageRange(in.PageRange, job.Pages)
	job.Duplex = in.Duplex && len(selectedPages)*copies > 1
	job.Copies = copies
	job.Orientation = normalizeOrientation(in.Orientation)
	_, job.PageRange, _ = ParsePageRange(in.PageRange, job.Pages)
	job.Scale, _ = normalizeScale(in.Scale)
}

// LockedOptions returns paid print options when available.
func (s *Service) LockedOptions(job *Job) (QuoteInput, bool) {
	if job == nil || !job.Paid {
		return QuoteInput{}, false
	}
	return QuoteInput{
		Color:       job.Color,
		Duplex:      job.Duplex,
		Copies:      job.Copies,
		Orientation: job.Orientation,
		PageRange:   job.PageRange,
		Scale:       job.Scale,
	}, true
}

func normalizeOrientation(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "landscape") {
		return "landscape"
	}
	return "portrait"
}

func normalizeScale(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "fit":
		return "fit", nil
	case "actual":
		return "actual", nil
	default:
		return "", fmt.Errorf("неизвестный режим масштабирования")
	}
}

func (s *Service) buildPreview(sourcePath, dir string) (string, PreviewKind, int, error) {
	ext := strings.ToLower(filepath.Ext(sourcePath))
	switch ext {
	case ".pdf":
		preview := filepath.Join(dir, "preview.pdf")
		if err := copyFile(sourcePath, preview); err != nil {
			return "", "", 0, err
		}
		pages, err := pageCount(preview)
		if err != nil {
			return "", "", 0, err
		}
		return preview, PreviewPDF, pages, nil

	case ".jpg", ".jpeg", ".png", ".bmp", ".tif", ".tiff":
		preview := filepath.Join(dir, "preview.pdf")
		if err := device.ImageToA4PDF(sourcePath, preview); err != nil {
			return "", "", 0, err
		}
		return preview, PreviewImage, 1, nil

	default:
		preview, err := s.convertWithLibreOffice(sourcePath, dir)
		if err != nil {
			return "", "", 0, err
		}
		pages, err := pageCount(preview)
		if err != nil {
			return "", "", 0, err
		}
		return preview, PreviewPDF, pages, nil
	}
}

func (s *Service) convertWithLibreOffice(sourcePath, dir string) (string, error) {
	bin, err := resolveLibreOffice(s.libreOfficePath)
	if err != nil {
		return "", err
	}

	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve source: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve outdir: %w", err)
	}

	preview := filepath.Join(absDir, "preview.pdf")
	var lastOut string
	for attempt := 1; attempt <= 2; attempt++ {
		profileDir, err := os.MkdirTemp("", "lo-profile-*")
		if err != nil {
			return "", fmt.Errorf("create libreoffice profile: %w", err)
		}

		profileURL := fileURL(profileDir)
		args := []string{
			"-env:UserInstallation=" + profileURL,
			"--headless",
			"--nologo",
			"--nofirststartwizard",
			"--nolockcheck",
			"--nodefault",
			"--norestore",
			"--convert-to", convertFilter(absSource),
			"--outdir", absDir,
			absSource,
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir = absDir
		out, err := cmd.CombinedOutput()
		lastOut = strings.TrimSpace(string(out))
		_ = os.RemoveAll(profileDir)

		if err != nil {
			return "", fmt.Errorf("конвертация LibreOffice: %w (%s)", err, lastOut)
		}

		if pdfPath, ok := findConvertedPDF(absDir, absSource); ok {
			if pdfPath != preview {
				if err := os.Rename(pdfPath, preview); err != nil {
					// Same filesystem copy fallback.
					if copyErr := copyFile(pdfPath, preview); copyErr != nil {
						return "", fmt.Errorf("save preview pdf: %v / %v", err, copyErr)
					}
					_ = os.Remove(pdfPath)
				}
			}
			return preview, nil
		}

		// LibreOffice sometimes exits 0 when another instance holds the default profile.
		time.Sleep(500 * time.Millisecond)
	}

	entries, _ := os.ReadDir(absDir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return "", fmt.Errorf("pdf после конвертации не найден (файлы: %s; libreoffice: %s)", strings.Join(names, ", "), lastOut)
}

func fileURL(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func convertFilter(sourcePath string) string {
	switch strings.ToLower(filepath.Ext(sourcePath)) {
	case ".doc", ".docx", ".odt", ".rtf", ".txt":
		return "pdf:writer_pdf_Export"
	case ".xls", ".xlsx", ".ods":
		return "pdf:calc_pdf_Export"
	case ".ppt", ".pptx", ".odp":
		return "pdf:impress_pdf_Export"
	default:
		return "pdf"
	}
}

func findConvertedPDF(dir, sourcePath string) (string, bool) {
	base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	candidates := []string{
		filepath.Join(dir, base+".pdf"),
		filepath.Join(dir, "source.pdf"),
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() && st.Size() > 0 {
			return candidate, true
		}
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "*.pdf"))
	for _, match := range matches {
		if strings.EqualFold(filepath.Base(match), "preview.pdf") {
			continue
		}
		if st, err := os.Stat(match); err == nil && st.Size() > 0 {
			return match, true
		}
	}
	return "", false
}

func pageCount(pdfPath string) (int, error) {
	n, err := api.PageCountFile(pdfPath)
	if err != nil {
		return 0, fmt.Errorf("читать pdf: %w", err)
	}
	if n < 1 {
		return 0, fmt.Errorf("не удалось определить число страниц")
	}
	return n, nil
}

func resolveLibreOffice(configured string) (string, error) {
	path, err := libreoffice.Find(configured)
	if err != nil {
		return "", fmt.Errorf("LibreOffice не найден. Установите LibreOffice для печати Office-документов")
	}
	return path, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
