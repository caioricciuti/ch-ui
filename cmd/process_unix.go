//go:build !windows

package cmd

import (
	"errors"
	"os"
	"syscall"
)

// processExists reports whether a process with this PID is alive. Signal 0
// checks without delivering anything; EPERM means it exists but belongs to
// another user.
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) && errors.Is(sysErr.Err, syscall.EPERM) {
		return true
	}
	return false
}

// terminateProcess asks a process to shut down gracefully.
func terminateProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}
