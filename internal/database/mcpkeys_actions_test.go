package database

import "testing"

// TestMCPKeyAllowActionsMigration checks that a key stored before the
// allow_actions column existed comes back with actions off, and that the
// permission never sticks to a read key.
func TestMCPKeyAllowActionsMigration(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	// An install from before the column: drop it, store a read_write key the
	// old way, then start again.
	mustExec(t, db, `ALTER TABLE mcp_keys DROP COLUMN allow_actions`)
	mustExec(t, db, `INSERT INTO mcp_keys (id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes)
		VALUES ('legacy', 'legacy', 'legacy-hash', 'chm_legacy', '`+connID+`', 'default', 'enc', 'read_write')`)
	if err := db.runMigrations(); err != nil {
		t.Fatal(err)
	}
	legacy, err := db.GetMCPKeyByHash("legacy-hash")
	if err != nil || legacy == nil {
		t.Fatalf("legacy key: %v %v", legacy, err)
	}
	if legacy.Scopes != "read_write" || legacy.AllowActions {
		t.Fatalf("legacy key after migration: scopes=%s allow_actions=%v, want read_write and false", legacy.Scopes, legacy.AllowActions)
	}

	read, err := db.CreateMCPKey(CreateMCPKeyParams{Name: "r", KeyHash: "h-read", KeyPrefix: "p", ConnectionID: connID, CHUser: "u", CHPasswordEnc: "enc", Scopes: "read", AllowActions: true})
	if err != nil {
		t.Fatal(err)
	}
	if read.AllowActions {
		t.Error("a read key must not carry the actions permission")
	}

	act, err := db.CreateMCPKey(CreateMCPKeyParams{Name: "a", KeyHash: "h-act", KeyPrefix: "p", ConnectionID: connID, CHUser: "u", CHPasswordEnc: "enc", Scopes: "read_write", AllowActions: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetMCPKeyByID(act.ID)
	if err != nil || stored == nil || !stored.AllowActions {
		t.Fatalf("read_write key with actions: %+v %v", stored, err)
	}
	rotated, err := db.RotateMCPKey(act.ID, "h-act-2", "p2", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetMCPKeyByID(rotated.ID); got == nil || !got.AllowActions {
		t.Error("rotation must keep the actions permission")
	}
}
