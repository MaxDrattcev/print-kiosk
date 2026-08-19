package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func publicURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// addr may be ":8080"
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
					slog.Info("opened browser", "url", url)
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	slog.Warn("server not ready in time, browser not opened", "url", url)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// start is a shell builtin; empty title arg avoids issues with quoted URLs
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
