package operations

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/performance"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestNextWeeklyUTCAndStrictBoundary(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	if got := NextWeekly(now, 1, 9); !got.Equal(now.AddDate(0, 0, 7)) {
		t.Fatalf("equal boundary: %s", got)
	}
	if got := NextWeekly(now.Add(-time.Second), 1, 9); !got.Equal(now) {
		t.Fatalf("before boundary: %s", got)
	}
	local := now.In(time.FixedZone("offset", 7200))
	if !NextWeekly(local, 2, 8).Equal(time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)) {
		t.Fatal("schedule changed with timezone")
	}
}

// CHUI_TEST_CLICKHOUSE_URL optionally runs these reads through a real disposable
// ClickHouse instance. The default exercises the authenticated gateway protocol.
func TestScheduledReportUsesDedicatedAccountAndPersistsOnce(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		data := `[]`
		if strings.Contains(msg.SQL, "system.query_log") {
			data = `[{"hash":"42","database":"analytics","baseline_runs":20,"current_runs":20,"baseline_p95_ms":100,"current_p95_ms":400}]`
		}
		if strings.Contains(msg.SQL, "system.parts") {
			data = `[{"bytes":"1048576"}]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	})
	runner := NewRunner(db, agent.Gateway, &config.Config{AppSecretKey: "test-secret"})
	active := true
	runner.isPro = func() bool { return active }
	now := time.Now().UTC()
	settings := database.OperationsReportSettings{ConnectionID: conn, Enabled: true, Weekday: 1, Hour: 9, NextRunAt: now.Add(-time.Minute).Format(time.RFC3339)}
	if err := db.SaveOperationsReportSettings(settings); err != nil {
		t.Fatal(err)
	}
	runner.Tick(context.Background(), now)
	if len(agent.Messages()) != 0 {
		t.Fatal("unconfigured account executed SQL")
	}
	if _, err := db.Conn().Exec(`UPDATE operations_report_settings SET last_attempt_at='' WHERE connection_id=?`, conn); err != nil {
		t.Fatal(err)
	}
	agent.CheckWorker(t, db, conn, "operations.report", func() {
		// Each invocation is a distinct due schedule, including after rotation.
		now = now.Add(time.Hour)
		settings.NextRunAt = now.Add(-time.Minute).Format(time.RFC3339)
		if err := db.SaveOperationsReportSettings(settings); err != nil {
			t.Fatal(err)
		}
		runner.Tick(context.Background(), now)
	})
	reports, err := db.ListOperationsReports(conn, 52)
	if err != nil || len(reports) != 2 {
		t.Fatalf("saved reports: %d %v", len(reports), err)
	}
	for _, report := range reports {
		if report.Body == "" || report.DeliveryStatus != "not_requested" {
			t.Fatal("missing persisted in-app report")
		}
	}
	before := len(agent.Messages())
	runner.Tick(context.Background(), now)
	if len(agent.Messages()) != before {
		t.Fatal("same schedule regenerated")
	}
	active = false
	runner.Tick(context.Background(), now.Add(8*24*time.Hour))
	if len(agent.Messages()) != before {
		t.Fatal("expired license queried ClickHouse")
	}
	for _, msg := range agent.Messages() {
		if msg.Settings["readonly"] != "1" || msg.Settings["max_execution_time"] != "25" {
			t.Fatal("report queries lack readonly bounds")
		}
	}
}

func TestReportSettingsRejectIncompleteAndHeaderRecipients(t *testing.T) {
	for _, s := range []database.OperationsReportSettings{
		{Weekday: 7}, {Hour: 24}, {ChannelID: "mail"}, {Recipients: []string{"ops@example.com"}},
		{ChannelID: "mail", Recipients: []string{"ops@example.com\r\nBcc:secret@example.com"}},
	} {
		if ValidateSettings(s) == nil {
			t.Fatalf("accepted invalid configuration: %+v", s)
		}
	}
	if err := ValidateSettings(database.OperationsReportSettings{Weekday: 1, Hour: 9, ChannelID: "mail", Recipients: []string{"ops@example.com"}}); err != nil {
		t.Fatal(err)
	}
}

func TestReportRanksMeasuredCPUAndLabelsCoverage(t *testing.T) {
	report := performance.Report{Current: performance.Window{Start: time.Now().Add(-7 * 24 * time.Hour), End: time.Now()}, Compared: 2, Insufficient: 1, Truncated: true, Coverage: "connected node",
		Patterns: []performance.Pattern{
			{Hash: "1", SampleQuery: "secret query text", Current: performance.Metrics{Runs: 100, MeanCPUMS: 10, Failures: 2}},
			{Hash: "2", Current: performance.Metrics{Runs: 20, MeanCPUMS: 100, Failures: 3}},
		}}
	p := BuildPayload("test", report)
	if p.Workloads[0].Hash != "2" || p.Failures != 5 {
		t.Fatalf("wrong summary: %+v", p)
	}
	if report.Patterns[0].Hash != "1" {
		t.Fatal("modified source ordering")
	}
	body := Render(p)
	if strings.Contains(body, "secret query text") {
		t.Fatal("email leaked workload query text")
	}
	if !strings.Contains(body, "limit reached") || !strings.Contains(body, "connected node") {
		t.Fatal("missing coverage caveat")
	}
}

func TestReportExplainsNonLatencyRegressions(t *testing.T) {
	before := performance.Metrics{Runs: 20, P95MS: 100, MeanMemoryBytes: 32 << 20, MeanReadBytes: 64 << 20}
	after := performance.Metrics{Runs: 20, P95MS: 100, MeanMemoryBytes: 96 << 20, MeanReadBytes: 128 << 20}
	body := Render(Payload{Regressions: []performance.Pattern{{Hash: "1", Baseline: before, Current: after, Changes: performance.Changes(before, after)}}})
	if !strings.Contains(body, "mean memory: 32.0 → 96.0 MiB") || !strings.Contains(body, "mean read volume: 64.0 → 128.0 MiB") {
		t.Fatalf("missing regression reason: %s", body)
	}
	if strings.Contains(body, "p95 latency") {
		t.Fatal("unchanged latency mislabeled as regression")
	}
}

func TestGeneratedReportCountsResolutionTimeWithFractionalBoundaries(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	runner := NewRunner(db, agent.Gateway, &config.Config{})
	runner.isPro = func() bool { return true }
	now := time.Now().UTC().Truncate(time.Second)
	_, window, err := performance.Windows(now, "7d")
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{window.Start.Add(-time.Second), window.Start.Add(100 * time.Millisecond), window.End.Add(-100 * time.Millisecond), window.End, window.End.Add(100 * time.Millisecond)} {
		item := &database.PerformanceInvestigation{ConnectionID: conn, Title: "test", QueryHash: "42", Database: "test", CreatedBy: "admin"}
		if err := db.CreatePerformanceInvestigation(item, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Conn().Exec(`UPDATE performance_investigations SET status='resolved',resolved_at=?,updated_at=? WHERE id=?`, at.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), item.ID); err != nil {
			t.Fatal(err)
		}
	}
	report, err := runner.Generate(context.Background(), conn, "reader", "", "admin", "", now, database.OperationsReportSettings{})
	if err != nil {
		t.Fatal(err)
	}
	var payload Payload
	if err := json.Unmarshal(report.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ResolvedInvestigations != 2 {
		t.Fatalf("got %d resolutions, want 2 inside half-open interval", payload.ResolvedInvestigations)
	}
}

func TestReportDeliveryUsesSavedSnapshotAndConfiguredChannel(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const secret = "report-test-only-secret"
	cfgJSON, _ := crypto.Encrypt(`{"from_email":"reports@example.com","api_key":"test-only"}`, secret)
	channel, err := db.CreateAlertChannel("reports", "resend", cfgJSON, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(db, nil, &config.Config{AppSecretKey: secret})
	calls := 0
	runner.Send = func(ctx context.Context, kind string, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
		calls++
		if kind != "resend" || len(recipients) != 1 || recipients[0] != "ops@example.com" || body != "fixed snapshot" {
			t.Fatal("delivery inputs changed")
		}
		if calls == 1 {
			return "", errors.New("temporary outage")
		}
		return "sent", nil
	}
	raw, _ := json.Marshal([]string{"ops@example.com"})
	report := database.OperationsReport{ChannelID: channel, RecipientsJSON: string(raw), Body: "fixed snapshot"}
	if runner.deliver(context.Background(), report) == nil {
		t.Fatal("email failure hidden")
	}
	if err = runner.deliver(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("unexpected delivery attempts")
	}
	// An inactive Pro license never performs queued deliveries.
	runner.DeliverDue(context.Background(), time.Now())
	if calls != 2 {
		t.Fatal("community sent a report")
	}
}
