package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultAppSecretKey exists for backward compatibility only.
	// New installs should persist a random key when this placeholder is detected.
	DefaultAppSecretKey = "ch-ui-default-secret-key-change-in-production"
)

type SecretKeySource string

const (
	SecretKeySourceConfigured SecretKeySource = "configured"
	SecretKeySourceFile       SecretKeySource = "file"
	SecretKeySourceGenerated  SecretKeySource = "generated"
)

// AppSecretKeyPath returns the default persisted key path based on the database path.
func AppSecretKeyPath(databasePath string) string {
	dbPath := strings.TrimSpace(databasePath)
	if dbPath == "" {
		dbPath = "./data/ch-ui.db"
	}
	return filepath.Join(filepath.Dir(dbPath), ".app_secret_key")
}

// EnsureAppSecretKey guarantees a non-default secret key.
// If the configured key is default/empty, it loads from the persisted key file,
// or generates and stores a new key.
func EnsureAppSecretKey(cfg *Config) (SecretKeySource, error) {
	if cfg == nil {
		return SecretKeySourceConfigured, fmt.Errorf("nil config")
	}

	current := strings.TrimSpace(cfg.AppSecretKey)
	if current != "" && current != DefaultAppSecretKey {
		return SecretKeySourceConfigured, nil
	}

	secretPath := AppSecretKeyPath(cfg.DatabasePath)
	if data, err := os.ReadFile(secretPath); err == nil {
		loaded := strings.TrimSpace(string(data))
		if loaded == "" {
			return SecretKeySourceFile, fmt.Errorf("empty app secret key file: %s", secretPath)
		}
		cfg.AppSecretKey = loaded
		return SecretKeySourceFile, nil
	} else if !os.IsNotExist(err) {
		return SecretKeySourceFile, fmt.Errorf("read app secret key file: %w", err)
	}

	secret, err := generateRandomSecret(48)
	if err != nil {
		return SecretKeySourceGenerated, err
	}

	if err := os.MkdirAll(filepath.Dir(secretPath), 0700); err != nil {
		return SecretKeySourceGenerated, fmt.Errorf("create secret key directory: %w", err)
	}
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0600); err != nil {
		return SecretKeySourceGenerated, fmt.Errorf("write app secret key file: %w", err)
	}

	cfg.AppSecretKey = secret
	return SecretKeySourceGenerated, nil
}

func generateRandomSecret(size int) (string, error) {
	if size <= 0 {
		size = 48
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random app secret key: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(buf), nil
}
