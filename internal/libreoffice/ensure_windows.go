//go:build windows

package libreoffice

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Ensure finds LibreOffice, or installs it when AutoInstall is true.
func Ensure(opt Options) (string, error) {
	if path, err := Find(opt.Configured); err == nil {
		slog.Info("LibreOffice found", "path", path)
		return path, nil
	}

	if !opt.AutoInstall {
		return "", fmt.Errorf("LibreOffice не найден (автоустановка отключена)")
	}

	slog.Info("LibreOffice not found, starting automatic install (needs admin / internet)")
	if err := install(opt.MSIURL); err != nil {
		return "", fmt.Errorf("автоустановка LibreOffice: %w", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if path, err := Find(opt.Configured); err == nil {
			slog.Info("LibreOffice installed", "path", path)
			return path, nil
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("LibreOffice установлен, но soffice.exe всё ещё не найден — перезапустите приложение")
}

func install(msiURL string) error {
	if err := installViaWinget(); err == nil {
		return nil
	} else {
		slog.Warn("winget install failed, trying MSI download", "error", err)
	}
	return installViaMSI(msiURL)
}

func installViaWinget() error {
	winget, err := exec.LookPath("winget")
	if err != nil {
		return fmt.Errorf("winget не найден: %w", err)
	}

	args := []string{
		"install",
		"--id", "TheDocumentFoundation.LibreOffice",
		"-e",
		"--source", "winget",
		"--accept-package-agreements",
		"--accept-source-agreements",
		"--disable-interactivity",
	}
	slog.Info("installing LibreOffice via winget (UAC may appear)")
	if err := runElevated(winget, args...); err != nil {
		cmd := exec.Command(winget, args...)
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return fmt.Errorf("winget: %w (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func installViaMSI(msiURL string) error {
	if msiURL == "" {
		var err error
		msiURL, err = resolveLatestMSIURL()
		if err != nil {
			return err
		}
	}

	tmpDir, err := os.MkdirTemp("", "print-kiosk-lo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	msiPath := filepath.Join(tmpDir, "LibreOffice.msi")
	slog.Info("downloading LibreOffice MSI", "url", msiURL, "dest", msiPath)
	if err := downloadFile(msiURL, msiPath); err != nil {
		return fmt.Errorf("download MSI: %w", err)
	}

	slog.Info("running LibreOffice MSI silent install (UAC may appear)")
	msiexec, err := exec.LookPath("msiexec")
	if err != nil {
		msiexec = `C:\Windows\System32\msiexec.exe`
	}
	args := []string{
		"/i", msiPath,
		"/qn",
		"/norestart",
		"RebootYesNo=No",
		"CREATEDESKTOPLINK=0",
		"REGISTER_ALL_MSO_TYPES=0",
		"ISCHECKFORPRODUCTUPDATES=0",
	}
	if err := runElevated(msiexec, args...); err != nil {
		cmd := exec.Command(msiexec, args...)
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return fmt.Errorf("msiexec: %w (%s)", runErr, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func runElevated(exe string, args ...string) error {
	quotedArgs := make([]string, 0, len(args))
	for _, a := range args {
		quotedArgs = append(quotedArgs, powershellQuote(a))
	}
	ps := fmt.Sprintf(
		`$p = Start-Process -FilePath %s -ArgumentList @(%s) -Verb RunAs -Wait -PassThru; if ($null -eq $p) { exit 1 }; exit $p.ExitCode`,
		powershellQuote(exe),
		strings.Join(quotedArgs, ","),
	)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevated install: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 45 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "print-kiosk/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	if written < 10*1024*1024 {
		return fmt.Errorf("файл слишком маленький (%d bytes), похоже на ошибку загрузки", written)
	}
	slog.Info("LibreOffice MSI downloaded", "bytes", written)
	return nil
}

var versionDirRe = regexp.MustCompile(`href="([0-9]+\.[0-9]+\.[0-9]+)/"`)

func resolveLatestMSIURL() (string, error) {
	const base = "https://download.documentfoundation.org/libreoffice/stable/"
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(base)
	if err != nil {
		return "", fmt.Errorf("list LibreOffice versions: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	matches := versionDirRe.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("не удалось найти версии LibreOffice на %s", base)
	}
	versions := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		v := m[1]
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) < 0
	})
	latest := versions[len(versions)-1]
	url := fmt.Sprintf("%s%s/win/x86_64/LibreOffice_%s_Win_x86-64.msi", base, latest, latest)
	slog.Info("resolved LibreOffice MSI URL", "version", latest, "url", url)
	return url, nil
}

func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			fmt.Sscanf(as[i], "%d", &ai)
		}
		if i < len(bs) {
			fmt.Sscanf(bs[i], "%d", &bi)
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}
