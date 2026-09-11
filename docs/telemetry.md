# Telemetry

CH-UI turns the OpenTelemetry data your collector already writes to ClickHouse
into a search and analysis surface: logs, traces with waterfalls, a metrics
explorer, a service map and monitors that raise alerts. Nothing is copied or
indexed elsewhere; every screen runs SQL against your tables, through the
same connection, credentials and guardrails as the query editor.

Telemetry lives under **Visualize → Telemetry** with six sections: Logs,
Traces, Metrics, Service map, Monitors and Sources.

## Send data with the OpenTelemetry collector

Point the collector's ClickHouse exporter at the database CH-UI can reach.
With `create_schema: true` the exporter creates `otel_logs`, `otel_traces`
and the `otel_metrics_*` tables on first start.

```yaml
exporters:
  clickhouse:
    endpoint: tcp://clickhouse:9000?dial_timeout=10s&compress=lz4
    database: default
    username: otel
    password: ${env:CLICKHOUSE_PASSWORD}
    create_schema: true
    ttl: 72h
    timeout: 5s
    sending_queue:
      queue_size: 1000
    retry_on_failure:
      enabled: true

service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [clickhouse]
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [clickhouse]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [clickhouse]
```

On ClickHouse 25 and newer the exporter can store attributes as `JSON`
columns instead of `Map` columns (`json: true` in the exporter config).
CH-UI supports both; Sources shows a `JSON attrs` badge when it detects the
JSON layout.

## Sources

A source tells CH-UI which table holds a signal and which columns play which
role. Sources are per connection.

- **Detect sources** scans the connection for the exporter's tables and
  proposes one source per signal, with the mapping already verified against
  the table's columns. Review the proposals and add the ones you want.
- **New source** maps your own tables: pick the database and table, then
  fill the roles (timestamp, body, severity, service, trace id, attribute
  maps for logs; timestamp, ids, span name, kind, duration and its unit,
  status, attributes, events and links for traces; one table per metric type
  for metrics). A check mark next to each field confirms the column exists.
- **Correlation** links a logs source to a traces source so a log line can
  open its trace and a span can list its logs.
- **Test** runs a count over the last hour and reports missing columns.

Only mapped and verified column names are ever used in SQL. Everything
typed into the search box becomes a literal, never an identifier.

## Search syntax

The same language works for logs and traces.

| Query | Meaning |
|---|---|
| `timeout` | whole-word match in the body (case-insensitive) |
| `"rate limit exceeded"` | exact phrase |
| `level:error` | severity, also `severity:` |
| `service:checkout` | service name |
| `service:api*` | wildcard on a value |
| `http.status_code:500` | an attribute, looked up in log, resource and scope attributes |
| `http.status_code:>=500` | numeric comparison on an attribute (`>`, `>=`, `<`, `<=`, `!=`) |
| `user.id:*` | the attribute exists |
| `duration:>200` | traces: span duration in milliseconds |
| `status:error`, `kind:server`, `span_name:"GET /api"` | traces roles |
| `level:error OR level:fatal` | boolean operators, `AND` is implicit between terms |
| `NOT level:debug`, `-level:debug` | negation |
| `(a OR b) c` | grouping |
| `ScopeName:otel.sdk` | any real column, written exactly as named |

## Logs

The Logs section is a search page: severity, service and attribute facets
on the left, a histogram stacked by severity on top (drag on it to zoom into
a range) and the matching rows below, newest first, loaded as you scroll.
Live tail polls every five seconds and pauses while a row is open.

Click a row to open the side panel:

- **Overview**: the parsed fields, with links to the trace when the row
  carries a trace id.
- **Attributes**: log, resource and scope attributes; hover a value to filter
  for it, filter it out, or copy it.
- **Trace**: the other logs of the same trace, and the waterfall when a
  traces source is correlated.
- **Context**: the rows before and after this one from the same service.
- **Raw**: the record as JSON.

Searches are in the URL, so a link reproduces the view. Save a search to
reuse it from the search box.

## Traces

Traces lists one row per trace: root span, service, duration, span count and
errors, with a histogram of trace counts and p50/p95 durations above it. The
search language applies to spans, so `service:payments status:error
duration:>500` finds slow failing calls.

Opening a trace shows the waterfall: spans as bars scaled to the trace,
nested by parent, coloured by service, with error marks and event ticks.
Click a span for its attributes, events (exceptions render their stack
trace), links and the logs recorded during the span.

## Metrics

The explorer picks a metric from the catalog (name, type, unit, description),
an aggregation, an optional group-by on an attribute, and a filter in the
search language. Aggregations by metric type:

- gauge and sum: `avg`, `sum`, `min`, `max`, `last`, `count`
- monotonic sums: `rate` (per second, resets handled)
- histograms: `p50`, `p95`, `p99` interpolated from the buckets, plus `avg`
  and `count`
- exponential histograms and summaries: `count` and `avg`

Series are capped at twenty per query; the table under the chart shows last,
min, max and average per series.

## Service map

Built from spans whose parent belongs to another service. Nodes are
services with span counts and error rate; edges are calls between them,
labelled with calls, errors or p95 latency. Click a service to search its
traces.

## Monitors

A monitor is a saved search with a threshold: every interval it counts the
matching logs or spans over a window and compares the count with the
threshold. When the condition holds, CH-UI records an alert event of type
`telemetry.monitor`, so a rule under **Governance → Alerts** can deliver it
to a channel. The monitor row shows its last value, state and error.

Monitors run in the background. Like the governance syncer they borrow the
ClickHouse credentials of an active admin session on that connection, and
each borrow is written to the audit log.

## Guardrails

- Every query is a read; there is no write path.
- Queries carry `max_execution_time` and `max_result_rows` limits and a
  result size cap.
- Interactive queries run as the signed-in user, through the connection's
  tunnel, and are recorded in query history like any other query.
- Identifiers come from verified source mappings; search input is escaped
  and only ever appears as a literal.

## Troubleshooting

- **No sources detected**: the connection's user cannot see the tables, or
  they are not named like the exporter's defaults. Map them by hand with
  New source.
- **Missing columns**: the mapping names a column the table lacks. Open the
  source and fix the field with the red mark, or leave optional fields empty.
- **Timestamps look wrong or filters return nothing**: the timestamp column
  must be `DateTime` or `DateTime64`. String timestamps are not supported.
- **Attributes are empty**: check whether the columns are `Map` or `JSON`.
  Both are supported, but a source created against the wrong layout needs
  its mapping re-verified with Test.
- **Traces open with no logs**: set the correlated logs source on the traces
  source, and make sure the log rows carry the trace id.
- **Monitors show state `error`**: no active admin session on the
  connection to borrow credentials from, or the query failed. The row shows
  the message.
