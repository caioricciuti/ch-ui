package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/license"
	"gopkg.in/yaml.v3"
)

type Config struct {
	// Server
	Port    int
	DevMode bool
	AppURL  string

	// Database
	DatabasePath string

	// Security
	AppSecretKey   string
	SessionMaxAge  int // seconds, default 7 days
	AllowedOrigins []string

	// Tunnel
	TunnelURL string

	// Embedded agent
	ClickHouseURL string // default http://localhost:8123

	// License
	LicenseJSON string // Stored signed license JSON (loaded from DB at startup)
}

// serverConfigFile is the YAML structure for the server config file.
type serverConfigFile struct {
	Port           int      `yaml:"port"`
	AppURL         string   `yaml:"app_url"`
	DatabasePath   string   `yaml:"database_path"`
	ClickHouseURL  string   `yaml:"clickhouse_url"`
	AppSecretKey   string   `yaml:"app_secret_key"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	TunnelURL      string   `yaml:"tunnel_url"`
}

// DefaultServerConfigPath returns the platform-specific default config path.
func DefaultServerConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "ch-ui", "server.yaml")
	default:
		return "/etc/ch-ui/server.yaml"
	}
}

// Load creates a Config by merging: config file -> env vars -> defaults.
// Priority: env vars > config file > defaults.
func Load(configPath string) *Config {
	cfg := &Config{
		Port:          3488,
		DatabasePath:  "./data/ch-ui.db",
		AppSecretKey:  "ch-ui-default-secret-key-change-in-production",
		SessionMaxAge: 7 * 24 * 60 * 60,
		ClickHouseURL: "http://localhost:8123",
	}

	// 1. Load from config file (overrides defaults)
	if configPath != "" {
		if err := loadServerConfigFile(configPath, cfg); err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("Failed to load config file", "path", configPath, "error", err)
			} else {
				slog.Warn("Config file not found", "path", configPath)
			}
		} else {
			slog.Info("Loaded config file", "path", configPath)
		}
	} else {
		// Try default path, silently ignore if not found
		defaultPath := DefaultServerConfigPath()
		if err := loadServerConfigFile(defaultPath, cfg); err == nil {
			slog.Info("Loaded config file", "path", defaultPath)
		}
	}

	// 2. Override with environment variables (highest priority)
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("APP_URL"); v != "" {
		cfg.AppURL = trimQuotes(v)
	}
	if v := os.Getenv("DATABASE_PATH"); v != "" {
		cfg.DatabasePath = v
	}
	if v := os.Getenv("CLICKHOUSE_URL"); v != "" {
		cfg.ClickHouseURL = v
	}
	if v := os.Getenv("APP_SECRET_KEY"); v != "" {
		cfg.AppSecretKey = trimQuotes(v)
	}
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		cfg.AllowedOrigins = nil
		for _, o := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, trimmed)
			}
		}
	}
	if v := os.Getenv("TUNNEL_URL"); v != "" {
		cfg.TunnelURL = v
	}

	// Derive defaults for computed fields
	if cfg.AppURL == "" {
		cfg.AppURL = "http://localhost:" + strconv.Itoa(cfg.Port)
	}
	if cfg.TunnelURL == "" {
		cfg.TunnelURL = "ws://127.0.0.1:" + strconv.Itoa(cfg.Port) + "/connect"
	}

	cfg.DevMode = os.Getenv("NODE_ENV") != "production"

	return cfg
}

func loadServerConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var fc serverConfigFile
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return err
	}

	if fc.Port != 0 {
		cfg.Port = fc.Port
	}
	if fc.AppURL != "" {
		cfg.AppURL = fc.AppURL
	}
	if fc.DatabasePath != "" {
		cfg.DatabasePath = fc.DatabasePath
	}
	if fc.ClickHouseURL != "" {
		cfg.ClickHouseURL = fc.ClickHouseURL
	}
	if fc.AppSecretKey != "" {
		cfg.AppSecretKey = fc.AppSecretKey
	}
	if len(fc.AllowedOrigins) > 0 {
		cfg.AllowedOrigins = fc.AllowedOrigins
	}
	if fc.TunnelURL != "" {
		cfg.TunnelURL = fc.TunnelURL
	}

	return nil
}

// GenerateServerTemplate returns a YAML config template for the server.
func GenerateServerTemplate() string {
	return `# CH-UI Server Configuration
#
# Place this file at:
#   macOS: ~/.config/ch-ui/server.yaml
#   Linux: /etc/ch-ui/server.yaml
#
# All settings can also be set via environment variables.
# Priority: env vars > config file > defaults

# HTTP port (default: 3488)
port: 3488

# Public URL of the server
# app_url: https://ch-ui.yourcompany.com

# SQLite database path (default: ./data/ch-ui.db)
# database_path: /var/lib/ch-ui/ch-ui.db

# ClickHouse HTTP endpoint (default: http://localhost:8123)
# clickhouse_url: http://localhost:8123

# Secret key for session encryption (CHANGE THIS in production)
# app_secret_key: your-random-secret-here

# Allowed CORS origins
# allowed_origins:
#   - https://ch-ui.yourcompany.com
`
}

func (c *Config) IsProduction() bool {
	return !c.DevMode
}

func (c *Config) IsPro() bool {
	info := license.ValidateLicense(c.LicenseJSON)
	return info.Valid && strings.EqualFold(strings.TrimSpace(info.Edition), "pro")
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
