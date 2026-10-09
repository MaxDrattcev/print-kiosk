//go:build windows

package executil

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideWindowPreservesProcessFlags(t *testing.T) {
	cmd := exec.Command("powershell")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	HideWindow(cmd)
	if !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags != 0x08000200 {
		t.Fatalf("unexpected process settings: %+v", cmd.SysProcAttr)
	}
}
