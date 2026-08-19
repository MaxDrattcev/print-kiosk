//go:build !windows

package libreoffice

import "log/slog"

// Ensure checks that LibreOffice is available. Auto-install is Windows-only.
func Ensure(opt Options) (string, error) {
	path, err := Find(opt.Configured)
	if err != nil {
		if opt.AutoInstall {
			slog.Info("LibreOffice auto-install is only supported on Windows")
		}
		return "", err
	}
	return path, nil
}
