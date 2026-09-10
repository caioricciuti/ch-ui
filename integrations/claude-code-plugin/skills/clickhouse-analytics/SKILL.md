---
name: clickhouse-analytics
description: Answer data questions against ClickHouse through the ch-ui MCP server. Use when the user asks about data, tables, metrics, dashboards or query performance in ClickHouse, or when ch-ui MCP tools are available.
---

# ClickHouse analytics through CH-UI

The `ch-ui` MCP server exposes one ClickHouse connection with read-only
tools. Every query runs with `readonly=2`, a row cap and a 60 second limit,
and is recorded in CH-UI's query history. You cannot break anything, but you
can waste time on full scans. Plan first.

## The loop

1. **Find the data.** `search_catalog` with a short term from the question
   (`orders`, `latency`, `signup`). It returns tables, columns, saved queries
   and dashboards. If a **verified** saved query matches the question, run it
   with `run_saved_query` and stop.
2. **Understand the table.** `describe_table` before writing SQL. Read the
   `sorting_key` and `partition_key` in `table_info`, the `sample_rows`, and
   `storage.rows`. Filter on the sorting key whenever you can; it is the
   only index ClickHouse has by default.
3. **Size the query.** On tables above ~10M rows call `estimate_query` with
   the SELECT. If `total_rows` is large, tighten the WHERE clause, then run
   with `max_bytes` set to roughly twice the estimate's bytes as a safety
   budget.
4. **Run.** `run_select` with an explicit `LIMIT` and only the columns you
   need. Use `format: "csv"` for wide results. `truncated: true` means the
   answer is incomplete: aggregate or filter instead of asking for more rows.
5. **Check the plan** if a query is slow: `explain_query` shows whether the
   primary key or a skip index was used.

Never page through raw rows to compute something ClickHouse can aggregate.

## ClickHouse SQL that models get wrong

- Time bucketing: `toStartOfHour(ts)`, `toStartOfDay(ts)`, `toDate(ts)`.
  There is no `date_trunc('hour', ts)` in older versions; prefer the `toStartOf*` family.
- Distinct counts: `uniq(x)` (approximate, fast) or `uniqExact(x)`.
  `count(DISTINCT x)` works but is slower on big tables.
- Percentiles: `quantile(0.95)(latency_ms)`, `quantiles(0.5, 0.95)(x)`.
- Conditional aggregates: `countIf(cond)`, `sumIf(v, cond)`, `avgIf(v, cond)`.
- Strings and numbers do not coerce: compare `String` columns to quoted
  literals and cast with `toUInt64()` / `toString()` explicitly.
- Dates: `today()`, `now()`, `now() - INTERVAL 7 DAY`, `toDate('2026-01-31')`.
- Arrays: `arrayJoin(arr)` to explode, `has(arr, x)`, `length(arr)`.
- `LIMIT n BY key` gives the top n per group without a window function.
- `FINAL` on ReplacingMergeTree / CollapsingMergeTree tables when you need
  deduplicated rows; it is slow, so prefer `argMax`-style aggregation.
- Identifiers with unusual characters need backticks, not double quotes.
- Do not add a `FORMAT` clause; the server returns JSON or CSV itself.

## Reading the tools' output

- `describe_table.table_info.create_table_query` is the source of truth for
  engine, keys and TTL. `storage.last_modified` says how fresh the data is.
- `estimate_query.assessment` is a plain-language read of the scan size.
- `run_select.stats.rows_read` vs `row_count`: a huge gap means the query
  scanned far more than it returned; add a sorting-key filter.
- `query_insights_top` (Pro) answers "what was slow" from `system.query_log`
  without you writing that SQL.

## Creating things

With a write-scope key, `save_query`, `create_dashboard`, `create_model` and
`create_pipeline` create **drafts** in CH-UI. Nothing runs against
ClickHouse from those tools. Tell the user what you created and where to
review it. Ask before creating anything the user did not request.
