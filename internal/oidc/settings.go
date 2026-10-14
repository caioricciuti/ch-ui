// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package oidc

import (
	"strings"

	"github.com/caioricciuti/ch-ui/internal/config"
)

// Settings source values.
const (
	SourceEnv = "env"
	SourceDB  = "db"
)

// Settings is the effective OIDC SSO configuration, regardless of whether it
// came from the environment/YAML config or from the admin-managed database
// config. ClientSecret is always plaintext here (decrypted by the caller when
// loaded from the database).
type Settings struct {
	Enabled        bool
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	RedirectURL    string // full callback URL, e.g. https://host/api/auth/oidc/callback
	ConnectionID   string // connection SSO sessions use (default: the embedded connection)
	GroupsClaim    string // ID-token claim holding group memberships (default "groups")
	AllowedDomains []string
	AdminGroups    []string
	AnalystGroups  []string
	ViewerGroups   []string
	// AcceptUnverifiedEmail lets a login through when the ID token has no
	// email_verified claim at all (IdPs that never send it). An explicit
	// email_verified=false is always refused.
	AcceptUnverifiedEmail bool
	Source                string // SourceEnv or SourceDB
}

// Complete reports whether the settings carry everything needed to run the
// authorization-code flow.
func (s Settings) Complete() bool {
	return strings.TrimSpace(s.IssuerURL) != "" &&
		strings.TrimSpace(s.ClientID) != "" &&
		strings.TrimSpace(s.ClientSecret) != "" &&
		strings.TrimSpace(s.RedirectURL) != ""
}

// SettingsFromConfig builds Settings from the environment/YAML server config.
// Enabled mirrors config.OIDCEnabled(): all required fields present.
func SettingsFromConfig(cfg *config.Config) Settings {
	return Settings{
		Enabled:               cfg.OIDCEnabled(),
		IssuerURL:             cfg.OIDCIssuerURL,
		ClientID:              cfg.OIDCClientID,
		ClientSecret:          cfg.OIDCClientSecret,
		RedirectURL:           cfg.OIDCRedirectURL,
		ConnectionID:          cfg.OIDCConnectionID,
		GroupsClaim:           cfg.OIDCGroupsClaim,
		AllowedDomains:        cfg.OIDCAllowedDomains,
		AdminGroups:           cfg.OIDCAdminGroups,
		AnalystGroups:         cfg.OIDCAnalystGroups,
		AcceptUnverifiedEmail: cfg.OIDCAcceptUnverifiedEmail,
		Source:                SourceEnv,
	}
}

// Resolve picks the effective SSO settings. Precedence: environment/YAML
// config always wins when it is fully set; the database (admin UI) config
// applies only when no env config exists. Returns false when SSO is not
// configured (or incompletely configured) by either source.
func Resolve(env, db Settings) (Settings, bool) {
	if env.Enabled && env.Complete() {
		env.Source = SourceEnv
		return env, true
	}
	if db.Enabled && db.Complete() {
		db.Source = SourceDB
		return db, true
	}
	return Settings{}, false
}
