package models

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

// A slow group on one connection must not hold up the groups of another,
// tick must return while it runs, and a later tick must not start it again.
func TestSchedulerRunsConnectionsSideBySide(t *testing.T) {
	db, slowConn := testutil.WorkerDB(t)
	fastConn, err := db.CreateConnection(database.CreateConnectionParams{Name: "fast", TunnelToken: "fast-token"})
	if err != nil {
		t.Fatal(err)
	}
	answer := func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	}
	// The slow connection's agent takes a while to answer its model's
	// statement; it holds only that connection's socket.
	slowAgent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.Contains(msg.SQL, "slow_marker") {
			time.Sleep(1500 * time.Millisecond)
		}
		return answer(msg)
	})
	slowAgent.AddAgent(t, "fast-token", answer)
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range []string{slowConn, fastConn} {
		if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "model", Mode: "service_account", Username: "svc", EncryptedPassword: enc}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.CreateModel(slowConn, "slow", "", "default", "view", "SELECT 1 AS slow_marker", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateModel(fastConn, "fast", "", "default", "view", "SELECT 1 AS id", "", "", ""); err != nil {
		t.Fatal(err)
	}
	slowID := scheduledPipeline(t, db, slowConn, "slow", true)
	fastID := scheduledPipeline(t, db, fastConn, "fast", true)

	s := NewScheduler(db, NewRunner(db, slowAgent.Gateway, "test-secret"))
	began := time.Now()
	s.tick()
	if time.Since(began) > time.Second {
		t.Fatal("tick must not wait for runs to finish")
	}
	lastStatus := func(id string) string {
		t.Helper()
		p, err := db.GetModelPipelineByID(id)
		if err != nil || p == nil {
			t.Fatalf("pipeline %s: %v %v", id, p, err)
		}
		if p.LastStatus == nil {
			return ""
		}
		return *p.LastStatus
	}
	deadline := time.Now().Add(time.Second)
	for lastStatus(fastID) == "" {
		if time.Now().After(deadline) {
			t.Fatal("the fast connection's group waited for the slow one")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := lastStatus(fastID); got != "success" {
		t.Fatalf("fast group: %s", got)
	}
	if got := lastStatus(slowID); got != "" {
		t.Fatalf("slow group finished too early: %s", got)
	}

	// Still due and still running: the next tick leaves it alone.
	s.tick()
	s.wait()
	if got := lastStatus(slowID); got != "success" {
		t.Fatalf("slow group: %s", got)
	}
	runs, err := db.GetModelRuns(slowConn, 10, 0)
	if err != nil || len(runs) != 1 {
		t.Fatalf("the slow group must run once, got %d runs (%v)", len(runs), err)
	}
}
