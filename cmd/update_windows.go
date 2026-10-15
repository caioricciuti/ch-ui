//go:build windows

package cmd

import (
	"os"
	"path/filepath"
)

const elevationHint = "try again from a terminal opened with Run as administrator"

// replacedSuffix marks the previous binary moved aside by an update.
const replacedSuffix = ".old"

// replaceBinary swaps in the downloaded binary. Windows refuses to overwrite
// or delete a running .exe but allows renaming it, so the current binary is
// moved to <name>.old first and removed on the next start.
func replaceBinary(newPath, current string) error {
	old := current + replacedSuffix
	_ = os.Remove(old)
	if err := os.Rename(current, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, current); err != nil {
		_ = os.Rename(old, current)
		return err
	}
	return nil
}

// removeReplacedBinary deletes the binary a previous update moved aside. It
// fails quietly while that old binary is still running.
func removeReplacedBinary() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	_ = os.Remove(exe + replacedSuffix)
}
