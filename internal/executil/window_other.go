//go:build !windows

package executil

import "os/exec"

func HideWindow(cmd *exec.Cmd) {}
