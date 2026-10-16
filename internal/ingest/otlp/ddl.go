package otlp

import (
	"fmt"

	"github.com/caioricciuti/ch-ui/internal/safe"
)

// The CREATE statements follow the OpenTelemetry Collector ClickHouse
// exporter's sqltemplates (logs_table.sql, traces_table.sql,
// traces_id_ts_lookup_table.sql, traces_id_ts_lookup_mv.sql) with the
// default MergeTree engine. Every identifier goes through safe.Identifier,
// so any database, table or cluster name is safe to pass; validating that
// it is a sensible name is the caller's job.

func qualified(database, table string) string {
	return safe.Identifier(database) + "." + safe.Identifier(table)
}

func onClusterClause(cluster string) string {
	if cluster == "" {
		return ""
	}
	return " ON CLUSTER " + safe.Identifier(cluster)
}

func ttlClause(expr string, days int) string {
	if days <= 0 {
		return ""
	}
	return fmt.Sprintf("TTL %s + toIntervalDay(%d)\n", expr, days)
}

// LogsTableDDL returns the CREATE TABLE statement for the exporter's logs
// table. ttlDays <= 0 means no TTL; onCluster may be empty.
func LogsTableDDL(database, table string, ttlDays int, onCluster string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s%s (
	Timestamp DateTime64(9) CODEC(Delta(8), ZSTD(1)),
	TimestampTime DateTime DEFAULT toDateTime(Timestamp),
	TraceId String CODEC(ZSTD(1)),
	SpanId String CODEC(ZSTD(1)),
	TraceFlags UInt8,
	SeverityText LowCardinality(String) CODEC(ZSTD(1)),
	SeverityNumber UInt8,
	ServiceName LowCardinality(String) CODEC(ZSTD(1)),
	Body String CODEC(ZSTD(1)),
	ResourceSchemaUrl LowCardinality(String) CODEC(ZSTD(1)),
	ResourceAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
	ScopeSchemaUrl LowCardinality(String) CODEC(ZSTD(1)),
	ScopeName String CODEC(ZSTD(1)),
	ScopeVersion LowCardinality(String) CODEC(ZSTD(1)),
	ScopeAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
	LogAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
	EventName String CODEC(ZSTD(1)),
	INDEX idx_trace_id TraceId TYPE bloom_filter(0.001) GRANULARITY 1,
	INDEX idx_res_attr_key mapKeys(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_scope_attr_key mapKeys(ScopeAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_scope_attr_value mapValues(ScopeAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_log_attr_key mapKeys(LogAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_log_attr_value mapValues(LogAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_lower_body lower(Body) TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 8
) ENGINE = MergeTree
PARTITION BY toDate(TimestampTime)
PRIMARY KEY (ServiceName, TimestampTime)
ORDER BY (ServiceName, TimestampTime, Timestamp)
%sSETTINGS index_granularity = 8192, ttl_only_drop_parts = 1`,
		qualified(database, table), onClusterClause(onCluster), ttlClause("TimestampTime", ttlDays))
}

// TraceIDTsTable and TraceIDTsView name the exporter's per-trace time
// index and the materialized view that fills it.
func TraceIDTsTable(table string) string { return table + "_trace_id_ts" }

func TraceIDTsView(table string) string { return table + "_trace_id_ts_mv" }

// TracesTableDDL returns, in order, the CREATE statements for the spans
// table, its <table>_trace_id_ts lookup table and the materialized view
// that fills the lookup. Telemetry reads the lookup to bound trace queries.
func TracesTableDDL(database, table string, ttlDays int, onCluster string) []string {
	cluster := onClusterClause(onCluster)
	spans := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s%s (
	Timestamp DateTime64(9) CODEC(Delta(8), ZSTD(1)),
	TraceId String CODEC(ZSTD(1)),
	SpanId String CODEC(ZSTD(1)),
	ParentSpanId String CODEC(ZSTD(1)),
	TraceState String CODEC(ZSTD(1)),
	SpanName LowCardinality(String) CODEC(ZSTD(1)),
	SpanKind LowCardinality(String) CODEC(ZSTD(1)),
	ServiceName LowCardinality(String) CODEC(ZSTD(1)),
	ResourceAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
	ScopeName String CODEC(ZSTD(1)),
	ScopeVersion String CODEC(ZSTD(1)),
	SpanAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
	Duration UInt64 CODEC(ZSTD(1)),
	StatusCode LowCardinality(String) CODEC(ZSTD(1)),
	StatusMessage String CODEC(ZSTD(1)),
	Events Nested (
		Timestamp DateTime64(9),
		Name LowCardinality(String),
		Attributes Map(LowCardinality(String), String)
	) CODEC(ZSTD(1)),
	Links Nested (
		TraceId String,
		SpanId String,
		TraceState String,
		Attributes Map(LowCardinality(String), String)
	) CODEC(ZSTD(1)),
	INDEX idx_trace_id TraceId TYPE bloom_filter(0.001) GRANULARITY 1,
	INDEX idx_res_attr_key mapKeys(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_res_attr_value mapValues(ResourceAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_span_attr_key mapKeys(SpanAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_span_attr_value mapValues(SpanAttributes) TYPE bloom_filter(0.01) GRANULARITY 1,
	INDEX idx_duration Duration TYPE minmax GRANULARITY 1
) ENGINE = MergeTree
PARTITION BY toDate(Timestamp)
ORDER BY (ServiceName, SpanName, toDateTime(Timestamp))
%sSETTINGS index_granularity = 8192, ttl_only_drop_parts = 1`,
		qualified(database, table), cluster, ttlClause("toDateTime(Timestamp)", ttlDays))

	lookup := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s%s (
	TraceId String CODEC(ZSTD(1)),
	Start DateTime CODEC(Delta(4), ZSTD(1)),
	End DateTime CODEC(Delta(4), ZSTD(1)),
	INDEX idx_trace_id TraceId TYPE bloom_filter(0.01) GRANULARITY 1
) ENGINE = MergeTree
PARTITION BY toDate(Start)
ORDER BY (TraceId, Start)
%sSETTINGS index_granularity = 8192`,
		qualified(database, TraceIDTsTable(table)), cluster, ttlClause("toDateTime(Start)", ttlDays))

	view := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s%s
TO %s
AS SELECT
	TraceId,
	min(Timestamp) AS Start,
	max(Timestamp) AS End
FROM %s
WHERE TraceId != ''
GROUP BY TraceId`,
		qualified(database, TraceIDTsView(table)), cluster,
		qualified(database, TraceIDTsTable(table)), qualified(database, table))

	return []string{spans, lookup, view}
}
