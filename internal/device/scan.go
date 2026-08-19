package device

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
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
		return err
	}
	slog.Info("scan finished", "dest", destPDF)
	return nil
}

func imageToPDF(imgPath, pdfPath string) error {
	imp, err := api.Import("formsize:A4, position:full", types.POINTS)
	if err != nil {
		return fmt.Errorf("параметры PDF: %w", err)
	}
	if err := api.ImportImagesFile([]string{imgPath}, pdfPath, imp, nil); err != nil {
		return fmt.Errorf("собрать PDF: %w", err)
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
