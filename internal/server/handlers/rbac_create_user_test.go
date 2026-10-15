// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// lockedBuffer is a log sink the gateway's goroutines may write to while
// the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A create_user goes through plan, apply, audit, access history and the
// logs, and its password hash and salt appear in none of them: only the
// statement sent to ClickHouse carries them.
func TestRBACCreateUserNeverLeaksSecret(t *testing.T) {
	const hash = "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8"
	const salt = "f00dfacecafe1234"

	logs := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	db, err := dbtest.OpenIn(t, dir, "rbac.db")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "create-user-token"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAccess{}
	var sentMu sync.Mutex
	var sent []string
	agent := testutil.NewAgent(t, db, "create-user-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		sql := msg.SQL
		if sql == "" {
			sql = msg.Query
		}
		if strings.HasPrefix(sql, "CREATE USER") {
			sentMu.Lock()
			sent = append(sent, sql)
			sentMu.Unlock()
			if strings.Contains(sql, "chui_fail") {
				// ClickHouse can quote the statement in an error.
				return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 62. DB::Exception: Syntax error: failed at (" + sql + ") (version 25.8)"}
			}
		}
		return fake.respond(msg)
	})
	cfg := &config.Config{AppSecretKey: "create-user-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	enc, err := crypto.Encrypt("pw", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	router := (&RBACHandler{DB: db, Gateway: agent.Gateway, Config: cfg}).Routes()
	var seen strings.Builder
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			UserRole: "admin", ClickhouseUser: "admin", ConnectionID: conn, EncryptedPassword: enc, AuthSubject: "ops@example.test",
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		seen.WriteString(rec.Body.String())
		return rec
	}

	auth := `{"method":"sha256_hash","hash":"` + hash + `","salt":"` + salt + `"}`
	ops := `[{"kind":"create_user","name":"chui_ann","auth":` + auth + `,"hosts":[{"kind":"local"}]},` +
		`{"kind":"grant_privilege","target":{"type":"user","name":"chui_ann"},"access":["SELECT"],"database":"d"},` +
		`{"kind":"create_user","name":"chui_fail","auth":` + auth + `}]`
	rec := call("POST", "/plan", `{"ops":`+ops+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body.String())
	}
	var pr planResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil {
		t.Fatal(err)
	}
	if want := "CREATE USER `chui_ann` IDENTIFIED WITH sha256_hash BY '***' SALT '***' HOST LOCAL"; pr.Plan.Statements[0].SQL != want {
		t.Fatalf("plan SQL = %s", pr.Plan.Statements[0].SQL)
	}
	rec = call("POST", "/apply", `{"ops":`+ops+`,"plan_hash":"`+pr.Plan.PlanHash+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"failed":1`) || !strings.Contains(rec.Body.String(), `"kind":"drop_user","name":"chui_ann"`) {
		t.Fatalf("apply body = %s", rec.Body.String())
	}
	sentMu.Lock()
	defer sentMu.Unlock()
	if len(sent) != 2 || !strings.Contains(sent[0], "BY '"+hash+"' SALT '"+salt+"'") {
		t.Fatalf("ClickHouse did not get the real statements: %v", sent)
	}

	// History, as the API shows it.
	rec = call("GET", "/history", "")
	var hist struct {
		Snapshots []struct {
			ID string `json:"id"`
		} `json:"snapshots"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &hist)
	if len(hist.Snapshots) == 0 {
		t.Fatalf("no history: %s", rec.Body.String())
	}
	for _, s := range hist.Snapshots {
		call("GET", "/history/"+s.ID, "")
		call("GET", "/history/"+s.ID+"/diff", "")
	}

	audit, err := db.GetAuditLogs(200)
	if err != nil {
		t.Fatal(err)
	}
	auditJSON, _ := json.Marshal(audit)
	if !strings.Contains(string(auditJSON), "rbac.statement.failed") || !strings.Contains(string(auditJSON), "BY '***'") {
		t.Fatalf("audit lacks the masked statements: %s", auditJSON)
	}

	where := map[string]string{"responses": seen.String(), "audit log": string(auditJSON), "logs": logs.String()}
	// Everything on disk: the database, its WAL and anything else written.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		where[f] = string(b)
	}
	for place, text := range where {
		for _, secret := range []string{hash, salt, strings.ToUpper(hash)} {
			if strings.Contains(text, secret) {
				t.Errorf("%s holds a secret", place)
			}
		}
	}
}
