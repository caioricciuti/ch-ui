// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package oidc

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Manager holds the currently active OIDC provider (if any) and lets the
// admin SSO settings endpoint swap it at runtime without a server restart.
// A nil provider means SSO is not configured / not active.
//
// It also keeps the latest settings handed to Reload, even when building a
// provider from them failed, so sign-in always enforces the saved access
// rules (domains, groups, email verification) and never silently keeps
// using an identity provider the admin has since replaced.
type Manager struct {
	mu       sync.RWMutex
	provider *Provider
	settings Settings // latest settings passed to Reload
	err      error    // why the last Reload could not build a provider
}

// ErrProviderStale is returned by Current when the saved settings name a
// different identity provider (issuer, client, secret, redirect or groups
// claim) than the one that is loaded, because loading the new one failed.
var ErrProviderStale = errors.New("the saved SSO settings could not be loaded")

// NewManager returns an empty (inactive) manager.
func NewManager() *Manager {
	return &Manager{}
}

// Provider returns the active provider, or nil when SSO is inactive.
func (m *Manager) Provider() *Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}

// Current returns the provider to sign in with and the latest saved
// settings, whose access rules apply even when they could not be loaded
// into a new provider. A nil provider with a nil error means SSO is not
// active. When the saved settings changed the identity provider itself and
// it could not be loaded, it returns ErrProviderStale and no provider.
func (m *Manager) Current() (*Provider, Settings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.provider == nil {
		return nil, Settings{}, nil
	}
	if !m.settings.SameProvider(m.provider.settings) {
		return nil, m.settings, fmt.Errorf("%w: %v", ErrProviderStale, m.err)
	}
	return m.provider, m.settings, nil
}

// LastError returns why the latest Reload failed, or nil when it succeeded.
func (m *Manager) LastError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.err
}

// Active reports whether an initialized provider is available.
func (m *Manager) Active() bool {
	return m.Provider() != nil
}

// Reload runs issuer discovery for the given settings and, on success, swaps
// the active provider. On failure the previously active provider (if any)
// stays loaded, but Current applies the new settings' access rules and
// refuses sign-in if the new settings point at a different provider.
func (m *Manager) Reload(ctx context.Context, s Settings) error {
	var p *Provider
	err := fmt.Errorf("oidc settings are incomplete")
	if s.Enabled && s.Complete() {
		p, err = New(ctx, s)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.err = s, err
	if err != nil {
		return err
	}
	m.provider = p
	return nil
}

// SSOOnly reports whether the latest saved settings ask for SSO-only sign-in
// (configured) and whether it is in force (enforced): only while the provider
// those settings name is loaded, so a provider that failed to load never
// locks password sign-in out.
func (m *Manager) SSOOnly() (configured, enforced bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	configured = m.settings.SSOOnly
	enforced = configured && m.provider != nil && m.settings.SameProvider(m.provider.settings)
	return configured, enforced
}

// Disable deactivates SSO by dropping the active provider.
func (m *Manager) Disable() {
	m.mu.Lock()
	m.provider, m.settings, m.err = nil, Settings{}, nil
	m.mu.Unlock()
}
