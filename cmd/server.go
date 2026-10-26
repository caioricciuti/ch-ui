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
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	"github.com/caioricciuti/ch-ui/internal/license"
	"github.com/caioricciuti/ch-ui/internal/server"
	"github.com/caioricciuti/ch-ui/internal/server/handlers"
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
	serverIngestAddr     string
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
	pf.StringVar(&serverIngestAddr, "ingest-addr", "", "Also serve the /ingest receivers (and /v1 OTLP aliases) on this address, e.g. :4318")
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
		cfg.SetPort(serverPort)
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
	if cmd.Flags().Changed("ingest-addr") {
		cfg.IngestAddr = strings.TrimSpace(serverIngestAddr)
	}
	// --dev flag is the authority for dev mode in the server command.
	// Without it, always serve the embedded frontend (production mode).
	cfg.DevMode = devMode

	return serveConfig(cfg, nil)
}

// serveConfig runs the server with a loaded configuration until it stops or
// the process gets SIGINT/SIGTERM. A nil listener binds cfg.Port on every
// interface; `ch-ui desktop` passes a loopback listener instead.
func serveConfig(cfg *config.Config, ln net.Listener) error {
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

	publicKeyInEnv := strings.TrimSpace(cfg.AppSecretKey) == config.FormerComposeKey
	secretSource, err := config.EnsureAppSecretKey(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize app secret key: %w", err)
	}
	// Set only by CH-UI itself: the public Compose key is being retired.
	autoRotate := secretSource == config.SecretKeySourceReplacedPublic ||
		(publicKeyInEnv && cfg.AppSecretKeyPrevious == config.FormerComposeKey)
	if secretSource == config.SecretKeySourceReplacedPublic {
		slog.Warn("Stored secrets were encrypted with the public key from the old Compose file or with no saved key; generated a new persisted key and re-encrypting them with it",
			"path", config.AppSecretKeyPath(cfg.DatabasePath))
	}
	if publicKeyInEnv {
		slog.Warn("APP_SECRET_KEY is the public placeholder from the old Compose file and is ignored; remove it from your environment",
			"key_file", config.AppSecretKeyPath(cfg.DatabasePath))
	}
	if secretSource == config.SecretKeySourceGenerated {
		slog.Warn("APP_SECRET_KEY was not configured; generated a persisted secret key",
			"path", config.AppSecretKeyPath(cfg.DatabasePath))
	} else if secretSource == config.SecretKeySourceFile {
		slog.Info("Loaded persisted app secret key",
			"path", config.AppSecretKeyPath(cfg.DatabasePath))
	}
	rotating := cfg.AppSecretKeyPrevious != "" && cfg.AppSecretKeyPrevious != cfg.AppSecretKey
	if cfg.AppSecretKeyPrevious != "" && !rotating {
		slog.Warn("APP_SECRET_KEY_PREVIOUS equals APP_SECRET_KEY; nothing to rotate, you can remove APP_SECRET_KEY_PREVIOUS")
	}
	if rotating {
		if err := crypto.SetPreviousKey(cfg.AppSecretKeyPrevious); err != nil {
			return fmt.Errorf("failed to initialize previous app secret key: %w", err)
		}
	}

	// Initialize database
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	slog.Info("Database initialized", "path", cfg.DatabasePath)

	if rotating {
		if err := rotateStoredSecrets(db, cfg, autoRotate); err != nil {
			if secretSource == config.SecretKeySourceReplacedPublic {
				// Drop the new key so the next start retries the same rotation
				// instead of loading a key nothing was encrypted with.
				if rmErr := os.Remove(config.AppSecretKeyPath(cfg.DatabasePath)); rmErr != nil {
					slog.Error("Could not remove the generated key file", "error", rmErr)
				}
				return fmt.Errorf("re-encrypting stored secrets under the new key failed, nothing was changed: %w", err)
			}
		}
	}

	// Role overrides are keyed per SSO person since v2.13.2. An override on an
	// SSO service account used to apply to every SSO person; now it only
	// applies to password logins as that account, so tell the admin.
	if stale, err := db.SSOServiceAccountRoleOverrides(); err != nil {
		slog.Warn("Could not check role overrides on SSO service accounts", "error", err)
	} else {
		for _, r := range stale {
			slog.Warn("Role override on an SSO service account no longer applies to SSO users; set roles per person in Admin, Users",
				"service_account", r.Username, "connection", r.ConnectionName, "role", r.Role)
		}
	}

	// License precedence: CHUI_LICENSE_FILE > CHUI_LICENSE > database. The
	// environment path serves config-as-code deploys (Kubernetes Secrets,
	// Terraform); the database path serves licenses activated in the UI. An
	// environment license in its grace window is kept (read-only Pro), and a
	// rejected one never blocks startup.
	envLicense, envSource := config.LicenseFromEnv()
	dbLicense, err := db.GetSetting("license_json")
	if err != nil {
		slog.Warn("Could not read the license from the database", "error", err)
	}
	choice := config.ChooseLicense(envLicense, envSource, dbLicense, license.ValidateLicense)
	cfg.LicenseJSON = choice.JSON
	logLicenseChoice(choice, dbLicense != "")

	// Connector manager runs one in-process connector per direct connection.
	agents := embedded.NewManager(db, cfg.Port)
	agents.SameServer = handlers.SameClickHouseEndpoint

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
		if ln != nil {
			errCh <- srv.Serve(ln)
			return
		}
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
	if cmd.Flags().Changed("ingest-addr") {
		args = append(args, "--ingest-addr", serverIngestAddr)
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
	if err := terminateProcess(proc); err != nil {
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

// logLicenseChoice says which license the server runs with and why, so an
// operator never has to guess whether the environment or the UI license won.
func logLicenseChoice(choice config.LicenseChoice, haveDBLicense bool) {
	if choice.EnvRejected != "" {
		if choice.Source == "database" {
			slog.Warn("Ignoring license from environment; using the license activated in the UI instead",
				"reason", choice.EnvRejected)
		} else {
			slog.Warn("Ignoring license from environment; running without a license", "reason", choice.EnvRejected)
		}
	}
	if choice.JSON == "" {
		return
	}
	info := choice.Info
	switch {
	case info.Valid:
		slog.Info("License loaded", "source", choice.Source, "edition", info.Edition, "expires_at", info.ExpiresAt)
	case info.InGrace:
		slog.Warn("License loaded in read-only grace period; renew it before the grace window ends",
			"source", choice.Source, "edition", info.Edition, "expires_at", info.ExpiresAt, "grace_until", info.GraceUntil)
	default:
		slog.Warn("License loaded but not valid; Pro features are disabled", "source", choice.Source)
	}
	if choice.Source != "database" && haveDBLicense {
		slog.Info("A license activated in the UI is also stored; the environment license takes precedence")
	}
}

// rotateStoredSecrets re-encrypts every stored secret written with
// APP_SECRET_KEY_PREVIOUS so it reads with APP_SECRET_KEY alone. Values are
// never logged, only counts per kind and the ids of unreadable secrets. A
// failure leaves the database untouched; decryption still falls back to the
// previous key. automatic means CH-UI chose the previous key itself, so the
// log does not tell the operator to remove a setting they never added.
func rotateStoredSecrets(db *database.DB, cfg *config.Config, automatic bool) error {
	report, err := db.RotateSecrets(cfg.AppSecretKey, cfg.AppSecretKeyPrevious)
	if err != nil {
		slog.Error("Secret key rotation failed; nothing was changed, stored secrets still decrypt with the previous key", "error", err)
		return err
	}
	rotated := 0
	for _, kind := range report.Kinds() {
		rotated += report.Rotated[kind]
		slog.Info("Secret key rotation", "kind", kind,
			"reencrypted", report.Rotated[kind], "already_current", report.Current[kind])
	}
	for _, u := range report.Unreadable {
		if automatic {
			slog.Warn("Stored secret decrypts with neither the new key nor the old Compose key; left unchanged. If you used your own APP_SECRET_KEY before, set it again; otherwise re-enter the secret in the UI",
				"kind", u.Kind, "id", u.ID)
			continue
		}
		slog.Warn("Stored secret decrypts with neither APP_SECRET_KEY nor APP_SECRET_KEY_PREVIOUS; left unchanged, re-enter it in the UI",
			"kind", u.Kind, "id", u.ID)
	}
	if automatic {
		slog.Info("Secret key rotation complete", "reencrypted", rotated, "unreadable", len(report.Unreadable))
		return nil
	}
	slog.Info("Secret key rotation complete, you can remove APP_SECRET_KEY_PREVIOUS",
		"reencrypted", rotated, "unreadable", len(report.Unreadable))
	return nil
}
