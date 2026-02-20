//go:build darwin || linux

package cmd

import (
	"os/exec"
	"syscall"
)

func setProcessDetachedAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
