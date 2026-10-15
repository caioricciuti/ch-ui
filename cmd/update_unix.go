//go:build !windows

package cmd

import "os"

const elevationHint = "try running with sudo"

// replaceBinary moves the downloaded binary over the current one in a single
// rename. A running process keeps its old inode, so this is safe while the
// server runs.
func replaceBinary(newPath, current string) error {
	return os.Rename(newPath, current)
}

// removeReplacedBinary is a no-op here: replaceBinary leaves nothing behind.
func removeReplacedBinary() {}
