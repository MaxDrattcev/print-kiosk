//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// HideWindow prevents background device commands from creating a console or
// briefly taking focus away from the fullscreen kiosk browser.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
