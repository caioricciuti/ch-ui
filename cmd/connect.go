package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/service"
	"github.com/caioricciuti/ch-ui/connector/ui"
	"github.com/spf13/cobra"
)

var (
	connectURL        string
	connectKey        string
	connectCHURL      string
	connectDetach     bool
	connectTakeover   bool
	connectConfigPath string
)

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to a CH-UI server as a tunnel",
	Long: `Connect this machine's ClickHouse instance to a remote CH-UI server
via a secure WebSocket tunnel. Queries executed in the CH-UI dashboard
will be forwarded through this tunnel to your local ClickHouse.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		u := ui.New(false, false, false, false)
		u.Logo("")

		// Build CLI config from flags
		cliCfg := &config.Config{}
		if cmd.Flags().Changed("key") {
			cliCfg.Token = connectKey
		}
		if cmd.Flags().Changed("url") {
			cliCfg.TunnelURL = connectURL
		}
		if cmd.Flags().Changed("clickhouse-url") {
			cliCfg.ClickHouseURL = connectCHURL
		}
		cliCfg.Takeover = connectTakeover

		cfg, err := config.Load(connectConfigPath, cliCfg)
		if err != nil {
			u.Error("Configuration error: %v", err)
			if strings.Contains(strings.ToLower(err.Error()), "tunnel token is required") {
				u.Info("Create a tunnel token on your CH-UI server host with:")
				u.Info("  ch-ui tunnel create --name <connection-name>")
				u.Info("Then retry connect with --key <token> (or set TUNNEL_TOKEN).")
			}
			return err
		}

		if connectDetach {
			pid, logPath, err := startDetached()
			if err != nil {
				return fmt.Errorf("failed to start in background: %w", err)
			}
			u.Success("Started in background (PID %d)", pid)
			if logPath != "" {
				u.Info("Logs: %s", logPath)
			}
			return nil
		}

		if !connectTakeover {
			if running, err := service.New().IsRunning(); err == nil && running {
				u.Info("CH-UI service is already running on this machine")
				u.Info("Use 'ch-ui service status' to inspect it")
				u.Info("Use 'ch-ui service stop' to stop it before running connect")
				return nil
			}
		}

		releasePID, err := acquirePIDLock()
		if err != nil {
			u.DiagnosticError(ui.ErrorTypeConfig, "Local host",
				err.Error(),
				[]string{
					"Check current state with: ch-ui service status",
					"If this is stale, remove it and retry: rm -f " + pidFilePath(),
				},
			)
			return err
		}
		defer releasePID()

		conn := connector.New(cfg, u)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			u.Info("Shutting down...")
			conn.Shutdown()
		}()

		if err := conn.Run(); err != nil {
			u.Error("Connection error: %v", err)
			return err
		}
		return nil
	},
}

func init() {
	connectCmd.Flags().StringVar(&connectURL, "url", "", "CH-UI server WebSocket URL (ws:// or wss://)")
	connectCmd.Flags().StringVar(&connectKey, "key", "", "Tunnel token (cht_..., create on server with: ch-ui tunnel create --name <name>)")
	connectCmd.Flags().StringVar(&connectCHURL, "clickhouse-url", "", "ClickHouse HTTP URL (default: http://localhost:8123)")
	connectCmd.Flags().BoolVar(&connectDetach, "detach", false, "Run in background")
	connectCmd.Flags().BoolVar(&connectTakeover, "takeover", false, "Replace an existing active session")
	connectCmd.Flags().StringVarP(&connectConfigPath, "config", "c", "", "Path to config file")
	rootCmd.AddCommand(connectCmd)
}

// ── Detach ──────────────────────────────────────────────────────────────────

func startDetached() (int, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return 0, "", err
	}

	args := sanitizeDetachedArgs(os.Args[1:])
	if len(args) == 0 || args[0] != "connect" {
		return 0, "", fmt.Errorf("detach must be started from 'connect' command")
	}

	logDir := service.GetConfigDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return 0, "", err
	}
	logPath := filepath.Join(logDir, "ch-ui-connect.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, "", err
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "CHUI_DETACHED=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setProcessDetachedAttr(cmd)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, "", err
	}
	_ = logFile.Close()
	return cmd.Process.Pid, logPath, nil
}

func sanitizeDetachedArgs(in []string) []string {
	args := make([]string, 0, len(in))
	for _, a := range in {
		if a == "--detach" || strings.HasPrefix(a, "--detach=") {
			continue
		}
		args = append(args, a)
	}
	return args
}

// ── PID guard ───────────────────────────────────────────────────────────────

func pidFilePath() string {
	return filepath.Join(service.GetConfigDir(), "ch-ui.pid")
}

func acquirePIDLock() (func(), error) {
	pidPath := pidFilePath()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create state dir: %w", err)
	}

	for attempts := 0; attempts < 2; attempts++ {
		f, err := os.OpenFile(pidPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			pid := os.Getpid()
			_, writeErr := f.WriteString(strconv.Itoa(pid))
			closeErr := f.Close()
			if writeErr != nil {
				_ = os.Remove(pidPath)
				return nil, fmt.Errorf("failed to write pid file: %w", writeErr)
			}
			if closeErr != nil {
				_ = os.Remove(pidPath)
				return nil, fmt.Errorf("failed to finalize pid file: %w", closeErr)
			}
			return func() {
				currentPID, readErr := readPIDFile(pidPath)
				if readErr == nil && currentPID != os.Getpid() {
					return
				}
				_ = os.Remove(pidPath)
			}, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("failed to create pid file: %w", err)
		}

		existingPID, readErr := readPIDFile(pidPath)
		if readErr != nil {
			_ = os.Remove(pidPath)
			continue
		}
		if isProcessRunning(existingPID) {
			return nil, fmt.Errorf("another ch-ui connect process is already running (PID %d)", existingPID)
		}

		_ = os.Remove(pidPath)
	}

	return nil, fmt.Errorf("failed to acquire lock at %s", pidPath)
}

func readPIDFile(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid file")
	}
	return pid, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
