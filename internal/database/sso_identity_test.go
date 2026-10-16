package database_test

import (
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func openIdentityDB(t *testing.T) (*database.DB, string) {
	t.Helper()
	db, err := dbtest.Open(t, "sso_identity.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return db, conn
}

func TestSSOIdentityConfigDefaultsAndValidation(t *testing.T) {
	db, conn := openIdentityDB(t)

	cfg, err := db.GetSSOIdentityConfig(conn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != database.SSOModeShared || cfg.UserPrefix != "sso:" || cfg.Host != database.SSOHostAuto || cfg.LockAfterDays != 30 || cfg.DropAfterDays != 90 {
		t.Errorf("default config wrong: %+v", cfg)
	}

	bad := []struct {
		name string
		cfg  database.SSOIdentityConfig
		want string
	}{
		{"mode", database.SSOIdentityConfig{Mode: "magic"}, "mode must be"},
		{"prefix", database.SSOIdentityConfig{Mode: "shared", UserPrefix: "has space"}, "user_prefix"},
		{"host", database.SSOIdentityConfig{Mode: "shared", Host: "10.0.0.1, not-an-ip"}, "not an IP"},
		{"mapping without roles", database.SSOIdentityConfig{Mode: "per_person", RoleMappings: []database.SSORoleMapping{{Group: "eng"}}}, "has no roles"},
		{"per person without mapping", database.SSOIdentityConfig{Mode: "per_person"}, "at least one group"},
		{"drop before lock", database.SSOIdentityConfig{Mode: "per_person", FallbackRoles: []string{"r"}, LockAfterDays: 30, DropAfterDays: 10}, "drop_after_days"},
	}
	for _, c := range bad {
		err := db.SetSSOIdentityConfig(conn, c.cfg)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
	if err := db.SetSSOIdentityConfig("missing", database.SSOIdentityConfig{Mode: "shared"}); err == nil {
		t.Error("unknown connection must be an error")
	}

	saved := database.SSOIdentityConfig{
		Mode:            " Per_Person ",
		UserPrefix:      " sso: ",
		SettingsProfile: " chui_sso_limits ",
		Host:            "10.0.0.1 , 192.168.1.5",
		RoleMappings: []database.SSORoleMapping{
			{Group: " Engineering ", Roles: []string{"chui_analyst", " chui_analyst", ""}},
			{Group: "Admins", Roles: []string{"chui_admin"}},
			{Group: "", Roles: nil},
		},
		FallbackRoles: []string{"chui_viewer"},
		LockAfterDays: 7,
		DropAfterDays: 7,
	}
	if err := db.SetSSOIdentityConfig(conn, saved); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSSOIdentityConfig(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !got.PerPerson() || got.UserPrefix != "sso:" || got.SettingsProfile != "chui_sso_limits" || got.Host != "10.0.0.1,192.168.1.5" {
		t.Errorf("normalized config wrong: %+v", got)
	}
	if len(got.RoleMappings) != 2 || got.RoleMappings[0].Group != "Engineering" || len(got.RoleMappings[0].Roles) != 1 {
		t.Errorf("mappings not normalized: %+v", got.RoleMappings)
	}
	if roles := got.RolesForGroups([]string{"engineering", "ADMINS", "other"}); strings.Join(roles, ",") != "chui_admin,chui_analyst" {
		t.Errorf("RolesForGroups union wrong: %v", roles)
	}
	if roles := got.RolesForGroups([]string{"nobody"}); strings.Join(roles, ",") != "chui_viewer" {
		t.Errorf("fallback roles wrong: %v", roles)
	}
	got.FallbackRoles = nil
	if roles := got.RolesForGroups([]string{"nobody"}); roles != nil {
		t.Errorf("no match and no fallback must be nil, got %v", roles)
	}
	if managed := got.ManagedRoles(); strings.Join(managed, ",") != "chui_admin,chui_analyst" {
		t.Errorf("ManagedRoles wrong: %v", managed)
	}
	if got.UserName(" Ana@Example.COM ") != "sso:ana@example.com" {
		t.Errorf("UserName wrong: %q", got.UserName(" Ana@Example.COM "))
	}
}

func TestSSOIdentityAndClickHouseUsers(t *testing.T) {
	db, conn := openIdentityDB(t)

	ident, err := db.UpsertSSOIdentity("https://idp.example.com", "sub-1", "Ana@Example.com", "Ana", []string{"eng"})
	if err != nil {
		t.Fatal(err)
	}
	if ident.Email != "ana@example.com" || ident.ID == "" || len(ident.Groups) != 1 {
		t.Fatalf("identity wrong: %+v", ident)
	}
	again, err := db.UpsertSSOIdentity("https://idp.example.com", "sub-1", "ana.new@example.com", "Ana N", []string{"eng", "admins"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != ident.ID || again.Email != "ana.new@example.com" || len(again.Groups) != 2 {
		t.Errorf("same (issuer, sub) must update the same identity: %+v", again)
	}
	other, err := db.UpsertSSOIdentity("https://idp.example.com", "sub-2", "bo@example.com", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == ident.ID || other.Groups == nil || len(other.Groups) != 0 {
		t.Errorf("another subject must be another identity with empty groups: %+v", other)
	}
	if _, err := db.UpsertSSOIdentity("", "x", "a@b", "", nil); err == nil {
		t.Error("missing issuer must be refused")
	}

	if u, err := db.GetSSOClickHouseUser(ident.ID, conn); err != nil || u != nil {
		t.Fatalf("no user yet: %v %v", u, err)
	}
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: ident.ID, ConnectionID: conn, CHUser: "sso:ana@example.com", EncryptedPassword: "enc1", Roles: []string{"chui_analyst"}}); err != nil {
		t.Fatal(err)
	}
	u, err := db.GetSSOClickHouseUser(ident.ID, conn)
	if err != nil || u == nil || u.EncryptedPassword != "enc1" || len(u.Roles) != 1 || u.Locked() {
		t.Fatalf("saved user wrong: %+v %v", u, err)
	}
	if err := db.SetSSOClickHouseUserLocked(ident.ID, conn, true); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.GetSSOClickHouseUser(ident.ID, conn); !u.Locked() {
		t.Error("user must be locked")
	}
	// A re-sync rotates the password, replaces the roles and unlocks.
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: ident.ID, ConnectionID: conn, CHUser: "sso:ana@example.com", EncryptedPassword: "enc2", Roles: []string{"chui_admin", "chui_analyst"}}); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetSSOClickHouseUser(ident.ID, conn)
	if u.EncryptedPassword != "enc2" || len(u.Roles) != 2 || u.Locked() {
		t.Errorf("re-sync wrong: %+v", u)
	}
	if owner, err := db.SSOClickHouseUserOwner(conn, "sso:ana@example.com"); err != nil || owner != ident.ID {
		t.Errorf("owner wrong: %q %v", owner, err)
	}
	if owner, _ := db.SSOClickHouseUserOwner(conn, "default"); owner != "" {
		t.Errorf("unknown user must have no owner, got %q", owner)
	}
	// The same ClickHouse user cannot belong to two identities on one connection.
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: other.ID, ConnectionID: conn, CHUser: "sso:ana@example.com", EncryptedPassword: "x"}); err == nil {
		t.Error("duplicate ch_user on a connection must be refused")
	}
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: other.ID, ConnectionID: conn, CHUser: "sso:bo@example.com", EncryptedPassword: "x", Roles: nil}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListSSOClickHouseUsers(conn)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	for _, p := range list {
		if p.Email == "" || p.Roles == nil {
			t.Errorf("list row must carry the email and non-nil roles: %+v", p)
		}
	}
	if all, _ := db.ListSSOClickHouseUsers(""); len(all) != 2 {
		t.Errorf("empty connection id lists every connection, got %d", len(all))
	}
	if err := db.DeleteSSOClickHouseUser(other.ID, conn); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListSSOClickHouseUsers(conn); len(list) != 1 {
		t.Errorf("delete left %d rows", len(list))
	}
	// Deleting the connection cascades.
	if err := db.DeleteConnection(conn); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListSSOClickHouseUsers(""); len(list) != 0 {
		t.Errorf("connection delete must cascade, %d rows left", len(list))
	}
}

func TestBorrowSkipsPerPersonSSOUsers(t *testing.T) {
	db, conn := openIdentityDB(t)
	ident, err := db.UpsertSSOIdentity("iss", "sub", "ana@example.com", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: ident.ID, ConnectionID: conn, CHUser: "sso:ana@example.com", EncryptedPassword: "enc"}); err != nil {
		t.Fatal(err)
	}
	secret := "test-secret-key-0123456789abcdef"
	encPerson := mustEncrypt(t, "person-pw", secret)
	encShared := mustEncrypt(t, "shared-pw", secret)
	// Newest session first: the person's own user must be skipped.
	if _, err := db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "chui_svc", EncryptedPassword: encShared, Token: "t1", ExpiresAt: "2999-01-01T00:00:00Z", SSOIdentityID: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "sso:ana@example.com", EncryptedPassword: encPerson, Token: "t2", ExpiresAt: "2999-01-01T00:00:00Z", AuthSubject: "ana@example.com", SSOIdentityID: ident.ID}); err != nil {
		t.Fatal(err)
	}
	user, pw, err := db.BorrowSessionCredentials(conn, "schedule", secret)
	if err != nil {
		t.Fatal(err)
	}
	if user != "chui_svc" || pw != "shared-pw" {
		t.Errorf("borrow must skip the per-person user, got %s", user)
	}
	s, err := db.GetSession("t2")
	if err != nil || s == nil || s.SSOIdentityID == nil || *s.SSOIdentityID != ident.ID {
		t.Errorf("session must carry the identity id: %+v %v", s, err)
	}
}

func mustEncrypt(t *testing.T, plaintext, secret string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plaintext, secret)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}
