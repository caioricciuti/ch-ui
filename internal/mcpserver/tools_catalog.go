package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Catalog tools help the model plan before it writes SQL: find the right
// table or column, reuse SQL a human already vetted, and size a query before
// running it. ClickHouse's own agentic benchmark attributes most agent
// failures to the wrong plan rather than to bad SQL, so these are the tools
// that move accuracy.

const (
	searchMaxTables  = 50
	searchMaxColumns = 100
	searchMaxEntity  = 50
)

var systemDBs = "('system', 'INFORMATION_SCHEMA', 'information_schema')"

// paramNameRe matches a ClickHouse query-parameter name ({name:Type}).
var paramNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type searchCatalogArgs struct {
	Query string `json:"query" jsonschema:"case-insensitive substring to look for in table names, table comments, column names, column comments, saved queries and dashboards"`
}

type estimateArgs struct {
	SQL string `json:"sql" jsonschema:"the SELECT statement to estimate"`
}

type runSavedQueryArgs struct {
	ID       string            `json:"id,omitempty" jsonschema:"saved query id (from list_saved_queries or search_catalog); give id or name"`
	Name     string            `json:"name,omitempty" jsonschema:"saved query name, exact match, case-insensitive; give id or name"`
	Params   map[string]string `json:"params,omitempty" jsonschema:"values for the query's {name:Type} parameters; stored defaults are used for any omitted"`
	MaxRows  int               `json:"max_rows,omitempty" jsonschema:"maximum rows to return (default 100, max 2000)"`
	Format   string            `json:"format,omitempty" jsonschema:"json (default) or csv"`
	MaxBytes int64             `json:"max_bytes,omitempty" jsonschema:"abort if the query reads more than this many uncompressed bytes (0 = no budget)"`
}

func registerCatalogTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("Search catalog")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_catalog",
		Title:       title,
		Annotations: ann,
		Description: "Find tables, columns, saved queries and dashboards whose name or comment contains a term. Use it before writing SQL when you do not know where the data lives; prefer verified saved queries when one matches the question.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchCatalogArgs) (*mcp.CallToolResult, any, error) {
		q := strings.TrimSpace(args.Query)
		if q == "" {
			return errResult("query is required"), nil, nil
		}
		params := map[string]string{"param_q": q}
		set := allowedDBs(ak.key)
		keep := func(r map[string]any) bool {
			db, _ := r["database"].(string)
			return set == nil || set[db]
		}

		out := map[string]any{"query": q}
		tables, err := runCH(ctx, deps, ak,
			"SELECT database, name AS table, engine, comment, total_rows FROM system.tables WHERE database NOT IN "+systemDBs+
				" AND (positionCaseInsensitive(name, {q:String}) > 0 OR positionCaseInsensitive(comment, {q:String}) > 0) ORDER BY database, name LIMIT "+fmt.Sprint(searchMaxTables)+" FORMAT JSON",
			params, metaTimeout)
		if err != nil {
			return errResult("search_catalog failed: %v", err), nil, nil
		}
		out["tables"] = filterRows(tables, keep)

		columns, err := runCH(ctx, deps, ak,
			"SELECT database, table, name AS column, type, comment FROM system.columns WHERE database NOT IN "+systemDBs+
				" AND (positionCaseInsensitive(name, {q:String}) > 0 OR positionCaseInsensitive(comment, {q:String}) > 0) ORDER BY database, table, name LIMIT "+fmt.Sprint(searchMaxColumns)+" FORMAT JSON",
			params, metaTimeout)
		if err != nil {
			return errResult("search_catalog failed: %v", err), nil, nil
		}
		out["columns"] = filterRows(columns, keep)

		lq := strings.ToLower(q)
		saved := []map[string]any{}
		if all, err := deps.DB.GetSavedQueries(); err == nil {
			for _, sq := range all {
				if !savedQueryVisible(&sq, ak) {
					continue
				}
				desc := ""
				if sq.Description != nil {
					desc = *sq.Description
				}
				if strings.Contains(strings.ToLower(sq.Name), lq) || strings.Contains(strings.ToLower(desc), lq) || strings.Contains(strings.ToLower(sq.Query), lq) {
					saved = append(saved, map[string]any{"id": sq.ID, "name": sq.Name, "description": desc, "verified": sq.Verified})
					if len(saved) >= searchMaxEntity {
						break
					}
				}
			}
		}
		out["saved_queries"] = saved

		dashboards := []map[string]any{}
		if all, err := deps.DB.GetDashboards(); err == nil {
			for _, d := range all {
				desc := ""
				if d.Description != nil {
					desc = *d.Description
				}
				if strings.Contains(strings.ToLower(d.Name), lq) || strings.Contains(strings.ToLower(desc), lq) {
					dashboards = append(dashboards, map[string]any{"id": d.ID, "name": d.Name, "description": desc})
					if len(dashboards) >= searchMaxEntity {
						break
					}
				}
			}
		}
		out["dashboards"] = dashboards

		if len(out["tables"].([]map[string]any)) == 0 && len(out["columns"].([]map[string]any)) == 0 && len(saved) == 0 && len(dashboards) == 0 {
			out["note"] = "no matches; try a shorter or different term, or list_databases and list_tables to browse"
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Estimate query cost")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "estimate_query",
		Title:       title,
		Annotations: ann,
		Description: "Estimate what a SELECT would read (parts, rows, marks per table via EXPLAIN ESTIMATE) without running it. Use it before a query on a large table, then pass a max_bytes budget to run_select if the scan is big.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args estimateArgs) (*mcp.CallToolResult, any, error) {
		sql := strings.TrimSpace(args.SQL)
		if sql == "" {
			return errResult("sql is required"), nil, nil
		}
		head := leadingCommentsRe.ReplaceAllString(sql, "")
		if !selectOnlyRe.MatchString(head) {
			return errResult("estimate_query accepts SELECT / WITH statements only"), nil, nil
		}
		if trailingFormatRe.MatchString(sql) {
			return errResult("omit the FORMAT clause"), nil, nil
		}
		rows, err := runCH(ctx, deps, ak, "EXPLAIN ESTIMATE\n"+sql+"\nFORMAT JSON", nil, metaTimeout)
		if err != nil {
			return errResult("estimate failed: %v", err), nil, nil
		}
		var totalRows, totalParts, totalMarks float64
		for _, r := range rows {
			totalRows += num(r["rows"])
			totalParts += num(r["parts"])
			totalMarks += num(r["marks"])
		}
		out := map[string]any{
			"tables":      rows,
			"total_rows":  int64(totalRows),
			"total_parts": int64(totalParts),
			"total_marks": int64(totalMarks),
		}
		switch {
		case totalRows >= 1e9:
			out["assessment"] = "very large scan (1B+ rows): narrow the WHERE clause on the sorting key or partition key before running"
		case totalRows >= 1e8:
			out["assessment"] = "large scan (100M+ rows): consider a tighter filter; set max_bytes on run_select"
		case totalRows >= 1e7:
			out["assessment"] = "moderate scan (10M+ rows): fine for aggregates, slow for SELECT *"
		default:
			out["assessment"] = "small scan"
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Run saved query")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_saved_query",
		Title:       title,
		Annotations: ann,
		Description: "Run a saved query by id or name, with values for its {name:Type} parameters. Saved queries marked verified were reviewed by a human; prefer them over writing new SQL for the same question. Same caps, guards and audit as run_select.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args runSavedQueryArgs) (*mcp.CallToolResult, any, error) {
		sq, errRes := findSavedQuery(deps, ak, args.ID, args.Name)
		if errRes != nil {
			return errRes, nil, nil
		}
		merged := map[string]string{}
		if sq.Parameters != nil && strings.TrimSpace(*sq.Parameters) != "" {
			var stored map[string]string
			if err := jsonUnmarshal(*sq.Parameters, &stored); err == nil {
				for k, v := range stored {
					merged[k] = v
				}
			}
		}
		for k, v := range args.Params {
			if !paramNameRe.MatchString(k) {
				return errResult("invalid parameter name %q", k), nil, nil
			}
			merged[k] = v
		}
		extra := make(map[string]string, len(merged))
		for k, v := range merged {
			if paramNameRe.MatchString(k) {
				extra["param_"+k] = v
			}
		}
		return runReadQuery(ctx, deps, ak, readQuery{
			SQL: sq.Query, MaxRows: args.MaxRows, Format: args.Format, MaxBytes: args.MaxBytes,
			Extra:    extra,
			Endpoint: "/mcp/run_saved_query",
			Meta:     map[string]any{"saved_query": map[string]any{"id": sq.ID, "name": sq.Name, "verified": sq.Verified}},
		}), nil, nil
	})
}

// savedQueryVisible reports whether a saved query belongs to the key's
// connection or to no connection at all.
func savedQueryVisible(sq *database.SavedQuery, ak *authedKey) bool {
	return sq.ConnectionID == nil || *sq.ConnectionID == "" || *sq.ConnectionID == ak.key.ConnectionID
}

// findSavedQuery resolves a saved query by id or exact (case-insensitive)
// name, restricted to what the key may see.
func findSavedQuery(deps Deps, ak *authedKey, id, name string) (*database.SavedQuery, *mcp.CallToolResult) {
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	if id == "" && name == "" {
		return nil, errResult("give the saved query's id or name")
	}
	if id != "" {
		sq, err := deps.DB.GetSavedQueryByID(id)
		if err != nil {
			return nil, errResult("lookup failed: %v", err)
		}
		if sq == nil || !savedQueryVisible(sq, ak) {
			return nil, errResult("saved query %q not found on this connection", id)
		}
		return sq, nil
	}
	all, err := deps.DB.GetSavedQueries()
	if err != nil {
		return nil, errResult("lookup failed: %v", err)
	}
	var matches []database.SavedQuery
	for _, sq := range all {
		if savedQueryVisible(&sq, ak) && strings.EqualFold(sq.Name, name) {
			matches = append(matches, sq)
		}
	}
	switch len(matches) {
	case 0:
		return nil, errResult("no saved query named %q on this connection; use list_saved_queries or search_catalog", name)
	case 1:
		return &matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		return nil, errResult("%d saved queries are named %q; call again with one of these ids: %s", len(matches), name, strings.Join(ids, ", "))
	}
}

func filterRows(rows []map[string]any, keep func(map[string]any) bool) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// num coerces ClickHouse JSON numbers (float64, or strings for UInt64) to float64.
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		var f float64
		fmt.Sscanf(x, "%g", &f)
		return f
	}
	return 0
}
