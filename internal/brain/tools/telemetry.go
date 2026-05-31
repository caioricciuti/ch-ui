package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func RegisterTelemetry(r *Registry) {
	r.Register(listServices)
	r.Register(queryLogs)
	r.Register(queryTraces)
	r.Register(findTrace)
	r.Register(listMetrics)
	r.Register(queryMetrics)
}

var listServices = Tool{
	Name:        "list_services",
	Description: "List every distinct ServiceName seen in the user's OpenTelemetry logs and traces (last 24h), with row counts. Call this first when the user asks 'what's running?' or 'which services do we have?'.",
	Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
	Handler: func(tctx Context, _ json.RawMessage) (any, error) {
		sql := `
			SELECT ServiceName, sum(c) AS events, sum(logs) AS logs, sum(spans) AS spans
			FROM (
				SELECT ServiceName, count() AS c, count() AS logs, 0 AS spans
				FROM otel_logs WHERE TimestampTime >= now() - INTERVAL 1 DAY
				GROUP BY ServiceName
				UNION ALL
				SELECT ServiceName, count() AS c, 0 AS logs, count() AS spans
				FROM otel_traces WHERE Timestamp >= now() - INTERVAL 1 DAY
				GROUP BY ServiceName
			)
			WHERE ServiceName != ''
			GROUP BY ServiceName
			ORDER BY events DESC
			LIMIT 50`
		rows, err := runSelect(tctx, sql, 15*time.Second)
		if err != nil {
			return nil, fmt.Errorf("list services (does otel_logs / otel_traces exist? enable telemetry first): %w", err)
		}
		return map[string]any{"services": rows, "window": "24h"}, nil
	},
}

var queryLogs = Tool{
	Name:        "query_logs",
	Description: "Query the user's OpenTelemetry logs. Filters are AND'd; all optional. Use this when the user asks about errors, recent activity, what a service is doing, etc.",
	Parameters: mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"service":       map[string]any{"type": "string", "description": "Filter by ServiceName."},
			"severity_min":  map[string]any{"type": "string", "enum": []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}, "description": "Minimum severity (e.g. ERROR shows ERROR + FATAL)."},
			"search":        map[string]any{"type": "string", "description": "Substring match on the Body (case-insensitive)."},
			"since_minutes": map[string]any{"type": "integer", "description": "Window size in minutes (default 60, max 1440)."},
			"limit":         map[string]any{"type": "integer", "description": "Max rows (default 50, max 500)."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Service      string `json:"service"`
			SeverityMin  string `json:"severity_min"`
			Search       string `json:"search"`
			SinceMinutes int    `json:"since_minutes"`
			Limit        int    `json:"limit"`
		}
		_ = json.Unmarshal(args, &in)
		since := in.SinceMinutes
		if since <= 0 {
			since = 60
		}
		if since > 1440 {
			since = 1440
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}
		var where []string
		where = append(where, fmt.Sprintf("TimestampTime >= now() - INTERVAL %d MINUTE", since))
		if in.Service != "" {
			where = append(where, fmt.Sprintf("ServiceName = '%s'", sqlEscape(in.Service)))
		}
		if minSev := severityFloor(in.SeverityMin); minSev > 0 {
			where = append(where, fmt.Sprintf("SeverityNumber >= %d", minSev))
		}
		if in.Search != "" {
			where = append(where, fmt.Sprintf("positionCaseInsensitive(Body, '%s') > 0", sqlEscape(in.Search)))
		}
		sql := fmt.Sprintf(
			"SELECT Timestamp, ServiceName, SeverityText, Body, TraceId, SpanId, LogAttributes FROM otel_logs WHERE %s ORDER BY Timestamp DESC LIMIT %d",
			strings.Join(where, " AND "), limit,
		)
		rows, err := runSelect(tctx, sql, 20*time.Second)
		if err != nil {
			return nil, fmt.Errorf("query logs: %w (is telemetry enabled?)", err)
		}
		return map[string]any{"window_minutes": since, "count": len(rows), "logs": rows}, nil
	},
}

var queryTraces = Tool{
	Name:        "query_traces",
	Description: "Query recent spans. Useful to see slow operations or error rates per service. Returns up to 100 spans sorted by duration desc by default.",
	Parameters: mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"service":         map[string]any{"type": "string"},
			"span_name":       map[string]any{"type": "string", "description": "Exact match on the operation name."},
			"errors_only":     map[string]any{"type": "boolean", "description": "Only StatusCode='Error'."},
			"min_duration_ms": map[string]any{"type": "number", "description": "Floor on span duration in milliseconds."},
			"since_minutes":   map[string]any{"type": "integer"},
			"order_by":        map[string]any{"type": "string", "enum": []string{"duration", "time"}, "description": "Sort key (default duration)."},
			"limit":           map[string]any{"type": "integer", "description": "Max rows (default 50, max 500)."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Service       string  `json:"service"`
			SpanName      string  `json:"span_name"`
			ErrorsOnly    bool    `json:"errors_only"`
			MinDurationMs float64 `json:"min_duration_ms"`
			SinceMinutes  int     `json:"since_minutes"`
			OrderBy       string  `json:"order_by"`
			Limit         int     `json:"limit"`
		}
		_ = json.Unmarshal(args, &in)
		since := in.SinceMinutes
		if since <= 0 {
			since = 60
		}
		if since > 1440 {
			since = 1440
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}
		var where []string
		where = append(where, fmt.Sprintf("Timestamp >= now() - INTERVAL %d MINUTE", since))
		if in.Service != "" {
			where = append(where, fmt.Sprintf("ServiceName = '%s'", sqlEscape(in.Service)))
		}
		if in.SpanName != "" {
			where = append(where, fmt.Sprintf("SpanName = '%s'", sqlEscape(in.SpanName)))
		}
		if in.ErrorsOnly {
			where = append(where, "StatusCode = 'Error'")
		}
		if in.MinDurationMs > 0 {
			where = append(where, fmt.Sprintf("Duration >= %d", int64(in.MinDurationMs*1_000_000)))
		}
		orderClause := "Duration DESC"
		if in.OrderBy == "time" {
			orderClause = "Timestamp DESC"
		}
		sql := fmt.Sprintf(
			"SELECT Timestamp, TraceId, SpanId, ParentSpanId, ServiceName, SpanName, SpanKind, Duration/1e6 AS duration_ms, StatusCode, StatusMessage FROM otel_traces WHERE %s ORDER BY %s LIMIT %d",
			strings.Join(where, " AND "), orderClause, limit,
		)
		rows, err := runSelect(tctx, sql, 20*time.Second)
		if err != nil {
			return nil, fmt.Errorf("query traces: %w (is telemetry enabled?)", err)
		}
		return map[string]any{"window_minutes": since, "count": len(rows), "spans": rows}, nil
	},
}

var findTrace = Tool{
	Name:        "find_trace",
	Description: "Pull every span belonging to a trace id (the full waterfall) plus any correlated logs. Use this when the user gives you a trace_id or you've just shown them an error log that has one.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"trace_id"},
		"properties": map[string]any{
			"trace_id": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			TraceID string `json:"trace_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.TraceID = strings.TrimSpace(in.TraceID)
		if in.TraceID == "" {
			return nil, errors.New("trace_id is required")
		}
		spansSQL := fmt.Sprintf(
			"SELECT Timestamp, SpanId, ParentSpanId, ServiceName, SpanName, SpanKind, Duration/1e6 AS duration_ms, StatusCode, StatusMessage FROM otel_traces WHERE TraceId = '%s' ORDER BY Timestamp ASC",
			sqlEscape(in.TraceID),
		)
		spans, err := runSelect(tctx, spansSQL, 15*time.Second)
		if err != nil {
			return nil, fmt.Errorf("find spans: %w", err)
		}
		logsSQL := fmt.Sprintf(
			"SELECT Timestamp, ServiceName, SeverityText, Body, SpanId FROM otel_logs WHERE TraceId = '%s' ORDER BY Timestamp ASC LIMIT 200",
			sqlEscape(in.TraceID),
		)
		logs, _ := runSelect(tctx, logsSQL, 10*time.Second)
		return map[string]any{
			"trace_id": in.TraceID,
			"spans":    spans,
			"logs":     logs,
		}, nil
	},
}

var listMetrics = Tool{
	Name:        "list_metrics",
	Description: "List every metric name seen in otel_metrics_sum / otel_metrics_gauge / otel_metrics_histogram in the last 24h, with their type, unit, and the services emitting them. Call this first when the user asks about metrics they have.",
	Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
	Handler: func(tctx Context, _ json.RawMessage) (any, error) {
		sql := `
			SELECT MetricName, type, MetricUnit, groupArrayDistinctArray(ServiceName) AS services, sum(c) AS points
			FROM (
				SELECT MetricName, 'sum' AS type, anyLast(MetricUnit) AS MetricUnit, groupArrayDistinct(ServiceName) AS ServiceName, count() AS c FROM otel_metrics_sum WHERE TimeUnix >= now() - INTERVAL 1 DAY GROUP BY MetricName
				UNION ALL
				SELECT MetricName, 'gauge' AS type, anyLast(MetricUnit) AS MetricUnit, groupArrayDistinct(ServiceName) AS ServiceName, count() AS c FROM otel_metrics_gauge WHERE TimeUnix >= now() - INTERVAL 1 DAY GROUP BY MetricName
				UNION ALL
				SELECT MetricName, 'histogram' AS type, anyLast(MetricUnit) AS MetricUnit, groupArrayDistinct(ServiceName) AS ServiceName, count() AS c FROM otel_metrics_histogram WHERE TimeUnix >= now() - INTERVAL 1 DAY GROUP BY MetricName
			)
			GROUP BY MetricName, type, MetricUnit
			ORDER BY points DESC
			LIMIT 200`
		rows, err := runSelect(tctx, sql, 15*time.Second)
		if err != nil {
			return nil, fmt.Errorf("list metrics: %w (enable telemetry first)", err)
		}
		return map[string]any{"metrics": rows, "window": "24h"}, nil
	},
}

var queryMetrics = Tool{
	Name:        "query_metrics",
	Description: "Query a single metric's timeseries. For sum/gauge returns avg per bucket; for histograms returns count/sum/avg/min/max per bucket. Group by an attribute (e.g. 'http.status_code', 'service.name') to compare series.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"metric_name"},
		"properties": map[string]any{
			"metric_name":    map[string]any{"type": "string"},
			"metric_type":    map[string]any{"type": "string", "enum": []string{"sum", "gauge", "histogram"}, "description": "Defaults to sum then gauge then histogram (first table with data wins)."},
			"service":        map[string]any{"type": "string"},
			"group_by":       map[string]any{"type": "string", "description": "Attribute key from Attributes map (e.g. 'http.status_code'). Omit for total."},
			"since_minutes":  map[string]any{"type": "integer", "description": "Default 60, max 1440."},
			"bucket_seconds": map[string]any{"type": "integer", "description": "Time bucket size, default 60."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			MetricName    string `json:"metric_name"`
			MetricType    string `json:"metric_type"`
			Service       string `json:"service"`
			GroupBy       string `json:"group_by"`
			SinceMinutes  int    `json:"since_minutes"`
			BucketSeconds int    `json:"bucket_seconds"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.MetricName = strings.TrimSpace(in.MetricName)
		if in.MetricName == "" {
			return nil, errors.New("metric_name is required")
		}
		since := in.SinceMinutes
		if since <= 0 {
			since = 60
		}
		if since > 1440 {
			since = 1440
		}
		bucket := in.BucketSeconds
		if bucket <= 0 {
			bucket = 60
		}

		typ := strings.ToLower(strings.TrimSpace(in.MetricType))
		if typ == "" {
			for _, candidate := range []string{"sum", "gauge", "histogram"} {
				probe := fmt.Sprintf("SELECT 1 FROM otel_metrics_%s WHERE MetricName = '%s' AND TimeUnix >= now() - INTERVAL %d MINUTE LIMIT 1", candidate, sqlEscape(in.MetricName), since)
				rows, perr := runSelect(tctx, probe, 5*time.Second)
				if perr == nil && len(rows) > 0 {
					typ = candidate
					break
				}
			}
			if typ == "" {
				return nil, fmt.Errorf("metric %q not seen in the last %d minutes", in.MetricName, since)
			}
		}

		groupExpr := ""
		groupSelect := ""
		if in.GroupBy != "" {
			groupExpr = fmt.Sprintf(", Attributes['%s']", sqlEscape(in.GroupBy))
			groupSelect = fmt.Sprintf(", Attributes['%s'] AS series", sqlEscape(in.GroupBy))
		}

		var filters []string
		filters = append(filters, fmt.Sprintf("MetricName = '%s'", sqlEscape(in.MetricName)))
		filters = append(filters, fmt.Sprintf("TimeUnix >= now() - INTERVAL %d MINUTE", since))
		if in.Service != "" {
			filters = append(filters, fmt.Sprintf("ServiceName = '%s'", sqlEscape(in.Service)))
		}
		where := strings.Join(filters, " AND ")

		var sql string
		switch typ {
		case "histogram":
			sql = fmt.Sprintf(
				`SELECT toStartOfInterval(TimeUnix, INTERVAL %d SECOND) AS bucket%s,
				        sum(Count) AS count, sum(Sum) AS sum_value,
				        sum(Sum) / nullIf(sum(Count), 0) AS avg_value,
				        max(Max) AS max_value, min(Min) AS min_value
				 FROM otel_metrics_histogram WHERE %s
				 GROUP BY bucket%s
				 ORDER BY bucket ASC
				 LIMIT 1000`,
				bucket, groupSelect, where, groupExpr,
			)
		default:
			sql = fmt.Sprintf(
				`SELECT toStartOfInterval(TimeUnix, INTERVAL %d SECOND) AS bucket%s,
				        avg(Value) AS avg_value, max(Value) AS max_value, min(Value) AS min_value, count() AS samples
				 FROM otel_metrics_%s WHERE %s
				 GROUP BY bucket%s
				 ORDER BY bucket ASC
				 LIMIT 1000`,
				bucket, groupSelect, typ, where, groupExpr,
			)
		}
		rows, err := runSelect(tctx, sql, 25*time.Second)
		if err != nil {
			return nil, fmt.Errorf("query metrics: %w", err)
		}
		return map[string]any{
			"metric_name":    in.MetricName,
			"metric_type":    typ,
			"bucket_seconds": bucket,
			"window_minutes": since,
			"series":         rows,
		}, nil
	},
}

func severityFloor(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TRACE":
		return 1
	case "DEBUG":
		return 5
	case "INFO":
		return 9
	case "WARN":
		return 13
	case "ERROR":
		return 17
	case "FATAL":
		return 21
	}
	return 0
}
