package database

import (
	"database/sql"
	"testing"
)

func TestTelemetrySavedSearchesAndMonitors(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(`INSERT INTO connections (id, name, tunnel_token, type) VALUES ('conn-1', 'c', 'tok-1', 'direct')`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	srcID := "src-1"
	s := &TelemetrySavedSearch{ConnectionID: "conn-1", Kind: "logs", Name: "errors", Query: "level:error", RangePreset: "1h", SourceID: &srcID}
	id, err := db.CreateTelemetrySavedSearch(s)
	if err != nil {
		t.Fatalf("CreateTelemetrySavedSearch: %v", err)
	}
	list, err := db.ListTelemetrySavedSearches("conn-1")
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].SourceID == nil || *list[0].SourceID != srcID {
		t.Fatalf("list saved searches: %v %+v", err, list)
	}
	list[0].Name = "errors-2"
	if err := db.UpdateTelemetrySavedSearch(&list[0]); err != nil {
		t.Fatalf("UpdateTelemetrySavedSearch: %v", err)
	}
	got, _ := db.GetTelemetrySavedSearch(id)
	if got == nil || got.Name != "errors-2" {
		t.Fatalf("update not persisted: %+v", got)
	}
	if err := db.DeleteTelemetrySavedSearch(id); err != nil {
		t.Fatalf("DeleteTelemetrySavedSearch: %v", err)
	}
	if err := db.DeleteTelemetrySavedSearch(id); err != sql.ErrNoRows {
		t.Fatalf("second delete: want ErrNoRows, got %v", err)
	}

	m := &TelemetryMonitor{ConnectionID: "conn-1", Name: "5xx", Kind: "logs", SourceID: srcID, Query: "http.status_code:>=500",
		WindowSeconds: 300, IntervalSeconds: 60, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true}
	mid, err := db.CreateTelemetryMonitor(m)
	if err != nil {
		t.Fatalf("CreateTelemetryMonitor: %v", err)
	}
	all, err := db.ListTelemetryMonitors("")
	if err != nil || len(all) != 1 || all[0].LastState != "ok" || !all[0].Enabled {
		t.Fatalf("list monitors: %v %+v", err, all)
	}
	v := 42.0
	if err := db.RecordTelemetryMonitorRun(mid, &v, "firing", ""); err != nil {
		t.Fatalf("RecordTelemetryMonitorRun: %v", err)
	}
	got2, _ := db.GetTelemetryMonitor(mid)
	if got2 == nil || got2.LastState != "firing" || got2.LastValue == nil || *got2.LastValue != 42 || got2.LastRunAt == nil || got2.LastError != nil {
		t.Fatalf("run not recorded: %+v", got2)
	}
	if err := db.RecordTelemetryMonitorRun(mid, nil, "error", "boom"); err != nil {
		t.Fatalf("RecordTelemetryMonitorRun error: %v", err)
	}
	got3, _ := db.GetTelemetryMonitor(mid)
	if got3.LastState != "error" || got3.LastError == nil || *got3.LastError != "boom" || got3.LastValue != nil {
		t.Fatalf("error run not recorded: %+v", got3)
	}
	got3.Enabled = false
	got3.Threshold = 5
	if err := db.UpdateTelemetryMonitor(got3); err != nil {
		t.Fatalf("UpdateTelemetryMonitor: %v", err)
	}
	byConn, _ := db.ListTelemetryMonitors("conn-1")
	if len(byConn) != 1 || byConn[0].Enabled || byConn[0].Threshold != 5 {
		t.Fatalf("update not persisted: %+v", byConn)
	}
	if err := db.DeleteTelemetryMonitor(mid); err != nil {
		t.Fatalf("DeleteTelemetryMonitor: %v", err)
	}
}
