package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/connector/service"
	serverconfig "github.com/caioricciuti/ch-ui/internal/config"
	"github.com/spf13/cobra"
)

var (
	uninstallConfigPath string
	uninstallDBPath     string
	uninstallForce      bool
	uninstallPrintOnly  bool
	uninstallPIDFiles   []string
)

type uninstallPlan struct {
	serverConfigPath string
	databasePath     string
	serverPIDFiles   []string
	connectorPIDFile string
	connectorDir     string
	binaryPath       string // empty where no service binary is installed (Windows)
	// desktopDataDir is the `ch-ui desktop` folder. Uninstall stops a running
	// desktop instance but never deletes this folder: it holds the user's own
	// database and secret key.
	desktopDataDir string
	cleanupPaths   []string
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall CH-UI from this machine",
	Long: `Best-effort local uninstall for CH-UI.
This command stops services/processes, removes local CH-UI files, and prints manual
cleanup commands for anything that still requires privileged shell access.`,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().StringVarP(&uninstallConfigPath, "config", "c", "", "Path to server config file (used to locate database)")
	uninstallCmd.Flags().StringVar(&uninstallDBPath, "db", "", "Override server SQLite database path")
	uninstallCmd.Flags().BoolVar(&uninstallForce, "force", false, "Continue uninstall even if some steps fail")
	uninstallCmd.Flags().BoolVar(&uninstallPrintOnly, "print-only", false, "Only print cleanup commands without executing uninstall")
	uninstallCmd.Flags().StringSliceVar(&uninstallPIDFiles, "pid-file", nil, "Additional server PID file path to stop/remove (repeatable)")

	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	plan := buildUninstallPlan()

	fmt.Println("CH-UI uninstall (best effort)")
	fmt.Printf("Server config: %s\n", plan.serverConfigPath)
	fmt.Printf("Database:      %s\n", plan.databasePath)
	if plan.desktopDataDir != "" {
		fmt.Printf("Desktop data:  %s (kept)\n", plan.desktopDataDir)
	}

	if uninstallPrintOnly {
		printManualUninstallCommands(plan)
		return nil
	}

	var failures []string

	if plan.desktopDataDir != "" {
		if err := stopDesktopInstance(plan.desktopDataDir); err != nil {
			failures = append(failures, err.Error())
		}
	}

	if err := stopDetachedConnectProcess(plan.connectorPIDFile); err != nil {
		failures = append(failures, err.Error())
	}

	for _, pidFile := range plan.serverPIDFiles {
		if err := stopServerByPIDFile(pidFile); err != nil {
			failures = append(failures, err.Error())
		}
	}

	if err := uninstallConnectorService(); err != nil {
		failures = append(failures, err.Error())
	}

	if err := uninstallServerSystemService(); err != nil {
		failures = append(failures, err.Error())
	}

	for _, p := range plan.cleanupPaths {
		removed, err := removePathIfExists(p)
		if err != nil {
			failures = append(failures, fmt.Sprintf("remove %s: %v", p, err))
			continue
		}
		if removed {
			fmt.Printf("Removed %s\n", p)
		}
	}

	printManualUninstallCommands(plan)

	if len(failures) == 0 {
		fmt.Println("Uninstall completed.")
		return nil
	}

	fmt.Println("Uninstall completed with warnings:")
	for _, failure := range failures {
		fmt.Printf("  - %s\n", failure)
	}

	if uninstallForce {
		return nil
	}

	return errors.New("one or more uninstall steps failed (rerun with --force to continue)" +
		"\nUse the manual cleanup commands shown above")
}

func buildUninstallPlan() uninstallPlan {
	cfg := serverconfig.Load(uninstallConfigPath)

	if strings.TrimSpace(uninstallDBPath) != "" {
		cfg.DatabasePath = strings.TrimSpace(uninstallDBPath)
	}

	serverConfigPath := strings.TrimSpace(uninstallConfigPath)
	if serverConfigPath == "" {
		serverConfigPath = serverconfig.DefaultServerConfigPath()
	}

	pidFiles := append([]string{"ch-ui-server.pid", "/var/lib/ch-ui/run/ch-ui-server.pid"}, uninstallPIDFiles...)
	pidFiles = uniqueNonEmpty(pidFiles)

	binaryPath := service.BinaryPath
	if runtime.GOOS == "windows" {
		binaryPath = ""
	}
	desktopDir, _ := desktopDataDir(runtime.GOOS, os.Getenv)

	cleanupPaths := []string{
		binaryPath,
		service.GetConfigDir(),
		serverConfigPath,
		cfg.DatabasePath,
		"ch-ui-server.log",
	}
	cleanupPaths = append(cleanupPaths, pidFiles...)

	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		cleanupPaths = append(cleanupPaths,
			filepath.Join(home, "Library", "LaunchAgents", service.ServiceLabel+".plist"),
			filepath.Join(home, "Library", "Logs", "ch-ui"),
		)
	}

	if runtime.GOOS == "linux" {
		cleanupPaths = append(cleanupPaths,
			"/etc/systemd/system/ch-ui.service",
			"/etc/systemd/system/ch-ui-server.service",
		)
	}

	cleanupPaths = keepDesktopData(uniqueNonEmpty(cleanupPaths), desktopDir, service.GetConfigDir())

	return uninstallPlan{
		serverConfigPath: serverConfigPath,
		databasePath:     cfg.DatabasePath,
		serverPIDFiles:   pidFiles,
		connectorPIDFile: filepath.Join(service.GetConfigDir(), "ch-ui.pid"),
		connectorDir:     service.GetConfigDir(),
		binaryPath:       binaryPath,
		desktopDataDir:   desktopDir,
		cleanupPaths:     cleanupPaths,
	}
}

// keepDesktopData drops every cleanup path that is the desktop data folder,
// lies inside it, or contains it, so no step can remove it by accident. The
// connector folder may sit inside it (Windows: %AppData%\ch-ui\connect) and
// is still removed.
func keepDesktopData(paths []string, desktopDir, connectorDir string) []string {
	if desktopDir == "" {
		return paths
	}
	desk, err := filepath.Abs(desktopDir)
	if err != nil {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err == nil && pathWithin(desk, abs) {
			continue // the folder itself or a parent of it
		}
		if err == nil && pathWithin(abs, desk) && filepath.Clean(p) != filepath.Clean(connectorDir) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// pathWithin reports whether path is dir or inside it.
func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// stopDesktopInstance stops a running `ch-ui desktop` that keeps its state
// in dir. Its data stays.
func stopDesktopInstance(dir string) error {
	pidPath := filepath.Join(dir, desktopPIDName)
	addrPath := filepath.Join(dir, desktopAddrName)
	pid, err := readServerPIDFile(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		removeDesktopState(pidPath, addrPath)
		return nil
	}
	if !processExists(pid) {
		removeDesktopState(pidPath, addrPath)
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("locate desktop process %d: %w", pid, err)
	}
	if err := terminateProcess(proc); err != nil {
		return fmt.Errorf("stop desktop process %d: %w", pid, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			removeDesktopState(pidPath, addrPath)
			fmt.Printf("Stopped ch-ui desktop (PID %d)\n", pid)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for ch-ui desktop process %d to stop", pid)
}

func stopDetachedConnectProcess(pidFile string) error {
	pid, err := readPIDFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read connect pid file %s: %w", pidFile, err)
	}

	if !processExists(pid) {
		_ = os.Remove(pidFile)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("locate connect process %d: %w", pid, err)
	}
	if err := terminateProcess(proc); err != nil {
		return fmt.Errorf("stop connect process %d: %w", pid, err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			_ = os.Remove(pidFile)
			fmt.Printf("Stopped connect process (PID %d)\n", pid)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for connect process %d to stop", pid)
}

func stopServerByPIDFile(pidFile string) error {
	pid, running, err := getRunningServerPID(pidFile)
	if err != nil {
		return fmt.Errorf("inspect server pid file %s: %w", pidFile, err)
	}
	if !running {
		return nil
	}

	stopped, err := stopServer(pidFile, 10*time.Second)
	if err != nil {
		return fmt.Errorf("stop server process %d from %s: %w", pid, pidFile, err)
	}
	if stopped {
		fmt.Printf("Stopped server process (PID %d) from %s\n", pid, pidFile)
	}
	return nil
}

func uninstallConnectorService() error {
	svc := service.New()
	if !svc.IsInstalled() {
		fmt.Println("Connector service is not installed")
		return nil
	}

	fmt.Println("Stopping connector service...")
	_ = svc.Stop()

	fmt.Println("Uninstalling connector service...")
	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("uninstall connector service: %w", err)
	}

	fmt.Println("Connector service uninstalled")
	return nil
}

func uninstallServerSystemService() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	var warnings []string
	steps := [][]string{
		{"systemctl", "stop", "ch-ui-server"},
		{"systemctl", "disable", "ch-ui-server"},
		{"systemctl", "daemon-reload"},
	}

	for _, step := range steps {
		if err := runPrivileged(step[0], step[1:]...); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", strings.Join(step, " "), err))
		}
	}

	if len(warnings) == 0 {
		return nil
	}

	return errors.New(strings.Join(warnings, "; "))
}

func runPrivileged(name string, args ...string) error {
	cmdName := name
	cmdArgs := args
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		cmdArgs = append([]string{name}, args...)
		cmdName = "sudo"
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}

func removePathIfExists(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if path == "/" {
		return false, fmt.Errorf("refusing to remove root path")
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if info.IsDir() {
		if err := os.RemoveAll(path); err != nil {
			return false, err
		}
		return true, nil
	}

	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

func printManualUninstallCommands(plan uninstallPlan) {
	fmt.Println()
	fmt.Println("Manual cleanup commands (run if anything remains):")

	for _, cmd := range manualUninstallCommands(runtime.GOOS, plan) {
		fmt.Printf("  %s\n", cmd)
	}

	if plan.desktopDataDir != "" {
		fmt.Println()
		fmt.Println("Your ch-ui desktop data was kept (database and secret key):")
		fmt.Printf("  %s\n", plan.desktopDataDir)
		fmt.Println("To delete it too, remove that folder yourself:")
		fmt.Printf("  %s\n", removeDirCommand(runtime.GOOS, plan.desktopDataDir))
	}

	fmt.Println()
	fmt.Println("Optional verification:")
	fmt.Println("  ch-ui version")
	if runtime.GOOS != "windows" {
		fmt.Println("  ch-ui service status")
	}
	fmt.Println("  ch-ui server status")
}

func manualUninstallCommands(goos string, plan uninstallPlan) []string {
	if goos == "windows" {
		return manualWindowsUninstallCommands(plan)
	}

	commands := []string{}

	quotedConfig := shellQuote(plan.serverConfigPath)
	quotedDB := shellQuote(plan.databasePath)

	switch goos {
	case "darwin":
		home, _ := os.UserHomeDir()
		launchAgent := filepath.Join(home, "Library", "LaunchAgents", service.ServiceLabel+".plist")
		logDir := filepath.Join(home, "Library", "Logs", "ch-ui")

		commands = append(commands,
			"launchctl unload "+shellQuote(launchAgent)+" 2>/dev/null || true",
			"rm -f "+shellQuote(launchAgent),
			"rm -rf "+shellQuote(plan.connectorDir),
			"rm -rf "+shellQuote(logDir),
			"rm -f "+shellQuote(plan.binaryPath),
			"rm -f "+quotedConfig,
			"rm -f "+quotedDB,
		)
	default:
		commands = append(commands,
			"sudo systemctl stop ch-ui 2>/dev/null || true",
			"sudo systemctl disable ch-ui 2>/dev/null || true",
			"sudo rm -f /etc/systemd/system/ch-ui.service",
			"sudo systemctl stop ch-ui-server 2>/dev/null || true",
			"sudo systemctl disable ch-ui-server 2>/dev/null || true",
			"sudo rm -f /etc/systemd/system/ch-ui-server.service",
			"sudo systemctl daemon-reload",
			"sudo rm -rf "+shellQuote(plan.connectorDir),
			"sudo rm -f "+shellQuote(plan.binaryPath),
			"sudo rm -f "+quotedConfig,
			"sudo rm -f "+quotedDB,
		)
	}

	if len(plan.serverPIDFiles) > 0 {
		var quoted []string
		for _, p := range plan.serverPIDFiles {
			quoted = append(quoted, shellQuote(p))
		}
		commands = append(commands, "rm -f "+strings.Join(quoted, " "))
	}
	commands = append(commands,
		"rm -f "+shellQuote("ch-ui-server.log"),
	)

	return commands
}

// manualWindowsUninstallCommands are PowerShell commands. Windows has no
// service, systemd or sudo step: ch-ui service is not supported there.
func manualWindowsUninstallCommands(plan uninstallPlan) []string {
	commands := []string{
		removeDirCommand("windows", plan.connectorDir),
	}
	files := []string{plan.serverConfigPath, plan.databasePath}
	files = append(files, plan.serverPIDFiles...)
	files = append(files, "ch-ui-server.log")
	for _, f := range uniqueNonEmpty(files) {
		commands = append(commands, "Remove-Item -Force -ErrorAction SilentlyContinue "+psQuote(f))
	}
	return commands
}

// removeDirCommand is the shell command that deletes dir recursively.
func removeDirCommand(goos, dir string) string {
	if goos == "windows" {
		return "Remove-Item -Recurse -Force -ErrorAction SilentlyContinue " + psQuote(dir)
	}
	return "rm -rf " + shellQuote(dir)
}

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func uniqueNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
