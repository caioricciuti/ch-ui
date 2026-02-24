package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/version"
	"github.com/spf13/cobra"
)

const (
	releasesURL = "https://api.github.com/repos/caioricciuti/ch-ui/releases/latest"
)

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update CH-UI to the latest version",
	Long:  "Download the latest CH-UI release from GitHub and replace the current binary.",
	RunE:  runUpdate,
}

var (
	updateRestartServer bool
	updatePIDFile       string
	updateStopTimeout   time.Duration
)

func init() {
	updateCmd.Flags().BoolVar(&updateRestartServer, "restart-server", true, "Automatically restart a running CH-UI server after update")
	updateCmd.Flags().StringVar(&updatePIDFile, "pid-file", "ch-ui-server.pid", "Server PID file path used to detect/restart a running server")
	updateCmd.Flags().DurationVar(&updateStopTimeout, "stop-timeout", 10*time.Second, "Graceful stop timeout used when restarting after update")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	// Resolve current binary path
	currentBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine current binary path: %w", err)
	}
	currentBin, err = filepath.EvalSymlinks(currentBin)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}

	var runningPID int
	var running bool
	restartArgs := []string{"server", "--pid-file", updatePIDFile}
	if updateRestartServer {
		runningPID, running, err = getRunningServerPID(updatePIDFile)
		if err != nil {
			return fmt.Errorf("failed to inspect server status via PID file %q: %w", updatePIDFile, err)
		}
		if running {
			restartArgs = detectServerRestartArgs(runningPID, updatePIDFile)
			fmt.Printf("Detected running CH-UI server (PID %d); it will be restarted after update.\n", runningPID)
		}
	}

	// Check write permissions
	dir := filepath.Dir(currentBin)
	if err := checkWritable(dir); err != nil {
		return fmt.Errorf("cannot write to %s: %w (try running with sudo)", dir, err)
	}

	fmt.Printf("Current version: %s\n", version.Version)
	fmt.Println("Checking for updates...")

	// Fetch latest release info
	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestTag := release.TagName
	if latestTag == version.Version {
		fmt.Printf("Already up to date (%s)\n", version.Version)
		return nil
	}

	fmt.Printf("New version available: %s → %s\n", version.Version, latestTag)

	// Find the right asset for this platform
	assetName := fmt.Sprintf("ch-ui-%s-%s", runtime.GOOS, runtime.GOARCH)
	var assetURL string
	var checksumsURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			assetURL = a.BrowserDownloadURL
		}
		if a.Name == "checksums.txt" {
			checksumsURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no release asset found for %s/%s (expected %s)", runtime.GOOS, runtime.GOARCH, assetName)
	}

	// Download checksums
	var expectedHash string
	if checksumsURL != "" {
		expectedHash, err = fetchExpectedChecksum(checksumsURL, assetName)
		if err != nil {
			fmt.Printf("Warning: could not verify checksum: %v\n", err)
		}
	}

	// Download binary to temp file in the same directory (for atomic rename)
	tmpPath := currentBin + ".update-tmp"
	fmt.Printf("Downloading %s...\n", assetName)
	if err := downloadFile(assetURL, tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to download update: %w", err)
	}

	// Verify checksum
	if expectedHash != "" {
		actualHash, err := fileSHA256(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to compute checksum: %w", err)
		}
		if actualHash != expectedHash {
			os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
		}
		fmt.Println("Checksum verified ✓")
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpPath, currentBin); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("Updated successfully: %s → %s\n", version.Version, latestTag)

	if !updateRestartServer || !running {
		fmt.Println("Restart CH-UI to use the new version.")
		return nil
	}

	fmt.Printf("Restarting CH-UI server (PID %d)...\n", runningPID)
	stopped, err := stopServer(updatePIDFile, updateStopTimeout)
	if err != nil {
		return fmt.Errorf("binary updated to %s but failed to stop running server: %w", latestTag, err)
	}
	if !stopped {
		return fmt.Errorf("binary updated to %s but could not confirm server stop; run `ch-ui server restart --detach --pid-file %s`", latestTag, updatePIDFile)
	}

	prevPIDFile := serverPIDFile
	serverPIDFile = updatePIDFile
	pid, logPath, err := startDetachedServer(restartArgs)
	serverPIDFile = prevPIDFile
	if err != nil {
		return fmt.Errorf("binary updated to %s and server stopped, but failed to start it again: %w", latestTag, err)
	}

	fmt.Printf("CH-UI server restarted in background (PID %d)\n", pid)
	if logPath != "" {
		fmt.Printf("Logs: %s\n", logPath)
	}
	fmt.Println("Update complete and running the new version.")
	return nil
}

func detectServerRestartArgs(pid int, pidFile string) []string {
	args, err := readProcessArgs(pid)
	if err != nil {
		return []string{"server", "--pid-file", pidFile}
	}
	sanitized := sanitizeServerStartArgs(args, pidFile)
	if len(sanitized) == 0 {
		return []string{"server", "--pid-file", pidFile}
	}
	return sanitized
}

func readProcessArgs(pid int) ([]string, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("unsupported OS for process args inspection: %s", runtime.GOOS)
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty cmdline for PID %d", pid)
	}
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 {
			continue // executable path
		}
		if strings.TrimSpace(part) == "" {
			continue
		}
		out = append(out, part)
	}
	return out, nil
}

func sanitizeServerStartArgs(args []string, pidFile string) []string {
	// Safe fallback that keeps behavior predictable.
	out := []string{"server"}

	// Expect args from the running server process to start with "server"
	// (or "server start" in older/manual invocations).
	i := 0
	if len(args) > 0 && args[0] == "server" {
		i = 1
		if i < len(args) && args[i] == "start" {
			i++
		}
	}

	for i < len(args) {
		a := args[i]

		switch {
		case a == "server" || a == "start" || a == "stop" || a == "status" || a == "restart":
			i++
		case a == "--detach" || a == "-h" || a == "--help":
			i++
		case a == "--dev":
			out = append(out, a)
			i++
		case a == "--port" || a == "-p" ||
			a == "--config" || a == "-c" ||
			a == "--clickhouse-url" ||
			a == "--connection-name" ||
			a == "--pid-file" ||
			a == "--stop-timeout":
			if i+1 < len(args) {
				out = append(out, a, args[i+1])
				i += 2
				continue
			}
			i++
		case strings.HasPrefix(a, "--port=") ||
			strings.HasPrefix(a, "--config=") ||
			strings.HasPrefix(a, "--clickhouse-url=") ||
			strings.HasPrefix(a, "--connection-name=") ||
			strings.HasPrefix(a, "--pid-file=") ||
			strings.HasPrefix(a, "--stop-timeout="):
			out = append(out, a)
			i++
		default:
			i++
		}
	}

	if !hasFlag(out, "--pid-file") {
		out = append(out, "--pid-file", pidFile)
	}
	return out
}

func hasFlag(args []string, longName string) bool {
	for _, a := range args {
		if a == longName || strings.HasPrefix(a, longName+"=") {
			return true
		}
	}
	return false
}

func fetchLatestRelease() (*ghRelease, error) {
	resp, err := http.Get(releasesURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}
	return &release, nil
}

func fetchExpectedChecksum(url, assetName string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", assetName)
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkWritable(dir string) error {
	tmp := filepath.Join(dir, ".ch-ui-update-check")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(tmp)
}
