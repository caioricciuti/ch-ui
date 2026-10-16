// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package oidc wraps an OpenID Connect identity provider for CH-UI SSO. It
// authenticates the person; ClickHouse access is handled separately by the
// per-connection service account.
package oidc

import (
	"context"
	"fmt"
	"strings"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Provider holds an initialized OIDC verifier and OAuth2 config.
type Provider struct {
	verifier    *gooidc.IDTokenVerifier
	oauth2      oauth2.Config
	groupsClaim string
	settings    Settings
}

// Claims is the subset of ID-token claims CH-UI uses.
type Claims struct {
	// Issuer and Subject identify the person at the IdP: together they are
	// stable across email changes, which is why identities are keyed by them.
	Issuer  string
	Subject string
	Email   string
	// EmailVerified is the email_verified claim. EmailVerifiedPresent reports
	// whether the IdP sent the claim at all; some (Microsoft Entra ID commonly)
	// never do, which is different from an explicit false.
	EmailVerified        bool
	EmailVerifiedPresent bool
	Name                 string
	Groups               []string
}

// New initializes an OIDC provider from settings by discovering the issuer.
func New(ctx context.Context, s Settings) (*Provider, error) {
	provider, err := gooidc.NewProvider(ctx, strings.TrimSpace(s.IssuerURL))
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %q: %w", s.IssuerURL, err)
	}

	groupsClaim := strings.TrimSpace(s.GroupsClaim)
	if groupsClaim == "" {
		groupsClaim = "groups"
	}

	scopes := []string{gooidc.ScopeOpenID, "email", "profile"}
	if len(s.AdminGroups) > 0 || len(s.AnalystGroups) > 0 || len(s.ViewerGroups) > 0 {
		scopes = append(scopes, groupsClaim)
	}

	return &Provider{
		verifier: provider.Verifier(&gooidc.Config{ClientID: s.ClientID}),
		oauth2: oauth2.Config{
			ClientID:     s.ClientID,
			ClientSecret: s.ClientSecret,
			RedirectURL:  s.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
		groupsClaim: groupsClaim,
		settings:    s,
	}, nil
}

// Settings returns the settings this provider was initialized with.
func (p *Provider) Settings() Settings {
	return p.settings
}

// AuthCodeURL builds the IdP authorization URL with state, nonce and a PKCE
// S256 code challenge derived from verifier.
func (p *Provider) AuthCodeURL(state, nonce, verifier string) string {
	return p.oauth2.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

// Exchange swaps the authorization code for tokens (sending the PKCE code
// verifier) and verifies the ID token, checking that its nonce matches the
// one issued at login.
func (p *Provider) Exchange(ctx context.Context, code, expectedNonce, verifier string) (*Claims, error) {
	tok, err := p.oauth2.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, fmt.Errorf("no id_token in token response")
	}
	idToken, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	if idToken.Nonce != expectedNonce {
		return nil, fmt.Errorf("id_token nonce mismatch")
	}

	var raw map[string]interface{}
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	c := claimsFromRaw(idToken.Subject, raw, p.groupsClaim)
	c.Issuer = idToken.Issuer
	return c, nil
}

// claimsFromRaw extracts the claims CH-UI uses from a verified ID token's
// claim set.
func claimsFromRaw(subject string, raw map[string]interface{}, groupsClaim string) *Claims {
	c := &Claims{
		Subject: subject,
		Email:   asString(raw["email"]),
		Name:    asString(raw["name"]),
		Groups:  asStringSlice(raw[groupsClaim]),
	}
	c.EmailVerified, c.EmailVerifiedPresent = parseEmailVerified(raw)
	return c
}

// parseEmailVerified reads email_verified. A JSON bool is taken as is; the
// strings "true"/"false" are accepted because some IdPs (older AWS Cognito
// pools) send them that way. Any other value counts as present but not
// verified, so a malformed claim is refused rather than treated as missing.
func parseEmailVerified(raw map[string]interface{}) (verified, present bool) {
	v, ok := raw["email_verified"]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true"), true
	}
	return false, true
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func asStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}
