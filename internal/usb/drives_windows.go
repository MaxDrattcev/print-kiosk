//go:build windows

package usb

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const driveRemovable = 2

func ListRemovableDrives() ([]Drive, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getDriveType := kernel32.NewProc("GetDriveTypeW")
	getVolumeInformation := kernel32.NewProc("GetVolumeInformationW")

	var drives []Drive
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + `:\`
		rootUTF16, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}

		driveType, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(rootUTF16)))
		if driveType != driveRemovable {
			continue
		}

		label := string(letter) + ":"
		var volumeName [261]uint16
		ret, _, _ := getVolumeInformation.Call(
			uintptr(unsafe.Pointer(rootUTF16)),
			uintptr(unsafe.Pointer(&volumeName[0])),
			uintptr(len(volumeName)),
			0, 0, 0, 0, 0,
		)
		if ret != 0 {
			name := windows.UTF16ToString(volumeName[:])
			if strings.TrimSpace(name) != "" {
				label = name
			}
		}

		drives = append(drives, Drive{
			Name:  string(letter) + ":",
			Path:  root,
			Label: label,
		})
	}

	return drives, nil
}
