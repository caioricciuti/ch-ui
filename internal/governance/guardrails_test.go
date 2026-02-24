package governance

import (
	"path/filepath"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

type guardrailTestContext struct {
	db      *database.DB
	store   *Store
	service *GuardrailService
	connID  string
}

func newGuardrailTestContext(t *testing.T) *guardrailTestContext {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "guardrails.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := &guardrailTestContext{
		db:     db,
		store:  NewStore(db),
		connID: "conn-1",
	}
	ctx.service = NewGuardrailService(ctx.store, db)

	if _, err := db.Conn().Exec(
		`INSERT INTO connections (id, name, tunnel_token, status) VALUES (?, ?, ?, ?)`,
		ctx.connID,
		"Local",
		"token-1",
		"connected",
	); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	return ctx
}

func (c *guardrailTestContext) setAccessSyncFresh(t *testing.T) {
	t.Helper()
	if err := c.store.UpsertSyncState(c.connID, string(SyncAccess), "idle", nil, nil, 0); err != nil {
		t.Fatalf("upsert access sync state: %v", err)
	}
}

func (c *guardrailTestContext) createPolicy(t *testing.T, name, severity, mode string) string {
	t.Helper()
	id, err := c.store.CreatePolicy(
		c.connID,
		name,
		"",
		"table",
		"db",
		"tbl",
		"",
		"analyst",
		severity,
		mode,
		"admin",
	)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	return id
}

func TestGuardrailsWarnPolicyAllowsExecution(t *testing.T) {
	ctx := newGuardrailTestContext(t)
	ctx.setAccessSyncFresh(t)
	ctx.createPolicy(t, "warn-policy", "warn", "warn")

	decision, err := ctx.service.EvaluateQuery(ctx.connID, "alice", "SELECT * FROM db.tbl", "/api/query/run")
	if err != nil {
		t.Fatalf("evaluate query: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected query to be allowed, got blocked: %+v", decision.Block)
	}
}

func TestGuardrailsBlockPolicyBlocksAndPersistsViolation(t *testing.T) {
	ctx := newGuardrailTestContext(t)
	ctx.setAccessSyncFresh(t)
	policyID := ctx.createPolicy(t, "block-policy", "critical", "block")

	decision, err := ctx.service.EvaluateQuery(ctx.connID, "alice", "SELECT * FROM db.tbl", "/api/query/run")
	if err != nil {
		t.Fatalf("evaluate query: %v", err)
	}
	if decision.Allowed || decision.Block == nil {
		t.Fatalf("expected blocked decision, got %+v", decision)
	}
	if decision.Block.PolicyID != policyID {
		t.Fatalf("unexpected blocked policy: got %s want %s", decision.Block.PolicyID, policyID)
	}
	if decision.Block.EnforcementMode != "block" {
		t.Fatalf("unexpected enforcement mode: %s", decision.Block.EnforcementMode)
	}

	var detectionPhase, requestEndpoint string
	if err := ctx.db.Conn().QueryRow(
		`SELECT detection_phase, COALESCE(request_endpoint, '') FROM gov_policy_violations WHERE id = ?`,
		decision.Block.ViolationID,
	).Scan(&detectionPhase, &requestEndpoint); err != nil {
		t.Fatalf("load persisted violation: %v", err)
	}
	if detectionPhase != "pre_exec_block" {
		t.Fatalf("unexpected detection phase: %s", detectionPhase)
	}
	if requestEndpoint != "/api/query/run" {
		t.Fatalf("unexpected request endpoint: %s", requestEndpoint)
	}
}

func TestGuardrailsPickDeterministicBlockingPolicy(t *testing.T) {
	ctx := newGuardrailTestContext(t)
	ctx.setAccessSyncFresh(t)
	ctx.createPolicy(t, "zzz", "warn", "block")
	expected := ctx.createPolicy(t, "aaa", "warn", "block")
	ctx.createPolicy(t, "high", "critical", "warn")

	decision, err := ctx.service.EvaluateQuery(ctx.connID, "alice", "SELECT * FROM db.tbl", "/api/query/run")
	if err != nil {
		t.Fatalf("evaluate query: %v", err)
	}
	if decision.Allowed || decision.Block == nil {
		t.Fatalf("expected blocked decision, got %+v", decision)
	}
	if decision.Block.PolicyID != expected {
		t.Fatalf("expected lexical tiebreak policy %s, got %s", expected, decision.Block.PolicyID)
	}
}

func TestGuardrailsUncertainAccessStateAllowsAndEmitsAlert(t *testing.T) {
	ctx := newGuardrailTestContext(t)
	ctx.createPolicy(t, "block-policy", "warn", "block")

	decision, err := ctx.service.EvaluateQuery(ctx.connID, "alice", "SELECT * FROM db.tbl", "/api/query/run")
	if err != nil {
		t.Fatalf("evaluate query: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allowed decision when access sync state is uncertain")
	}

	var count int
	if err := ctx.db.Conn().QueryRow(
		`SELECT COUNT(*) FROM alert_events WHERE event_type = ? AND title = ?`,
		"policy.violation",
		"Guardrail evaluation uncertain",
	).Scan(&count); err != nil {
		t.Fatalf("count uncertain guardrail alerts: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 uncertain guardrail alert event, got %d", count)
	}
}

func TestExtractPolicyTablesFromQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "select join", query: "SELECT * FROM db.tbl a JOIN db2.tbl2 b ON a.id=b.id", want: []string{"db.tbl", "db2.tbl2"}},
		{name: "insert select", query: "INSERT INTO db.target SELECT * FROM db.source", want: []string{"db.source", "db.target"}},
		{name: "show tables from", query: "SHOW TABLES FROM db", want: []string{"db.__all_tables__"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractPolicyTablesFromQuery(tc.query)
			if len(got) != len(tc.want) {
				t.Fatalf("unexpected result size: got=%v want=%v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("unexpected table at %d: got=%s want=%s", i, got[i], tc.want[i])
				}
			}
		})
	}
}
