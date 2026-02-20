package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/service"
	"github.com/spf13/cobra"
)

// ── service (parent) ────────────────────────────────────────────────────────

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage CH-UI as a system service",
}

// ── service install ─────────────────────────────────────────────────────────

var (
	svcInstallKey string
	svcInstallURL string
	svcInstallCH  string
)

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install CH-UI connect as a system service",
	Long: `Install CH-UI as a system service (launchd on macOS, systemd on Linux)
so it automatically connects to the server on boot.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := service.New()

		if svc.IsInstalled() {
			fmt.Println("Service is already installed")
			fmt.Println("Use 'ch-ui service restart' to restart, or 'ch-ui service uninstall' first")
			return nil
		}

		// Resolve current binary
		currentBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to determine current binary path: %w", err)
		}
		currentBin, err = filepath.EvalSymlinks(currentBin)
		if err != nil {
			return fmt.Errorf("failed to resolve binary path: %w", err)
		}

		// Copy binary to service location if needed
		if currentBin != service.BinaryPath {
			fmt.Printf("Copying binary to %s...\n", service.BinaryPath)
			if err := copyFile(currentBin, service.BinaryPath); err != nil {
				return fmt.Errorf("failed to copy binary: %w (try: sudo cp %s %s)", err, currentBin, service.BinaryPath)
			}
			if err := os.Chmod(service.BinaryPath, 0755); err != nil {
				return fmt.Errorf("failed to set binary permissions: %w", err)
			}
			fmt.Printf("Binary installed at %s\n", service.BinaryPath)
		}

		// Create config file
		configPath := service.GetConfigPath()
		if svcInstallKey != "" {
			configDir := service.GetConfigDir()
			if err := os.MkdirAll(configDir, 0755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			chURL := svcInstallCH
			if chURL == "" {
				chURL = config.Defaults.ClickHouseURL
			}
			tURL := svcInstallURL
			if tURL == "" {
				tURL = config.Defaults.TunnelURL
			}

			configContent := fmt.Sprintf(`# CH-UI Configuration
tunnel_token: "%s"
clickhouse_url: "%s"
tunnel_url: "%s"
`, svcInstallKey, chURL, tURL)

			if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}
			fmt.Printf("Configuration saved to %s\n", configPath)
		} else if !fileExists(configPath) {
			return fmt.Errorf("no config file found at %s and no --key provided\n\nUsage:\n  ch-ui service install --key <token> --url <server-url>", configPath)
		}

		// Install the service
		fmt.Println("Installing service...")
		if err := svc.Install(configPath); err != nil {
			return fmt.Errorf("failed to install service: %w", err)
		}

		fmt.Println("Service installed and started")
		fmt.Println("  Check status: ch-ui service status")
		fmt.Println("  View logs:    ch-ui service logs -f")
		return nil
	},
}

// ── service uninstall ───────────────────────────────────────────────────────

var (
	svcUninstallPurge bool
	svcUninstallForce bool
)

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the CH-UI service",
	Long:  "Stop and remove the system service. Use --purge to also remove the binary and config files.",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := service.New()

		if !svc.IsInstalled() && !svcUninstallForce {
			fmt.Println("Service is not installed")
			return nil
		}

		fmt.Println("Stopping service...")
		_ = svc.Stop()

		fmt.Println("Removing service configuration...")
		if err := svc.Uninstall(); err != nil {
			if !svcUninstallForce {
				return fmt.Errorf("failed to uninstall service: %w", err)
			}
			fmt.Printf("Warning: failed to uninstall service: %v (continuing with --force)\n", err)
		}

		fmt.Println("Service uninstalled")

		if svcUninstallPurge {
			if fileExists(service.BinaryPath) {
				fmt.Printf("Removing binary %s...\n", service.BinaryPath)
				if err := os.Remove(service.BinaryPath); err != nil {
					fmt.Printf("Warning: failed to remove binary: %v\n", err)
				} else {
					fmt.Println("Binary removed")
				}
			}
			configDir := service.GetConfigDir()
			if fileExists(configDir) {
				fmt.Printf("Removing config directory %s...\n", configDir)
				if err := os.RemoveAll(configDir); err != nil {
					fmt.Printf("Warning: failed to remove config directory: %v\n", err)
				} else {
					fmt.Println("Configuration removed")
				}
			}
		}
		return nil
	},
}

// ── service start/stop/restart/status/logs ──────────────────────────────────

var serviceStartCmd = &cobra.Command{
	Use: "start", Short: "Start the service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := service.New().Start(); err != nil {
			return err
		}
		fmt.Println("Service started")
		return nil
	},
}

var serviceStopCmd = &cobra.Command{
	Use: "stop", Short: "Stop the service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := service.New().Stop(); err != nil {
			return err
		}
		fmt.Println("Service stopped")
		return nil
	},
}

var serviceRestartCmd = &cobra.Command{
	Use: "restart", Short: "Restart the service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := service.New().Restart(); err != nil {
			return err
		}
		fmt.Println("Service restarted")
		return nil
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := service.New()
		if !svc.IsInstalled() {
			fmt.Println("Service is not installed")
			fmt.Println("Install with: ch-ui service install --key <token> --url <server-url>")
			return nil
		}
		status, err := svc.Status()
		if err != nil {
			return fmt.Errorf("failed to get service status: %w", err)
		}
		running, _ := svc.IsRunning()
		fmt.Println()
		fmt.Printf("  Service:    %s\n", service.ServiceName)
		fmt.Printf("  Status:     %s\n", status)
		fmt.Printf("  Running:    %v\n", running)
		fmt.Printf("  Config:     %s\n", service.GetConfigPath())
		if logPath := svc.GetLogPath(); logPath != "" {
			fmt.Printf("  Logs:       %s\n", logPath)
		}
		fmt.Printf("  Platform:   %s\n", svc.Platform())
		fmt.Println()
		return nil
	},
}

var (
	svcLogsFollow bool
	svcLogsLines  int
)

var serviceLogsCmd = &cobra.Command{
	Use: "logs", Short: "View service logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return service.New().Logs(svcLogsFollow, svcLogsLines)
	},
}

// ── init ────────────────────────────────────────────────────────────────────

func init() {
	// Install flags
	serviceInstallCmd.Flags().StringVar(&svcInstallKey, "key", "", "Tunnel token (cht_...)")
	serviceInstallCmd.Flags().StringVar(&svcInstallURL, "url", "", "CH-UI server WebSocket URL")
	serviceInstallCmd.Flags().StringVar(&svcInstallCH, "clickhouse-url", "", "ClickHouse HTTP URL")

	// Uninstall flags
	serviceUninstallCmd.Flags().BoolVar(&svcUninstallPurge, "purge", false, "Also remove binary and config files")
	serviceUninstallCmd.Flags().BoolVar(&svcUninstallForce, "force", false, "Force uninstall even if errors occur")

	// Logs flags
	serviceLogsCmd.Flags().BoolVarP(&svcLogsFollow, "follow", "f", false, "Follow log output")
	serviceLogsCmd.Flags().IntVarP(&svcLogsLines, "lines", "n", 50, "Number of log lines to show")

	// Wire up
	serviceCmd.AddCommand(serviceInstallCmd, serviceUninstallCmd, serviceStartCmd, serviceStopCmd, serviceRestartCmd, serviceStatusCmd, serviceLogsCmd)
	rootCmd.AddCommand(serviceCmd)
}
