//go:build windows

package connector

import (
	"syscall"
	"unsafe"
)

// getDiskInfo returns total and free disk space for the C: drive
func getDiskInfo() (total, free uint64) {
	kernel32 := syscall.MustLoadDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.MustFindProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64

	path, _ := syscall.UTF16PtrFromString("C:\\")
	r, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if r == 0 {
		return 0, 0
	}

	return totalNumberOfBytes, totalNumberOfFreeBytes
}
