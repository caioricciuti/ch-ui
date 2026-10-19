package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestComposeKeyIsReplacedNotUsed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ch-ui.db")
	if err := os.WriteFile(dbPath, []byte("existing database"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DatabasePath: dbPath, AppSecretKey: FormerComposeKey}
	source, err := EnsureAppSecretKey(cfg)
	if err != nil || source != SecretKeySourceReplacedPublic {
		t.Fatalf("first start: %s %v", source, err)
	}
	if cfg.AppSecretKey == FormerComposeKey || cfg.AppSecretKey == "" {
		t.Fatal("public key kept as the current key")
	}
	if cfg.AppSecretKeyPrevious != FormerComposeKey {
		t.Fatalf("previous key %q, want the Compose key", cfg.AppSecretKeyPrevious)
	}

	// The old compose file still passes the public key on the next start.
	again := &Config{DatabasePath: dbPath, AppSecretKey: FormerComposeKey}
	source, err = EnsureAppSecretKey(again)
	if err != nil || source != SecretKeySourceFile {
		t.Fatalf("second start: %s %v", source, err)
	}
	if again.AppSecretKey != cfg.AppSecretKey || again.AppSecretKeyPrevious != FormerComposeKey {
		t.Fatal("second start did not reuse the generated key with the Compose fallback")
	}
}

func TestExistingDatabaseWithoutKeyRotatesFromComposeKey(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ch-ui.db")
	if err := os.WriteFile(dbPath, []byte("existing database"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DatabasePath: dbPath}
	source, err := EnsureAppSecretKey(cfg)
	if err != nil || source != SecretKeySourceReplacedPublic {
		t.Fatalf("got %s %v", source, err)
	}
	if cfg.AppSecretKeyPrevious != FormerComposeKey {
		t.Fatalf("previous key %q, want the Compose key", cfg.AppSecretKeyPrevious)
	}

	// Once the file exists the key loads from it with no fallback.
	reload := &Config{DatabasePath: dbPath}
	if source, err := EnsureAppSecretKey(reload); err != nil || source != SecretKeySourceFile || reload.AppSecretKeyPrevious != "" {
		t.Fatalf("reload: %s %v previous=%q", source, err, reload.AppSecretKeyPrevious)
	}
}

func TestExplicitPreviousKeyWins(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ch-ui.db")
	if err := os.WriteFile(dbPath, []byte("existing database"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DatabasePath: dbPath, AppSecretKey: FormerComposeKey, AppSecretKeyPrevious: "my-old-key"}
	if _, err := EnsureAppSecretKey(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AppSecretKeyPrevious != "my-old-key" {
		t.Fatalf("previous key overwritten: %q", cfg.AppSecretKeyPrevious)
	}
}

func TestComposeKeyInKeyFileIsReplaced(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ch-ui.db")
	if err := os.WriteFile(AppSecretKeyPath(dbPath), []byte(FormerComposeKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DatabasePath: dbPath}
	source, err := EnsureAppSecretKey(cfg)
	if err != nil || source != SecretKeySourceReplacedPublic || cfg.AppSecretKey == FormerComposeKey {
		t.Fatalf("got %s %v", source, err)
	}
	data, err := os.ReadFile(AppSecretKeyPath(dbPath))
	if err != nil || strings.TrimSpace(string(data)) != cfg.AppSecretKey {
		t.Fatal("key file not replaced with the new key")
	}
}
