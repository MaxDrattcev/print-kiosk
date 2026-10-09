//go:build !windows

package printjob

import "fmt"

func defaultWindowsPrinter() (string, error) {
	return "", fmt.Errorf("определение принтера Windows доступно только на Windows")
}
