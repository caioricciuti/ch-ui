package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/lineage"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// Observe tools expose what the Telemetry, Pipelines and Lineage pages show:
// which OpenTelemetry tables exist, the logs in them, the state of push
// ingest, and how tables feed each other. They reuse the SQL builders and
// row shaping of those pages, run with the key's own credentials, and refuse
// or filter anything in a database outside the key's allow-list. Traces,
// metrics, the service map and monitors are Pro (tools_observe_pro.go).

const (
	defaultObserveLimit = 50
	maxObserveLimit     = 500

	defaultLineageDepth = 3
	maxLineageDepth     = 10
	maxLineageNodes     = 500
)

// telemetrySettings carry the limits of telemetry.QuerySettings as request
// settings; see runTelemetry.
var telemetrySettings = map[string]string{
	"max_execution_time": "25",
	"max_result_rows":    strconv.Itoa(telemetry.MaxResultRows),
}

// runTelemetry runs SQL built by internal/telemetry. Those builders end in
// telemetry.QuerySettings, a SETTINGS clause that readonly=1 refuses, so it
// is cut here and the same limits travel as request settings.
func runTelemetry(ctx context.Context, deps Deps, ak *authedKey, sql string) ([]map[string]any, error) {
	sql = strings.TrimSuffix(strings.TrimSpace(sql), telemetry.QuerySettings)
	return runCH(ctx, deps, ak, sql, telemetrySettings, metaTimeout)
}

// clampLimit applies the observe tools' row cap.
func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// rangeArgs is the time range shared by the telemetry tools.
type rangeArgs struct {
	From  string `json:"from,omitempty" jsonschema:"range start, RFC 3339 (e.g. 2026-10-16T08:00:00Z); overrides since"`
	To    string `json:"to,omitempty" jsonschema:"range end, RFC 3339; default now"`
	Since string `json:"since,omitempty" jsonschema:"relative range ending at to when from is not given: 15m, 1h (default), 6h, 24h, 7d"`
}

// resolve returns the range as RFC 3339 bounds.
func (a rangeArgs) resolve(now time.Time) (from, to string, err error) {
	end := now.UTC()
	if strings.TrimSpace(a.To) != "" {
		if end, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(a.To)); err != nil {
			return "", "", fmt.Errorf("to must be RFC 3339, e.g. 2026-10-16T08:00:00Z")
		}
	}
	var start time.Time
	if strings.TrimSpace(a.From) != "" {
		if start, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(a.From)); err != nil {
			return "", "", fmt.Errorf("from must be RFC 3339, e.g. 2026-10-16T08:00:00Z")
		}
	} else {
		d, err := parseSince(a.Since)
		if err != nil {
			return "", "", err
		}
		start = end.Add(-d)
	}
	if !end.After(start) {
		return "", "", fmt.Errorf("from must be before to")
	}
	return start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), nil
}

// parseSince reads a relative range: a Go duration (15m, 6h) or days (7d).
func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Hour, nil
	}
	var d time.Duration
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("since must look like 15m, 1h, 24h or 7d")
		}
		d = time.Duration(days) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("since must look like 15m, 1h, 24h or 7d")
		}
	}
	if d <= 0 || d > 366*24*time.Hour {
		return 0, fmt.Errorf("since must be between 1s and 366d")
	}
	return d, nil
}

// observeSource resolves a telemetry source of kind on the key's connection:
// the one named by id, else the first enabled one. Sources in a database
// outside the allow-list are refused.
func observeSource(deps Deps, ak *authedKey, id string, kind telemetry.Kind) (*telemetry.Source, *mcp.CallToolResult) {
	id = strings.TrimSpace(id)
	if id != "" {
		src, err := deps.DB.GetTelemetrySource(id)
		if err != nil {
			return nil, errResult("failed to load telemetry source: %v", err)
		}
		if src == nil || src.ConnectionID != ak.key.ConnectionID {
			return nil, errResult("telemetry source %q not found on this connection; telemetry_sources lists them", id)
		}
		if src.Kind != kind {
			return nil, errResult("source %q is a %s source; this tool needs a %s source", id, src.Kind, kind)
		}
		if !dbAllowed(ak.key, src.Database) {
			return nil, errResult("source %q reads database %q, which is not in this key's allowlist", id, src.Database)
		}
		return src, nil
	}
	all, err := deps.DB.ListTelemetrySources(ak.key.ConnectionID)
	if err != nil {
		return nil, errResult("failed to list telemetry sources: %v", err)
	}
	for i := range all {
		if all[i].Kind == kind && all[i].Enabled && dbAllowed(ak.key, all[i].Database) {
			return &all[i], nil
		}
	}
	return nil, errResult("no enabled %s source on this connection that this key may read; telemetry_sources lists them, and an admin adds sources in Telemetry", kind)
}

// sourceColumns reads the column name -> type map of a source table.
func sourceColumns(ctx context.Context, deps Deps, ak *authedKey, db, table string) (map[string]string, error) {
	sql, err := telemetry.DescribeSQL(db, table)
	if err != nil {
		return nil, err
	}
	rows, err := runCH(ctx, deps, ak, sql, nil, metaTimeout)
	if err != nil {
		return nil, err
	}
	cols := telemetry.ColumnTypes(rows)
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s.%s has no columns or does not exist", db, table)
	}
	return cols, nil
}

// sourceRef is how results name the source they read.
func sourceRef(src *telemetry.Source) map[string]any {
	ref := map[string]any{"id": src.ID, "name": src.Name, "kind": src.Kind, "database": src.Database}
	if src.Table != "" {
		ref["table"] = src.Table
	}
	return ref
}

type searchLogsArgs struct {
	SourceID string `json:"source_id,omitempty" jsonschema:"logs source id from telemetry_sources; default the first enabled logs source"`
	rangeArgs
	Query    string   `json:"query,omitempty" jsonschema:"search: words match the body; field:value matches level, service, trace_id, span_id or any attribute key (e.g. http.status_code:>=500); quote phrases; AND, OR, NOT or - to negate; * wildcards"`
	Severity []string `json:"severity,omitempty" jsonschema:"keep only these severity texts, e.g. [\"ERROR\",\"WARN\"]"`
	Services []string `json:"services,omitempty" jsonschema:"keep only these service names"`
	Order    string   `json:"order,omitempty" jsonschema:"desc (default, newest first) or asc"`
	Limit    int      `json:"limit,omitempty" jsonschema:"rows to return (default 50, max 500)"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call, to page on with the same filters"`
}

type ingestStatusArgs struct {
	PipelineID string `json:"pipeline_id,omitempty" jsonschema:"one pipeline's id; default every push pipeline on this connection"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"pipelines per page (default 50, max 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous next_cursor to fetch the next page"`
}

type tableLineageArgs struct {
	Database  string `json:"database" jsonschema:"database of the table"`
	Table     string `json:"table" jsonschema:"table, view, materialized view or dictionary name"`
	Direction string `json:"direction,omitempty" jsonschema:"upstream (what feeds it), downstream (what it feeds) or both (default)"`
	Depth     int    `json:"depth,omitempty" jsonschema:"hops to follow in each direction (default 3, max 10)"`
}

// registerObserveTools registers the community observe tools, and the Pro
// ones when a Pro license is active.
func registerObserveTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("List telemetry sources")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "telemetry_sources",
		Title:       title,
		Annotations: ann,
		Description: "List the OpenTelemetry sources configured on this connection: which database and tables hold logs, traces and metrics. Call it first to find a source_id for the telemetry tools.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		all, err := deps.DB.ListTelemetrySources(ak.key.ConnectionID)
		if err != nil {
			return errResult("failed to list telemetry sources: %v", err), nil, nil
		}
		sources := make([]map[string]any, 0, len(all))
		for i := range all {
			s := &all[i]
			if !dbAllowed(ak.key, s.Database) {
				continue
			}
			item := sourceRef(s)
			item["enabled"] = s.Enabled
			if s.Tables != nil {
				item["tables"] = s.Tables
			}
			if s.CorrelatedLogs != "" {
				item["correlated_logs"] = s.CorrelatedLogs
			}
			if s.CorrelatedTraces != "" {
				item["correlated_traces"] = s.CorrelatedTraces
			}
			sources = append(sources, item)
		}
		out := map[string]any{"sources": sources}
		if len(sources) == 0 {
			out["note"] = "no telemetry sources this key may read; an admin adds them in Telemetry"
		} else if deps.Config == nil || !deps.Config.IsPro() {
			out["note"] = "search_logs works on logs sources; traces, metrics, the service map and monitors need a CH-UI Pro license"
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Search logs")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_logs",
		Title:       title,
		Annotations: ann,
		Description: "Search log lines of a logs source in a time range (default the last hour), newest first, with the same search language as the Logs page. Returns timestamp, severity, service, body, trace and span ids and attributes; page with next_cursor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchLogsArgs) (*mcp.CallToolResult, any, error) {
		src, errRes := observeSource(deps, ak, args.SourceID, telemetry.KindLogs)
		if errRes != nil {
			return errRes, nil, nil
		}
		if src.Logs == nil {
			return errResult("source %q has no logs mapping", src.ID), nil, nil
		}
		from, to, err := args.resolve(time.Now())
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		order := strings.ToLower(strings.TrimSpace(args.Order))
		if order != "" && order != "asc" && order != "desc" {
			return errResult("order must be asc or desc"), nil, nil
		}
		var cursor *telemetry.Cursor
		if args.Cursor != "" {
			cur, err := telemetry.DecodeCursor(args.Cursor)
			if err != nil {
				return errResult("invalid cursor; pass the next_cursor value from the previous call"), nil, nil
			}
			cursor = &cur
		}
		cols, err := sourceColumns(ctx, deps, ak, src.Database, src.Table)
		if err != nil {
			return errResult("search_logs failed: %v", err), nil, nil
		}
		limit := clampLimit(args.Limit, defaultObserveLimit, maxObserveLimit)
		params := telemetry.LogsParams{From: from, To: to, Q: args.Query, Severity: args.Severity, Services: args.Services}
		sql, err := telemetry.SearchSQL(src, cols, params, order, limit+1, cursor)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		rows, err := runTelemetry(ctx, deps, ak, sql)
		if err != nil {
			return errResult("search_logs failed: %v", err), nil, nil
		}
		out := map[string]any{"source": sourceRef(src), "from": from, "to": to}
		if len(rows) > limit {
			rows = rows[:limit]
			out["next_cursor"] = telemetry.LogCursor(rows[len(rows)-1])
		}
		out["rows"] = telemetry.ShapeLogRows(rows)
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Ingest status")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ingest_status",
		Title:       title,
		Annotations: ann,
		Description: "State of the push pipelines (OTLP, Elasticsearch, Splunk HEC and webhook ingest) on this connection: signal, status, target tables, last error, and since the server started the requests received, rejected by reason, rows written and the spool backlog. Use it when telemetry looks stale or missing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ingestStatusArgs) (*mcp.CallToolResult, any, error) {
		items, errRes := ingestStatus(deps, ak, strings.TrimSpace(args.PipelineID))
		if errRes != nil {
			return errRes, nil, nil
		}
		page, next, err := pageOf(items, args.Cursor, clampPageSize(args.PageSize))
		if err != nil {
			return errResult("invalid cursor; pass the next_cursor value from the previous page"), nil, nil
		}
		out := map[string]any{"pipelines": page}
		if next != "" {
			out["next_cursor"] = next
		}
		if len(items) == 0 {
			out["note"] = "no push pipelines on this connection that this key may see"
		} else if deps.IngestHub == nil {
			out["note"] = "the ingest spool is not open on this server, so live counters are unavailable"
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Table lineage")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "table_lineage",
		Title:       title,
		Annotations: ann,
		Description: "Upstream and downstream lineage of a table from ClickHouse metadata: the tables, materialized views, views, dictionaries and external sources that feed it or read from it, with edge kinds (triggers, writes, reads, routes, sources). Use it before changing or dropping a table.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args tableLineageArgs) (*mcp.CallToolResult, any, error) {
		return tableLineage(ctx, deps, ak, args), nil, nil
	})

	if deps.Config != nil && deps.Config.IsPro() {
		registerObserveProTools(srv, deps, ak)
	}
}

// ingestStatus lists the push pipelines of the key's connection with their
// live counters. A pipeline writing to a database outside the allow-list is
// left out. Ingest keys are never read.
func ingestStatus(deps Deps, ak *authedKey, pipelineID string) ([]map[string]any, *mcp.CallToolResult) {
	all, err := deps.DB.GetPipelines()
	if err != nil {
		return nil, errResult("failed to list pipelines: %v", err)
	}
	sources, err := deps.DB.PipelineSourceNodes()
	if err != nil {
		return nil, errResult("failed to list pipeline sources: %v", err)
	}
	now := time.Now()
	items := []map[string]any{}
	for _, p := range all {
		if p.ConnectionID != ak.key.ConnectionID || (pipelineID != "" && p.ID != pipelineID) {
			continue
		}
		signal := pipelines.PushSignal(sources[p.ID])
		if signal == "" {
			continue
		}
		sinks, ok := pipelineSinks(deps, ak, p.ID)
		if !ok {
			continue
		}
		item := map[string]any{
			"id":              p.ID,
			"name":            p.Name,
			"status":          p.Status,
			"signal":          signal,
			"sinks":           sinks,
			"last_started_at": p.LastStartedAt,
			"last_error":      p.LastError,
		}
		if deps.IngestHub != nil {
			view := deps.IngestHub.View(p.ID, now)
			view.Signal = signal
			view.LastError = nil // already at the top level
			item["stats"] = view
		}
		items = append(items, item)
	}
	if pipelineID != "" && len(items) == 0 {
		return nil, errResult("push pipeline %q not found on this connection (or it writes to a database outside this key's allowlist)", pipelineID)
	}
	return items, nil
}

// pipelineSinks returns the ClickHouse tables a pipeline writes to; ok is
// false when one of them is in a database outside the key's allow-list.
func pipelineSinks(deps Deps, ak *authedKey, pipelineID string) (sinks []map[string]string, ok bool) {
	nodes, _, err := deps.DB.GetPipelineGraph(pipelineID)
	if err != nil {
		return nil, allowedDBs(ak.key) == nil
	}
	sinks = []map[string]string{}
	for _, n := range nodes {
		if n.NodeType != "sink_clickhouse" {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal([]byte(n.ConfigEncrypted), &cfg)
		db, _ := cfg["database"].(string)
		if db == "" {
			db = "default" // the sink's own default
		}
		table, _ := cfg["table"].(string)
		if !dbAllowed(ak.key, db) {
			return nil, false
		}
		sinks = append(sinks, map[string]string{"database": db, "table": table})
	}
	return sinks, true
}

// tableLineage answers table_lineage. The graph is read with the key's own
// account; for an allow-listed key only the allowed databases are loaded,
// and nodes in other databases (neighbours Build adds) are dropped before
// the walk, so a path never leads through them.
func tableLineage(ctx context.Context, deps Deps, ak *authedKey, args tableLineageArgs) *mcp.CallToolResult {
	db, table := strings.TrimSpace(args.Database), strings.TrimSpace(args.Table)
	if db == "" || table == "" {
		return errResult("database and table are required")
	}
	if !dbAllowed(ak.key, db) {
		return errResult("database %q is not in this key's allowlist", db)
	}
	direction := strings.ToLower(strings.TrimSpace(args.Direction))
	if direction == "" {
		direction = "both"
	}
	if direction != "both" && direction != "upstream" && direction != "downstream" {
		return errResult("direction must be upstream, downstream or both")
	}
	depth := clampLimit(args.Depth, defaultLineageDepth, maxLineageDepth)

	var dbs []string
	for d := range allowedDBs(ak.key) {
		dbs = append(dbs, d)
	}
	sort.Strings(dbs)
	q := lineageQuery(deps, ak)
	g, err := lineage.Load(ctx, q, dbs)
	var tooLarge lineage.TooLargeError
	if errors.As(err, &tooLarge) && len(dbs) != 1 {
		// Too many objects to read them all: read the table's own database
		// and its direct neighbours instead.
		g, err = lineage.Load(ctx, q, []string{db})
		if err == nil {
			g.Meta.Warnings = append(g.Meta.Warnings, "more than "+strconv.Itoa(lineage.MaxObjects)+" objects are visible: only "+db+" and its direct neighbours were read")
		}
	}
	if err != nil {
		return errResult("table_lineage failed (is system.tables readable by this key's ClickHouse user?): %v", err)
	}

	root := db + "." + table
	nodes, edges, found := lineageWalk(g, root, direction, depth, func(n lineage.Node) bool {
		return n.Database == "" || dbAllowed(ak.key, n.Database)
	})
	if !found {
		return errResult("%s not found: no visible table, view or dictionary by that name for this key's ClickHouse user", root)
	}
	out := map[string]any{"root": root, "direction": direction, "depth": depth, "nodes": nodes, "edges": edges}
	if len(nodes) >= maxLineageNodes {
		out["truncated"] = true
		out["note"] = fmt.Sprintf("capped at %d nodes; lower depth or pick one direction", maxLineageNodes)
	}
	if len(g.Meta.Warnings) > 0 {
		out["warnings"] = g.Meta.Warnings
	}
	return jsonResult(out)
}

// lineageQuery runs lineage metadata queries through runCH with the key's
// credentials. Rows decode into maps first; 64-bit counts arrive quoted and
// lineage.Count reads them back exactly.
func lineageQuery(deps Deps, ak *authedKey) lineage.Query {
	return func(ctx context.Context, sql string, params map[string]string, dst any) error {
		extra := map[string]string{"max_execution_time": "25"}
		for k, v := range params {
			extra[k] = v
		}
		rows, err := runCH(ctx, deps, ak, sql, extra, metaTimeout)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		b, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, dst)
	}
}

// lineageWalk returns the part of g reachable from root within depth hops,
// upstream (against the edges), downstream (along them) or both, over the
// nodes keep accepts. found is false when root is not a kept node.
func lineageWalk(g lineage.Graph, root, direction string, depth int, keep func(lineage.Node) bool) (nodes []lineage.Node, edges []lineage.Edge, found bool) {
	byID := make(map[string]lineage.Node, len(g.Nodes))
	for _, n := range g.Nodes {
		if keep(n) {
			byID[n.ID] = n
		}
	}
	if _, ok := byID[root]; !ok {
		return nil, nil, false
	}
	in := map[string][]int{}  // node -> edges ending at it
	out := map[string][]int{} // node -> edges starting at it
	for i, e := range g.Edges {
		if _, ok := byID[e.From]; !ok {
			continue
		}
		if _, ok := byID[e.To]; !ok {
			continue
		}
		in[e.To] = append(in[e.To], i)
		out[e.From] = append(out[e.From], i)
	}
	seenNode := map[string]bool{root: true}
	seenEdge := map[int]bool{}
	walk := func(adj map[string][]int, next func(lineage.Edge) string) {
		frontier := []string{root}
		visited := map[string]bool{root: true}
		for hop := 0; hop < depth && len(frontier) > 0 && len(seenNode) < maxLineageNodes; hop++ {
			var nextFrontier []string
			for _, id := range frontier {
				for _, ei := range adj[id] {
					seenEdge[ei] = true
					n := next(g.Edges[ei])
					if visited[n] {
						continue
					}
					visited[n] = true
					seenNode[n] = true
					nextFrontier = append(nextFrontier, n)
				}
			}
			frontier = nextFrontier
		}
	}
	if direction != "downstream" {
		walk(in, func(e lineage.Edge) string { return e.From })
	}
	if direction != "upstream" {
		walk(out, func(e lineage.Edge) string { return e.To })
	}
	ids := make([]string, 0, len(seenNode))
	for id := range seenNode {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > maxLineageNodes {
		ids = ids[:maxLineageNodes]
	}
	kept := make(map[string]bool, len(ids))
	nodes = make([]lineage.Node, 0, len(ids))
	for _, id := range ids {
		kept[id] = true
		nodes = append(nodes, byID[id])
	}
	edges = []lineage.Edge{}
	for i, e := range g.Edges {
		if seenEdge[i] && kept[e.From] && kept[e.To] {
			edges = append(edges, e)
		}
	}
	return nodes, edges, true
}
