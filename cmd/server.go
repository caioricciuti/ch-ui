package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	"github.com/caioricciuti/ch-ui/internal/license"
	"github.com/caioricciuti/ch-ui/internal/server"
	"github.com/caioricciuti/ch-ui/internal/version"
	"github.com/spf13/cobra"
)

// FrontendFS holds the embedded frontend filesystem, set by main before Execute().
var FrontendFS fs.FS

var (
	serverPort           int
	devMode              bool
	serverClickHouse     string
	serverConnectionName string
	serverAllowLoginURL  bool
	serverTrustedProxies string
	serverDetach         bool
	serverConfig         string
	serverPIDFile        string
	serverStopTimeout    time.Duration
	restartDetach        bool
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the CH-UI server",
	Long:  "Start the CH-UI HTTP server that serves the API, frontend, and tunnel gateway.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		serverPIDFile = resolvePIDFile(serverPIDFile)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer(cmd)
	},
}

var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the CH-UI server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer(cmd)
	},
}

var serverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the CH-UI server",
	RunE: func(cmd *cobra.Command, args []string) error {
		stopped, err := stopServer(serverPIDFile, serverStopTimeout)
		if err != nil {
			return err
		}
		if stopped {
			fmt.Println("CH-UI server stopped")
		}
		return nil
	},
}

var serverStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show CH-UI server status",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, running, err := getRunningServerPID(serverPIDFile)
		if err != nil {
			return err
		}
		if running {
			fmt.Printf("CH-UI server is running (PID %d)\n", pid)
			fmt.Printf("PID file: %s\n", serverPIDFile)
			return nil
		}

		addr := fmt.Sprintf("127.0.0.1:%d", serverPort)
		if isTCPPortOpen(addr) {
			fmt.Printf("CH-UI server PID file not found, but port %d is in use.\n", serverPort)
			fmt.Printf("Another process may be listening on %s.\n", addr)
			return nil
		}

		fmt.Println("CH-UI server is not running")
		return nil
	},
}

var serverRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the CH-UI server",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := stopServer(serverPIDFile, serverStopTimeout)
		if err != nil {
			return err
		}

		if restartDetach {
			startArgs := buildServerStartArgs(cmd)
			pid, logPath, err := startDetachedServer(startArgs)
			if err != nil {
				return fmt.Errorf("failed to restart in background: %w", err)
			}
			fmt.Printf("CH-UI server restarted in background (PID %d)\n", pid)
			if logPath != "" {
				fmt.Printf("Logs: %s\n", logPath)
			}
			return nil
		}

		serverDetach = false
		return runServer(cmd)
	},
}

func init() {
	pf := serverCmd.PersistentFlags()
	pf.IntVarP(&serverPort, "port", "p", 3488, "Port to listen on")
	pf.BoolVar(&devMode, "dev", false, "Enable development mode (proxy to Vite)")
	pf.StringVar(&serverClickHouse, "clickhouse-url", "", "Local ClickHouse HTTP URL for the embedded connection")
	pf.StringVar(&serverConnectionName, "connection-name", "", "Display name for the embedded local connection")
	pf.BoolVar(&serverAllowLoginURL, "allow-login-url", false, "Allow signing in with a ClickHouse URL typed on the login page (the server connects to it; trusted networks only)")
	pf.StringVar(&serverTrustedProxies, "trusted-proxies", "", "Comma-separated IPs or CIDRs of reverse proxies whose X-Forwarded-For is trusted (default: loopback, private and link-local ranges; \"none\" trusts no proxy)")
	pf.StringVarP(&serverConfig, "config", "c", "", "Path to config file")
	pf.StringVar(&serverPIDFile, "pid-file", "ch-ui-server.pid", "Path to server PID file")
	pf.DurationVar(&serverStopTimeout, "stop-timeout", 10*time.Second, "Graceful stop timeout")

	serverCmd.Flags().BoolVar(&serverDetach, "detach", false, "Run server in background")
	serverStartCmd.Flags().BoolVar(&serverDetach, "detach", false, "Run server in background")

	serverRestartCmd.Flags().BoolVar(&restartDetach, "detach", true, "Run restarted server in background")

	serverCmd.AddCommand(serverStartCmd, serverStopCmd, serverStatusCmd, serverRestartCmd)
	rootCmd.AddCommand(serverCmd)
}

func runServer(cmd *cobra.Command) error {
	if serverDetach {
		startArgs := buildServerStartArgs(cmd)
		pid, logPath, err := startDetachedServer(startArgs)
		if err != nil {
			return fmt.Errorf("failed to start in background: %w", err)
		}
		fmt.Printf("CH-UI server started in background (PID %d)\n", pid)
		if logPath != "" {
			fmt.Printf("Logs: %s\n", logPath)
		}
		return nil
	}

	if err := preparePIDFileForStart(serverPIDFile); err != nil {
		return err
	}
	if err := writeServerPIDFile(serverPIDFile, os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file %q: %w", serverPIDFile, err)
	}
	defer cleanupServerPIDFile(serverPIDFile, os.Getpid())

	// Load configuration
	cfg := config.Load(serverConfig)

	// Override with flags if provided
	if cmd.Flags().Changed("port") {
		cfg.Port = serverPort
	}
	if cmd.Flags().Changed("clickhouse-url") {
		cfg.ClickHouseURL = strings.TrimSpace(serverClickHouse)
	}
	if cmd.Flags().Changed("connection-name") {
		cfg.ConnectionName = strings.TrimSpace(serverConnectionName)
	}
	if cmd.Flags().Changed("allow-login-url") {
		cfg.AllowLoginURL = serverAllowLoginURL
	}
	if cmd.Flags().Changed("trusted-proxies") {
		cfg.TrustedProxies = config.ParseTrustedProxies(serverTrustedProxies)
	}
	// --dev flag is the authority for dev mode in the server command.
	// Without it, always serve the embedded frontend (production mode).
	cfg.DevMode = devMode

	// Setup structured logging
	logLevel := slog.LevelInfo
	if cfg.DevMode {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("Starting CH-UI server",
		"version", version.Version,
		"port", cfg.Port,
		"dev", cfg.DevMode,
	)
	if cfg.AllowLoginURL {
		slog.Warn("Sign-in with a ClickHouse URL is enabled: anyone who can reach the login page can make this server connect to any ClickHouse URL it can reach. Enable only on trusted networks.")
	}
	trustedProxies, err := cfg.TrustedProxyPrefixes()
	if err != nil {
		return fmt.Errorf("invalid trusted_proxies: %w", err)
	}
	switch {
	case len(trustedProxies) == 0:
		slog.Info("Trusting no reverse proxy: X-Forwarded-For is ignored, every request is attributed to its TCP peer")
	case cfg.TrustedProxies == nil:
		slog.Info("Trusting X-Forwarded-For from loopback, private and link-local addresses (default); set trusted_proxies to narrow it", "ranges", fmt.Sprint(trustedProxies))
	default:
		slog.Info("Trusting X-Forwarded-For from configured reverse proxies", "ranges", fmt.Sprint(trustedProxies))
	}

	secretSource, err := config.EnsureAppSecretKey(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize app secret key: %w", err)
	}
	if secretSource == config.SecretKeySourceGenerated {
		slog.Warn("APP_SECRET_KEY was not configured; generated a persisted secret key",
			"path", config.AppSecretKeyPath(cfg.DatabasePath))
	} else if secretSource == config.SecretKeySourceFile {
		slog.Info("Loaded persisted app secret key",
			"path", config.AppSecretKeyPath(cfg.DatabasePath))
	}

	// Initialize database
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	slog.Info("Database initialized", "path", cfg.DatabasePath)

	// Role overrides are keyed per SSO person since v2.13.2. An override on an
	// SSO service account used to apply to every SSO person; now it only
	// applies to password logins as that account, so tell the admin.
	if stale, err := db.SSOServiceAccountRoleOverrides(); err != nil {
		slog.Warn("Could not check role overrides on SSO service accounts", "error", err)
	} else {
		for _, r := range stale {
			slog.Warn("Role override on an SSO service account no longer applies to SSO users; set roles per person in Admin, Users",
				"service_account", r.Username, "role", r.Role)
		}
	}

	// License precedence: CHUI_LICENSE_FILE > CHUI_LICENSE > database. The
	// environment path serves config-as-code deploys (Kubernetes Secrets,
	// Terraform); the database path serves licenses activated in the UI. An
	// invalid environment license is ignored with a warning so a bad secret
	// never blocks startup.
	if lic, src := config.LicenseFromEnv(); lic != "" {
		if info := license.ValidateLicense(lic); info.Valid {
			cfg.LicenseJSON = lic
			slog.Info("License loaded", "source", src, "edition", info.Edition, "expires_at", info.ExpiresAt)
		} else {
			slog.Warn("Ignoring invalid or expired license from environment", "source", src)
		}
	}
	if cfg.LicenseJSON == "" {
		if stored, err := db.GetSetting("license_json"); err == nil && stored != "" {
			cfg.LicenseJSON = stored
			slog.Info("License loaded from database")
		}
	}

	// Connector manager runs one in-process connector per direct connection.
	agents := embedded.NewManager(db, cfg.Port)

	// Create and start server
	srv := server.New(cfg, db, FrontendFS, agents)

	// Launch connectors (embedded connection from config + any direct
	// connections created at runtime).
	if err := agents.Start(cfg.ClickHouseURL, cfg.ConnectionName); err != nil {
		slog.Warn("Failed to start direct connectors", "error", err)
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	select {
	case err := <-errCh:
		agents.StopAll()
		return err
	case <-ctx.Done():
		slog.Info("Shutting down server...")
		agents.StopAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverStopTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func buildServerStartArgs(cmd *cobra.Command) []string {
	args := []string{"server"}
	if cmd.Flags().Changed("port") {
		args = append(args, fmt.Sprintf("--port=%d", serverPort))
	}
	if cmd.Flags().Changed("dev") && devMode {
		args = append(args, "--dev")
	}
	if cmd.Flags().Changed("config") && strings.TrimSpace(serverConfig) != "" {
		args = append(args, "--config", serverConfig)
	}
	if cmd.Flags().Changed("clickhouse-url") && strings.TrimSpace(serverClickHouse) != "" {
		args = append(args, "--clickhouse-url", serverClickHouse)
	}
	if cmd.Flags().Changed("connection-name") && strings.TrimSpace(serverConnectionName) != "" {
		args = append(args, "--connection-name", serverConnectionName)
	}
	if cmd.Flags().Changed("allow-login-url") {
		args = append(args, fmt.Sprintf("--allow-login-url=%t", serverAllowLoginURL))
	}
	if cmd.Flags().Changed("trusted-proxies") {
		args = append(args, "--trusted-proxies", serverTrustedProxies)
	}
	// Always include absolute PID file path so the child process and
	// future update/restart commands can reliably locate the PID file
	// regardless of the caller's working directory.
	args = append(args, "--pid-file", serverPIDFile)
	if cmd.Flags().Changed("stop-timeout") {
		args = append(args, fmt.Sprintf("--stop-timeout=%s", serverStopTimeout.String()))
	}
	return args
}

func startDetachedServer(args []string) (int, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return 0, "", err
	}

	if err := preparePIDFileForStart(serverPIDFile); err != nil {
		return 0, "", err
	}

	logPath := filepath.Join(".", "ch-ui-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, "", err
	}

	child := exec.Command(exe, args...)
	child.Env = append(os.Environ(), "CHUI_DETACHED=1")
	child.Stdout = logFile
	child.Stderr = logFile
	setProcessDetachedAttr(child)

	if err := child.Start(); err != nil {
		_ = logFile.Close()
		return 0, "", err
	}
	_ = logFile.Close()

	absLog, _ := filepath.Abs(logPath)
	return child.Process.Pid, absLog, nil
}

func stopServer(pidFile string, timeout time.Duration) (bool, error) {
	pid, running, err := getRunningServerPID(pidFile)
	if err != nil {
		return false, err
	}
	if !running {
		addr := fmt.Sprintf("127.0.0.1:%d", serverPort)
		if isTCPPortOpen(addr) {
			fmt.Printf("CH-UI server PID file not found, but port %d is in use.\n", serverPort)
			fmt.Printf("This can happen after upgrading from an older build without PID management.\n")
			fmt.Printf("Stop that process once manually, then start with this build.\n")
			fmt.Printf("Expected PID file: %s\n", pidFile)
			return false, nil
		}
		fmt.Println("CH-UI server is not running")
		return false, nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("failed to locate process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if !processExists(pid) {
			_ = os.Remove(pidFile)
			return false, nil
		}
		return false, fmt.Errorf("failed to stop PID %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			_ = os.Remove(pidFile)
			return true, nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return false, fmt.Errorf("timeout waiting for PID %d to stop (waited %s)", pid, timeout.String())
}

func getRunningServerPID(pidFile string) (int, bool, error) {
	pid, err := readServerPIDFile(pidFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}

	if processExists(pid) {
		return pid, true, nil
	}

	_ = os.Remove(pidFile)
	return 0, false, nil
}

func preparePIDFileForStart(pidFile string) error {
	pid, running, err := getRunningServerPID(pidFile)
	if err != nil {
		return err
	}
	if running {
		// In containers, PID 1 is always reused. A stale PID file from a
		// previous run that recorded our own PID means the old process was
		// killed before cleanup (e.g. Docker SIGKILL after stop timeout).
		if pid == os.Getpid() {
			slog.Warn("Removing stale PID file (matches current process)", "pid", pid, "file", pidFile)
			_ = os.Remove(pidFile)
			return nil
		}
		return fmt.Errorf("server already running (PID %d); stop it first with `ch-ui server stop`", pid)
	}
	return nil
}

func writeServerPIDFile(pidFile string, pid int) error {
	if strings.TrimSpace(pidFile) == "" {
		return fmt.Errorf("pid file path is empty")
	}
	dir := filepath.Dir(pidFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

func readServerPIDFile(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return 0, fmt.Errorf("pid file %q is empty", pidFile)
	}
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID in %q", pidFile)
	}
	return pid, nil
}

func cleanupServerPIDFile(pidFile string, expectedPID int) {
	pid, err := readServerPIDFile(pidFile)
	if err != nil {
		return
	}
	if pid == expectedPID {
		_ = os.Remove(pidFile)
	}
}

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

func isTCPPortOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// resolvePIDFile converts a relative PID file path to absolute so that
// server detection works regardless of the caller's working directory.
func resolvePIDFile(pidFile string) string {
	if filepath.IsAbs(pidFile) {
		return pidFile
	}
	abs, err := filepath.Abs(pidFile)
	if err != nil {
		return pidFile
	}
	return abs
}
