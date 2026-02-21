package config

import (
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
