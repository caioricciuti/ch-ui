package monitor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TestTickFollowsLicense checks that enabled monitors are not evaluated while
// there is no Pro license, are evaluated during grace and while active, and
// stay enabled throughout.
func TestTickFollowsLicense(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		data := `[{"c":0}]`
		if strings.HasPrefix(msg.SQL, "DESCRIBE") {
			data = `[{"name":"Timestamp","type":"DateTime64(9)"},{"name":"Body","type":"String"}]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	})
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "telemetry.monitor", Mode: "service_account", Username: "worker", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	mapping := telemetry.DefaultLogsMapping()
	source, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Name: "logs", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &mapping, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: "monitor", Kind: "logs", SourceID: source, WindowSeconds: 60, IntervalSeconds: 30, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	makeDue := func() {
		t.Helper()
		if _, err := db.Conn().Exec("UPDATE telemetry_monitors SET last_run_at=NULL WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
	}

	access := config.ProNone
	gate := config.NewProGate("Telemetry monitors", func() config.ProAccess { return access })
	r := NewMonitorRunner(db, agent.Gateway, "test-secret", gate.Allow)

	makeDue()
	r.tick()
	if n := len(agent.Messages()); n != 0 {
		t.Fatalf("ProNone: %d queries sent, want 0", n)
	}
	m, err := db.GetTelemetryMonitor(id)
	if err != nil || m == nil || !m.Enabled || m.LastRunAt != nil {
		t.Fatalf("ProNone must leave the monitor enabled and unevaluated: %#v %v", m, err)
	}

	sent := 0
	for _, state := range []config.ProAccess{config.ProGrace, config.ProActive} {
		access = state
		makeDue()
		r.tick()
		n := len(agent.Messages())
		if n <= sent {
			t.Fatalf("access %v: no queries sent", state)
		}
		sent = n
		m, err := db.GetTelemetryMonitor(id)
		if err != nil || m == nil || m.LastRunAt == nil || m.LastState != "ok" {
			t.Fatalf("access %v: monitor not evaluated: %#v %v", state, m, err)
		}
	}

	access = config.ProNone
	makeDue()
	r.tick()
	if n := len(agent.Messages()); n != sent {
		t.Fatalf("back to ProNone: %d queries sent, want %d", n, sent)
	}
}
