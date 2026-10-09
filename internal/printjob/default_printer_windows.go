//go:build windows

package printjob

import (
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

// Read the default queue directly, without starting PowerShell or querying WMI.
func defaultWindowsPrinter() (string, error) {
	proc := windows.NewLazySystemDLL("winspool.drv").NewProc("GetDefaultPrinterW")
	var size uint32
	proc.Call(0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return "", fmt.Errorf("принтер Windows по умолчанию не задан; выберите принтер в настройках киоска")
	}
	buffer := make([]uint16, size)
	result, _, err := proc.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if result == 0 {
		return "", fmt.Errorf("не удалось определить принтер Windows: %w", err)
	}
	name := windows.UTF16ToString(buffer)
	if name == "" {
		return "", fmt.Errorf("принтер Windows по умолчанию не задан")
	}
	return name, nil
}
