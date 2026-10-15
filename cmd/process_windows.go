//go:build windows

package cmd

import (
	"errors"
	"os"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// processExists reports whether a process with this PID is alive. Windows
// has no signal 0, so open the process and check that it has not exited.
// Access denied means it exists but belongs to another user.
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// terminateProcess stops a process. Windows cannot deliver SIGTERM to a
// detached process, so this ends it immediately. SQLite keeps the database
// consistent through its journal.
func terminateProcess(proc *os.Process) error {
	return proc.Kill()
}
