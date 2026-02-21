package service

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const systemdServiceTemplate = `[Unit]
Description=CH-UI Tunnel
Documentation=https://ch-ui.com/docs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s connect --config %s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`

const systemdServicePath = "/etc/systemd/system/ch-ui.service"

func (m *Manager) systemdIsInstalled() bool {
	return fileExists(systemdServicePath)
}

func (m *Manager) systemdIsRunning() (bool, error) {
	output, err := runCommand("systemctl", "is-active", ServiceName)
	if err != nil {
		return false, nil // Not running or not installed
	}
	return strings.TrimSpace(output) == "active", nil
}

func (m *Manager) systemdInstall(configPath string) error {
	// Create config directory with proper permissions
	configDir := SystemConfigDir
	if err := os.MkdirAll(configDir, 0755); err != nil {
		// Try with sudo
		_, err = runCommandWithSudo(true, "mkdir", "-p", configDir)
		if err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	// Generate service content
	serviceContent := fmt.Sprintf(systemdServiceTemplate,
		BinaryPath,
		configPath,
		ServiceName,
		configDir,
	)

	// Write service file (needs sudo)
	tmpFile := "/tmp/ch-ui-agent.service"
	if err := os.WriteFile(tmpFile, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Move to systemd directory
	_, err := runCommandWithSudo(true, "mv", tmpFile, systemdServicePath)
	if err != nil {
		return fmt.Errorf("failed to install service file: %w", err)
	}

	// Set permissions
	runCommandWithSudo(true, "chmod", "644", systemdServicePath)

	// Reload systemd
	_, err = runCommandWithSudo(true, "systemctl", "daemon-reload")
	if err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	// Enable the service
	_, err = runCommandWithSudo(true, "systemctl", "enable", ServiceName)
	if err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	// Start the service
	_, err = runCommandWithSudo(true, "systemctl", "start", ServiceName)
	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (m *Manager) systemdUninstall() error {
	// Stop the service
	runCommandWithSudo(true, "systemctl", "stop", ServiceName)

	// Disable the service
	runCommandWithSudo(true, "systemctl", "disable", ServiceName)

	// Remove service file
	if fileExists(systemdServicePath) {
		_, err := runCommandWithSudo(true, "rm", systemdServicePath)
		if err != nil {
			return fmt.Errorf("failed to remove service file: %w", err)
		}
	}

	// Reload systemd
	runCommandWithSudo(true, "systemctl", "daemon-reload")

	return nil
}

func (m *Manager) systemdStart() error {
	if !m.systemdIsInstalled() {
		return fmt.Errorf("service not installed. Run 'ch-ui service install' first")
	}

	running, _ := m.systemdIsRunning()
	if running {
		return fmt.Errorf("service is already running")
	}

	_, err := runCommandWithSudo(true, "systemctl", "start", ServiceName)
	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (m *Manager) systemdStop() error {
	running, _ := m.systemdIsRunning()
	if !running {
		return fmt.Errorf("service is not running")
	}

	_, err := runCommandWithSudo(true, "systemctl", "stop", ServiceName)
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	return nil
}

func (m *Manager) systemdRestart() error {
	if !m.systemdIsInstalled() {
		return fmt.Errorf("service not installed. Run 'ch-ui service install' first")
	}

	_, err := runCommandWithSudo(true, "systemctl", "restart", ServiceName)
	if err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	return nil
}

func (m *Manager) systemdStatus() (string, error) {
	if !m.systemdIsInstalled() {
		return "not installed", nil
	}

	output, _ := runCommand("systemctl", "is-active", ServiceName)
	status := strings.TrimSpace(output)

	switch status {
	case "active":
		// Get more details
		detailOutput, _ := runCommand("systemctl", "show", ServiceName, "--property=MainPID,ActiveEnterTimestamp")
		var pid, since string
		for _, line := range strings.Split(detailOutput, "\n") {
			if strings.HasPrefix(line, "MainPID=") {
				pid = strings.TrimPrefix(line, "MainPID=")
			}
			if strings.HasPrefix(line, "ActiveEnterTimestamp=") {
				since = strings.TrimPrefix(line, "ActiveEnterTimestamp=")
			}
		}
		if pid != "" && pid != "0" {
			if since != "" {
				return fmt.Sprintf("running (PID: %s, since: %s)", pid, since), nil
			}
			return fmt.Sprintf("running (PID: %s)", pid), nil
		}
		return "running", nil
	case "inactive":
		return "stopped", nil
	case "failed":
		return "failed (check logs with: ch-ui service logs)", nil
	default:
		return status, nil
	}
}

func (m *Manager) systemdLogs(follow bool, lines int) error {
	args := []string{"-u", ServiceName, "-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}

	cmd := exec.Command("journalctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
