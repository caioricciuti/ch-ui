package database_test

import (
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Dashboard shares, OAuth codes and MCP keys can reference an SSO person's
// per-person ClickHouse user instead of copying its password. The reference
// round-trips, and rows without one read back empty.
func TestSSOCredentialRefsRoundTrip(t *testing.T) {
	db, conn := openIdentityDB(t)

	dash, err := db.CreateDashboard("Shared", "", "ana@x.com")
	if err != nil {
		t.Fatal(err)
	}
	byRef, err := db.CreateDashboardShare(database.CreateDashboardShareParams{
		DashboardID: dash, AccessLevel: "viewer", CreatedBy: "ana@x.com",
		ConnectionID: conn, ClickhouseUser: "sso:ana@x.com", SSOIdentityID: "ident-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	byCopy, err := db.CreateDashboardShare(database.CreateDashboardShareParams{
		DashboardID: dash, AccessLevel: "viewer", ConnectionID: conn, ClickhouseUser: "alice", EncryptedPassword: "enc",
	})
	if err != nil {
		t.Fatal(err)
	}
	byToken, err := db.GetDashboardShareByToken(byRef.Token)
	byID, err2 := db.GetDashboardShareByID(byRef.ID)
	if err != nil || err2 != nil || byToken == nil || byID == nil {
		t.Fatalf("read shares: %v %v", err, err2)
	}
	for name, s := range map[string]*database.DashboardShare{"by token": byToken, "by id": byID} {
		if s.SSOIdentityID != "ident-1" || s.EncryptedPassword != "" || s.ClickhouseUser != "sso:ana@x.com" || s.Visibility != "public" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if s, err := db.GetDashboardShareByToken(byCopy.Token); err != nil || s == nil || s.SSOIdentityID != "" || s.EncryptedPassword != "enc" {
		t.Errorf("share without reference: %+v %v", s, err)
	}

	code := &database.OAuthCode{
		CodeHash: "h-ref", ClientID: "c", ClientName: "Client", RedirectURI: "https://x", Scope: "read", CodeChallenge: "cc",
		ConnectionID: conn, CHUser: "sso:ana@x.com", SSOIdentityID: "ident-1", Subject: "ana@x.com",
		ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}
	if err := db.CreateOAuthCode(code); err != nil {
		t.Fatal(err)
	}
	plain := *code
	plain.CodeHash, plain.SSOIdentityID, plain.CHPasswordEnc = "h-copy", "", "enc"
	if err := db.CreateOAuthCode(&plain); err != nil {
		t.Fatal(err)
	}
	got, err := db.ConsumeOAuthCode("h-ref")
	if err != nil || got == nil || got.SSOIdentityID != "ident-1" || got.CHPasswordEnc != "" {
		t.Fatalf("consume referenced code: %+v, %v", got, err)
	}
	got, err = db.ConsumeOAuthCode("h-copy")
	if err != nil || got == nil || got.SSOIdentityID != "" || got.CHPasswordEnc != "enc" {
		t.Fatalf("consume copied code: %+v, %v", got, err)
	}

	ref, err := db.CreateMCPKey(database.CreateMCPKeyParams{
		Name: "Client (ana@x.com)", KeyHash: "k-ref", KeyPrefix: "chm_ref...", ConnectionID: conn,
		CHUser: "sso:ana@x.com", SSOIdentityID: "ident-1", Kind: "oauth", Subject: "ana@x.com", ClientID: "c", CreatedBy: "oauth:ana@x.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	api, err := db.CreateMCPKey(database.CreateMCPKeyParams{
		Name: "api", KeyHash: "k-api", KeyPrefix: "chm_api...", ConnectionID: conn, CHUser: "svc", CHPasswordEnc: "enc", CreatedBy: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	byHash, err := db.GetMCPKeyByHash("k-ref")
	byKeyID, err2 := db.GetMCPKeyByID(ref.ID)
	if err != nil || err2 != nil || byHash == nil || byKeyID == nil {
		t.Fatalf("read keys: %v %v", err, err2)
	}
	for name, k := range map[string]*database.MCPKey{"by hash": byHash, "by id": byKeyID} {
		if k.SSOIdentityID != "ident-1" || k.CHPasswordEnc != "" || k.Kind != "oauth" {
			t.Errorf("%s: %+v", name, k)
		}
	}
	if k, err := db.GetMCPKeyByID(api.ID); err != nil || k == nil || k.SSOIdentityID != "" || k.CHPasswordEnc != "enc" {
		t.Errorf("api key: %+v %v", k, err)
	}
	keys, err := db.ListMCPKeys()
	if err != nil || len(keys) != 2 {
		t.Fatalf("list: %d keys, %v", len(keys), err)
	}
	seen := map[string]string{}
	for _, k := range keys {
		seen[k.ID] = k.SSOIdentityID
	}
	if seen[ref.ID] != "ident-1" || seen[api.ID] != "" {
		t.Errorf("list references: %v", seen)
	}

	// Rotation carries the reference with the rest of the binding.
	rotated, err := db.RotateMCPKey(ref.ID, "k-rot", "chm_rot...", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if k, err := db.GetMCPKeyByHash("k-rot"); err != nil || k == nil || k.SSOIdentityID != "ident-1" || k.ID != rotated.ID || k.CHPasswordEnc != "" {
		t.Errorf("rotated key: %+v %v", k, err)
	}
}
