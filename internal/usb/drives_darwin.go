//go:build darwin

package usb

import (
	"fmt"
	"os"
	"strings"
)

var ignoredVolumes = map[string]bool{
	"Macintosh HD":        true,
	"Macintosh HD - Data": true,
}

func ListRemovableDrives() ([]Drive, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil, fmt.Errorf("read /Volumes: %w", err)
	}

	var drives []Drive
	for _, entry := range entries {
		name := entry.Name()
		if ignoredVolumes[name] || strings.HasPrefix(name, ".") {
			continue
		}
		// Concatenate instead of Join to preserve unusual volume names (trailing spaces).
		path := "/Volumes/" + name
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		drives = append(drives, Drive{
			Name:  name,
			Path:  path,
			Label: name,
		})
	}
	return drives, nil
}
