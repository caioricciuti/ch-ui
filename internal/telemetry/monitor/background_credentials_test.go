package monitor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		data := `[{"c":0}]`
		if strings.HasPrefix(msg.SQL, "DESCRIBE") {
			data = `[{"name":"Timestamp","type":"DateTime64(9)"},{"name":"Body","type":"String"}]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	})
	mapping := telemetry.DefaultLogsMapping()
	source, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Name: "logs", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &mapping, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: "monitor", Kind: "logs", SourceID: source, WindowSeconds: 60, IntervalSeconds: 30, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r := NewMonitorRunner(db, agent.Gateway, "test-secret")
	agent.CheckWorker(t, db, conn, "telemetry.monitor", func() {
		if _, err := db.Conn().Exec("UPDATE telemetry_monitors SET last_run_at=NULL WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		r.tick()
	})
	m, err := db.GetTelemetryMonitor(id)
	if err != nil || m.LastState != "ok" || m.LastValue == nil || *m.LastValue != 0 {
		t.Fatalf("monitor result: %#v %v", m, err)
	}
}
