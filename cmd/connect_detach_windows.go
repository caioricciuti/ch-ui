//go:build windows

package cmd

import (
	"os/exec"
	"syscall"
)

const detachedProcess = 0x00000008

// setProcessDetachedAttr starts the child without a console and in its own
// process group, so closing the terminal or pressing Ctrl+C there does not
// stop it.
func setProcessDetachedAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}
