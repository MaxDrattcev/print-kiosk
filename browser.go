package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func publicURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			return "http://127.0.0.1" + addr + "/"
		}
		return "http://127.0.0.1:8080/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/", host, port)
}

func openBrowserWhenReady(url string) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url + "healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if err := openBrowser(url); err != nil {
					slog.Warn("failed to open browser", "url", url, "error", err)
				} else {
					slog.Info("opened kiosk browser", "url", url)
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	slog.Warn("server not ready in time, browser not opened", "url", url)
}

func openBrowser(url string) error {
	profile := filepath.Join("data", "browser-kiosk")
	if abs, err := filepath.Abs(profile); err == nil {
		profile = abs
	}
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return fmt.Errorf("kiosk browser profile: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		if exe, edge := findWindowsChromium(); exe != "" {
			args := chromiumKioskArgs(url, profile, edge)
			cmd := exec.Command(exe, args...)
			return cmd.Start()
		}
		return exec.Command("cmd", "/c", "start", "", url).Start()
	case "darwin":
		if exe := findDarwinChrome(); exe != "" {
			args := chromiumKioskArgs(url, profile, false)
			return exec.Command(exe, args...).Start()
		}
		return exec.Command("open", url).Start()
	default:
		for _, name := range []string{"google-chrome", "chromium-browser", "chromium", "microsoft-edge"} {
			if path, err := exec.LookPath(name); err == nil {
				return exec.Command(path, chromiumKioskArgs(url, profile, false)...).Start()
			}
		}
		return exec.Command("xdg-open", url).Start()
	}
}

func chromiumKioskArgs(url, profile string, edge bool) []string {
	args := []string{
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-session-crashed-bubble",
		"--disable-infobars",
		"--disable-translate",
		"--kiosk",
		"--start-fullscreen",
	}
	if edge {
		args = append(args, "--edge-kiosk-type=fullscreen")
	}
	return append(args, url)
}

func findWindowsChromium() (exe string, edge bool) {
	var candidates []string
	for _, root := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
		)
	}
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, strings.HasSuffix(strings.ToLower(path), "msedge.exe")
		}
	}
	if path, err := exec.LookPath("msedge"); err == nil {
		return path, true
	}
	if path, err := exec.LookPath("chrome"); err == nil {
		return path, false
	}
	return "", false
}

func findDarwinChrome() string {
	for _, path := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return ""
}
