package otlp

import (
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// The DDL must hold every column Telemetry reads with its default mappings.
func TestDDLHasTelemetryColumns(t *testing.T) {
	logs := LogsTableDDL("otel", "otel_logs", 0, "")
	lm := telemetry.DefaultLogsMapping()
	logSrc := telemetry.Source{Kind: telemetry.KindLogs, Logs: &lm}
	for _, c := range logSrc.RequiredColumns() {
		if !strings.Contains(logs, "\n\t"+c+" ") {
			t.Errorf("logs DDL lacks column %s", c)
		}
	}
	traces := TracesTableDDL("otel", "otel_traces", 0, "")
	if len(traces) != 3 {
		t.Fatalf("want 3 statements, got %d", len(traces))
	}
	tm := telemetry.DefaultTracesMapping()
	traceSrc := telemetry.Source{Kind: telemetry.KindTraces, Traces: &tm, Table: "otel_traces"}
	for _, c := range traceSrc.RequiredColumns() {
		if !strings.Contains(traces[0], "\n\t"+c+" ") {
			t.Errorf("traces DDL lacks column %s", c)
		}
	}
	for _, c := range []string{"\n\tEvents Nested (", "\n\tLinks Nested (", "\n\t\tAttributes Map(LowCardinality(String), String)"} {
		if !strings.Contains(traces[0], c) {
			t.Errorf("traces DDL lacks %q", c)
		}
	}
	// Telemetry looks the lookup table up by this exact name.
	if !strings.Contains(traces[1], "`otel`.`"+telemetry.TraceIDTsTable(&traceSrc)+"`") {
		t.Errorf("lookup table name differs from telemetry's: %s", traces[1])
	}
	for _, c := range []string{"TraceId String", "Start DateTime", "End DateTime"} {
		if !strings.Contains(traces[1], c) {
			t.Errorf("lookup DDL lacks %q", c)
		}
	}
	for _, c := range []string{"CREATE MATERIALIZED VIEW IF NOT EXISTS `otel`.`otel_traces_trace_id_ts_mv`",
		"TO `otel`.`otel_traces_trace_id_ts`", "FROM `otel`.`otel_traces`", "min(Timestamp) AS Start", "max(Timestamp) AS End"} {
		if !strings.Contains(traces[2], c) {
			t.Errorf("view DDL lacks %q", c)
		}
	}
}

func TestDDLOptions(t *testing.T) {
	plain := LogsTableDDL("db", "t", 0, "")
	if strings.Contains(plain, "TTL") || strings.Contains(plain, "ON CLUSTER") {
		t.Errorf("unexpected TTL or cluster: %s", plain)
	}
	withTTL := LogsTableDDL("db", "t", 30, "main")
	for _, c := range []string{"CREATE TABLE IF NOT EXISTS `db`.`t` ON CLUSTER `main` (", "TTL TimestampTime + toIntervalDay(30)\nSETTINGS"} {
		if !strings.Contains(withTTL, c) {
			t.Errorf("missing %q in %s", c, withTTL)
		}
	}
	tr := TracesTableDDL("db", "t", 7, "c")
	for i, c := range []string{"TTL toDateTime(Timestamp) + toIntervalDay(7)", "TTL toDateTime(Start) + toIntervalDay(7)", "ON CLUSTER `c`"} {
		if !strings.Contains(tr[i], c) {
			t.Errorf("statement %d lacks %q: %s", i, c, tr[i])
		}
	}
	// Hostile names stay inside their backticks.
	evil := LogsTableDDL("a`; DROP DATABASE x; --", `t\`, 0, "c`")
	if !strings.Contains(evil, "`a``; DROP DATABASE x; --`.`t\\\\`") || !strings.Contains(evil, "ON CLUSTER `c```") {
		t.Errorf("identifiers not escaped: %s", evil)
	}
}

func TestResponses(t *testing.T) {
	if b, ct := SuccessResponse("application/x-protobuf"); len(b) != 0 || b == nil || ct != "application/x-protobuf" {
		t.Errorf("protobuf success = %q %s", b, ct)
	}
	if b, ct := SuccessResponse("application/json; charset=utf-8"); string(b) != "{}" || ct != "application/json" {
		t.Errorf("JSON success = %q %s", b, ct)
	}
	if b, ct := SuccessResponse(""); len(b) != 0 || ct != "application/x-protobuf" {
		t.Errorf("default success = %q %s", b, ct)
	}

	b, _ := PartialSuccessLogsResponse("application/json", 3, `bad "rows"`)
	if string(b) != `{"partialSuccess":{"rejectedLogRecords":"3","errorMessage":"bad \"rows\""}}` {
		t.Errorf("JSON partial = %s", b)
	}
	b, _ = PartialSuccessTracesResponse("application/x-protobuf", 300, "x")
	// partial_success (1, len 6) { rejected_spans (1) = 300 as varint, error_message (2) = "x" }
	want := []byte{0x0a, 0x06, 0x08, 0xac, 0x02, 0x12, 0x01, 'x'}
	if string(b) != string(want) {
		t.Errorf("protobuf partial = %x, want %x", b, want)
	}

	b, _ = StatusResponse("application/x-protobuf", 3, "bad")
	if string(b) != string([]byte{0x08, 0x03, 0x12, 0x03, 'b', 'a', 'd'}) {
		t.Errorf("protobuf status = %x", b)
	}
	b, _ = StatusResponse("application/json", 8, "slow down")
	if string(b) != `{"code":8,"message":"slow down"}` {
		t.Errorf("JSON status = %s", b)
	}
}
