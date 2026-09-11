# Telemetry

CH-UI turns the OpenTelemetry data your collector already writes to ClickHouse
into a search and analysis surface: logs, traces with waterfalls, a metrics
explorer, a service map, and monitors that raise alerts. Nothing is copied or
indexed elsewhere. Every screen runs SQL against your tables, through the same
connection and credentials as the query editor.

Telemetry lives under **Visualize → Telemetry**, with six sections in the
sidebar: Logs, Traces, Metrics, Service map, Monitors and Sources.

**Editions.** Logs, Sources and saved searches are part of the open-source
core. Traces, Metrics, Service map and Monitors need a Pro license. On the
community edition those sections show a lock in the sidebar and a license
page, and their API endpoints return `402`.

**Upgrading from v2.10 or earlier.** The single-table setup wizard is gone.
Open **Sources** and click **Detect sources** to pick up the exporter's
tables, or map your own. `GET`/`PUT /api/telemetry/config` now return
`410 Gone`.

The same guide is published at <https://ch-ui.com/docs/telemetry/>.

## Send data with the OpenTelemetry collector

Point the collector's ClickHouse exporter at a database CH-UI can reach. With
`create_schema: true` the exporter creates `otel_logs`, `otel_traces` and the
`otel_metrics_*` tables on first start.

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

A source tells CH-UI which table holds a signal and which column plays which
role. Sources are per connection. Creating and editing them needs the analyst
or admin role.

- **Detect sources** scans the connection for the exporter's tables and
  proposes one source per signal, with the mapping already checked against
  the table's columns. Optional roles the table lacks are left empty. Review
  the proposals and add the ones you want.
- **New source** maps your own tables: pick the database and table, then fill
  the roles. For logs: timestamp, body, severity, service, trace id and the
  attribute maps. For traces: timestamp, trace and span ids, span name, kind,
  duration and its unit, status, attributes, events and links. For metrics:
  one table per metric type. A check mark next to each field confirms the
  column exists.
- **Correlation** links a logs source to a traces source, so a log line can
  open its trace and a span can list its logs.
- **Test** reports missing columns and, when the mapping is complete, counts
  the rows in the last hour and shows the latest timestamp.

Only mapped and verified column names are ever used in SQL. Everything typed
into the search box becomes a literal, never an identifier.

## Search syntax

The same language works for logs and traces. Terms next to each other are
combined with `AND`.

| Query | Meaning |
|---|---|
| `timeout` | Word match in the body, case-insensitive. Words with punctuation match as a substring |
| `time*` | Wildcard in free text |
| `"rate limit exceeded"` | Phrase, matched as a case-insensitive substring of the body |
| `level:error` | Severity, case-insensitive. `severity:` works too |
| `service:checkout` | Service name |
| `service:api*` | Wildcard on a value |
| `http.status_code:500` | An attribute. Logs look in log, resource and scope attributes; traces in span and resource attributes |
| `http.status_code:>=500` | Comparison: `>`, `>=`, `<`, `<=`, `!=` (`!=` also works on strings) |
| `user.id:*` | The attribute exists |
| `duration:>200` | Traces: span duration in milliseconds, whatever unit the source stores |
| `status:error`, `kind:server` | Trace roles, case-insensitive |
| `span_name:"GET /api"` | Span name, case-sensitive |
| `level:error OR level:fatal` | Boolean `OR` |
| `NOT level:debug`, `-level:debug` | Negation |
| `(a OR b) c` | Grouping |
| `ScopeName:otel.sdk` | Any real column, written exactly as named |

The time range defaults to the last hour; pick a preset or an absolute range.
The whole search, range included, lives in the URL, so a link reproduces the
view.

## Logs

The Logs section is a search page: severity, service and attribute facets on
the left, a histogram stacked by severity on top (drag across it to zoom into
a range), and the matching rows below, newest first, loaded as you scroll
(200 rows per page). Facets show the top 20 severities, 50 services, 50
attribute keys and 10 values per key.

**Live tail** polls every five seconds. It pauses while a row is open, when you
scroll away from the top, and when an absolute range is set.

Click a row to open the side panel:

- **Overview**: the parsed fields, with a link to the trace when the row
  carries a trace id.
- **Attributes**: log, resource and scope attributes. Hover a value to filter
  for it, filter it out, or copy it.
- **Trace**: the other logs of the same trace, and the waterfall when a traces
  source is correlated.
- **Context**: the rows before and after this one from the same service.
- **Raw**: the record as JSON.

The panel can also copy a link to the row. On the keyboard, `j`/`k` or the
arrow keys move between rows, `Enter` opens one and `Esc` closes it.

**Saved searches** keep a query for reuse from the search box. They are shared
on the connection and are part of the community edition.

## Traces (Pro)

Traces lists one row per trace: start time, root span, service, duration,
span count and errors, 100 per page. Above it, a histogram shows trace counts
over time with errors stacked, plus p50 and p95 durations. The search
language applies to spans, so `service:payments status:error duration:>500`
finds slow failing calls.

Opening a trace shows the waterfall: spans as bars scaled to the trace, nested
by parent, coloured by service, with error marks and event ticks. Subtrees
can be collapsed. Click a span for its panel: Overview, Attributes, Events
(exceptions render their stack trace), Links, and the Logs recorded during the
span.

Trace and span ids can be copied from the panel. `j`/`k` move between spans
and `Esc` closes the panel.

## Metrics (Pro)

The explorer plots up to four queries at once. Each one picks a metric from the
catalog (name, type, unit, description), an aggregation, an optional
group-by on up to two attributes, and a filter in the search language.

| Metric type | Aggregations |
|---|---|
| Gauge | `avg`, `sum`, `min`, `max`, `last`, `count` |
| Sum | `rate` (default, per second, meant for monotonic counters), `sum`, `avg`, `max`, `last`, `count` |
| Histogram | `p50`, `p95` (default), `p99` interpolated from the buckets, `avg`, `count` |
| Exponential histogram | `count`, `avg` |
| Summary | `count`, `sum`, `avg` |

Each query shows up to 20 series. The table under the chart gives last, min,
max and average per series.

## Service map (Pro)

Built from spans whose parent belongs to another service, over at most 500,000
spans in the selected range. Nodes are services with span counts and error
rate; edges are calls between them, labelled with calls, errors or p95 latency
(switch with the toggle). A routes table below lists each caller and callee
pair with p50 and p95. Click a service to search its traces.

## Monitors (Pro)

A monitor is a saved search with a threshold. Every interval it counts the
matching logs or spans over a window and compares the count with the threshold
(greater than, at least, less than, at most).

| Setting | Range |
|---|---|
| Window | 60 seconds to 7 days |
| Interval | 30 seconds to 24 hours |
| Severity | `info`, `warn`, `error`, `critical` |

When a monitor goes from OK to firing, CH-UI records one alert event of type
`telemetry.monitor`, so a rule under **Governance → Alerts** can deliver it to
a channel. It does not emit again while it stays firing. Firing and recovering
are both written to the audit log (`telemetry.monitor.fired`,
`telemetry.monitor.recovered`); recovery raises no alert event. The monitor row
shows its last value, state and error.

Monitors run in the background and are checked every 30 seconds. Like the
governance syncer, background runs borrow the ClickHouse credentials of an
active session on the connection, so they need someone signed in to that
connection. **Run now** evaluates a monitor immediately with your own
credentials.

## Brain integration

The Brain assistant has telemetry tools built in (`query_logs`,
`query_traces`, `list_services`, `query_metrics`), so you can ask things like
*"show me error logs for the checkout service in the last hour"* in plain
language and get the underlying query back.

## Guardrails

- Every query is a read; there is no write path to your tables.
- Queries run with `max_execution_time = 25` seconds and
  `max_result_rows = 100000` (`result_overflow_mode = 'break'`).
- Interactive queries run as the signed-in user, through the connection's
  tunnel. They are not written to query history.
- Identifiers come from verified source mappings; search input is escaped and
  only ever appears as a literal.
- Creating or changing sources, saved searches and monitors needs the analyst
  or admin role, and each change is audited.

## Troubleshooting

- **No sources detected**: the connection's user cannot see the tables, or
  they are not named like the exporter's defaults. Map them by hand with
  **New source**.
- **Missing columns**: the mapping names a column the table lacks. Open the
  source and fix the field with the red mark, or leave optional fields empty.
- **Timestamps look wrong or filters return nothing**: the timestamp column
  must be `DateTime` or `DateTime64`. String timestamps are not supported.
- **Attributes are empty**: check whether the columns are `Map` or `JSON`.
  Both are supported, but a source created against the wrong layout needs its
  mapping re-checked with **Test**.
- **Traces open with no logs**: set the correlated logs source on the traces
  source, and make sure the log rows carry the trace id.
- **Monitors show state `error`**: nobody is signed in to the connection to
  borrow credentials from, or the query failed. The row shows the message.
