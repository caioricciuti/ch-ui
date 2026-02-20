package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>connect</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>ThrottleInterval</key>
    <integer>5</integer>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>WorkingDirectory</key>
    <string>/tmp</string>
</dict>
</plist>
`

func (m *Manager) launchdPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", ServiceLabel+".plist")
}

func (m *Manager) launchdLogDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "ch-ui")
}

func (m *Manager) launchdLogPath() string {
	return filepath.Join(m.launchdLogDir(), "agent.log")
}

func (m *Manager) launchdIsInstalled() bool {
	return fileExists(m.launchdPlistPath())
}

func (m *Manager) launchdIsRunning() (bool, error) {
	output, err := runCommand("launchctl", "list")
	if err != nil {
		return false, err
	}
	return strings.Contains(output, ServiceLabel), nil
}

func (m *Manager) launchdInstall(configPath string) error {
	// Create log directory
	logDir := m.launchdLogDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create LaunchAgents directory if it doesn't exist
	agentsDir := filepath.Dir(m.launchdPlistPath())
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	// Generate plist content
	logPath := m.launchdLogPath()
	plistContent := fmt.Sprintf(launchdPlistTemplate,
		ServiceLabel,
		BinaryPath,
		configPath,
		logPath,
		logPath,
	)

	// Write plist file
	if err := os.WriteFile(m.launchdPlistPath(), []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	// Load the service
	_, err := runCommand("launchctl", "load", m.launchdPlistPath())
	if err != nil {
		return fmt.Errorf("failed to load service: %w", err)
	}

	return nil
}

func (m *Manager) launchdUninstall() error {
	// Stop the service first (ignore errors if not running)
	_ = m.launchdStop()

	// Unload the service
	if m.launchdIsInstalled() {
		runCommand("launchctl", "unload", m.launchdPlistPath())
	}

	// Remove plist file
	plistPath := m.launchdPlistPath()
	if fileExists(plistPath) {
		if err := os.Remove(plistPath); err != nil {
			return fmt.Errorf("failed to remove plist file: %w", err)
		}
	}

	return nil
}

func (m *Manager) launchdStart() error {
	if !m.launchdIsInstalled() {
		return fmt.Errorf("service not installed. Run 'ch-ui service install' first")
	}

	// Check if already running
	running, _ := m.launchdIsRunning()
	if running {
		return fmt.Errorf("service is already running")
	}

	// Start the service
	_, err := runCommand("launchctl", "start", ServiceLabel)
	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (m *Manager) launchdStop() error {
	running, _ := m.launchdIsRunning()
	if !running {
		return fmt.Errorf("service is not running")
	}

	_, err := runCommand("launchctl", "stop", ServiceLabel)
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	return nil
}

func (m *Manager) launchdRestart() error {
	if !m.launchdIsInstalled() {
		return fmt.Errorf("service not installed. Run 'ch-ui service install' first")
	}

	// Stop if running
	running, _ := m.launchdIsRunning()
	if running {
		runCommand("launchctl", "stop", ServiceLabel)
	}

	// Start the service
	_, err := runCommand("launchctl", "start", ServiceLabel)
	if err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	return nil
}

func (m *Manager) launchdStatus() (string, error) {
	if !m.launchdIsInstalled() {
		return "not installed", nil
	}

	output, _ := runCommand("launchctl", "list")
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, ServiceLabel) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				pid := parts[0]
				status := parts[1]
				if pid != "-" {
					return fmt.Sprintf("running (PID: %s)", pid), nil
				}
				if status != "0" {
					return fmt.Sprintf("stopped (last exit: %s)", status), nil
				}
			}
			return "stopped", nil
		}
	}

	return "not running", nil
}

func (m *Manager) launchdLogs(follow bool, lines int) error {
	logPath := m.launchdLogPath()

	if !fileExists(logPath) {
		fmt.Println("No logs found yet. Service may not have started.")
		return nil
	}

	if follow {
		cmd := exec.Command("tail", "-f", "-n", strconv.Itoa(lines), logPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("tail", "-n", strconv.Itoa(lines), logPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
