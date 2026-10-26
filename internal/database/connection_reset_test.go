package database

import (
	"testing"
	"time"
)

// seedConnectionState seeds connID with one of everything bound to its
// ClickHouse server and returns the ids of the identity, MCP key, OAuth
// refresh token hash and dashboard share it made.
func seedConnectionState(t *testing.T, db *DB, connID, tag string) (identityID, keyID, refreshHash, shareID string) {
	t.Helper()
	exp := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if _, err := db.CreateSession(CreateSessionParams{ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "enc", Token: "sess-" + tag, ExpiresAt: exp, UserRole: "analyst"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("alice", connID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(connID, "svc", "svc-enc"); err != nil {
		t.Fatal(err)
	}
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-"+tag, tag+"@x.com", tag, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSSOClickHouseUser(SSOClickHouseUser{IdentityID: ident.ID, ConnectionID: connID, CHUser: "sso_" + tag, EncryptedPassword: "enc"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(connID, BackgroundCredential{Worker: "schedule", Mode: "service_account", Username: "bg", EncryptedPassword: "bg-enc"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(connID, BackgroundCredential{Worker: "model", Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	key, err := db.CreateMCPKey(CreateMCPKeyParams{Name: "k", KeyHash: "hash-" + tag, KeyPrefix: "p", ConnectionID: connID, CHUser: "default", CHPasswordEnc: "enc", Kind: "oauth", Subject: tag + "@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	refreshHash = "refresh-" + tag
	if err := db.CreateOAuthRefreshToken(&OAuthRefreshToken{TokenHash: refreshHash, KeyID: key.ID, ClientID: "c", Scope: "read", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateOAuthCode(&OAuthCode{CodeHash: "code-" + tag, ClientID: "c", ConnectionID: connID, CHUser: "default", CHPasswordEnc: "enc", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}
	dash, err := db.CreateDashboard("d-"+tag, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	share, err := db.CreateDashboardShare(CreateDashboardShareParams{DashboardID: dash, AccessLevel: "viewer", ConnectionID: connID, ClickhouseUser: "default", EncryptedPassword: "enc"})
	if err != nil {
		t.Fatal(err)
	}
	return ident.ID, key.ID, refreshHash, share.ID
}

func TestRepointConnectionServerClearsOldServerState(t *testing.T) {
	db := openTestDB(t)
	oldID, err := db.CreateConnection(CreateConnectionParams{Name: "old", TunnelToken: "t-old", Type: ConnectionTypeDirect, ClickHouseURL: "http://old:8123"})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := db.CreateConnection(CreateConnectionParams{Name: "other", TunnelToken: "t-other", Type: ConnectionTypeDirect, ClickHouseURL: "http://other:8123"})
	if err != nil {
		t.Fatal(err)
	}
	identity, keyID, refresh, shareID := seedConnectionState(t, db, oldID, "a")
	otherIdentity, otherKey, otherRefresh, otherShare := seedConnectionState(t, db, otherID, "b")

	reset, err := db.RepointConnectionServer(oldID, "http://new:8123")
	if err != nil {
		t.Fatal(err)
	}
	want := ConnectionReset{Sessions: 1, RoleOverrides: 1, SSOUsers: 1, SSOAccount: true, BackgroundAccounts: 1, MCPKeys: 1, DashboardShares: 1}
	if *reset != want {
		t.Fatalf("reset = %+v, want %+v", *reset, want)
	}

	conn, err := db.GetConnectionByID(oldID)
	if err != nil || conn == nil || conn.ClickHouseURL != "http://new:8123" {
		t.Fatalf("connection after repoint = %+v, %v", conn, err)
	}
	if s, _ := db.GetSession("sess-a"); s != nil {
		t.Error("session on the old server survived")
	}
	if role, _ := db.GetUserRole("alice", oldID); role != "" {
		t.Errorf("role override survived: %q", role)
	}
	if u, _ := db.GetSSOClickHouseUser(identity, oldID); u != nil {
		t.Errorf("per-person row survived: %+v", u)
	}
	if user, enc, _ := db.GetConnectionSSOAccount(oldID); user != "" || enc != "" {
		t.Errorf("SSO account survived: %q", user)
	}
	if c, _ := db.GetBackgroundCredential(oldID, "schedule"); c.Mode != "disabled" || c.Username != "" || c.EncryptedPassword != "" {
		t.Errorf("background service account = %+v, want disabled and empty", c)
	}
	if c, _ := db.GetBackgroundCredential(oldID, "model"); c.Mode != "disabled" {
		t.Errorf("disabled worker = %+v, want still disabled", c)
	}
	if k, _ := db.GetMCPKeyByID(keyID); k == nil || k.RevokedAt == nil {
		t.Errorf("MCP key not revoked: %+v", k)
	}
	if r, _ := db.GetOAuthRefreshToken(refresh); r == nil || r.RevokedAt == nil {
		t.Errorf("refresh token not revoked: %+v", r)
	}
	if c, _ := db.ConsumeOAuthCode("code-a"); c != nil {
		t.Error("OAuth code survived")
	}
	if s, _ := db.GetDashboardShareByID(shareID); s != nil {
		t.Error("dashboard share survived")
	}
	// The SSO identity settings are kept: they hold no credential.
	if cfg, err := db.GetSSOIdentityConfig(oldID); err != nil || cfg.Mode != SSOModeShared {
		t.Errorf("identity config = %+v, %v", cfg, err)
	}

	// The other connection is untouched.
	if s, _ := db.GetSession("sess-b"); s == nil {
		t.Error("other connection's session was cleared")
	}
	if role, _ := db.GetUserRole("alice", otherID); role != "admin" {
		t.Errorf("other connection's role = %q", role)
	}
	if u, _ := db.GetSSOClickHouseUser(otherIdentity, otherID); u == nil {
		t.Error("other connection's per-person row was cleared")
	}
	if user, _, _ := db.GetConnectionSSOAccount(otherID); user != "svc" {
		t.Errorf("other connection's SSO account = %q", user)
	}
	if c, _ := db.GetBackgroundCredential(otherID, "schedule"); c.Mode != "service_account" || c.Username != "bg" {
		t.Errorf("other connection's background account = %+v", c)
	}
	if k, _ := db.GetMCPKeyByID(otherKey); k == nil || k.RevokedAt != nil {
		t.Errorf("other connection's key = %+v", k)
	}
	if r, _ := db.GetOAuthRefreshToken(otherRefresh); r == nil || r.RevokedAt != nil {
		t.Errorf("other connection's refresh token = %+v", r)
	}
	if s, _ := db.GetDashboardShareByID(otherShare); s == nil {
		t.Error("other connection's share was deleted")
	}

	if _, err := db.RepointConnectionServer("missing", "http://x:8123"); err == nil {
		t.Error("repointing a missing connection must fail")
	}
}

func TestRemoveSSOClickHouseUserAccess(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	otherConn, err := db.CreateConnection(CreateConnectionParams{Name: "o", TunnelToken: "tok-o"})
	if err != nil {
		t.Fatal(err)
	}
	ana, err := db.UpsertSSOIdentity("https://idp.example", "sub-ana", "ana@x.com", "Ana", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{connID, otherConn} {
		if err := db.SaveSSOClickHouseUser(SSOClickHouseUser{IdentityID: ana.ID, ConnectionID: c, CHUser: "sso_ana", EncryptedPassword: "enc"}); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(hash, conn string, p CreateMCPKeyParams) string {
		t.Helper()
		p.Name, p.KeyHash, p.KeyPrefix, p.ConnectionID = hash, hash, "p", conn
		k, err := db.CreateMCPKey(p)
		if err != nil {
			t.Fatal(err)
		}
		return k.ID
	}
	ref := mk("ref", connID, CreateMCPKeyParams{CHUser: "sso_ana", SSOIdentityID: ana.ID, Kind: "oauth", Subject: "ana@x.com"})
	legacy := mk("legacy", connID, CreateMCPKeyParams{CHUser: "sso_ana", CHPasswordEnc: "enc", Kind: "oauth", Subject: "Ana@X.com"})
	password := mk("password", connID, CreateMCPKeyParams{CHUser: "ana@x.com", CHPasswordEnc: "enc", Kind: "oauth", Subject: "ana@x.com"})
	elsewhere := mk("elsewhere", otherConn, CreateMCPKeyParams{CHUser: "sso_ana", SSOIdentityID: ana.ID, Kind: "oauth", Subject: "ana@x.com"})
	exp := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if err := db.CreateOAuthRefreshToken(&OAuthRefreshToken{TokenHash: "rt", KeyID: ref, ClientID: "c", Scope: "read", ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}

	n, err := db.RemoveSSOClickHouseUserAccess(ana.ID, connID, "ana@x.com")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("revoked %d keys, want 2 (the referencing key and the legacy OAuth token)", n)
	}
	u, err := db.GetSSOClickHouseUser(ana.ID, connID)
	if err != nil || u == nil {
		t.Fatalf("row must be kept for the drop: %+v %v", u, err)
	}
	if !u.Removed() || !u.Locked() || u.LockedInClickHouse() {
		t.Fatalf("row = %+v, want removed, treated as locked, not locked in ClickHouse", u)
	}
	for id, wantRevoked := range map[string]bool{ref: true, legacy: true, password: false, elsewhere: false} {
		k, err := db.GetMCPKeyByID(id)
		if err != nil || k == nil {
			t.Fatal(err)
		}
		if (k.RevokedAt != nil) != wantRevoked {
			t.Errorf("key %s revoked = %v, want %v", k.Name, k.RevokedAt != nil, wantRevoked)
		}
	}
	if r, _ := db.GetOAuthRefreshToken("rt"); r == nil || r.RevokedAt == nil {
		t.Errorf("refresh token not revoked: %+v", r)
	}
	if o, _ := db.GetSSOClickHouseUser(ana.ID, otherConn); o == nil || o.Locked() {
		t.Errorf("the other connection's row = %+v, want untouched", o)
	}
	people, err := db.ListSSOClickHouseUsers(connID)
	if err != nil || len(people) != 1 || !people[0].Removed() {
		t.Fatalf("listed = %+v, %v", people, err)
	}

	// A fresh sign-in re-provisions the user and clears the mark.
	if err := db.SaveSSOClickHouseUser(SSOClickHouseUser{IdentityID: ana.ID, ConnectionID: connID, CHUser: "sso_ana", EncryptedPassword: "enc2"}); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.GetSSOClickHouseUser(ana.ID, connID); u == nil || u.Locked() {
		t.Fatalf("after sign-in = %+v, want usable", u)
	}
}
