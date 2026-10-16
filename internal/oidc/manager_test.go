// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package oidc

import (
	"context"
	"errors"
	"testing"
)

func TestSettingsSameProvider(t *testing.T) {
	base := Settings{IssuerURL: "https://idp", ClientID: "c", ClientSecret: "s", RedirectURL: "https://app/cb"}
	same := base
	same.AllowedDomains = []string{"example.com"}
	same.AcceptUnverifiedEmail = true
	same.GroupsClaim = "groups" // the default
	if !base.SameProvider(same) {
		t.Fatal("access rules and the default groups claim must not count as a provider change")
	}
	for name, mut := range map[string]func(*Settings){
		"issuer":       func(s *Settings) { s.IssuerURL = "https://other" },
		"client":       func(s *Settings) { s.ClientID = "c2" },
		"secret":       func(s *Settings) { s.ClientSecret = "s2" },
		"redirect":     func(s *Settings) { s.RedirectURL = "https://app/cb2" },
		"groups claim": func(s *Settings) { s.GroupsClaim = "roles" },
		"groups scope": func(s *Settings) { s.AdminGroups = []string{"admins"} },
	} {
		o := base
		mut(&o)
		if base.SameProvider(o) {
			t.Errorf("%s change should count as a provider change", name)
		}
	}
}

func TestManagerCurrentAfterFailedReload(t *testing.T) {
	// Nothing listens on port 1, so discovery fails.
	loaded := Settings{Enabled: true, IssuerURL: "http://127.0.0.1:1", ClientID: "c", ClientSecret: "s", RedirectURL: "https://app/cb"}
	m := NewManager()
	if p, _, err := m.Current(); p != nil || err != nil {
		t.Fatalf("empty manager: %v %v", p, err)
	}
	m.provider, m.settings = &Provider{settings: loaded}, loaded

	rules := loaded
	rules.AllowedDomains = []string{"example.com"}
	if err := m.Reload(context.Background(), rules); err == nil {
		t.Fatal("reload against an unreachable issuer should fail")
	}
	p, s, err := m.Current()
	if err != nil || p == nil || len(s.AllowedDomains) != 1 {
		t.Fatalf("same provider: want the loaded provider with the new rules, got %v %+v %v", p, s, err)
	}
	if m.LastError() == nil {
		t.Fatal("LastError should report the failed reload")
	}

	moved := loaded
	moved.ClientID = "c2"
	_ = m.Reload(context.Background(), moved)
	if p, _, err := m.Current(); p != nil || !errors.Is(err, ErrProviderStale) {
		t.Fatalf("changed provider: want ErrProviderStale, got %v %v", p, err)
	}

	m.Disable()
	if p, _, err := m.Current(); p != nil || err != nil || m.LastError() != nil {
		t.Fatalf("after Disable: %v %v %v", p, err, m.LastError())
	}
}

func TestManagerSSOOnlyNeedsLoadedProvider(t *testing.T) {
	loaded := Settings{Enabled: true, IssuerURL: "http://127.0.0.1:1", ClientID: "c", ClientSecret: "s", RedirectURL: "https://app/cb", SSOOnly: true}
	m := NewManager()
	if c, e := m.SSOOnly(); c || e {
		t.Fatalf("empty manager: configured=%v enforced=%v", c, e)
	}

	// Startup with an unreachable IdP: configured, never enforced.
	if err := m.Reload(context.Background(), loaded); err == nil {
		t.Fatal("reload against an unreachable issuer should fail")
	}
	if c, e := m.SSOOnly(); !c || e {
		t.Fatalf("failed load: configured=%v enforced=%v, want true false", c, e)
	}

	m.provider, m.settings, m.err = &Provider{settings: loaded}, loaded, nil
	if c, e := m.SSOOnly(); !c || !e {
		t.Fatalf("loaded: configured=%v enforced=%v, want true true", c, e)
	}

	// A new provider that failed to load is not enforced either.
	moved := loaded
	moved.ClientID = "c2"
	if err := m.Reload(context.Background(), moved); err == nil {
		t.Fatal("reload against an unreachable issuer should fail")
	}
	if _, e := m.SSOOnly(); e {
		t.Fatal("SSO-only must not be enforced while the saved provider is not loaded")
	}

	m.Disable()
	if c, e := m.SSOOnly(); c || e {
		t.Fatalf("disabled: configured=%v enforced=%v", c, e)
	}
}
