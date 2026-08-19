package device

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
)

type ScanOptions struct {
	Color bool
	DPI   int
}

func (o ScanOptions) dpi() int {
	if o.DPI < 75 {
		return 200
	}
	return o.DPI
}

// ScanToPDF acquires a page from the scanner and writes an A4 PDF.
// dryRun writes a placeholder file so kiosk flows can be tested without hardware.
func ScanToPDF(destPDF string, opt ScanOptions, dryRun bool) error {
	if destPDF == "" {
		return fmt.Errorf("не задан файл скана")
	}
	if err := os.MkdirAll(filepath.Dir(destPDF), 0o755); err != nil {
		return fmt.Errorf("каталог скана: %w", err)
	}
	if dryRun {
		slog.Info("scan dry-run", "dest", destPDF, "color", opt.Color)
		return writePlaceholderPDF(destPDF)
	}

	slog.Info("scan started", "dest", destPDF, "color", opt.Color, "dpi", opt.dpi(), "os", runtime.GOOS)

	var err error
	switch runtime.GOOS {
	case "windows":
		err = scanWindows(destPDF, opt)
	default:
		err = scanUnix(destPDF, opt)
	}
	if err != nil {
		slog.Warn("scan failed", "dest", destPDF, "error", err)
		return err
	}
	slog.Info("scan finished", "dest", destPDF)
	return nil
}

func imageToPDF(imgPath, pdfPath string) error {
	st, err := os.Stat(imgPath)
	if err != nil || st.Size() < 1024 {
		return fmt.Errorf("сканер вернул пустое изображение. Положите документ на стекло и повторите")
	}

	pngPath := filepath.Join(filepath.Dir(imgPath), "scan-norm.png")
	if err := writeNormalizedPNG(imgPath, pngPath); err != nil {
		return err
	}
	defer os.Remove(pngPath)

	// A4 + вписать изображение. pos:full делает страницу размером с пиксели скана —
	// при печати на A4 выходит только уголок.
	imp, err := api.Import("formsize:A4, position:c, scalefactor:1", types.POINTS)
	if err != nil {
		slog.Warn("scan pdf params", "error", err)
		return fmt.Errorf("не удалось подготовить скан")
	}
	if err := api.ImportImagesFile([]string{pngPath}, pdfPath, imp, nil); err != nil {
		slog.Warn("scan pdf import", "error", err, "image", imgPath, "bytes", st.Size())
		return fmt.Errorf("не удалось обработать скан. Положите документ на стекло и повторите")
	}
	return nil
}

func writeNormalizedPNG(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil || len(data) < 1024 {
		return fmt.Errorf("сканер вернул пустое изображение. Положите документ на стекло и повторите")
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Warn("scan image decode", "error", err, "bytes", len(data), "magic", fmt.Sprintf("%x", data[:min(8, len(data))]))
		return fmt.Errorf("не удалось обработать скан. Положите документ на стекло и повторите")
	}
	if img.Bounds().Dx() < 8 || img.Bounds().Dy() < 8 {
		return fmt.Errorf("сканер вернул пустое изображение. Положите документ на стекло и повторите")
	}
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("не удалось сохранить скан")
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		slog.Warn("scan png encode", "error", err, "format", format)
		return fmt.Errorf("не удалось обработать скан")
	}
	return nil
}

func scanUnix(destPDF string, opt ScanOptions) error {
	bin, err := exec.LookPath("scanimage")
	if err != nil {
		return fmt.Errorf("сканер не найден: установите SANE (scanimage) или используйте Windows WIA")
	}

	dir := filepath.Dir(destPDF)
	img := filepath.Join(dir, "scan.png")
	mode := "Gray"
	if opt.Color {
		mode = "Color"
	}
	cmd := exec.Command(bin,
		"--format=png",
		"--resolution", fmt.Sprintf("%d", opt.dpi()),
		"--mode", mode,
		"-o", img,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("сканирование: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	defer os.Remove(img)
	return imageToPDF(img, destPDF)
}

func writePlaceholderPDF(path string) error {
	const pdf = `%PDF-1.4
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources<< /Font<< /F1 5 0 R >> >> >>endobj
4 0 obj<< /Length 120 >>stream
BT
/F1 28 Tf
72 720 Td
(PrintStart scan) Tj
0 -40 Td
/F1 16 Tf
(Document scanned successfully) Tj
ET
endstream
endobj
5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000266 00000 n 
0000000438 00000 n 
trailer<< /Size 6 /Root 1 0 R >>
startxref
517
%%EOF
`
	return os.WriteFile(path, []byte(pdf), 0o644)
}
