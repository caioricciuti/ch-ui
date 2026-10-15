package database

import (
	"testing"
	"time"
)

func TestRevokeAllAdmins(t *testing.T) {
	db := openTestDB(t)

	connID, err := db.CreateConnection(CreateConnectionParams{
		Name:        "revoke-test",
		TunnelToken: "cht_revoke_test",
		Type:        ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	sessions := []CreateSessionParams{
		{ConnectionID: connID, ClickhouseUser: "default", EncryptedPassword: "enc", Token: "tok-default", ExpiresAt: expires, UserRole: "admin"},
		{ConnectionID: connID, ClickhouseUser: "ana", EncryptedPassword: "enc", Token: "tok-ana", ExpiresAt: expires, UserRole: "analyst"},
	}
	for _, p := range sessions {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatalf("CreateSession %s: %v", p.Token, err)
		}
	}
	for user, role := range map[string]string{"default": "admin", "sso:alice@example.com": "admin", "ana": "analyst"} {
		if err := db.SetUserRole(user, connID, role); err != nil {
			t.Fatalf("SetUserRole %s: %v", user, err)
		}
	}

	revoked, err := db.RevokeAllAdmins()
	if err != nil {
		t.Fatalf("RevokeAllAdmins: %v", err)
	}
	if len(revoked) != 2 || revoked[0].String() != "default on revoke-test" || revoked[1].String() != "sso:alice@example.com" {
		t.Fatalf("revoked = %v", revoked)
	}

	if n, _ := db.CountUsersWithRole("admin"); n != 0 {
		t.Fatalf("admins left = %d", n)
	}
	if r, _ := db.GetUserRole("ana", connID); r != "analyst" {
		t.Fatalf("ana role = %q, want analyst kept", r)
	}
	if r := sessionRole(t, db, "tok-default"); r != "viewer" {
		t.Fatalf("default session role = %q, want viewer", r)
	}
	if r := sessionRole(t, db, "tok-ana"); r != "analyst" {
		t.Fatalf("ana session role = %q, want analyst", r)
	}

	revoked, err = db.RevokeAllAdmins()
	if err != nil || len(revoked) != 0 {
		t.Fatalf("second RevokeAllAdmins = %v, %v", revoked, err)
	}
}

func TestActorRole(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "actor-role", TunnelToken: "cht_actor_role", Type: ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, p := range []CreateSessionParams{
		{ConnectionID: connID, ClickhouseUser: "ana", EncryptedPassword: "enc", Token: "tok-ana", ExpiresAt: expires, UserRole: "analyst"},
		{ConnectionID: connID, ClickhouseUser: "sso_svc", EncryptedPassword: "enc", Token: "tok-sso", ExpiresAt: expires, UserRole: "admin", AuthSubject: "eve@example.com"},
	} {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatal(err)
		}
	}
	check := func(actor, want string) {
		t.Helper()
		got, err := db.ActorRole(actor, connID)
		if err != nil || got != want {
			t.Fatalf("ActorRole(%q) = %q, %v; want %q", actor, got, err, want)
		}
	}
	check("ana", "analyst")           // cached on the session
	check("eve@example.com", "admin") // SSO person, by email
	check("sso_svc", "viewer")        // the service account is not the person
	check("nobody", "viewer")         // never signed in
	check("", "viewer")
	if err := db.SetUserRole("ana", connID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("sso:eve@example.com", "", "viewer"); err != nil {
		t.Fatal(err)
	}
	check("ana", "admin") // the override wins
	check("eve@example.com", "viewer")
}
