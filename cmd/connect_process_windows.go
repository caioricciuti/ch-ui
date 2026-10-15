//go:build windows

package cmd

func isProcessRunning(pid int) bool {
	return processExists(pid)
}
