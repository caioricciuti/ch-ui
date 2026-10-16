package config

import "testing"

// --port after Load must carry the derived AppURL and TunnelURL with it, and
// leave explicitly set ones alone.
func TestSetPortRebuildsDerivedURLs(t *testing.T) {
	t.Setenv("APP_URL", "")
	t.Setenv("TUNNEL_URL", "")
	cfg := Load("")
	cfg.SetPort(3491)
	if cfg.AppURL != "http://localhost:3491" || cfg.TunnelURL != "ws://127.0.0.1:3491/connect" {
		t.Fatalf("derived urls not rebuilt: %q %q", cfg.AppURL, cfg.TunnelURL)
	}

	t.Setenv("APP_URL", "https://ch.example.com")
	t.Setenv("TUNNEL_URL", "wss://ch.example.com/connect")
	cfg = Load("")
	cfg.SetPort(3491)
	if cfg.AppURL != "https://ch.example.com" || cfg.TunnelURL != "wss://ch.example.com/connect" || cfg.Port != 3491 {
		t.Fatalf("explicit urls changed: %q %q %d", cfg.AppURL, cfg.TunnelURL, cfg.Port)
	}
}
