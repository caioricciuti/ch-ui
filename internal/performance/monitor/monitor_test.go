package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestMonitorOptInCredentialsCadenceAndLicense(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	row := map[string]interface{}{"hash": "42", "database": "analytics", "baseline_runs": 10, "current_runs": 20, "baseline_p95_ms": 100, "current_p95_ms": 400}
	data, _ := json.Marshal([]map[string]interface{}{row})
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: data}
	})
	active := true
	m := New(db, agent.Gateway, "test-secret", func() bool { return active })
	stop := make(chan struct{})
	now := time.Now()
	m.tick(now, stop)
	if len(agent.Messages()) != 0 {
		t.Fatal("default configuration executed a scan")
	}
	if err := db.SetPerformanceMonitor(conn, true); err != nil {
		t.Fatal(err)
	}
	enc, err := crypto.Encrypt("human-secret", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "human-admin", EncryptedPassword: enc, Token: "human", UserRole: "admin", ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	m.tick(now, stop)
	state, _ := db.GetPerformanceMonitor(conn)
	if len(agent.Messages()) != 0 || !strings.Contains(state.LastError, "dedicated") {
		t.Fatal("worker borrowed human credentials")
	}
	for i, password := range []string{"first-secret", "rotated-secret"} {
		enc, err := crypto.Encrypt(password, "test-secret")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "performance", Mode: "service_account", Username: "performance-reader", EncryptedPassword: enc}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Conn().Exec("UPDATE performance_monitor SET last_scan_at='' WHERE connection_id=?", conn); err != nil {
			t.Fatal(err)
		}
		m.tick(now, stop)
		messages := agent.Messages()
		if len(messages) != i+1 {
			t.Fatalf("worker did not run after account rotation: %d", len(messages))
		}
		msg := messages[len(messages)-1]
		if msg.User != "performance-reader" || msg.Password != password || msg.Settings["readonly"] != "1" || msg.Settings["max_execution_time"] != "25" {
			t.Fatalf("wrong worker credentials or limits: user=%s settings=%v", msg.User, msg.Settings)
		}
		state, err := db.GetPerformanceMonitor(conn)
		if err != nil || state.Report == nil || len(state.Report.Regressions) != 1 || state.LastError != "" {
			t.Fatalf("scan not persisted: %+v %v", state, err)
		}
		m.tick(now, stop)
		if len(agent.Messages()) != i+1 {
			t.Fatal("hourly cadence ignored")
		}
	}
	before := len(agent.Messages())
	active = false
	m.tick(now.Add(2*time.Hour), stop)
	if len(agent.Messages()) != before {
		t.Fatal("expired license executed SQL")
	}
	active = true
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "performance", Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	m.tick(now.Add(2*time.Hour), stop)
	if len(agent.Messages()) != before {
		t.Fatal("disabled account executed SQL")
	}
	state, _ = db.GetPerformanceMonitor(conn)
	if state.LastError == "" || state.Report == nil || len(state.Report.Regressions) != 1 {
		t.Fatal("failed scan did not preserve stale evidence")
	}
	if err := db.SetPerformanceMonitor(conn, false); err != nil {
		t.Fatal(err)
	}
	m.tick(now.Add(4*time.Hour), stop)
	if len(agent.Messages()) != before {
		t.Fatal("disabled monitor executed SQL")
	}
}

func TestMonitorStopsBeforeScanningConnections(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	if err := db.SetPerformanceMonitor(conn, true); err != nil {
		t.Fatal(err)
	}
	m := New(db, nil, "", func() bool { return true })
	stop := make(chan struct{})
	close(stop)
	m.tick(time.Now(), stop)
	state, err := db.GetPerformanceMonitor(conn)
	if err != nil || state.LastScanAt != "" {
		t.Fatal("stopped worker attempted a scan")
	}
}
