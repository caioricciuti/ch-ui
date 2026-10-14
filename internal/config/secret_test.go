package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureAppSecretKeyConfigured(t *testing.T) {
	cfg := &Config{
		DatabasePath: "./data/ch-ui.db",
		AppSecretKey: "already-configured-secret",
	}

	source, err := EnsureAppSecretKey(cfg)
	if err != nil {
		t.Fatalf("EnsureAppSecretKey returned error: %v", err)
	}
	if source != SecretKeySourceConfigured {
		t.Fatalf("unexpected source: got %s want %s", source, SecretKeySourceConfigured)
	}
	if cfg.AppSecretKey != "already-configured-secret" {
		t.Fatalf("configured secret should be preserved")
	}
}

func TestEnsureAppSecretKeyGenerateAndReload(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ch-ui.db")

	cfg := &Config{
		DatabasePath: dbPath,
		AppSecretKey: DefaultAppSecretKey,
	}

	source, err := EnsureAppSecretKey(cfg)
	if err != nil {
		t.Fatalf("EnsureAppSecretKey returned error: %v", err)
	}
	if source != SecretKeySourceGenerated {
		t.Fatalf("unexpected source on first run: got %s want %s", source, SecretKeySourceGenerated)
	}
	if cfg.AppSecretKey == "" || cfg.AppSecretKey == DefaultAppSecretKey {
		t.Fatalf("generated secret should be non-empty and non-default")
	}

	first := cfg.AppSecretKey
	cfgReload := &Config{
		DatabasePath: dbPath,
		AppSecretKey: DefaultAppSecretKey,
	}

	source, err = EnsureAppSecretKey(cfgReload)
	if err != nil {
		t.Fatalf("EnsureAppSecretKey reload returned error: %v", err)
	}
	if source != SecretKeySourceFile {
		t.Fatalf("unexpected source on reload: got %s want %s", source, SecretKeySourceFile)
	}
	if cfgReload.AppSecretKey != first {
		t.Fatalf("reloaded secret mismatch")
	}
}

func TestAppSecretKeyPreviousLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("app_secret_key: new\napp_secret_key_previous: old-from-yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_SECRET_KEY", "")
	t.Setenv("APP_SECRET_KEY_PREVIOUS", "")
	if got := Load(path).AppSecretKeyPrevious; got != "old-from-yaml" {
		t.Fatalf("yaml: got %q", got)
	}
	t.Setenv("APP_SECRET_KEY_PREVIOUS", `"old-from-env"`)
	if got := Load(path).AppSecretKeyPrevious; got != "old-from-env" {
		t.Fatalf("env: got %q", got)
	}
}
