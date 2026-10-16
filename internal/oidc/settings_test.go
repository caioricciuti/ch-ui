// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package oidc

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
)

func envSettings() Settings {
	return SettingsFromConfig(&config.Config{
		OIDCIssuerURL:    "https://idp.example.com",
		OIDCClientID:     "env-client",
		OIDCClientSecret: "env-secret",
		OIDCRedirectURL:  "https://ch-ui.example.com/api/auth/oidc/callback",
	})
}

func dbSettings() Settings {
	return Settings{
		Enabled:      true,
		IssuerURL:    "https://db-idp.example.com",
		ClientID:     "db-client",
		ClientSecret: "db-secret",
		RedirectURL:  "https://db.example.com/api/auth/oidc/callback",
		Source:       SourceDB,
	}
}

func TestResolvePrecedenceEnvWinsOverDB(t *testing.T) {
	got, ok := Resolve(envSettings(), dbSettings())
	if !ok {
		t.Fatal("expected SSO to resolve as configured")
	}
	if got.Source != SourceEnv {
		t.Fatalf("Source = %q, want %q (env config must win over DB config)", got.Source, SourceEnv)
	}
	if got.ClientID != "env-client" || got.IssuerURL != "https://idp.example.com" {
		t.Fatalf("resolved settings should come from env, got %+v", got)
	}
}

func TestResolveDBAppliesWhenEnvUnset(t *testing.T) {
	got, ok := Resolve(SettingsFromConfig(&config.Config{}), dbSettings())
	if !ok {
		t.Fatal("expected DB config to apply when env config is absent")
	}
	if got.Source != SourceDB || got.ClientID != "db-client" {
		t.Fatalf("resolved settings should come from DB, got %+v", got)
	}
}

func TestResolveIncompleteEnvFallsBackToDB(t *testing.T) {
	// Env config missing the client secret is not "set"; DB config applies.
	env := SettingsFromConfig(&config.Config{
		OIDCIssuerURL:   "https://idp.example.com",
		OIDCClientID:    "env-client",
		OIDCRedirectURL: "https://ch-ui.example.com/api/auth/oidc/callback",
	})
	got, ok := Resolve(env, dbSettings())
	if !ok {
		t.Fatal("expected DB config to apply when env config is incomplete")
	}
	if got.Source != SourceDB {
		t.Fatalf("Source = %q, want %q", got.Source, SourceDB)
	}
}

func TestResolveDisabledOrIncompleteDB(t *testing.T) {
	disabled := dbSettings()
	disabled.Enabled = false
	if _, ok := Resolve(SettingsFromConfig(&config.Config{}), disabled); ok {
		t.Fatal("disabled DB config must not resolve")
	}

	incomplete := dbSettings()
	incomplete.ClientSecret = ""
	if _, ok := Resolve(SettingsFromConfig(&config.Config{}), incomplete); ok {
		t.Fatal("incomplete DB config must not resolve")
	}

	if _, ok := Resolve(SettingsFromConfig(&config.Config{}), Settings{}); ok {
		t.Fatal("no config at all must not resolve")
	}
}

// SSO-only follows the same precedence as the rest of the SSO config: env
// wins entirely, including an env that leaves it off over a DB that sets it.
func TestResolveSSOOnlyFollowsPrecedence(t *testing.T) {
	db := dbSettings()
	db.SSOOnly = true
	if got, _ := Resolve(envSettings(), db); got.SSOOnly {
		t.Fatal("env config without SSO-only must win over the DB setting")
	}
	env := SettingsFromConfig(&config.Config{
		OIDCIssuerURL:    "https://idp.example.com",
		OIDCClientID:     "env-client",
		OIDCClientSecret: "env-secret",
		OIDCRedirectURL:  "https://ch-ui.example.com/api/auth/oidc/callback",
		OIDCSSOOnly:      true,
	})
	if got, _ := Resolve(env, dbSettings()); !got.SSOOnly {
		t.Fatal("env SSO-only lost")
	}
	if got, _ := Resolve(SettingsFromConfig(&config.Config{}), db); !got.SSOOnly {
		t.Fatal("DB SSO-only lost when env is unset")
	}
}

// OIDC_SSO_ONLY=true forces SSO-only on over the admin (DB) config, even when
// the rest of the env SSO config is unset.
func TestResolveEnvSSOOnlyForcesDB(t *testing.T) {
	env := SettingsFromConfig(&config.Config{OIDCSSOOnly: true})
	got, ok := Resolve(env, dbSettings())
	if !ok || got.Source != SourceDB || !got.SSOOnly {
		t.Fatalf("env SSO-only must force the DB config on: %+v", got)
	}
}
