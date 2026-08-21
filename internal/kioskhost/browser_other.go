//go:build !windows

package kioskhost

import "fmt"

func MinimizeBrowser() error {
	return fmt.Errorf("сворачивание поддерживается на Windows")
}
func CloseBrowser() error { return nil }
