package monitor

import (
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		cmp   string
		v, th float64
		want  bool
	}{
		{"gt", 11, 10, true}, {"gt", 10, 10, false}, {"gte", 10, 10, true},
		{"lt", 9, 10, true}, {"lt", 10, 10, false}, {"lte", 10, 10, true}, {"bogus", 1, 0, false},
	}
	for _, c := range cases {
		if got := Compare(c.cmp, c.v, c.th); got != c.want {
			t.Errorf("Compare(%s, %v, %v) = %v, want %v", c.cmp, c.v, c.th, got, c.want)
		}
	}
}

func TestFingerprintBucket(t *testing.T) {
	m := &database.TelemetryMonitor{ID: "m1", IntervalSeconds: 60}
	base := time.Unix(1_700_000_000, 0)
	a := FingerprintBucket(m, base)
	b := FingerprintBucket(m, base.Add(30*time.Second))
	c := FingerprintBucket(m, base.Add(61*time.Second))
	if a != b {
		t.Fatalf("same interval must share a fingerprint: %s vs %s", a, b)
	}
	if a == c {
		t.Fatalf("next interval must change the fingerprint: %s", a)
	}
	if !strings.HasPrefix(a, "telemetry.monitor:m1:") {
		t.Fatalf("unexpected fingerprint %s", a)
	}
	// An interval under the minimum is treated as exactly the minimum.
	short := &database.TelemetryMonitor{ID: "m2", IntervalSeconds: 5}
	clamped := &database.TelemetryMonitor{ID: "m2", IntervalSeconds: MinMonitorInterval}
	for _, at := range []time.Time{base, base.Add(20 * time.Second), base.Add(90 * time.Second)} {
		if FingerprintBucket(short, at) != FingerprintBucket(clamped, at) {
			t.Fatalf("interval below the minimum must be clamped to %ds", MinMonitorInterval)
		}
	}
}

func TestMonitorDue(t *testing.T) {
	now := time.Now()
	if !monitorDue(&database.TelemetryMonitor{IntervalSeconds: 60}, now) {
		t.Fatal("never run must be due")
	}
	recent := now.Add(-10 * time.Second).UTC().Format(time.RFC3339)
	if monitorDue(&database.TelemetryMonitor{IntervalSeconds: 60, LastRunAt: &recent}, now) {
		t.Fatal("ran 10s ago with 60s interval must not be due")
	}
	old := now.Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	if !monitorDue(&database.TelemetryMonitor{IntervalSeconds: 60, LastRunAt: &old}, now) {
		t.Fatal("ran 2m ago with 60s interval must be due")
	}
}

func TestValidateMonitor(t *testing.T) {
	good := database.TelemetryMonitor{Name: "x", Kind: "logs", SourceID: "s", WindowSeconds: 300, IntervalSeconds: 60, Comparator: "gt", Severity: "warn"}
	if err := ValidateMonitor(&good); err != nil {
		t.Fatalf("valid monitor rejected: %v", err)
	}
	bad := []database.TelemetryMonitor{
		{Kind: "logs", SourceID: "s", WindowSeconds: 300, IntervalSeconds: 60, Comparator: "gt", Severity: "warn"},
		{Name: "x", Kind: "metrics", SourceID: "s", WindowSeconds: 300, IntervalSeconds: 60, Comparator: "gt", Severity: "warn"},
		{Name: "x", Kind: "logs", WindowSeconds: 300, IntervalSeconds: 60, Comparator: "gt", Severity: "warn"},
		{Name: "x", Kind: "logs", SourceID: "s", WindowSeconds: 10, IntervalSeconds: 60, Comparator: "gt", Severity: "warn"},
		{Name: "x", Kind: "logs", SourceID: "s", WindowSeconds: 300, IntervalSeconds: 5, Comparator: "gt", Severity: "warn"},
		{Name: "x", Kind: "logs", SourceID: "s", WindowSeconds: 300, IntervalSeconds: 60, Comparator: "eq", Severity: "warn"},
		{Name: "x", Kind: "logs", SourceID: "s", WindowSeconds: 300, IntervalSeconds: 60, Comparator: "gt", Severity: "loud"},
	}
	for i, m := range bad {
		if err := ValidateMonitor(&m); err == nil {
			t.Errorf("case %d should be rejected", i)
		}
	}
}

func TestCountSQL(t *testing.T) {
	logsMap := telemetry.DefaultLogsMapping()
	src := &telemetry.Source{ID: "s1", ConnectionID: "c1", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &logsMap}
	m := &database.TelemetryMonitor{ConnectionID: "c1", Kind: "logs", SourceID: "s1", Query: "level:error", WindowSeconds: 300}
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	sql, err := CountSQL(m, src, map[string]string{}, now)
	if err != nil {
		t.Fatalf("CountSQL: %v", err)
	}
	for _, want := range []string{"SELECT count() AS c FROM `default`.`otel_logs` WHERE", "2026-09-11T09:55:00", "2026-09-11T10:00:00", "lower(`SeverityText`) = 'error'", telemetry.QuerySettings} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in %s", want, sql)
		}
	}
	tracesMap := telemetry.DefaultTracesMapping()
	tsrc := &telemetry.Source{ID: "s2", ConnectionID: "c1", Kind: telemetry.KindTraces, Database: "default", Table: "otel_traces", Traces: &tracesMap}
	tm := &database.TelemetryMonitor{ConnectionID: "c1", Kind: "traces", SourceID: "s2", Query: "status:error duration:>500", WindowSeconds: 60}
	tsql, err := CountSQL(tm, tsrc, map[string]string{}, now)
	if err != nil {
		t.Fatalf("CountSQL traces: %v", err)
	}
	for _, want := range []string{"`otel_traces`", "lower(`StatusCode`) = 'error'", "`Duration` > 500000000"} {
		if !strings.Contains(tsql, want) {
			t.Errorf("missing %q in %s", want, tsql)
		}
	}
	if _, err := CountSQL(m, tsrc, nil, now); err == nil {
		t.Fatal("logs monitor on a traces source must be rejected")
	}
	other := &telemetry.Source{ID: "s3", ConnectionID: "c2", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &logsMap}
	if _, err := CountSQL(m, other, nil, now); err == nil {
		t.Fatal("source on another connection must be rejected")
	}
	if _, err := CountSQL(&database.TelemetryMonitor{ConnectionID: "c1", Kind: "logs", Query: "(broken", WindowSeconds: 60}, src, nil, now); err == nil {
		t.Fatal("bad query must error")
	}
}

func TestShouldEmit(t *testing.T) {
	cases := []struct {
		name      string
		firing    bool
		lastState string
		want      bool
	}{
		{"first firing from ok", true, "ok", true},
		{"first firing from empty state", true, "", true},
		{"firing again after an error", true, "error", true},
		{"still firing, stay quiet", true, "firing", false},
		{"not firing", false, "ok", false},
		{"recovered", false, "firing", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldEmit(c.firing, c.lastState); got != c.want {
				t.Errorf("ShouldEmit(%v, %q) = %v, want %v", c.firing, c.lastState, got, c.want)
			}
		})
	}
}
