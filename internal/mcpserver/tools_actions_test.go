package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/reports"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

var (
	actionTools    = []string{"run_model", "start_pipeline", "stop_pipeline"}
	proActionTools = []string{"run_schedule_now", "run_saved_report_now"}
	statusTools    = []string{"get_model_run", "pipeline_status"}
)

// withRunners adds the runners the action tools need to deps.
func withRunners(deps Deps) Deps {
	deps.ModelRunner = models.NewRunner(deps.DB, deps.Gateway, "test-secret")
	deps.PipelineRunner = pipelines.NewRunner(deps.DB, deps.Gateway, deps.Config)
	deps.ReportRunner = reports.NewRunner(deps.DB, deps.Gateway, nil, "test-secret", "", nil)
	return deps
}

// actionDeps is writeDeps plus a read_write key with the actions
// permission.
func actionDeps(t *testing.T) (deps Deps, readKey, writeKey, actKey string) {
	t.Helper()
	deps, readKey, writeKey = writeDeps(t)
	deps = withRunners(deps)
	conns, err := deps.DB.GetConnections()
	if err != nil || len(conns) == 0 {
		t.Fatalf("connections: %v", err)
	}
	enc, _ := crypto.Encrypt("chpass", deps.Config.AppSecretKey)
	actKey, hash, prefix := GenerateKey()
	if _, err := deps.DB.CreateMCPKey(database.CreateMCPKeyParams{Name: "actor", KeyHash: hash, KeyPrefix: prefix, ConnectionID: conns[0].ID, CHUser: "default", CHPasswordEnc: enc, Scopes: "read_write", AllowActions: true, CreatedBy: "admin"}); err != nil {
		t.Fatalf("create action key: %v", err)
	}
	return deps, readKey, writeKey, actKey
}

func toolList(t *testing.T, deps Deps, key string) string {
	t.Helper()
	return mcpRequest(t, Handler(deps), key, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`).Body.String()
}

func TestActionToolsGating(t *testing.T) {
	deps, readKey, writeKey, actKey := actionDeps(t)

	for name, key := range map[string]string{"read": readKey, "read_write": writeKey} {
		list := toolList(t, deps, key)
		for _, tool := range append(append([]string{}, actionTools...), proActionTools...) {
			if strings.Contains(list, `"`+tool+`"`) {
				t.Errorf("%s key without actions must not see %q", name, tool)
			}
		}
		for _, tool := range statusTools {
			if !strings.Contains(list, `"`+tool+`"`) {
				t.Errorf("%s key should see the read-only %q", name, tool)
			}
		}
	}

	list := toolList(t, deps, actKey)
	for _, tool := range actionTools {
		if !strings.Contains(list, `"`+tool+`"`) {
			t.Errorf("key with actions should see %q", tool)
		}
	}
	for _, tool := range proActionTools {
		if strings.Contains(list, `"`+tool+`"`) {
			t.Errorf("%q is Pro and must be absent without a license", tool)
		}
	}

	deps.Config.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	list = toolList(t, deps, actKey)
	for _, tool := range proActionTools {
		if !strings.Contains(list, `"`+tool+`"`) {
			t.Errorf("key with actions on Pro should see %q", tool)
		}
	}
	if strings.Contains(toolList(t, deps, writeKey), `"run_schedule_now"`) {
		t.Error("Pro does not give actions to a key without the permission")
	}

	// A role-demoted OAuth token loses actions with write.
	ak := &authedKey{key: &database.MCPKey{Scopes: "read_write", AllowActions: true}, roleReadOnly: true}
	if ak.canAct() {
		t.Error("a demoted OAuth token must not act")
	}
	ak.roleReadOnly = false
	if !ak.canAct() {
		t.Error("read_write with actions should act")
	}
	ak.key.Scopes = "read"
	if ak.canAct() {
		t.Error("a read key must not act, whatever allow_actions says")
	}
}

// Clients that can confirm get an input request first; nothing runs until
// the user accepts, and a decline runs nothing.
func TestRunModelAsksForConfirmation(t *testing.T) {
	deps, _, _, actKey := actionDeps(t)
	conns, _ := deps.DB.GetConnections()
	if _, err := deps.DB.CreateModel(conns[0].ID, "daily", "", "default", "view", "SELECT 1", "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	h := Handler(deps)
	caps := `{"elicitation":{"form":{}}}`

	r := decodeToolResult(t, callToolVersioned(t, h, actKey, newProtocol, caps, "run_model", `{"model":"daily"}`, ""))
	req, ok := r.Result.InputRequests[confirmInputID]
	if !ok {
		t.Fatalf("want a confirmation request, got %+v", r.Result)
	}
	if !strings.Contains(string(req), `Run model \"daily\"`) || !strings.Contains(string(req), `user \"default\"`) {
		t.Errorf("prompt should name the model and user: %s", req)
	}
	if runs, _ := deps.DB.GetModelRuns(conns[0].ID, 10, 0); len(runs) != 0 {
		t.Fatalf("nothing may run before the user answers, got %d runs", len(runs))
	}

	decline := `"inputResponses":{"` + confirmInputID + `":{"action":"accept","content":{"confirm":false}}}`
	r = decodeToolResult(t, callToolVersioned(t, h, actKey, newProtocol, caps, "run_model", `{"model":"daily"}`, decline))
	if len(r.Result.Content) == 0 || !strings.Contains(r.Result.Content[0].Text, "declined") {
		t.Fatalf("declined run: %+v", r.Result)
	}
	if runs, _ := deps.DB.GetModelRuns(conns[0].ID, 10, 0); len(runs) != 0 {
		t.Fatalf("a declined run must not run, got %d runs", len(runs))
	}
}

// actingKey is an authedKey with the actions permission over fakeDeps.
func actingDeps(t *testing.T, allowed string) (Deps, *authedKey, *fakeCH) {
	t.Helper()
	deps, ak, f := fakeDeps(t, allowed)
	deps = withRunners(deps)
	ak.key.Scopes = "read_write"
	ak.key.AllowActions = true
	ak.key.CHUser = "mcp_user"
	ak.chPassword = "mcp_pass"
	return deps, ak, f
}

func callAction(t *testing.T, deps Deps, ak *authedKey, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	srv := buildServer(deps, ak)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (f *fakeCH) all() []tunnel.GatewayMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tunnel.GatewayMessage(nil), f.sent...)
}

func TestRunModelRunsAsKeyUser(t *testing.T) {
	deps, ak, f := actingDeps(t, "")
	if _, err := deps.DB.CreateModel(ak.key.ConnectionID, "daily", "", "default", "view", "SELECT 1 AS x", "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	res := callAction(t, deps, ak, "run_model", map[string]any{"model": "daily"})
	if res.IsError {
		t.Fatalf("run_model: %s", resultText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "success" || out["run_id"] == "" {
		t.Fatalf("unexpected result: %v", out)
	}
	sent := f.all()
	if len(sent) == 0 {
		t.Fatal("the build sent nothing to ClickHouse")
	}
	for _, m := range sent {
		if m.User != "mcp_user" || m.Password != "mcp_pass" {
			t.Fatalf("model DDL ran as %q, want the key's user: %s", m.User, m.SQL)
		}
	}
	runs, _ := deps.DB.GetModelRuns(ak.key.ConnectionID, 10, 0)
	if len(runs) != 1 || runs[0].TriggeredBy == nil || *runs[0].TriggeredBy != "mcp:test" {
		t.Fatalf("run should be recorded as triggered by the key: %+v", runs)
	}

	// get_model_run reads it back by run id and by model.
	status := callAction(t, deps, ak, "get_model_run", map[string]any{"run_id": runs[0].ID})
	if status.IsError || !strings.Contains(resultText(status), `"model":"daily"`) {
		t.Fatalf("get_model_run by id: %s", resultText(status))
	}
	status = callAction(t, deps, ak, "get_model_run", map[string]any{"model": "daily"})
	if status.IsError || !strings.Contains(resultText(status), `"latest_result"`) {
		t.Fatalf("get_model_run by model: %s", resultText(status))
	}
}

func TestRunModelAllowList(t *testing.T) {
	cases := []struct {
		name, target, sql string
	}{
		{"target outside the list", "analytics", "SELECT 1"},
		{"reads outside the list", "sales", "SELECT * FROM secret.payroll"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps, ak, f := actingDeps(t, "sales")
			if _, err := deps.DB.CreateModel(ak.key.ConnectionID, "m", "", c.target, "view", c.sql, "", "", "admin"); err != nil {
				t.Fatal(err)
			}
			res := callAction(t, deps, ak, "run_model", map[string]any{"model": "m"})
			if !res.IsError || !strings.Contains(resultText(res), "allowlist") {
				t.Fatalf("want an allowlist refusal, got: %s", resultText(res))
			}
			if runs, _ := deps.DB.GetModelRuns(ak.key.ConnectionID, 10, 0); len(runs) != 0 {
				t.Fatalf("a refused run must not be recorded, got %d", len(runs))
			}
			for _, m := range f.all() {
				if !strings.Contains(m.SQL, "currentDatabase()") {
					t.Fatalf("a refused run must not send DDL: %s", m.SQL)
				}
			}
		})
	}
}

func TestRunModelAllowListPassesInsideTheList(t *testing.T) {
	deps, ak, _ := actingDeps(t, "sales")
	if _, err := deps.DB.CreateModel(ak.key.ConnectionID, "m", "", "sales", "view", "SELECT * FROM sales.orders", "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if res := callAction(t, deps, ak, "run_model", map[string]any{"model": "m"}); res.IsError {
		t.Fatalf("a model inside the allowlist should run: %s", resultText(res))
	}
}

func TestActionToolsConnectionScoping(t *testing.T) {
	deps, ak, _ := actingDeps(t, "")
	other, err := deps.DB.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.DB.CreateModel(other, "foreign", "", "default", "view", "SELECT 1", "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	pid, err := deps.DB.CreatePipeline("foreign-pipe", "", other, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for tool, args := range map[string]map[string]any{
		"run_model":       {"model": "foreign"},
		"get_model_run":   {"model": "foreign"},
		"start_pipeline":  {"pipeline": pid},
		"stop_pipeline":   {"pipeline": "foreign-pipe"},
		"pipeline_status": {"pipeline": pid},
	} {
		res := callAction(t, deps, ak, tool, args)
		if !res.IsError || !strings.Contains(resultText(res), "not found on this connection") {
			t.Errorf("%s must not reach another connection's entity: %s", tool, resultText(res))
		}
	}
	runID, err := deps.DB.CreateModelRun(other, 1, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if res := callAction(t, deps, ak, "get_model_run", map[string]any{"run_id": runID}); !res.IsError {
		t.Errorf("a run on another connection must be not found: %s", resultText(res))
	}
}

// pipelineWith stores a pipeline with one source and a ClickHouse sink.
func pipelineWith(t *testing.T, db *database.DB, conn, name, sourceType, sourceCfg, sinkDB string) string {
	t.Helper()
	id, err := db.CreatePipeline(name, "", conn, "admin")
	if err != nil {
		t.Fatal(err)
	}
	nodes := []database.PipelineNode{
		{ID: id + "-src", NodeType: sourceType, Label: "src", ConfigEncrypted: sourceCfg},
		{ID: id + "-sink", NodeType: "sink_clickhouse", Label: "sink", ConfigEncrypted: `{"database":"` + sinkDB + `","table":"t"}`},
	}
	edges := []database.PipelineEdge{{ID: id + "-e", SourceNodeID: id + "-src", TargetNodeID: id + "-sink"}}
	if err := db.SavePipelineGraph(id, nodes, edges, `{}`); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPipelineActionGuards(t *testing.T) {
	deps, ak, _ := actingDeps(t, "sales")
	outside := pipelineWith(t, deps.DB, ak.key.ConnectionID, "outside", "source_webhook", `{}`, "secret")
	sqlite := pipelineWith(t, deps.DB, ak.key.ConnectionID, "local-file", "source_database", `{"db_type":"sqlite","connection_string":"/tmp/x.db"}`, "sales")
	idle := pipelineWith(t, deps.DB, ak.key.ConnectionID, "idle", "source_webhook", `{}`, "sales")

	for tool, args := range map[string]map[string]any{
		"start_pipeline":  {"pipeline": outside},
		"pipeline_status": {"pipeline": outside},
	} {
		res := callAction(t, deps, ak, tool, args)
		if !res.IsError || !strings.Contains(resultText(res), "allowlist") {
			t.Errorf("%s on a sink outside the allowlist: %s", tool, resultText(res))
		}
	}
	if res := callAction(t, deps, ak, "start_pipeline", map[string]any{"pipeline": sqlite}); !res.IsError || !strings.Contains(resultText(res), "SQLite") {
		t.Errorf("SQLite source must be refused: %s", resultText(res))
	}
	if res := callAction(t, deps, ak, "stop_pipeline", map[string]any{"pipeline": idle}); !res.IsError || !strings.Contains(resultText(res), "not running") {
		t.Errorf("stopping an idle pipeline: %s", resultText(res))
	}
	res := callAction(t, deps, ak, "pipeline_status", map[string]any{"pipeline": "idle", "runs": 1000})
	if res.IsError || !strings.Contains(resultText(res), `"recent_runs"`) {
		t.Errorf("pipeline_status: %s", resultText(res))
	}
}

func TestRunScheduleNowRunsAsKeyUser(t *testing.T) {
	deps, ak, f := actingDeps(t, "")
	deps.Config.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	conn := ak.key.ConnectionID
	qid, err := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "nightly", Query: "INSERT INTO t SELECT 1", ConnectionID: conn, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.DB.CreateSchedule("nightly-job", qid, conn, "0 3 * * *", "UTC", "admin", 0); err != nil {
		t.Fatal(err)
	}
	res := callAction(t, deps, ak, "run_schedule_now", map[string]any{"schedule": "nightly-job"})
	if res.IsError || !strings.Contains(resultText(res), `"status":"success"`) {
		t.Fatalf("run_schedule_now: %s", resultText(res))
	}
	sent := f.sentContaining("INSERT INTO t")
	if len(sent) != 1 || sent[0].User != "mcp_user" || sent[0].Settings["readonly"] != "" {
		t.Fatalf("schedule should run once as the key user without readonly: %+v", sent)
	}

	// Allow-listed keys cannot reach it.
	ak.key.AllowedDatabases = "sales"
	res = callAction(t, deps, ak, "run_schedule_now", map[string]any{"schedule": "nightly-job"})
	if !res.IsError || !strings.Contains(resultText(res), "allow-list") {
		t.Fatalf("allow-listed key: %s", resultText(res))
	}
}

// Reports belong to people: API keys are refused, an OAuth sign-in reaches
// only its own reports on this connection.
func TestRunReportNowOwnership(t *testing.T) {
	deps, ak, _ := actingDeps(t, "")
	deps.Config.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	qid, err := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: ak.key.ConnectionID, CreatedBy: "bob@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	bob := "bob@example.com"
	if _, err := deps.DB.CreateReportSubscription(database.ReportSubscription{
		Name: "weekly", SourceType: reports.SourceSavedQuery, SavedQueryID: &qid, ConnectionID: ak.key.ConnectionID,
		Cron: "0 8 * * 1", Timezone: "UTC", Enabled: true, Format: "csv", CreatedBy: &bob,
	}); err != nil {
		t.Fatal(err)
	}

	res := callAction(t, deps, ak, "run_saved_report_now", map[string]any{"report": "weekly"})
	if !res.IsError || !strings.Contains(resultText(res), "OAuth") {
		t.Fatalf("API key must be refused: %s", resultText(res))
	}

	ak.key.Kind, ak.key.Subject = "oauth", "alice@example.com"
	res = callAction(t, deps, ak, "run_saved_report_now", map[string]any{"report": "weekly"})
	if !res.IsError || !strings.Contains(resultText(res), "not found among your reports") {
		t.Fatalf("someone else's report must not be reachable: %s", resultText(res))
	}
}

// The confirmation says when a build drops the existing table.
func TestRunModelPromptNamesTheDrop(t *testing.T) {
	deps, _, _, actKey := actionDeps(t)
	conns, _ := deps.DB.GetConnections()
	if _, err := deps.DB.CreateModel(conns[0].ID, "rollup", "", "default", "table", "SELECT 1", "MergeTree", "tuple()", "admin"); err != nil {
		t.Fatal(err)
	}
	r := decodeToolResult(t, callToolVersioned(t, Handler(deps), actKey, newProtocol, `{"elicitation":{"form":{}}}`, "run_model", `{"model":"rollup"}`, ""))
	if req := string(r.Result.InputRequests[confirmInputID]); !strings.Contains(req, "is dropped") {
		t.Fatalf("prompt should say the old table is dropped: %s", req)
	}
}
