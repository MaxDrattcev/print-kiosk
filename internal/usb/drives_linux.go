//go:build linux

package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ListRemovableDrives() ([]Drive, error) {
	candidates := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join("/media", filepath.Base(home)),
			filepath.Join("/run/media", filepath.Base(home)),
		)
	}
	candidates = append(candidates, "/media", "/mnt")

	seen := map[string]bool{}
	var drives []Drive
	for _, base := range candidates {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(base, entry.Name())
			if seen[path] {
				continue
			}
			seen[path] = true
			drives = append(drives, Drive{
				Name:  entry.Name(),
				Path:  path,
				Label: entry.Name(),
			})
		}
	}
	return drives, nil
}
