package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ServiceName      = "ch-ui"
	ServiceLabel     = "com.ch-ui"
	BinaryPath       = "/usr/local/bin/ch-ui"
	SystemConfigDir  = "/etc/ch-ui"
	SystemConfigPath = "/etc/ch-ui/config.yaml"
)

// Manager provides cross-platform service management
type Manager struct {
	platform string
}

// New creates a new service manager for the current platform
func New() *Manager {
	return &Manager{platform: runtime.GOOS}
}

// IsInstalled checks if the service is installed
func (m *Manager) IsInstalled() bool {
	switch m.platform {
	case "darwin":
		return m.launchdIsInstalled()
	case "linux":
		return m.systemdIsInstalled()
	default:
		return false
	}
}

// IsRunning checks if the service is currently running
func (m *Manager) IsRunning() (bool, error) {
	switch m.platform {
	case "darwin":
		return m.launchdIsRunning()
	case "linux":
		return m.systemdIsRunning()
	default:
		return false, fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Install installs the service
func (m *Manager) Install(configPath string) error {
	switch m.platform {
	case "darwin":
		return m.launchdInstall(configPath)
	case "linux":
		return m.systemdInstall(configPath)
	default:
		return fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Uninstall removes the service
func (m *Manager) Uninstall() error {
	switch m.platform {
	case "darwin":
		return m.launchdUninstall()
	case "linux":
		return m.systemdUninstall()
	default:
		return fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Start starts the service
func (m *Manager) Start() error {
	switch m.platform {
	case "darwin":
		return m.launchdStart()
	case "linux":
		return m.systemdStart()
	default:
		return fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Stop stops the service
func (m *Manager) Stop() error {
	switch m.platform {
	case "darwin":
		return m.launchdStop()
	case "linux":
		return m.systemdStop()
	default:
		return fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Restart restarts the service
func (m *Manager) Restart() error {
	switch m.platform {
	case "darwin":
		return m.launchdRestart()
	case "linux":
		return m.systemdRestart()
	default:
		return fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Status returns the service status as a string
func (m *Manager) Status() (string, error) {
	switch m.platform {
	case "darwin":
		return m.launchdStatus()
	case "linux":
		return m.systemdStatus()
	default:
		return "", fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// Logs returns recent service logs
func (m *Manager) Logs(follow bool, lines int) error {
	switch m.platform {
	case "darwin":
		return m.launchdLogs(follow, lines)
	case "linux":
		return m.systemdLogs(follow, lines)
	default:
		return fmt.Errorf("unsupported platform: %s", m.platform)
	}
}

// GetLogPath returns the path to the log file (for macOS)
func (m *Manager) GetLogPath() string {
	switch m.platform {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Logs", "ch-ui", "agent.log")
	case "linux":
		return "" // Uses journald
	default:
		return ""
	}
}

// Platform returns the current platform
func (m *Manager) Platform() string {
	return m.platform
}

// NeedsSudo returns true if sudo is needed for service operations
func (m *Manager) NeedsSudo() bool {
	// macOS launchd user agents don't need sudo
	// Linux systemd system services need sudo
	return m.platform == "linux"
}

// runCommand runs a command and returns combined output
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// runCommandWithSudo runs a command with sudo if needed
func runCommandWithSudo(needsSudo bool, name string, args ...string) (string, error) {
	if needsSudo && os.Geteuid() != 0 {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	return runCommand(name, args...)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetConfigPath returns the appropriate config path based on platform
func GetConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "ch-ui", "config.yaml")
	default:
		return SystemConfigPath
	}
}

// GetConfigDir returns the appropriate config directory based on platform
func GetConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "ch-ui")
	default:
		return SystemConfigDir
	}
}
