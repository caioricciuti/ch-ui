package database

import (
	"errors"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/performance"
)

func TestPerformanceInvestigationPersistenceAndIsolation(t *testing.T) {
	db := openTestDB(t)
	for _, id := range []string{"a", "b"} {
		if _, err := db.conn.Exec(`INSERT INTO connections(id,name,tunnel_token) VALUES (?,?,?)`, id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	_, window, _ := performance.Windows(time.Now(), "1h")
	item := &PerformanceInvestigation{ConnectionID: "a", Title: "Slower lookups", QueryHash: "42", Database: "analytics", SampleQuery: "SELECT ?", Owner: "owner@example.test", CreatedBy: "human@example.test",
		Baseline: performance.Snapshot{Range: "1h", Window: window, Metrics: performance.Metrics{Runs: 20, P95MS: 1000}}}
	if err := db.CreatePerformanceInvestigation(item, "Before index change"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetPerformanceInvestigation("a", item.ID)
	if err != nil || got == nil || got.Baseline.Metrics.P95MS != 1000 || got.CreatedBy != "human@example.test" {
		t.Fatalf("baseline not persisted: %+v %v", got, err)
	}
	other, err := db.GetPerformanceInvestigation("b", item.ID)
	if err != nil || other != nil {
		t.Fatal("cross-connection read")
	}
	if events, err := db.ListPerformanceEvents("b", item.ID); err != nil || len(events) != 0 {
		t.Fatal("cross-connection history")
	}
	if items, err := db.ListPerformanceInvestigations("b"); err != nil || len(items) != 0 {
		t.Fatal("cross-connection list")
	}
	malicious := *item
	malicious.ConnectionID = "b"
	if err := db.UpdatePerformanceInvestigation(&malicious, "other", "note"); !errors.Is(err, ErrPerformanceConflict) {
		t.Fatal("cross-connection write allowed")
	}
	item.Status = "resolved"
	if err := db.UpdatePerformanceInvestigation(item, "human@example.test", "Added projection"); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetPerformanceInvestigation("a", item.ID)
	if err != nil || got.ResolvedAt == "" || got.Revision != 2 {
		t.Fatalf("resolution not recorded: %+v %v", got, err)
	}
	resolvedAt := got.ResolvedAt
	if err := db.UpdatePerformanceInvestigation(item, "stale", "must not persist"); !errors.Is(err, ErrPerformanceConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if err := db.UpdatePerformanceInvestigation(got, "human@example.test", "Follow-up note"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetPerformanceInvestigation("a", item.ID)
	if got.ResolvedAt != resolvedAt {
		t.Fatal("editing resolved investigation changed resolution time")
	}
	got.Status = "open"
	if err := db.UpdatePerformanceInvestigation(got, "human@example.test", "Reopened"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetPerformanceInvestigation("a", item.ID)
	if got.ResolvedAt != "" || got.Baseline.Metrics.P95MS != 1000 {
		t.Fatal("reopen failed or immutable baseline changed")
	}
	events, err := db.ListPerformanceEvents("a", item.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("history atomicity broken: %d %v", len(events), err)
	}
	for _, event := range events {
		if event.Actor != "human@example.test" {
			t.Fatalf("bad attribution: %+v", event)
		}
	}
	comparison := performance.Comparison{Baseline: item.Baseline, Current: item.Baseline, Sufficient: true}
	if err := db.AddPerformanceComparison("b", item.ID, "other", comparison); err == nil {
		t.Fatal("cross-connection comparison allowed")
	}
	if err := db.AddPerformanceComparison("a", item.ID, "human@example.test", comparison); err != nil {
		t.Fatal(err)
	}
	events, _ = db.ListPerformanceEvents("a", item.ID)
	if len(events) != 5 || events[0].Kind != "comparison" {
		t.Fatal("comparison history missing")
	}
}

func TestPerformanceMonitorPreservesLastSuccessfulScan(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(`INSERT INTO connections(id,name,tunnel_token) VALUES ('a','a','a')`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPerformanceMonitor("a", true); err != nil {
		t.Fatal(err)
	}
	report := performance.Report{Supported: true, Regressions: []performance.Pattern{{Hash: "42"}}}
	if err := db.RecordPerformanceScan("a", &report, ""); err != nil {
		t.Fatal(err)
	}
	first, err := db.GetPerformanceMonitor("a")
	if err != nil || first.Report == nil || first.ReportAt == "" {
		t.Fatal("successful scan missing")
	}
	if err := db.RecordPerformanceScan("a", nil, "connection offline"); err != nil {
		t.Fatal(err)
	}
	failed, err := db.GetPerformanceMonitor("a")
	if err != nil || failed.LastError != "connection offline" || failed.ReportAt != first.ReportAt || len(failed.Report.Regressions) != 1 {
		t.Fatalf("failure replaced valid evidence: %+v %v", failed, err)
	}
	if err := db.SetPerformanceMonitor("a", false); err != nil {
		t.Fatal(err)
	}
	if ids, err := db.PerformanceMonitorConnections(); err != nil || len(ids) != 0 {
		t.Fatalf("disabled connection remains enabled: %v %v", ids, err)
	}
}
