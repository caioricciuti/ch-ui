package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestQueryEndpointsBlockedByGuardrailPolicy(t *testing.T) {
	h, cleanup := newBlockedQueryHandler(t)
	defer cleanup()

	tests := []struct {
		name       string
		path       string
		body       string
		invoke     func(http.ResponseWriter, *http.Request)
		wantCTJSON bool
	}{
		{name: "run", path: "/api/query/run", body: `{"query":"SELECT * FROM db.tbl"}`, invoke: h.ExecuteQuery, wantCTJSON: true},
		{name: "stream", path: "/api/query/stream", body: `{"query":"SELECT * FROM db.tbl"}`, invoke: h.StreamQuery, wantCTJSON: true},
		{name: "sample", path: "/api/query/sample", body: `{"query":"SELECT * FROM db.tbl"}`, invoke: h.SampleQuery, wantCTJSON: true},
		{name: "explain", path: "/api/query/explain", body: `{"query":"SELECT * FROM db.tbl"}`, invoke: h.ExplainQuery, wantCTJSON: true},
		{name: "plan", path: "/api/query/plan", body: `{"query":"SELECT * FROM db.tbl"}`, invoke: h.QueryPlan, wantCTJSON: true},
		{name: "profile", path: "/api/query/profile", body: `{"query":"SELECT * FROM db.tbl"}`, invoke: h.QueryProfile, wantCTJSON: true},
		{name: "explorer", path: "/api/query/explorer-data", body: `{"database":"db","table":"tbl"}`, invoke: h.ExplorerData, wantCTJSON: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
				ConnectionID:      "conn-1",
				ClickhouseUser:    "alice",
				EncryptedPassword: "unused",
			}))

			rr := httptest.NewRecorder()
			tc.invoke(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Fatalf("expected status 403, got %d body=%s", rr.Code, rr.Body.String())
			}
			if tc.wantCTJSON && rr.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("expected application/json content type, got %q", rr.Header().Get("Content-Type"))
			}
			if !bytes.Contains(rr.Body.Bytes(), []byte(`"code":"policy_blocked"`)) {
				t.Fatalf("expected policy_blocked code in response, got %s", rr.Body.String())
			}
		})
	}
}

func newBlockedQueryHandler(t *testing.T) (*QueryHandler, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query_guardrails.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	store := governance.NewStore(db)
	service := governance.NewGuardrailService(store, db)

	if _, err := db.Conn().Exec(
		`INSERT INTO connections (id, name, tunnel_token, status) VALUES (?, ?, ?, ?)`,
		"conn-1", "Local", "token-1", "connected",
	); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	if err := store.UpsertSyncState("conn-1", string(governance.SyncAccess), "idle", nil, nil, 0); err != nil {
		t.Fatalf("upsert access sync state: %v", err)
	}

	if _, err := store.CreatePolicy(
		"conn-1",
		"Block table",
		"",
		"table",
		"db",
		"tbl",
		"",
		"analyst",
		"warn",
		"block",
		"admin",
	); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	h := &QueryHandler{
		DB:         db,
		Guardrails: service,
		Config:     nil,
	}

	cleanup := func() {
		_ = db.Close()
	}
	return h, cleanup
}
