package printjob

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var (
	ErrPaperOut = errors.New("printer is out of paper")
	ErrPaperJam = errors.New("printer has a paper jam")
)

// IsPaperOut reports whether a printer error explicitly indicates that the
// device has run out of paper. The text fallback also covers errors returned
// directly by printer utilities and drivers outside the Windows spooler.
func IsPaperOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrPaperOut) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"paperout", "paper out", "out of paper", "no paper", "нет бумаги", "закончилась бумага"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// IsPaperJam reports whether a printer error explicitly indicates a paper
// jam. A jam blocks the device but does not change the stored paper balance.
func IsPaperJam(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrPaperJam) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"paperjam", "paper jam", "jammed", "замятие", "замята бумага"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

type PrintOptions struct {
	Color       bool
	Duplex      bool
	Copies      int
	Orientation string
	PageRange   string
	Scale       string
}

func (s *Service) Print(job *Job, opt PrintOptions) error {
	if job == nil {
		return fmt.Errorf("job is nil")
	}
	return s.PrintFile(job.PreviewPath, opt)
}

// PrintFile sends an existing PDF/image to the configured printer.
func (s *Service) PrintFile(filePath string, opt PrintOptions) error {
	if filePath == "" {
		return fmt.Errorf("нет файла для печати")
	}
	if opt.Copies < 1 {
		opt.Copies = 1
	}

	if s.dryRun {
		slog.Info("print dry-run", "file", filePath, "copies", opt.Copies, "color", opt.Color, "duplex", opt.Duplex, "printer", s.printerName)
		return nil
	}

	switch runtime.GOOS {
	case "windows":
		return s.printWindows(filePath, opt)
	default:
		return printUnix(filePath, opt)
	}
}

func printUnix(filePath string, opt PrintOptions) error {
	if bin, err := exec.LookPath("lp"); err == nil {
		return printLP(bin, filePath, opt)
	}
	if bin, err := exec.LookPath("lpr"); err == nil {
		return printLPR(bin, filePath, opt)
	}
	return fmt.Errorf("команда печати не найдена (lp/lpr)")
}

func printLP(bin, filePath string, opt PrintOptions) error {
	args := []string{
		"-n", strconv.Itoa(opt.Copies),
		"-o", "media=A4",
		"-o", "PageSize=A4",
		"-o", "CNPdeUseJobAccount=False",
		"-o", "CNAuthenticate=False",
		"-o", "CNUseSecuredPrint=False",
		"-o", "CNJobExecMode=print",
	}
	if opt.Orientation == "landscape" {
		args = append(args, "-o", "orientation-requested=4")
	} else if opt.Orientation == "portrait" {
		args = append(args, "-o", "orientation-requested=3")
	}
	if strings.TrimSpace(opt.PageRange) != "" {
		args = append(args, "-P", opt.PageRange)
	}
	if opt.Scale == "actual" {
		args = append(args, "-o", "scaling=100")
	} else {
		args = append(args, "-o", "fit-to-page")
	}
	if opt.Duplex {
		sides, canon := duplexBinding(opt.Orientation)
		args = append(args, "-o", "sides="+sides, "-o", "CNDuplex="+canon)
	} else {
		args = append(args, "-o", "sides=one-sided", "-o", "CNDuplex=None")
	}
	args = append(args, filePath)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("печать lp: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func printLPR(bin, filePath string, opt PrintOptions) error {
	args := []string{"-#", strconv.Itoa(opt.Copies), "-o", "media=A4"}
	if opt.Orientation == "landscape" {
		args = append(args, "-o", "landscape")
	}
	if strings.TrimSpace(opt.PageRange) != "" {
		args = append(args, "-o", "page-ranges="+opt.PageRange)
	}
	if opt.Scale == "actual" {
		args = append(args, "-o", "scaling=100")
	} else {
		args = append(args, "-o", "fit-to-page")
	}
	if opt.Duplex {
		sides, _ := duplexBinding(opt.Orientation)
		args = append(args, "-o", "sides="+sides)
	} else {
		args = append(args, "-o", "sides=one-sided")
	}
	args = append(args, filePath)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("печать lpr: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Service) printWindows(filePath string, opt PrintOptions) error {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("путь файла: %w", err)
	}

	spoolPrinter, err := resolveWindowsPrinter(s.printerName)
	if err != nil {
		return err
	}

	if sumatra := resolveSumatra(s.sumatraPath); sumatra != "" {
		return monitorWindowsPrint(spoolPrinter, abs, func() error {
			return printSumatra(sumatra, abs, s.printerName, opt)
		})
	}

	slog.Warn("SumatraPDF не найден, печать через ассоциацию Windows (менее надёжно)")
	if err := monitorWindowsPrint(spoolPrinter, abs, func() error {
		return printWindowsShell(abs, s.printerName, opt)
	}); err != nil {
		slog.Warn("windows shell print failed", "error", err)
		return fmt.Errorf("не найден SumatraPDF. Скачайте SumatraPDF и положите SumatraPDF.exe в папку с киоском")
	}
	return nil
}

func printSumatra(bin, filePath, printer string, opt PrintOptions) error {
	// fit — вписать страницу в лист. noscale даёт «только уголок», если PDF не A4.
	scale := "fit"
	if opt.Scale == "actual" {
		scale = "noscale"
	}
	settings := []string{scale, "paper=A4"}
	if strings.TrimSpace(opt.PageRange) != "" {
		settings = append(settings, opt.PageRange)
	}
	if opt.Orientation == "landscape" {
		settings = append(settings, "landscape")
	} else if opt.Orientation == "portrait" {
		settings = append(settings, "portrait")
	}
	if opt.Duplex {
		_, _, sumatra := duplexBindingSettings(opt.Orientation)
		settings = append(settings, sumatra)
	} else {
		settings = append(settings, "simplex")
	}
	if opt.Color {
		settings = append(settings, "color")
	} else {
		settings = append(settings, "monochrome")
	}
	if opt.Copies > 1 {
		settings = append(settings, fmt.Sprintf("%dx", opt.Copies))
	}

	args := []string{"-silent", "-exit-when-done"}
	if strings.TrimSpace(printer) != "" {
		args = append(args, "-print-to", printer)
	} else {
		args = append(args, "-print-to-default")
	}
	args = append(args, "-print-settings", strings.Join(settings, ","), filePath)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("печать SumatraPDF: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	slog.Info("printed via SumatraPDF", "file", filePath, "printer", printer, "copies", opt.Copies)
	return nil
}

// duplexBindingSettings implements book-style duplex printing: portrait
// sheets turn over their long edge, while landscape sheets turn over their
// short edge so the reverse side remains upright.
func duplexBindingSettings(orientation string) (cups, canon, sumatra string) {
	if normalizeOrientation(orientation) == "landscape" {
		return "two-sided-short-edge", "DuplexTop", "duplexshort"
	}
	return "two-sided-long-edge", "DuplexFront", "duplexlong"
}

func duplexBinding(orientation string) (cups, canon string) {
	cups, canon, _ = duplexBindingSettings(orientation)
	return cups, canon
}

func printWindowsShell(filePath, printer string, opt PrintOptions) error {
	escaped := strings.ReplaceAll(filePath, "'", "''")
	printerEsc := strings.ReplaceAll(printer, "'", "''")

	var ps string
	if strings.TrimSpace(printer) != "" {
		ps = fmt.Sprintf(
			`$file = '%s'; $printer = '%s'; for ($i = 0; $i -lt %d; $i++) { Start-Process -FilePath $file -Verb PrintTo -ArgumentList $printer -WindowStyle Hidden -Wait }`,
			escaped, printerEsc, opt.Copies,
		)
	} else {
		ps = fmt.Sprintf(
			`$file = '%s'; for ($i = 0; $i -lt %d; $i++) { Start-Process -FilePath $file -Verb Print -WindowStyle Hidden -Wait }`,
			escaped, opt.Copies,
		)
	}

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("печать windows: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	slog.Info("printed via Windows shell", "file", filePath, "printer", printer, "copies", opt.Copies)
	return nil
}

func resolveSumatra(configured string) string {
	candidates := []string{}
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "SumatraPDF.exe"),
			filepath.Join(dir, "tools", "SumatraPDF.exe"),
		)
		if matches, err := filepath.Glob(filepath.Join(dir, "SumatraPDF*.exe")); err == nil {
			for _, m := range matches {
				base := strings.ToLower(filepath.Base(m))
				if strings.Contains(base, "install") || strings.Contains(base, "setup") || strings.Contains(base, "uninstall") {
					continue
				}
				candidates = append(candidates, m)
			}
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates,
			filepath.Join(local, "SumatraPDF", "SumatraPDF.exe"),
			filepath.Join(local, "Programs", "SumatraPDF", "SumatraPDF.exe"),
		)
	}
	candidates = append(candidates,
		`C:\Program Files\SumatraPDF\SumatraPDF.exe`,
		`C:\Program Files (x86)\SumatraPDF\SumatraPDF.exe`,
	)
	if p, err := exec.LookPath("SumatraPDF.exe"); err == nil {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("SumatraPDF"); err == nil {
		candidates = append(candidates, p)
	}

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// FindSumatra returns the resolved SumatraPDF path, or empty if not found.
func FindSumatra(configured string) string {
	return resolveSumatra(configured)
}
