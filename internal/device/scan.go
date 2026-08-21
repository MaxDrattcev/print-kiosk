package device

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
	if err := ImageToA4PDF(imgPath, pdfPath); err != nil {
		slog.Warn("scan pdf import", "error", err, "image", imgPath, "bytes", st.Size())
		return fmt.Errorf("не удалось обработать скан. Положите документ на стекло и повторите")
	}
	return nil
}

// ImageToA4PDF places an image on a portrait A4 page, centered and fitted.
// Needed before printing: SumatraPDF often sends a blank sheet for raw JPEG/PNG.
func ImageToA4PDF(imgPath, pdfPath string) error {
	return ImageToA4PDFOrientation(imgPath, pdfPath, false)
}

// ImageToA4PDFOrientation centers an image on a portrait or landscape A4 page.
func ImageToA4PDFOrientation(imgPath, pdfPath string, landscape bool) error {
	if pdfPath == "" {
		return fmt.Errorf("не задан файл PDF")
	}
	if err := os.MkdirAll(filepath.Dir(pdfPath), 0o755); err != nil {
		return fmt.Errorf("каталог PDF: %w", err)
	}

	pngPath := filepath.Join(filepath.Dir(pdfPath), "img-norm.png")
	if err := writeNormalizedPNG(imgPath, pngPath); err != nil {
		return err
	}
	defer os.Remove(pngPath)

	// pos:full makes the PDF page equal to image pixels — A4 print then shows a corner or a blank sheet.
	pageSize := "A4"
	if landscape {
		pageSize = "A4L"
	}
	imp, err := api.Import("formsize:"+pageSize+", position:c, scalefactor:1", types.POINTS)
	if err != nil {
		slog.Warn("image pdf params", "error", err)
		return fmt.Errorf("не удалось подготовить изображение")
	}
	if err := api.ImportImagesFile([]string{pngPath}, pdfPath, imp, nil); err != nil {
		slog.Warn("image pdf import", "error", err, "image", imgPath)
		return fmt.Errorf("не удалось подготовить изображение к печати")
	}
	return nil
}

func writeNormalizedPNG(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil || len(data) < 32 {
		return fmt.Errorf("не удалось прочитать изображение")
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Warn("image decode", "error", err, "bytes", len(data), "magic", fmt.Sprintf("%x", data[:min(8, len(data))]))
		return fmt.Errorf("не удалось обработать изображение")
	}
	if img.Bounds().Dx() < 8 || img.Bounds().Dy() < 8 {
		return fmt.Errorf("изображение слишком маленькое")
	}
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("не удалось сохранить изображение")
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		slog.Warn("image png encode", "error", err, "format", format)
		return fmt.Errorf("не удалось обработать изображение")
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
	pngPath := strings.TrimSuffix(path, filepath.Ext(path)) + "-placeholder.png"
	img := image.NewRGBA(image.Rect(0, 0, 1240, 1754))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	ink := image.NewUniform(color.RGBA{R: 48, G: 76, B: 130, A: 255})
	for i := 0; i < 9; i++ {
		y := 260 + i*105
		width := 850
		if i == 0 {
			width = 620
		}
		draw.Draw(img, image.Rect(180, y, 180+width, y+24), ink, image.Point{}, draw.Src)
	}
	f, err := os.Create(pngPath)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	defer os.Remove(pngPath)
	return ImageToA4PDF(pngPath, path)
}
