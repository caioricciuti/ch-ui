package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// opsFixture is a key's connection plus a second connection whose data the
// key must never see.
type opsFixture struct {
	deps  Deps
	ak    *authedKey
	f     *fakeCH
	conn  string
	other string
}

func newOpsFixture(t *testing.T, allowed string) *opsFixture {
	t.Helper()
	deps, ak, f := fakeDeps(t, allowed)
	other, err := deps.DB.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	return &opsFixture{deps: deps, ak: ak, f: f, conn: ak.key.ConnectionID, other: other}
}

func (o *opsFixture) call(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerOpsTools(srv, o.deps, o.ak)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// decode calls a tool that must succeed and decodes its JSON result.
func (o *opsFixture) decode(t *testing.T, tool string, args map[string]any, v any) {
	t.Helper()
	res := o.call(t, tool, args)
	if res.IsError {
		t.Fatalf("%s: %s", tool, resultText(res))
	}
	if err := json.Unmarshal([]byte(resultText(res)), v); err != nil {
		t.Fatalf("%s: decode: %v (%s)", tool, err, resultText(res))
	}
}

func (o *opsFixture) sqlAlert(t *testing.T, conn, name, sql string, firing bool) string {
	t.Helper()
	id, err := o.deps.DB.CreateSQLAlert(&database.SQLAlert{ConnectionID: conn, Name: name, SQL: sql, Mode: "value",
		Comparator: ">", Threshold: 1, Severity: alerts.SeverityWarn, IntervalSeconds: 60, TimeoutSeconds: 10,
		ForEvaluations: 1, ResolveEvaluations: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if firing {
		if _, err := o.deps.DB.Conn().Exec(`UPDATE sql_alerts SET state = 'firing' WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func (o *opsFixture) diskFiring(t *testing.T, conn, node string) {
	t.Helper()
	sev, since := alerts.SeverityCritical, "2026-10-16T10:00:00Z"
	if err := o.deps.DB.SaveHealthAlertStates([]database.HealthAlertState{{
		ConnectionID: conn, CheckKind: clusterhealth.CheckDisk, Node: node, State: "firing", Severity: &sev, Since: &since,
	}}); err != nil {
		t.Fatal(err)
	}
}

func (o *opsFixture) event(t *testing.T, conn, eventType, title, sourceRef string) string {
	t.Helper()
	c := conn
	id, err := o.deps.DB.CreateAlertEvent(&c, eventType, alerts.SeverityWarn, title, "message of "+title, nil, "", sourceRef)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func names(items []map[string]any, key string) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		s, _ := it[key].(string)
		out[s] = true
	}
	return out
}

// The ops tools mirror Pro routes, so they exist only with an active license.
func TestOpsToolsRegisteredOnlyWithPro(t *testing.T) {
	tools := []string{"list_alerts", "firing_alerts", "alert_events", "list_schedules", "list_reports", "cluster_health"}
	for _, pro := range []bool{false, true} {
		deps, ak, _ := fakeDeps(t, "")
		cfg := &config.Config{}
		if pro {
			cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
		}
		deps.Config = cfg
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
		listed := map[string]*mcp.Tool{}
		for tool, err := range cs.Tools(ctx, nil) {
			if err != nil {
				t.Fatal(err)
			}
			listed[tool.Name] = tool
		}
		cs.Close()
		for _, name := range tools {
			tool, ok := listed[name]
			if ok != pro {
				t.Errorf("pro=%v: %s registered=%v", pro, name, ok)
			}
			if ok && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
				t.Errorf("%s must be annotated read-only", name)
			}
		}
	}
}

func TestListAlertsScopedToConnectionAndAllowList(t *testing.T) {
	for _, allowed := range []string{"", "analytics"} {
		o := newOpsFixture(t, allowed)
		o.sqlAlert(t, o.conn, "qualified", "SELECT count() FROM analytics.events", false)
		o.sqlAlert(t, o.conn, "elsewhere", "SELECT count() FROM hr.salaries", false)
		o.sqlAlert(t, o.conn, "unqualified", "SELECT count() FROM events", false)
		o.sqlAlert(t, o.other, "other connection", "SELECT count() FROM analytics.events", false)
		o.diskFiring(t, o.conn, "node-a")
		o.diskFiring(t, o.other, "node-b")

		var out struct {
			HealthChecks []struct {
				CheckKind string           `json:"check_kind"`
				Active    []map[string]any `json:"active"`
			} `json:"health_checks"`
			SQLAlerts []map[string]any `json:"sql_alerts"`
			Note      string           `json:"note"`
		}
		o.decode(t, "list_alerts", nil, &out)

		got := names(out.SQLAlerts, "name")
		want := map[string]bool{"qualified": true, "elsewhere": true, "unqualified": true}
		if allowed != "" {
			want = map[string]bool{"qualified": true}
			if out.Note == "" {
				t.Errorf("allow-listed key: want a note on hidden SQL alerts")
			}
		}
		if len(got) != len(want) {
			t.Errorf("allowed=%q: sql alerts %v, want %v", allowed, got, want)
		}
		for n := range want {
			if !got[n] {
				t.Errorf("allowed=%q: missing SQL alert %q", allowed, n)
			}
		}

		if len(out.HealthChecks) != len(clusterhealth.HealthChecks) {
			t.Errorf("want every built-in check, got %d", len(out.HealthChecks))
		}
		for _, c := range out.HealthChecks {
			wantActive := 0
			if c.CheckKind == clusterhealth.CheckDisk {
				wantActive = 1
			}
			if len(c.Active) != wantActive {
				t.Errorf("%s: active %v, want %d node(s)", c.CheckKind, c.Active, wantActive)
			}
			for _, n := range c.Active {
				if n["node"] != "node-a" {
					t.Errorf("%s: node %v belongs to another connection", c.CheckKind, n["node"])
				}
			}
		}
	}
}

func TestFiringAlertsScopedToConnectionAndAllowList(t *testing.T) {
	for _, allowed := range []string{"", "analytics"} {
		o := newOpsFixture(t, allowed)
		o.sqlAlert(t, o.conn, "qualified", "SELECT count() FROM analytics.events", true)
		o.sqlAlert(t, o.conn, "elsewhere", "SELECT count() FROM hr.salaries", true)
		o.sqlAlert(t, o.conn, "quiet", "SELECT count() FROM analytics.events", false)
		o.sqlAlert(t, o.other, "other connection", "SELECT 1", true)
		o.diskFiring(t, o.conn, "node-a")
		o.diskFiring(t, o.other, "node-b")
		key := alerts.PipelineAlertKey("p1", "stalled")
		c := o.conn
		if _, err := o.deps.DB.CreateAlertEvent(&c, alerts.EventTypeIngestStalled, alerts.SeverityWarn, "Ingest stalled: otel", "no data",
			map[string]any{"status": "firing", "alert_key": key, "incident_id": "2026-10-16T10:00:00Z", "pipeline_id": "p1"},
			key+":firing", alerts.PipelineSourceRef("p1")); err != nil {
			t.Fatal(err)
		}

		var out struct {
			Incidents []map[string]any `json:"incidents"`
		}
		o.decode(t, "firing_alerts", nil, &out)
		got := names(out.Incidents, "name")
		want := []string{"Disk usage", "qualified", "elsewhere", "Ingest stalled: otel"}
		if allowed != "" {
			want = []string{"Disk usage", "qualified"}
		}
		if len(out.Incidents) != len(want) {
			t.Errorf("allowed=%q: incidents %v, want %v", allowed, got, want)
		}
		for _, n := range want {
			if !got[n] {
				t.Errorf("allowed=%q: missing incident %q", allowed, n)
			}
		}
		for _, inc := range out.Incidents {
			if inc["node"] == "node-b" || inc["name"] == "other connection" {
				t.Errorf("incident of another connection leaked: %v", inc)
			}
		}
	}
}

func TestAlertEventsScopedFilteredAndCapped(t *testing.T) {
	for _, allowed := range []string{"", "analytics"} {
		o := newOpsFixture(t, allowed)
		okID := o.sqlAlert(t, o.conn, "qualified", "SELECT count() FROM analytics.events", false)
		hrID := o.sqlAlert(t, o.conn, "elsewhere", "SELECT count() FROM hr.salaries", false)
		o.event(t, o.conn, alerts.EventTypeHealthDisk, "disk", "health:disk")
		o.event(t, o.conn, alerts.EventTypeSQLAlert, "sql ok", okID)
		o.event(t, o.conn, alerts.EventTypeSQLAlert, "sql hr", hrID)
		o.event(t, o.conn, alerts.EventTypeScheduleFailed, "schedule", "s1")
		o.event(t, o.conn, alerts.EventTypePolicyViolation, "policy", "v1")
		o.event(t, o.conn, alerts.EventTypeAccessDrift, "drift", "d1")
		o.event(t, o.other, alerts.EventTypeHealthDisk, "other connection", "health:disk")
		old := o.event(t, o.conn, alerts.EventTypeHealthDisk, "two days ago", "health:disk")
		if _, err := o.deps.DB.Conn().Exec(`UPDATE alert_events SET created_at = ? WHERE id = ?`,
			time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339), old); err != nil {
			t.Fatal(err)
		}

		var out struct {
			Range  string           `json:"range"`
			Events []map[string]any `json:"events"`
		}
		o.decode(t, "alert_events", nil, &out)
		got := names(out.Events, "title")
		want := []string{"disk", "sql ok", "sql hr", "schedule"}
		if allowed != "" {
			want = []string{"disk", "sql ok"}
		}
		if out.Range != "24h" || len(out.Events) != len(want) {
			t.Errorf("allowed=%q: range %q events %v, want %v", allowed, out.Range, got, want)
		}
		for _, n := range want {
			if !got[n] {
				t.Errorf("allowed=%q: missing event %q", allowed, n)
			}
		}
		for _, ev := range out.Events {
			if _, ok := ev["payload_json"]; ok {
				t.Errorf("payload must not be returned: %v", ev)
			}
		}

		o.decode(t, "alert_events", map[string]any{"range": "7d"}, &out)
		if !names(out.Events, "title")["two days ago"] {
			t.Errorf("allowed=%q: 7d range should include the older event", allowed)
		}

		o.decode(t, "alert_events", map[string]any{"limit": 1}, &out)
		if len(out.Events) != 1 {
			t.Errorf("allowed=%q: limit 1 returned %d events", allowed, len(out.Events))
		}
	}
}

func TestListSchedulesByEffectiveConnection(t *testing.T) {
	for _, allowed := range []string{"", "analytics"} {
		o := newOpsFixture(t, allowed)
		db := o.deps.DB
		bound, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "bound", Query: "SELECT 1", ConnectionID: o.conn, CreatedBy: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		loose, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "loose", Query: "SELECT 1", CreatedBy: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		inherit, _ := db.CreateSchedule("inherits connection", bound, "", "0 * * * *", "UTC", "alice", 0)
		if _, err := db.CreateSchedule("own connection", loose, o.conn, "0 * * * *", "UTC", "alice", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateSchedule("other connection", loose, o.other, "0 * * * *", "UTC", "alice", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateSchedule("no connection", loose, "", "0 * * * *", "UTC", "alice", 0); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdateScheduleStatus(inherit, "error", "table hr.salaries does not exist", nil); err != nil {
			t.Fatal(err)
		}

		var out struct {
			Schedules  []map[string]any `json:"schedules"`
			NextCursor string           `json:"next_cursor"`
		}
		o.decode(t, "list_schedules", nil, &out)
		got := names(out.Schedules, "name")
		if len(out.Schedules) != 2 || !got["inherits connection"] || !got["own connection"] {
			t.Errorf("allowed=%q: schedules %v", allowed, got)
		}
		for _, s := range out.Schedules {
			if s["name"] != "inherits connection" {
				continue
			}
			if s["saved_query_name"] != "bound" || s["last_status"] != "error" {
				t.Errorf("schedule fields: %v", s)
			}
			_, hasErr := s["last_error"]
			if hasErr != (allowed == "") {
				t.Errorf("allowed=%q: last_error present=%v", allowed, hasErr)
			}
		}

		o.decode(t, "list_schedules", map[string]any{"page_size": 1}, &out)
		if len(out.Schedules) != 1 || out.NextCursor == "" {
			t.Errorf("page_size 1: %d schedules, cursor %q", len(out.Schedules), out.NextCursor)
		}
	}
}

func TestListReportsOnlyOwnOnConnection(t *testing.T) {
	o := newOpsFixture(t, "")
	mine, other := "default", "bob@example.com"
	for _, r := range []struct {
		name, conn string
		by         *string
	}{
		{"mine", o.conn, &mine},
		{"someone else's", o.conn, &other},
		{"mine elsewhere", o.other, &mine},
		{"nobody's", o.conn, nil},
	} {
		if _, err := o.deps.DB.CreateReportSubscription(database.ReportSubscription{
			Name: r.name, SourceType: "saved_query", ConnectionID: r.conn, Cron: "0 8 * * *", Timezone: "UTC",
			Enabled: true, Format: "csv", CreatedBy: r.by,
		}); err != nil {
			t.Fatal(err)
		}
	}
	var out struct {
		Reports []map[string]any `json:"reports"`
	}
	o.decode(t, "list_reports", nil, &out)
	if len(out.Reports) != 1 || out.Reports[0]["name"] != "mine" {
		t.Fatalf("want only the key person's report on this connection, got %v", out.Reports)
	}
	if strings.Contains(resultText(o.call(t, "list_reports", nil)), "recipients") {
		t.Errorf("report recipients must not be returned")
	}
}

// cluster_health aggregates system tables across all databases, so an
// allow-listed key is refused before anything reaches ClickHouse.
func TestClusterHealthRefusesAllowListedKeys(t *testing.T) {
	o := newOpsFixture(t, "analytics")
	res := o.call(t, "cluster_health", nil)
	if !res.IsError || !strings.Contains(resultText(res), "analytics") {
		t.Errorf("want refusal naming the allow-list, got %s", resultText(res))
	}
	if n := len(o.f.sentContaining("system.")); n != 0 {
		t.Errorf("allow-listed key sent %d system queries", n)
	}

	o = newOpsFixture(t, "")
	var out struct {
		Nodes []map[string]any `json:"nodes"`
	}
	o.decode(t, "cluster_health", nil, &out)
	if len(out.Nodes) == 0 {
		t.Errorf("want node samples")
	}
	sent := o.f.sentContaining("system.")
	if len(sent) == 0 {
		t.Fatal("unrestricted key should query system tables")
	}
	for _, m := range sent {
		if m.Settings["readonly"] != "1" || m.User != "default" {
			t.Errorf("cluster_health must run readonly with the key's credentials: user %q settings %v", m.User, m.Settings)
		}
	}
}
