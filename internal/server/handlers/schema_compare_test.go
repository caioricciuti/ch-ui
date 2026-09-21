package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestSchemaCredentialsNeverBorrowOtherUsersOrBackgroundAccounts(t *testing.T) {
	db, connection := testutil.WorkerDB(t)
	other, err := db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := crypto.Encrypt("alice-password", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateSession(database.CreateSessionParams{ConnectionID: other, ClickhouseUser: "alice", EncryptedPassword: encrypted, Token: "other-human-token", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), AuthSubject: "someone-else@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	h := &SchemaCompareHandler{DB: db, Config: &config.Config{AppSecretKey: "test-secret"}}
	session := &middleware.SessionInfo{ConnectionID: connection, ClickhouseUser: "alice", EncryptedPassword: encrypted, AuthSubject: "alice@example.com"}
	if _, err := h.credentials(session, schemaEndpoint{ConnectionID: other, Database: "default"}); err == nil {
		t.Fatal("borrowed another session")
	}
	credential, err := h.credentials(session, schemaEndpoint{ConnectionID: connection, Database: "default"})
	if err != nil || credential.user != "alice" || credential.password != "alice-password" {
		t.Fatalf("own session: %+v %v", credential, err)
	}
	credential, err = h.credentials(session, schemaEndpoint{ConnectionID: other, Database: "default", Username: "bob", Password: "explicit"})
	if err != nil || credential.user != "bob" || credential.password != "explicit" {
		t.Fatalf("explicit: %+v %v", credential, err)
	}
	if _, err := h.credentials(nil, schemaEndpoint{ConnectionID: other, Database: "default", Username: "bob"}); err == nil {
		t.Fatal("anonymous explicit credentials allowed")
	}
	router := h.Routes()
	for _, path := range []string{"/connections", "/compare"} {
		method := "GET"
		if path == "/compare" {
			method = "POST"
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if rr.Code != 401 {
			t.Fatalf("anonymous %s: %d", path, rr.Code)
		}
	}
}

func TestSchemaCompareExecutesOnlyBoundedReadOnlyMetadataQueries(t *testing.T) {
	db, connection := testutil.WorkerDB(t)
	encrypted, err := crypto.Encrypt("caller-password", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		var data string
		switch {
		case strings.Contains(msg.SQL, "system.databases"):
			data = `[{"name":"visible"}]`
		case strings.Contains(msg.SQL, "system.tables"):
			data = `[{"name":"events","engine":"MergeTree","sorting_key":"id","partition_key":"","primary_key":"id","sampling_key":"","create_table_query":"CREATE TABLE events (id UInt64) ENGINE = MergeTree ORDER BY id"}]`
		case strings.Contains(msg.SQL, "system.columns"):
			data = `[{"table":"events","name":"id","type":"UInt64","position":1}]`
			if msg.Settings["param_database"] == "target" {
				data = `[{"table":"events","name":"id","type":"UInt32","position":1}]`
			}
		default:
			t.Errorf("unexpected SQL: %s", msg.SQL)
			data = `[]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	})
	h := &SchemaCompareHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
	body, _ := json.Marshal(map[string]any{"source": schemaEndpoint{ConnectionID: connection, Database: "source'; DROP TABLE x; --"}, "target": schemaEndpoint{ConnectionID: connection, Database: "target"}})
	req := httptest.NewRequest("POST", "/compare", strings.NewReader(string(body)))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: connection, ClickhouseUser: "caller", EncryptedPassword: encrypted}))
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("compare: %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "column.id") {
		t.Fatalf("missing type diff: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "caller-password") {
		t.Fatal("password returned")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("schema response is cacheable")
	}
	queries := agent.Messages()
	if len(queries) != 6 {
		t.Fatalf("expected 6 introspections, got %d", len(queries))
	}
	for _, msg := range queries {
		if !strings.HasPrefix(msg.SQL, "SELECT ") || strings.Contains(msg.SQL, "DROP TABLE") {
			t.Fatalf("unsafe SQL: %s", msg.SQL)
		}
		if msg.User != "caller" || msg.Password != "caller-password" {
			t.Fatal("wrong caller credentials")
		}
		if msg.Settings["readonly"] != "1" || msg.Settings["result_overflow_mode"] != "throw" || msg.Settings["max_execution_time"] != "15" {
			t.Fatalf("unbounded introspection: %+v", msg.Settings)
		}
	}
}

func TestSchemaCompareMetadataFailuresAreNotEmptySuccess(t *testing.T) {
	for _, mode := range []string{"denied", "malformed", "hidden", "null"} {
		t.Run(mode, func(t *testing.T) {
			db, connection := testutil.WorkerDB(t)
			encrypted, err := crypto.Encrypt("password", "test-secret")
			if err != nil {
				t.Fatal(err)
			}
			agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
				if mode == "denied" {
					return &tunnel.AgentMessage{Type: "query_error", Error: "secret remote details"}
				}
				data := `[]`
				if mode == "malformed" {
					data = `{"unexpected":"shape"}`
				}
				if mode == "null" {
					data = `null`
				}
				return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
			})
			h := &SchemaCompareHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
			body, _ := json.Marshal(map[string]any{"source": schemaEndpoint{ConnectionID: connection, Database: "source"}, "target": schemaEndpoint{ConnectionID: connection, Database: "target"}})
			req := httptest.NewRequest("POST", "/compare", strings.NewReader(string(body)))
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: connection, ClickhouseUser: "caller", EncryptedPassword: encrypted}))
			rr := httptest.NewRecorder()
			h.Routes().ServeHTTP(rr, req)
			if rr.Code != 502 {
				t.Fatalf("metadata failure became success: %d %s", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "secret remote details") {
				t.Fatal("remote error leaked")
			}
		})
	}
}
