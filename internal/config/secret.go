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
	// SecretKeySourceReplacedPublic means a key was generated to replace
	// FormerComposeKey, which is set as AppSecretKeyPrevious so stored
	// secrets are re-encrypted on startup.
	SecretKeySourceReplacedPublic SecretKeySource = "replaced-public"
)

// FormerComposeKey was the APP_SECRET_KEY default in docker-compose.yml up to
// v2.28.1. It is public, so it is never used as the current key.
const FormerComposeKey = "please-change-me-to-a-random-secret"

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
	// An install still on the public Compose key gets a persisted random key
	// and its secrets re-encrypted from the old one, without manual steps.
	replacing := current == FormerComposeKey
	if replacing {
		current = ""
	}
	if current != "" && current != DefaultAppSecretKey {
		return SecretKeySourceConfigured, nil
	}

	secretPath := AppSecretKeyPath(cfg.DatabasePath)
	if data, err := os.ReadFile(secretPath); err == nil {
		loaded := strings.TrimSpace(string(data))
		if loaded == "" {
			return SecretKeySourceFile, fmt.Errorf("empty app secret key file: %s", secretPath)
		}
		if loaded != FormerComposeKey {
			cfg.AppSecretKey = loaded
			if replacing {
				// Already replaced on an earlier start; keep the old key as a
				// fallback while the environment still carries it.
				usePreviousKey(cfg, FormerComposeKey)
			}
			return SecretKeySourceFile, nil
		}
		replacing = true
	} else if !os.IsNotExist(err) {
		return SecretKeySourceFile, fmt.Errorf("read app secret key file: %w", err)
	}

	// An existing database with no key anywhere was almost certainly written
	// under the old Compose default (the current Compose file leaves the key
	// empty). Secrets under any other key are left unreadable, not changed.
	if !replacing && cfg.AppSecretKeyPrevious == "" {
		if info, err := os.Stat(cfg.DatabasePath); err == nil && info.Size() > 0 {
			replacing = true
		} else if err != nil && !os.IsNotExist(err) {
			return SecretKeySourceFile, fmt.Errorf("inspect database before generating key: %w", err)
		}
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
	if replacing {
		usePreviousKey(cfg, FormerComposeKey)
		return SecretKeySourceReplacedPublic, nil
	}
	return SecretKeySourceGenerated, nil
}

// usePreviousKey sets the rotation source unless one was configured.
func usePreviousKey(cfg *Config, key string) {
	if strings.TrimSpace(cfg.AppSecretKeyPrevious) == "" {
		cfg.AppSecretKeyPrevious = key
	}
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
