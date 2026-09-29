// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"testing"
	"time"
)

// Two SSO people share one ClickHouse service account, and a password user
// logs in as that same account. Each must be a separate user with its own role.
func setupSharedSSOAccount(t *testing.T) (*DB, string) {
	t.Helper()
	db := openTestDB(t)

	connID, err := db.CreateConnection(CreateConnectionParams{
		Name:        "sso-test",
		TunnelToken: "cht_sso_test",
		Type:        ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	if err := db.SetConnectionSSOAccount(connID, "svc_sso", "enc"); err != nil {
		t.Fatalf("SetConnectionSSOAccount: %v", err)
	}

	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	sessions := []CreateSessionParams{
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "enc", Token: "tok-alice", ExpiresAt: expires, UserRole: "viewer", AuthSubject: "alice@example.com"},
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "enc", Token: "tok-bob", ExpiresAt: expires, UserRole: "analyst", AuthSubject: "bob@example.com"},
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "enc", Token: "tok-pw", ExpiresAt: expires, UserRole: "viewer"},
	}
	for _, p := range sessions {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatalf("CreateSession %s: %v", p.Token, err)
		}
	}
	return db, connID
}

func sessionRole(t *testing.T, db *DB, token string) string {
	t.Helper()
	s, err := db.GetSession(token)
	if err != nil || s == nil {
		t.Fatalf("GetSession %s: %v", token, err)
	}
	if s.UserRole == nil {
		return ""
	}
	return *s.UserRole
}

func TestRoleKey(t *testing.T) {
	if got := RoleKey("svc_sso", "alice@example.com"); got != "sso:alice@example.com" {
		t.Fatalf("SSO key = %q", got)
	}
	if got := RoleKey("analyst", ""); got != "analyst" {
		t.Fatalf("password key = %q", got)
	}
}

func TestGetUsersSeparatesSSOPeopleOnSharedAccount(t *testing.T) {
	db, _ := setupSharedSSOAccount(t)

	users, err := db.GetUsers()
	if err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	byKey := map[string]SessionUser{}
	for _, u := range users {
		byKey[u.Username] = u
	}
	if len(byKey) != 3 {
		t.Fatalf("want 3 users (alice, bob, password svc_sso), got %d: %+v", len(byKey), users)
	}
	alice := byKey["sso:alice@example.com"]
	if !alice.ViaSSO || alice.DisplayName != "alice@example.com" || alice.ClickhouseUser != "svc_sso" {
		t.Fatalf("alice row wrong: %+v", alice)
	}
	if byKey["sso:bob@example.com"].UserRole != "analyst" {
		t.Fatalf("bob keeps his own role, got %+v", byKey["sso:bob@example.com"])
	}
	pw := byKey["svc_sso"]
	if pw.ViaSSO || pw.SessionCount != 1 {
		t.Fatalf("password row must count only the password session: %+v", pw)
	}
}

func TestSetSessionsUserRoleTouchesOnlyThatPerson(t *testing.T) {
	db, _ := setupSharedSSOAccount(t)

	if err := db.SetSessionsUserRole("sso:alice@example.com", "admin"); err != nil {
		t.Fatalf("SetSessionsUserRole alice: %v", err)
	}
	if got := sessionRole(t, db, "tok-alice"); got != "admin" {
		t.Fatalf("alice = %q, want admin", got)
	}
	if got := sessionRole(t, db, "tok-bob"); got != "analyst" {
		t.Fatalf("bob changed to %q", got)
	}
	if got := sessionRole(t, db, "tok-pw"); got != "viewer" {
		t.Fatalf("password session changed to %q", got)
	}

	// A role on the shared account name reaches only password logins.
	if err := db.SetSessionsUserRole("svc_sso", "admin"); err != nil {
		t.Fatalf("SetSessionsUserRole svc_sso: %v", err)
	}
	if got := sessionRole(t, db, "tok-pw"); got != "admin" {
		t.Fatalf("password session = %q, want admin", got)
	}
	if got := sessionRole(t, db, "tok-bob"); got != "analyst" {
		t.Fatalf("service account role leaked to bob: %q", got)
	}
}

func TestSSOServiceAccountRoleOverrides(t *testing.T) {
	db, _ := setupSharedSSOAccount(t)

	for key, role := range map[string]string{"svc_sso": "admin", "sso:alice@example.com": "analyst", "someone": "viewer"} {
		if err := db.SetUserRole(key, role); err != nil {
			t.Fatalf("SetUserRole %s: %v", key, err)
		}
	}
	stale, err := db.SSOServiceAccountRoleOverrides()
	if err != nil {
		t.Fatalf("SSOServiceAccountRoleOverrides: %v", err)
	}
	if len(stale) != 1 || stale[0].Username != "svc_sso" || stale[0].Role != "admin" {
		t.Fatalf("want only the svc_sso override, got %+v", stale)
	}
}
