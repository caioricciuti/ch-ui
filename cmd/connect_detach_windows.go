//go:build windows

package cmd

import "os/exec"

func setProcessDetachedAttr(cmd *exec.Cmd) {
	// No-op on windows.
}
