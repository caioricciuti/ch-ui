// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// SettingSSOConfig is the settings key holding the admin-managed SSO (OIDC)
// config JSON. It only applies when no OIDC env/YAML config is set — the
// environment always wins (see internal/oidc.Resolve).
const SettingSSOConfig = "sso_config"

// SSOConfig is the admin-managed OIDC SSO configuration stored as a JSON blob
// in the settings table. ClientSecretEnc is encrypted with the app secret key
// (crypto.Encrypt), same as connection passwords — never stored in plaintext.
type SSOConfig struct {
	Enabled         bool     `json:"enabled"`
	IssuerURL       string   `json:"issuer_url"`
	ClientID        string   `json:"client_id"`
	ClientSecretEnc string   `json:"client_secret_enc"`
	RedirectBaseURL string   `json:"redirect_base_url"` // e.g. https://ch-ui.example.com (callback path is appended)
	AdminGroups     []string `json:"admin_groups"`
	AnalystGroups   []string `json:"analyst_groups"`
	ViewerGroups    []string `json:"viewer_groups"`
	AllowedDomains  []string `json:"allowed_domains"`
}

// Validate rejects configs that could never complete an OIDC flow. Only
// enforced when the config is enabled; a disabled draft may be partial.
func (c SSOConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	issuer := strings.TrimSpace(c.IssuerURL)
	if issuer == "" {
		return fmt.Errorf("issuer URL is required")
	}
	if u, err := url.Parse(issuer); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("issuer URL must be a valid http(s) URL")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("client ID is required")
	}
	if strings.TrimSpace(c.ClientSecretEnc) == "" {
		return fmt.Errorf("client secret is required")
	}
	if base := strings.TrimSpace(c.RedirectBaseURL); base != "" {
		if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("redirect base URL must be a valid http(s) URL")
		}
	}
	return nil
}

// GetSSOConfig loads the stored SSO config. The zero value (Enabled=false)
// is returned when nothing has been saved yet.
func (db *DB) GetSSOConfig() (SSOConfig, error) {
	var cfg SSOConfig
	raw, err := db.GetSetting(SettingSSOConfig)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return SSOConfig{}, fmt.Errorf("parse sso config: %w", err)
	}
	return cfg, nil
}

// SetSSOConfig validates and persists the SSO config as a single JSON blob in
// the settings table (same pattern as the retention config).
func (db *DB) SetSSOConfig(cfg SSOConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal sso config: %w", err)
	}
	return db.SetSetting(SettingSSOConfig, string(data))
}
