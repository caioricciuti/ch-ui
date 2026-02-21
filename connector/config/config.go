package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all agent configuration
type Config struct {
	// Required
	Token string `yaml:"tunnel_token"`

	// URLs
	ClickHouseURL string `yaml:"clickhouse_url"`
	TunnelURL     string `yaml:"tunnel_url"`

	// Timing
	ReconnectDelay     time.Duration `yaml:"reconnect_delay"`
	MaxReconnectDelay  time.Duration `yaml:"max_reconnect_delay"`
	HeartbeatInterval  time.Duration `yaml:"heartbeat_interval"`
	InsecureSkipVerify bool          `yaml:"insecure_skip_verify"`

	// Output control
	Verbose bool `yaml:"-"`
	Quiet   bool `yaml:"-"`
	NoColor bool `yaml:"-"`
	JSON    bool `yaml:"-"`
	// Connect behavior
	Takeover bool `yaml:"-"`
}

// Default configuration values
var Defaults = Config{
	ClickHouseURL:      "http://localhost:8123",
	TunnelURL:          "ws://127.0.0.1:3488/connect",
	ReconnectDelay:     1 * time.Second,
	MaxReconnectDelay:  30 * time.Second,
	HeartbeatInterval:  30 * time.Second,
	InsecureSkipVerify: false,
}

// configFile is the YAML structure for config file
type configFile struct {
	TunnelToken        string `yaml:"tunnel_token"`
	ClickHouseURL      string `yaml:"clickhouse_url"`
	TunnelURL          string `yaml:"tunnel_url"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// DefaultConfigPath returns the platform-specific default config path
func DefaultConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "ch-ui", "config.yaml")
	default: // linux and others
		return "/etc/ch-ui/config.yaml"
	}
}

// Load creates a Config by merging: CLI flags -> config file -> environment variables
// Priority: CLI flags override config file, config file overrides env vars
func Load(configPath string, cliConfig *Config) (*Config, error) {
	cfg := Defaults

	// 1. Load from config file (lowest priority after defaults)
	if configPath != "" {
		if err := loadFromFile(configPath, &cfg); err != nil {
			// Only error if file was explicitly specified and doesn't exist
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load config file: %w", err)
			}
		}
	} else {
		// Try default path, ignore if not exists
		_ = loadFromFile(DefaultConfigPath(), &cfg)
	}

	// 2. Override with environment variables
	loadFromEnv(&cfg)

	// 3. Override with CLI flags (highest priority)
	if cliConfig != nil {
		mergeConfig(&cfg, cliConfig)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func loadFromFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var fc configFile
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	if fc.TunnelToken != "" {
		cfg.Token = fc.TunnelToken
	}
	if fc.ClickHouseURL != "" {
		cfg.ClickHouseURL = fc.ClickHouseURL
	}
	if fc.TunnelURL != "" {
		cfg.TunnelURL = fc.TunnelURL
	}
	cfg.InsecureSkipVerify = fc.InsecureSkipVerify

	return nil
}

func loadFromEnv(cfg *Config) {
	if v := os.Getenv("TUNNEL_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("CLICKHOUSE_URL"); v != "" {
		cfg.ClickHouseURL = v
	}
	if v := os.Getenv("TUNNEL_URL"); v != "" {
		cfg.TunnelURL = v
	}
	if v := os.Getenv("TUNNEL_INSECURE_SKIP_VERIFY"); v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
		cfg.InsecureSkipVerify = true
	}
}

func mergeConfig(dst, src *Config) {
	if src.Token != "" {
		dst.Token = src.Token
	}
	if src.ClickHouseURL != "" && src.ClickHouseURL != Defaults.ClickHouseURL {
		dst.ClickHouseURL = src.ClickHouseURL
	}
	if src.TunnelURL != "" && src.TunnelURL != Defaults.TunnelURL {
		dst.TunnelURL = src.TunnelURL
	}
	if src.ReconnectDelay != 0 && src.ReconnectDelay != Defaults.ReconnectDelay {
		dst.ReconnectDelay = src.ReconnectDelay
	}
	if src.MaxReconnectDelay != 0 && src.MaxReconnectDelay != Defaults.MaxReconnectDelay {
		dst.MaxReconnectDelay = src.MaxReconnectDelay
	}
	if src.HeartbeatInterval != 0 && src.HeartbeatInterval != Defaults.HeartbeatInterval {
		dst.HeartbeatInterval = src.HeartbeatInterval
	}
	dst.Verbose = src.Verbose
	dst.Quiet = src.Quiet
	dst.NoColor = src.NoColor
	dst.JSON = src.JSON
	dst.Takeover = src.Takeover
	if src.InsecureSkipVerify {
		dst.InsecureSkipVerify = true
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("tunnel token is required (use --key, TUNNEL_TOKEN env, or config file)")
	}

	if !strings.HasPrefix(c.Token, "cht_") {
		return fmt.Errorf("invalid tunnel token format (should start with 'cht_')")
	}

	if !strings.HasPrefix(c.TunnelURL, "ws://") && !strings.HasPrefix(c.TunnelURL, "wss://") {
		return fmt.Errorf("tunnel URL must start with ws:// or wss://")
	}

	if !strings.HasPrefix(c.ClickHouseURL, "http://") && !strings.HasPrefix(c.ClickHouseURL, "https://") {
		return fmt.Errorf("ClickHouse URL must start with http:// or https://")
	}

	return nil
}

// GenerateTemplate returns a YAML config template
func GenerateTemplate() string {
	return `# CH-UI Agent Configuration
#
# This file can be placed at:
#   - Linux: /etc/ch-ui/config.yaml
#   - macOS: ~/.config/ch-ui/config.yaml
#
# All settings can also be specified via environment variables or CLI flags.
# Priority: CLI flags > Environment variables > Config file

# Required: Your tunnel token from CH-UI server (ch-ui tunnel create --name <name>)
tunnel_token: "cht_your_token_here"

# ClickHouse HTTP API URL (default: http://localhost:8123)
clickhouse_url: "http://localhost:8123"

# CH-UI tunnel URL (default: ws://127.0.0.1:3488/connect)
tunnel_url: "ws://127.0.0.1:3488/connect"

# Skip TLS certificate validation for tunnel connection (unsafe, dev only)
# insecure_skip_verify: false
`
}

// Redacted returns a copy of the config with sensitive fields redacted
func (c *Config) Redacted() Config {
	redacted := *c
	if redacted.Token != "" {
		if len(redacted.Token) > 8 {
			redacted.Token = redacted.Token[:8] + "..."
		} else {
			redacted.Token = "***"
		}
	}
	return redacted
}
