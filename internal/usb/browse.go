package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var supportedExts = map[string]bool{
	".pdf":  true,
	".doc":  true,
	".docx": true,
	".xls":  true,
	".xlsx": true,
	".ppt":  true,
	".pptx": true,
	".odt":  true,
	".ods":  true,
	".odp":  true,
	".txt":  true,
	".rtf":  true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".bmp":  true,
	".tif":  true,
	".tiff": true,
}

type Listing struct {
	Path    string  `json:"path"`
	Root    string  `json:"root"`
	Parent  string  `json:"parent,omitempty"`
	Label   string  `json:"label"`
	Entries []Entry `json:"entries"`
}

func ListDir(path string) (*Listing, error) {
	drives, err := ListRemovableDrives()
	if err != nil {
		return nil, err
	}
	if len(drives) == 0 {
		return nil, fmt.Errorf("usb drive not found")
	}

	root, ok := findRoot(path, drives)
	if !ok {
		return nil, fmt.Errorf("path is outside usb drives")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var items []Entry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		fi, err := entry.Info()
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			items = append(items, Entry{
				Name:  name,
				Path:  full,
				IsDir: true,
			})
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !supportedExts[ext] {
			continue
		}
		items = append(items, Entry{
			Name:  name,
			Path:  full,
			IsDir: false,
			Size:  fi.Size(),
			Ext:   ext,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	listing := &Listing{
		Path:    path,
		Root:    root.Path,
		Label:   root.Label,
		Entries: items,
	}

	absPath, _ := filepath.Abs(path)
	absRoot, _ := filepath.Abs(root.Path)
	if filepath.Clean(absPath) != filepath.Clean(absRoot) {
		listing.Parent = filepath.Dir(path)
	}

	return listing, nil
}

func findRoot(path string, drives []Drive) (Drive, bool) {
	for _, drive := range drives {
		ok, err := pathWithinRoot(path, drive.Path)
		if err == nil && ok {
			return drive, true
		}
	}
	return Drive{}, false
}

func IsSupportedExt(ext string) bool {
	return supportedExts[strings.ToLower(ext)]
}

// ValidateUSBFile ensures path is a readable file on a currently mounted USB drive.
func ValidateUSBFile(path string) error {
	drives, err := ListRemovableDrives()
	if err != nil {
		return fmt.Errorf("list usb drives: %w", err)
	}
	if _, ok := findRoot(path, drives); !ok {
		return fmt.Errorf("path is outside usb drives")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory")
	}
	if !IsSupportedExt(filepath.Ext(path)) {
		return fmt.Errorf("unsupported file type")
	}
	return nil
}
