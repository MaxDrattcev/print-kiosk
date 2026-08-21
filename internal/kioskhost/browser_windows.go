//go:build windows

package kioskhost

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procShowWindow               = user32.NewProc("ShowWindow")
	procPostMessage              = user32.NewProc("PostMessageW")
)

const (
	swMinimize = 6
	wmClose    = 0x0010
)

func browserProcessIDs(root uint32) (map[uint32]bool, error) {
	ids := map[uint32]bool{root: true}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	var entries []windows.ProcessEntry32
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err == nil {
		for {
			entries = append(entries, entry)
			if err := windows.Process32Next(snapshot, &entry); err != nil {
				break
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, e := range entries {
			if ids[e.ParentProcessID] && !ids[e.ProcessID] {
				ids[e.ProcessID] = true
				changed = true
			}
		}
	}
	return ids, nil
}

func visitBrowserWindows(action func(hwnd uintptr)) (int, error) {
	pid := browserPID()
	if pid == 0 {
		return 0, fmt.Errorf("браузер терминала не запущен приложением")
	}
	ids, err := browserProcessIDs(pid)
	if err != nil {
		return 0, err
	}
	count := 0
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var owner uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if ids[owner] && visible != 0 {
			action(hwnd)
			count++
		}
		return 1
	})
	result, _, callErr := procEnumWindows.Call(callback, 0)
	if result == 0 && callErr != windows.ERROR_SUCCESS {
		return count, callErr
	}
	return count, nil
}

func MinimizeBrowser() error {
	count, err := visitBrowserWindows(func(hwnd uintptr) { procShowWindow.Call(hwnd, swMinimize) })
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("окно браузера не найдено")
	}
	return nil
}

func CloseBrowser() error {
	_, err := visitBrowserWindows(func(hwnd uintptr) { procPostMessage.Call(hwnd, wmClose, 0, 0) })
	return err
}
