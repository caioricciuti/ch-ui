---
title: Telemetry
description: Search OpenTelemetry logs, traces and metrics stored in ClickHouse, with a service map and monitors that raise alerts. Nothing copied, every screen is SQL against your tables
---

CH-UI turns the OpenTelemetry data in ClickHouse into a search and analysis
surface, whether your collector writes it there or you
[send it to CH-UI](#send-data-here): logs, traces with waterfalls, a metrics
explorer, a service map, and monitors that raise alerts. Nothing is copied or
indexed elsewhere. Every screen runs SQL against your tables, through the same
connection and credentials as the query editor.

Telemetry lives under **Visualize → Telemetry**, with six sections in the
sidebar: Logs, Traces, Metrics, Service map, Monitors and Sources.

:::note
**Logs**, **Sources** and saved searches are part of the open-source core.
**Traces**, **Metrics**, **Service map** and **Monitors** need a
[Pro license](/docs/pricing). On the community edition those sections show a
lock in the sidebar and a license page, and their API endpoints return `402`.
:::

:::caution
**Upgrading from v2.10 or earlier:** the single-table setup wizard is gone.
Open **Sources** and click **Detect sources** to pick up the exporter's
tables, or map your own. `GET`/`PUT /api/telemetry/config` now return
`410 Gone`.
:::

## Send data with the OpenTelemetry collector

Point the collector's
[ClickHouse exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/clickhouseexporter)
at a database CH-UI can reach. With `create_schema: true` the exporter creates
`otel_logs`, `otel_traces` and the `otel_metrics_*` tables on first start.

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

## Send data here

No collector writing to ClickHouse yet? CH-UI can receive OTLP itself. Create
a [pipeline](/docs/pipelines) with an **Ingest** source (signal `logs` or
`traces`) and a ClickHouse sink with **Create Table If Not Exists** on, name
the table `otel_logs` or `otel_traces`, create an ingest key and run it. Then
send OTLP over HTTP to CH-UI:

```yaml
exporters:
  otlphttp/chui-logs:
    endpoint: https://ch-ui.example.com/ingest
    headers:
      Authorization: "Bearer ${env:CHUI_LOGS_KEY}"
```

or, from an OTel SDK:

```bash
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://ch-ui.example.com/ingest/v1/logs
export OTEL_EXPORTER_OTLP_LOGS_HEADERS="Authorization=Bearer%20chi_..."
```

CH-UI spools each request on disk before it answers and writes it to the
exporter's table layout (traces include the `<table>_trace_id_ts` lookup), so
**Detect sources** below finds the tables and every Telemetry screen works on
them. Each key serves one pipeline and one signal: logs and traces need a
pipeline and a key each. Keys, limits, the separate `:4318` listener and
configs for Vector and Fluent Bit are on the [Ingest](/docs/ingest) page.

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
| `url:http://x/a`, `ts:12:00` | A value runs to the next space, colons included. Quote it when it has spaces |
| `user.id:*` | The attribute exists |
| `duration:>200` | Traces: span duration in milliseconds, whatever unit the source stores |
| `status:error`, `kind:server` | Trace roles, case-insensitive |
| `span_name:"GET /api"` | Span name, case-sensitive |
| `level:error OR level:fatal` | Boolean `OR` |
| `NOT level:debug`, `-level:debug` | Negation |
| `(a OR b) c` | Grouping |
| `ScopeName:otel.sdk` | Any real column, written exactly as named |

`-field:value` and `field:!=value` match the same rows: every row where no
attribute map holds that value, including rows that do not have the key at
all. An attribute is looked up in each map (log, resource and scope
attributes), and `!=` must hold in all of them. To keep only rows that have
the key with another value, add an existence check: `user.id:* -user.id:42`.

The time range defaults to the last hour; pick a preset or an absolute range.
The whole search, range included, lives in the URL, so a link reproduces the
view.

## Logs

The Logs section is a search page: severity, service and attribute facets on
the left, a histogram stacked by severity on top (drag across it to zoom into
a range), and the matching rows below, newest first, loaded as you scroll
(200 rows per page). Facets show the top 20 severities, 50 services, 50
attribute keys and 10 values per key.

Each facet ignores its own filter and keeps the others, so picking `ERROR`
still lists the other severities with their counts, and clicking a value of
`http.method` still shows the other methods. Only top-level terms of that
field are ignored; a query with a top-level `OR` is applied as written.

Facet counts are read from a sample of at most 200,000 matching rows. When the
range holds more, the facet headers carry an **approx.** badge (hover it for
the sample size) and the counts describe the sample, not the whole range. If a
facet query fails, the panel says so instead of going blank.

### Reading the histograms

These notes apply to the Logs histogram and the Traces histogram:

- The tooltip shows the count for each severity in the hovered bucket, not the
  stacked height, whatever order the bars are painted in. Severity aliases
  such as `warning` and `WARN` are counted together.
- The chart keeps the exact range that was fetched. The first and last buckets
  are clipped to it, and the tooltip shows the clipped interval.
- Buckets with no rows are drawn as empty gaps in their real position rather
  than being skipped. On Traces, empty buckets have no p50 or p95 point
  instead of a zero duration.
- Dragging to zoom uses the range you dragged, clamped to the range that was
  fetched. It is not widened to the next bucket.
- If the histogram request fails, the old histogram is cleared and the chart
  shows **No data in this range** rather than keeping stale bars.

**Live tail** asks every five seconds for the rows newer than the newest one on
screen and adds them on top; the histogram and facets refresh at most every 30
seconds. The button reads **Paused** while a row is open or the list is
scrolled away from the top, and is disabled on an absolute range, which has
no new rows. Paging uses the row's timestamp, trace id, span id and a hash of
its content, so rows that share a timestamp are neither skipped nor repeated
between pages.

Click a row to open the side panel:

- **Overview**: the parsed fields, with a link to the trace when the row
  carries a trace id.
- **Attributes**: log, resource and scope attributes. Hover a value to filter
  for it, filter it out, or copy it.
- **Trace**: the other logs of the same trace, and the waterfall when a traces
  source is correlated.
- **Context**: the rows before and after this one from the same service,
  within an hour on either side. Rows sharing the anchor's timestamp are told
  apart by their content, so the highlighted row is the one you opened.
- **Raw**: the record as JSON.

The panel can also copy a link to the row. The link pins the current range to
absolute bounds and names the row, so opening it later reopens the same row
even after a relative range like the last hour has moved on. On the keyboard, `j`/`k` or the
arrow keys move between rows, `Enter` opens one and `Esc` closes it.

**Saved searches** keep a query, its time range and its source for reuse from
the search box; loading one switches back to that source. Severity and service
facet selections are not saved: write them into the query
(`level:error service:api`) to keep them. Saved searches are shared on the
connection and are part of the community edition.

## Traces (Pro)

Traces lists one row per trace: start time, root span, service, duration,
span count and errors, 100 per page. Above it, a histogram shows trace counts
over time with errors stacked, plus p50 and p95 durations. The search
language applies to spans, so `service:payments status:error duration:>500`
finds slow failing calls.

Opening a trace shows the waterfall: spans as bars scaled to the trace, nested
by parent, coloured by service, with error marks and event ticks. Subtrees
can be collapsed. Click a span for its panel:

- **Overview**, **Attributes**, **Events** (exceptions render their stack
  trace), **Links**, and **Logs** recorded during the span.

Trace and span ids can be copied from the panel. `j`/`k` move between spans
and `Esc` closes the panel.

A span counts as an error when its status is `Error` or `STATUS_CODE_ERROR`,
in any case.

Opening a trace looks its spans up by trace id within a time window, not over
the whole table. When the exporter's `<traces table>_trace_id_ts` index table
exists, CH-UI reads the trace's start and end from it and searches that window
plus one minute either side. Without it, the window is one hour either side of
the trace's start as listed (or of the log line, when opened from Logs), so spans
further than an hour away are not shown; a link that carries no time searches
without a bound. The trace's log lines are read from the span window plus one
minute either side.

## Metrics (Pro)

The explorer plots up to four queries at once. Each one picks a metric from the
catalog (name, type, unit, description), an aggregation, an optional
group-by on up to two attributes, and a filter in the search language.

| Metric type | Aggregations |
|---|---|
| Gauge | `avg`, `sum`, `min`, `max`, `last`, `count` |
| Sum | `rate` (default, per second, cumulative monotonic counters only), `sum`, `avg`, `max`, `last`, `count` |
| Histogram | `p50`, `p95` (default), `p99` interpolated from the buckets, `avg`, `count` |
| Exponential histogram | `count`, `avg` |
| Summary | `count`, `sum`, `avg` |

Each query shows up to 20 series. The table under the chart gives last, min,
max and average per series.

- `rate` is computed per raw series (service, point attributes and resource
  attributes), so two hosts reporting the same counter are not mixed, and a
  drop counts as a counter reset. It refuses delta or non-monotonic sums with
  an error: use `sum` for deltas and `avg` or `last` for up-down counters.
- Histogram percentiles use, per series, the increase of the bucket counts
  within each bucket of the chart for cumulative histograms, and the summed
  counts for delta histograms. `avg` and `count` on a cumulative histogram
  still read the running totals the exporter stores.
- A query that reaches the 100,000 row cap shows a **partial data** badge;
  narrow the range or group by fewer keys.

## Service map (Pro)

Built from spans whose parent belongs to another service. When the range holds
more than 500,000 spans, the map samples whole traces (the same traces on the
caller and callee side, so no call loses its parent), scales the counts back
up, and shows an **approx.** badge with the sample rate. Nodes are services
with span counts and error rate; edges are calls between them, labelled with calls, errors or p95 latency
(switch with the toggle). A routes table below lists each caller and callee
pair with p50 and p95. Click a service to search its traces.

## Monitors (Pro)

A monitor is a saved search with a threshold. Every interval it counts the
matching logs or spans over a window and compares the count with the threshold
(greater than, at least, less than, at most). The window ends 60 seconds
before the evaluation, so rows still on their way from the collector are not
missed: a 5 minute window evaluated at 10:00:00 counts 09:54:00 to 09:59:00.

| Setting | Range |
|---|---|
| Window | 60 seconds to 7 days |
| Interval | 30 seconds to 24 hours |
| Severity | `info`, `warn`, `error`, `critical` |

When a monitor goes from OK to firing, CH-UI records one alert event of type
`telemetry.monitor`, so a rule under
[Governance → Alerts](/docs/alerts) can deliver it to a channel. It does not
emit again while it stays firing. Firing and recovering are both written to
the [audit log](/docs/audit-log) (`telemetry.monitor.fired`,
`telemetry.monitor.recovered`); recovery raises no alert event. The monitor row
shows its last value, state and error.

When an evaluation fails (a broken query, a missing table, lost access), the
monitor goes to the error state and records one `telemetry.monitor` alert
event titled *evaluation failed*, at `warn` severity, so a silent monitor does
not pass for a quiet one. It does not emit again while it keeps failing. A
monitor that was firing, failed, and fires again is still the same incident
and raises no second firing event.

Monitors run in the background and are checked every 30 seconds, up to four
at a time, so one slow query does not hold up the rest. An administrator
can configure **Admin → Connections → Background accounts → Telemetry monitors**
to use a dedicated ClickHouse account, borrow an active session (the default for
existing installations), or disable background execution. A dedicated account
lets monitoring continue after everyone signs out. It needs SELECT grants
on the monitored logs and traces tables. A disabled or undecryptable account
never falls back to a human session. See
[Background Accounts](/docs/background-accounts). **Run now** evaluates a
monitor immediately with your own credentials, even when background execution
is disabled.

## Brain integration

The [Brain](/docs/brain) assistant has telemetry tools built in
(`query_logs`, `query_traces`, `list_services`, `query_metrics`), so you can
ask things like *"show me error logs for the checkout service in the last
hour"* in plain language and get the underlying query back.

## Guardrails

- Every query is a read; there is no write path to your tables.
- Queries run with `max_execution_time = 25` seconds and
  `max_result_rows = 100000` (`result_overflow_mode = 'break'`). Where that
  cap can cut an answer silently, the page says so (metrics show
  **partial data**).
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
- **Span events and links are empty** on a source saved with v2.11.0's first
  build: sources with the empty value repair themselves on load.
- **Traces open with no logs**: set the correlated logs source on the traces
  source, and make sure the log rows carry the trace id.
- **Monitors show state `error`**: source discovery or the evaluation query
  failed. The row shows the message; check the configured account's grants.
- **Monitors stop updating**: check the connection status and the
  [background account](/docs/background-accounts) mode. Session mode needs
  someone signed in to the connection. Disabled mode, an unreadable saved
  password, or no usable session skips evaluation and leaves the previous
  result visible. The server's debug logs record credential lookup failures.
