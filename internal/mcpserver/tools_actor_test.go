package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// An OAuth token granted by an SSO person runs as the shared service account
// but records history and audit under the person (the key Subject). An admin
// API key has no subject and records under its ClickHouse user.
func TestRecordQueryUsesKeySubjectAsActor(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "actor.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "cht_actor", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	deps := Deps{DB: db}

	alice := &authedKey{key: &database.MCPKey{Name: "alice token", ConnectionID: connID, CHUser: "svc_sso", Subject: "alice@example.com", Kind: "oauth"}}
	apiKey := &authedKey{key: &database.MCPKey{Name: "api key", ConnectionID: connID, CHUser: "svc_sso", Kind: "api"}}

	recordQuery(deps, alice, "SELECT 'alice'", "success", "", time.Millisecond, 1)
	recordQuery(deps, apiKey, "SELECT 'api'", "success", "", time.Millisecond, 1)

	deadline := time.Now().Add(3 * time.Second)
	for {
		aliceRows, err := db.GetQueryHistory("alice@example.com", connID, "", "", 50, 0)
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		apiRows, err := db.GetQueryHistory("svc_sso", connID, "", "", 50, 0)
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		logs, err := db.GetAuditLogs(20)
		if err != nil {
			t.Fatalf("audit logs: %v", err)
		}
		audits := 0
		for _, l := range logs {
			if l.Action == "mcp.query.execute" {
				audits++
			}
		}
		if len(aliceRows) == 1 && len(apiRows) == 1 && audits == 2 {
			if aliceRows[0].QueryText != "SELECT 'alice'" || aliceRows[0].Source != "mcp" || aliceRows[0].User != "svc_sso" {
				t.Fatalf("alice's row: %+v", aliceRows[0])
			}
			if apiRows[0].QueryText != "SELECT 'api'" {
				t.Fatalf("api key row: %+v", apiRows[0])
			}
			for _, l := range logs {
				if l.Action != "mcp.query.execute" || l.Username == nil {
					continue
				}
				switch *l.Username {
				case "alice@example.com":
					if l.ChUser == nil || *l.ChUser != "svc_sso" {
						t.Fatalf("alice's audit ch_user = %v, want svc_sso", l.ChUser)
					}
				case "svc_sso":
					if l.ChUser != nil {
						t.Fatalf("api key audit ch_user = %q, want empty (same as username)", *l.ChUser)
					}
				default:
					t.Fatalf("unexpected audit username %q", *l.Username)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("alice rows %d, api rows %d, audits %d; want 1, 1, 2", len(aliceRows), len(apiRows), audits)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWriteAuditUsesKeySubjectAsActor(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "actor.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "cht_actor_w", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	bob := &authedKey{key: &database.MCPKey{Name: "bob token", ConnectionID: connID, CHUser: "svc_sso", Subject: "bob@example.com", Kind: "oauth"}}
	audit(Deps{DB: db}, bob, "dashboard.created", "test")

	deadline := time.Now().Add(3 * time.Second)
	for {
		logs, err := db.GetAuditLogs(20)
		if err != nil {
			t.Fatalf("audit logs: %v", err)
		}
		for _, l := range logs {
			if l.Action == "dashboard.created" {
				if l.Username == nil || *l.Username != "bob@example.com" || l.ChUser == nil || *l.ChUser != "svc_sso" {
					t.Fatalf("audit username=%v ch_user=%v, want bob@example.com / svc_sso", l.Username, l.ChUser)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no dashboard.created audit row")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
