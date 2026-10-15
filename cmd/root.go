package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ch-ui",
	Short: "CH-UI - ClickHouse UI and management platform",
	Long:  "CH-UI is a single binary that serves a ClickHouse management platform for local and remote deployments.",
}

func init() {
	loadEnvFile(".env")
}

// startedWithoutArgs is true when ch-ui ran with no arguments on Windows,
// usually a double-click in Explorer: the console window closes as soon as
// the process exits, so errors wait for Enter and desktop says to close the
// window instead of pressing Ctrl+C.
var startedWithoutArgs bool

func Execute() {
	removeReplacedBinary()

	if len(os.Args) == 1 {
		command := noArgsCommand(runtime.GOOS)
		startedWithoutArgs = command == "desktop"
		rootCmd.SetArgs([]string{command})
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if startedWithoutArgs {
			waitForEnter(os.Stdin, os.Stderr)
		}
		os.Exit(1)
	}
}

// noArgsCommand is the command ch-ui runs without arguments: the personal
// desktop mode on Windows, where a double-click is the common start, and
// the server everywhere else.
func noArgsCommand(goos string) string {
	if goos == "windows" {
		return "desktop"
	}
	return "server"
}

// waitForEnter keeps a double-clicked console window open until the user
// has read the error.
func waitForEnter(in io.Reader, out io.Writer) {
	fmt.Fprint(out, "Press Enter to close this window.")
	_, _ = bufio.NewReader(in).ReadString('\n')
}

// loadEnvFile reads a .env file and sets environment variables.
// Existing env vars are NOT overwritten (real env takes precedence).
// Silently does nothing if the file doesn't exist.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		// Don't overwrite existing env vars
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
