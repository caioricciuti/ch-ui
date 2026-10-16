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
	// SSOOnly turns off ClickHouse username/password sign-in while this
	// provider is loaded (see Manager.SSOOnly).
	SSOOnly bool
	Source  string // SourceEnv or SourceDB
}

// Complete reports whether the settings carry everything needed to run the
// authorization-code flow.
func (s Settings) Complete() bool {
	return strings.TrimSpace(s.IssuerURL) != "" &&
		strings.TrimSpace(s.ClientID) != "" &&
		strings.TrimSpace(s.ClientSecret) != "" &&
		strings.TrimSpace(s.RedirectURL) != ""
}

// SameProvider reports whether s and o build the same Provider: every field
// New reads, including whether any group mapping asks for the groups scope.
// Access rules (allowed domains, which groups map to which role, accepting
// unverified email) are not part of it.
func (s Settings) SameProvider(o Settings) bool {
	hasGroups := func(x Settings) bool {
		return len(x.AdminGroups) > 0 || len(x.AnalystGroups) > 0 || len(x.ViewerGroups) > 0
	}
	groupsClaim := func(x Settings) string {
		if c := strings.TrimSpace(x.GroupsClaim); c != "" {
			return c
		}
		return "groups"
	}
	return strings.TrimSpace(s.IssuerURL) == strings.TrimSpace(o.IssuerURL) &&
		s.ClientID == o.ClientID &&
		s.ClientSecret == o.ClientSecret &&
		s.RedirectURL == o.RedirectURL &&
		groupsClaim(s) == groupsClaim(o) &&
		hasGroups(s) == hasGroups(o)
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
		SSOOnly:               cfg.OIDCSSOOnly,
		Source:                SourceEnv,
	}
}

// Resolve picks the effective SSO settings. Precedence: environment/YAML
// config always wins when it is fully set; the database (admin UI) config
// applies only when no env config exists. The one exception is SSO-only: an
// env/YAML true forces it on over the database config as well. Returns false when SSO is not
// configured (or incompletely configured) by either source.
func Resolve(env, db Settings) (Settings, bool) {
	if env.Enabled && env.Complete() {
		env.Source = SourceEnv
		return env, true
	}
	if db.Enabled && db.Complete() {
		db.Source = SourceDB
		// OIDC_SSO_ONLY=true forces SSO-only on top of the admin config; unset
		// or false leaves the stored value in charge.
		db.SSOOnly = db.SSOOnly || env.SSOOnly
		return db, true
	}
	return Settings{}, false
}
