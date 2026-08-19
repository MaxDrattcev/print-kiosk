package usb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SaveFileToDrive copies srcPath onto a removable drive as fileName.
// drivePath must be the root of a currently mounted USB volume (or a folder under it).
func SaveFileToDrive(drivePath, fileName, srcPath string) (string, error) {
	drives, err := ListRemovableDrives()
	if err != nil {
		return "", fmt.Errorf("list usb drives: %w", err)
	}
	if len(drives) == 0 {
		return "", fmt.Errorf("флешка не найдена")
	}

	root, ok := findRoot(drivePath, drives)
	if !ok {
		return "", fmt.Errorf("путь вне USB-накопителя")
	}

	// Always write to the drive root for kiosk simplicity.
	destDir := root.Path
	base := filepath.Base(fileName)
	if base == "." || base == "" || base == string(filepath.Separator) {
		return "", fmt.Errorf("некорректное имя файла")
	}
	dest := uniquePath(filepath.Join(destDir, base))

	if err := copyFileContents(srcPath, dest); err != nil {
		return "", fmt.Errorf("запись на флешку: %w", err)
	}
	return dest, nil
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return fmt.Sprintf("%s_%d%s", stem, os.Getpid(), ext)
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
