// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

func TestCostsConfigWriteIsAdminOnlyAndAudited(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "costs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	router := (&CostsHandler{DB: db, Config: &config.Config{}}).Routes()
	for _, role := range []string{"viewer", "analyst"} {
		if rec := serveAs(router, role, conn, http.MethodPut, "/config", `{"currency":"EUR"}`); rec.Code != http.StatusForbidden {
			t.Errorf("%s PUT /config: status %d, want 403", role, rec.Code)
		}
	}
	if rec := serveAs(router, "admin", conn, http.MethodPut, "/config", `{"currency":"EUR"}`); rec.Code != http.StatusOK {
		t.Fatalf("admin PUT /config: status %d: %s", rec.Code, rec.Body.String())
	}
	logs, err := db.GetAuditLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != "costs.config.updated" || logs[0].Details == nil || !strings.Contains(*logs[0].Details, "EUR") {
		t.Fatalf("want one costs.config.updated audit entry, got %+v", logs)
	}
	if rec := serveAs(router, "viewer", conn, http.MethodGet, "/config", ""); rec.Code != http.StatusOK {
		t.Errorf("viewer GET /config: status %d, reads stay open", rec.Code)
	}
}

func TestClusterHealthKillRequiresWriter(t *testing.T) {
	router := (&ClusterHealthHandler{Config: &config.Config{}}).Routes()
	for _, path := range []string{"/kill-query", "/kill-mutation"} {
		if rec := serveAs(router, "viewer", "c", http.MethodPost, path, `{}`); rec.Code != http.StatusForbidden {
			t.Errorf("viewer POST %s: status %d, want 403", path, rec.Code)
		}
		// Writers get past the role check; the empty body is then rejected.
		if rec := serveAs(router, "analyst", "c", http.MethodPost, path, `{}`); rec.Code != http.StatusBadRequest {
			t.Errorf("analyst POST %s: status %d, want 400", path, rec.Code)
		}
	}
}

// Kills a real running query through the handler, with the session's own
// ClickHouse credentials, and checks the audit entry.
func TestClusterHealthKillQueryLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	password := os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	enc, err := crypto.Encrypt(password, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &ClusterHealthHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}, Store: clusterhealth.NewStore(db)}
	router := h.Routes()
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			ConnectionID: conn, ClickhouseUser: user, EncryptedPassword: enc, UserRole: "analyst",
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	queryID := fmt.Sprintf("chui-kill-test-%d", time.Now().UnixNano())
	client := connector.NewCHClient(endpoint, false)
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err := client.Execute(ctx, "SELECT count() FROM numbers(1000000000000) WHERE NOT ignore(sipHash64(number))", user, password,
			map[string]string{"query_id": queryID, "max_execution_time": "60"})
		done <- err
	}()

	var rec *httptest.ResponseRecorder
	for i := 0; i < 50; i++ {
		time.Sleep(200 * time.Millisecond)
		rec = post("/kill-query", `{"query_id":"`+queryID+`"}`)
		if rec.Code == http.StatusOK {
			break
		}
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("kill-query: status %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("killed query finished without an error")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("query was not cancelled")
	}
	logs, err := db.GetAuditLogs(10)
	if err != nil || len(logs) == 0 || logs[0].Action != "cluster_health.query.killed" {
		t.Fatalf("missing kill audit entry: %v %+v", err, logs)
	}

	if rec := post("/kill-query", `{"query_id":"`+queryID+`"}`); rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Errorf("second kill: status %d", rec.Code)
	}
	if rec := post("/kill-mutation", `{"database":"nope","table":"nope","mutation_id":"x"}`); rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Errorf("kill-mutation of nothing: status %d: %s", rec.Code, rec.Body.String())
	}
}
