package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func pathWithinRoot(target, root string) (bool, error) {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false, fmt.Errorf("resolve path: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("resolve root: %w", err)
	}

	// Preserve unusual volume names (e.g. trailing spaces); only normalize "." / "..".
	absTarget = cleanPathKeepTrailingSpaces(absTarget)
	absRoot = cleanPathKeepTrailingSpaces(absRoot)

	if resolved, err := filepath.EvalSymlinks(absTarget); err == nil {
		absTarget = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}

	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	return true, nil
}

func cleanPathKeepTrailingSpaces(p string) string {
	if p == "" {
		return p
	}
	trailing := ""
	for len(p) > 0 && p[len(p)-1] == ' ' {
		trailing += " "
		p = p[:len(p)-1]
	}
	return filepath.Clean(p) + trailing
}
