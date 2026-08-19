package libreoffice

import (
	"fmt"
	"os"
	"os/exec"
)

// Find returns an absolute or PATH-resolved soffice binary.
func Find(configured string) (string, error) {
	candidates := []string{}
	if configured != "" {
		candidates = append(candidates, configured)
	}
	candidates = append(candidates,
		"soffice",
		"soffice.exe",
		"/Applications/LibreOffice.app/Contents/MacOS/soffice",
		`C:\Program Files\LibreOffice\program\soffice.exe`,
		`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
	)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("LibreOffice не найден")
}
