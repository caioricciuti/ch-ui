// Package testutil provides a protocol-level agent for worker integration tests.
package testutil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/gorilla/websocket"
)

type Agent struct {
	Gateway *tunnel.Gateway
	mu      sync.Mutex
	queries []tunnel.GatewayMessage
}

// NewWorkerAgent optionally executes the worker suite against a disposable
// ClickHouse. Normal CI retains deterministic protocol fixtures.
func NewWorkerAgent(t *testing.T, db *database.DB, respond func(tunnel.GatewayMessage) *tunnel.AgentMessage) *Agent {
	if endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL"); endpoint != "" {
		client := connector.NewCHClient(endpoint, false)
		respond = func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := client.Execute(ctx, msg.SQL, msg.User, msg.Password, msg.Settings)
			if err != nil {
				return &tunnel.AgentMessage{Type: "query_error", Error: err.Error()}
			}
			data, _ := json.Marshal(result.Data)
			meta, _ := json.Marshal(result.Meta)
			return &tunnel.AgentMessage{Type: "query_result", Data: data, Meta: meta}
		}
	}
	return NewAgent(t, db, "worker-token", respond)
}

func WorkerDB(t *testing.T) (*database.DB, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "worker", TunnelToken: "worker-token"})
	if err != nil {
		t.Fatal(err)
	}
	return db, conn
}

// NewAgent authenticates through the real gateway and answers query messages.
// Returning nil leaves a request pending, allowing cancellation tests.
func NewAgent(t *testing.T, db *database.DB, token string, respond func(tunnel.GatewayMessage) *tunnel.AgentMessage) *Agent {
	t.Helper()
	a := &Agent{Gateway: tunnel.NewGateway(db)}
	t.Cleanup(a.Gateway.Stop)
	srv := httptest.NewServer(http.HandlerFunc(a.Gateway.HandleWebSocket))
	t.Cleanup(srv.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(tunnel.AgentMessage{Type: "auth", Token: token}); err != nil {
		t.Fatal(err)
	}
	var reply tunnel.GatewayMessage
	if err := ws.ReadJSON(&reply); err != nil || reply.Type != "auth_ok" {
		t.Fatalf("auth: %s %v", reply.Type, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg tunnel.GatewayMessage
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == "ping" {
				if err := ws.WriteJSON(tunnel.AgentMessage{Type: "pong"}); err != nil {
					return
				}
				continue
			}
			a.mu.Lock()
			a.queries = append(a.queries, msg)
			a.mu.Unlock()
			if response := respond(msg); response != nil {
				response.ID = msg.ID
				response.QueryID = msg.QueryID
				if err := ws.WriteJSON(response); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		ws.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("agent did not stop")
		}
	})
	return a
}

func (a *Agent) Messages() []tunnel.GatewayMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]tunnel.GatewayMessage(nil), a.queries...)
}

// CheckWorker exercises actual worker execution without sessions, then rotates
// and disables the account. Disabled execution must send no further SQL.
func (a *Agent) CheckWorker(t *testing.T, db *database.DB, conn, worker string, run func()) {
	t.Helper()
	username := "worker_" + strings.ReplaceAll(worker, ".", "_")
	for _, password := range []string{"first-secret", "rotated-secret"} {
		if endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL"); endpoint != "" {
			client := connector.NewCHClient(endpoint, false)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := client.Execute(ctx, "CREATE USER OR REPLACE "+username+" IDENTIFIED BY '"+password+"'", os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), nil)
			if err == nil {
				_, err = client.Execute(ctx, "GRANT SELECT, INSERT, CREATE TABLE, CREATE VIEW, DROP TABLE, DROP VIEW, ALTER, SHOW ON *.* TO "+username, os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), nil)
			}
			cancel()
			if err != nil {
				t.Fatal(err)
			}
		}
		enc, err := crypto.Encrypt(password, "test-secret")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: worker, Mode: "service_account", Username: username, EncryptedPassword: enc}); err != nil {
			t.Fatal(err)
		}
		before := len(a.Messages())
		run()
		msgs := a.Messages()[before:]
		if len(msgs) == 0 {
			t.Fatal("worker did not execute SQL")
		}
		for _, msg := range msgs {
			if msg.Type == "query" && (msg.User != username || msg.Password != password) {
				t.Fatalf("wrong credentials for %s", worker)
			}
		}
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: worker, Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	before := len(a.Messages())
	run()
	if len(a.Messages()) != before {
		t.Fatal("disabled worker executed SQL")
	}
}
