package database

import (
	"testing"
	"time"
)

func TestOAuthKeyRole(t *testing.T) {
	db, a, b, l := scopeFixture(t)
	live := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	gone := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	for _, p := range []CreateSessionParams{
		{ConnectionID: a, ClickhouseUser: "bob", Token: "bob-a", ExpiresAt: live, UserRole: "viewer"},
		{ConnectionID: a, ClickhouseUser: "svc", Token: "carol-old", ExpiresAt: gone, UserRole: "admin", AuthSubject: "carol@example.com"},
		{ConnectionID: l, ClickhouseUser: "eve", Token: "eve-l", ExpiresAt: live, UserRole: "admin"},
	} {
		p.EncryptedPassword = "enc"
		if _, err := db.CreateSession(p); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		key  MCPKey
		want string
	}{
		{"api keys have no person", MCPKey{Kind: "api", ConnectionID: a, CHUser: "bob", GrantRole: "admin"}, ""},
		{"live session beats the issue-time role", MCPKey{Kind: "oauth", ConnectionID: a, CHUser: "bob", Subject: "bob", GrantRole: "analyst"}, "viewer"},
		{"no session on that connection: issue-time role", MCPKey{Kind: "oauth", ConnectionID: b, CHUser: "bob", Subject: "bob", GrantRole: "analyst"}, "analyst"},
		{"expired session is ignored", MCPKey{Kind: "oauth", ConnectionID: a, CHUser: "svc", Subject: "carol@example.com", GrantRole: "analyst"}, "analyst"},
		{"nothing known: viewer", MCPKey{Kind: "oauth", ConnectionID: a, CHUser: "svc", Subject: "carol@example.com"}, "viewer"},
		{"login page connection caps admin", MCPKey{Kind: "oauth", ConnectionID: l, CHUser: "eve", Subject: "eve"}, "analyst"},
	}
	for _, tc := range cases {
		got, err := db.OAuthKeyRole(&tc.key)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q (%v), want %q", tc.name, got, err, tc.want)
		}
	}

	unknown := MCPKey{Kind: "oauth", ConnectionID: a, CHUser: "svc", Subject: "carol@example.com"}
	if role, known, err := db.OAuthKeyRoleKnown(&unknown); err != nil || known || role != "viewer" {
		t.Fatalf("nothing known: got %q known=%v (%v)", role, known, err)
	}

	// An override wins over sessions and the issue-time role, and counts as
	// knowing the role.
	if err := db.SetUserRole("sso:carol@example.com", "", "viewer"); err != nil {
		t.Fatal(err)
	}
	k := MCPKey{Kind: "oauth", ConnectionID: a, CHUser: "svc", Subject: "carol@example.com", GrantRole: "admin"}
	if got, _ := db.OAuthKeyRole(&k); got != "viewer" {
		t.Fatalf("override: got %q, want viewer", got)
	}
	if role, known, _ := db.OAuthKeyRoleKnown(&unknown); !known || role != "viewer" {
		t.Fatalf("override only: got %q known=%v", role, known)
	}
}
