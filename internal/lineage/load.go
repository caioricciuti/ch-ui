package lineage

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/safe"
)

// MaxObjects bounds the system.tables rows one graph reads.
const MaxObjects = 50000

// Query runs one metadata query with the caller's ClickHouse account and
// decodes its JSON rows into dst. params carries param_* bindings.
type Query func(ctx context.Context, sql string, params map[string]string, dst any) error

// TooLargeError reports more visible objects than MaxObjects.
type TooLargeError struct{}

func (TooLargeError) Error() string {
	return "More than " + strconv.Itoa(MaxObjects) + " objects are visible; choose databases to narrow the graph"
}

// TablesSQL selects every visible object. Definitions (ddl, as_select) are
// read for every view, MV and dictionary in all databases, so a reader in
// another database still links to a selected table. With databases, other
// rows skip engine_full unless their engine routes data or reads from
// outside. strict drops the other rows.
func TablesSQL(filtered, strict, asSelect bool) string {
	engineFull := "engine_full"
	if filtered && !strict {
		engineFull = "if(has({databases:Array(String)}, database) OR NOT endsWith(engine, 'MergeTree'), engine_full, '')"
	}
	// as_select is empty for every engine but View and MaterializedView.
	asSelectColumn := "''"
	if asSelect {
		asSelectColumn = "as_select"
	}
	ddl := "if(engine IN ('View', 'MaterializedView', 'Dictionary'), create_table_query, '')"
	where := ""
	if filtered && strict {
		where = " AND has({databases:Array(String)}, database)"
	}
	return "SELECT database, name, toString(uuid) AS uuid, engine, " +
		engineFull + " AS engine_full, " +
		ddl + " AS ddl, " +
		asSelectColumn + " AS as_select, " +
		"dependencies_database, dependencies_table, total_rows, total_bytes " +
		"FROM system.tables " +
		"WHERE is_temporary = 0 AND database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')" + where +
		" LIMIT " + strconv.Itoa(MaxObjects+1)
}

// ArrayParam renders names as a ClickHouse Array(String) parameter value.
func ArrayParam(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = safe.QuotedLiteral(v)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// isUnknownTableError reports a system table this server version lacks.
func isUnknownTableError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "UNKNOWN_TABLE") || strings.Contains(err.Error(), "Code: 60."))
}

// isUnknownColumnError reports a column this server version lacks.
func isUnknownColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range []string{"UNKNOWN_IDENTIFIER", "Missing columns", "Unknown expression identifier", "There is no column", "Code: 47."} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// Load reads metadata with q and builds the graph of dbs (every database
// when empty). system.tables is required; the other system tables are
// optional and degrade to warnings. A TooLargeError means dbs must narrow.
func Load(ctx context.Context, q Query, dbs []string) (Graph, error) {
	params := map[string]string{}
	if len(dbs) > 0 {
		params["param_databases"] = ArrayParam(dbs)
	}
	var (
		wg             sync.WaitGroup
		dicts          []DictionaryRow
		dictErr        error
		refreshes      []RefreshRow
		refreshWarning string
		host           string
		multiNode      bool
		hostWarning    string
		tables         []TableRow
		tablesErr      error
		tablesWarnings []string
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		tables, tablesWarnings, tablesErr = loadTables(ctx, q, dbs, params)
	}()
	go func() {
		defer wg.Done()
		sql := "SELECT database, name, source, toString(status) AS status FROM system.dictionaries"
		if len(dbs) > 0 {
			sql += " WHERE has({databases:Array(String)}, database)"
		}
		dictErr = q(ctx, sql+" LIMIT "+strconv.Itoa(MaxObjects), params, &dicts)
	}()
	go func() {
		defer wg.Done()
		refreshes, refreshWarning = loadRefreshes(ctx, q)
	}()
	go func() {
		defer wg.Done()
		host, multiNode, hostWarning = loadHost(ctx, q)
	}()
	wg.Wait()
	if tablesErr != nil {
		return Graph{}, tablesErr
	}
	if dictErr != nil {
		dicts = nil
	}

	g := Build(Input{Tables: tables, Dictionaries: dicts, Refreshes: refreshes, Databases: dbs})
	g.Meta.Host = host
	g.Meta.MultiNode = multiNode
	g.Meta.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	g.Meta.Warnings = append(g.Meta.Warnings, tablesWarnings...)
	if dictErr != nil {
		for _, t := range tables {
			if t.Engine == "Dictionary" {
				g.Meta.Warnings = append(g.Meta.Warnings, "system.dictionaries not readable: dictionary sources come from their definitions only")
				break
			}
		}
	}
	if refreshWarning != "" {
		g.Meta.Warnings = append(g.Meta.Warnings, refreshWarning)
	}
	if hostWarning != "" {
		g.Meta.Warnings = append(g.Meta.Warnings, hostWarning)
	}
	return g, nil
}

func loadTables(ctx context.Context, q Query, dbs []string, params map[string]string) ([]TableRow, []string, error) {
	filtered := len(dbs) > 0
	var warnings []string
	asSelect := true
	run := func(strict bool) ([]TableRow, error) {
		var rows []TableRow
		err := q(ctx, TablesSQL(filtered, strict, asSelect), params, &rows)
		if err != nil && asSelect && isUnknownColumnError(err) && strings.Contains(err.Error(), "as_select") {
			// Servers before as_select: definitions are parsed from the DDL.
			asSelect = false
			rows = nil
			err = q(ctx, TablesSQL(filtered, strict, asSelect), params, &rows)
		}
		return rows, err
	}
	rows, err := run(false)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > MaxObjects {
		if !filtered {
			return nil, nil, TooLargeError{}
		}
		if rows, err = run(true); err != nil {
			return nil, nil, err
		}
		if len(rows) > MaxObjects {
			return nil, nil, TooLargeError{}
		}
		warnings = append(warnings, "More than "+strconv.Itoa(MaxObjects)+" objects are visible: objects outside the selected databases are not read, so references to them show as missing")
	}
	return rows, warnings, nil
}

func loadRefreshes(ctx context.Context, q Query) ([]RefreshRow, string) {
	var rows []RefreshRow
	err := q(ctx, "SELECT database, view, toString(status) AS status, toUnixTimestamp(last_success_time) AS last_success, toUnixTimestamp(next_refresh_time) AS next FROM system.view_refreshes LIMIT "+strconv.Itoa(MaxObjects), nil, &rows)
	if err != nil && !isUnknownTableError(err) && isUnknownColumnError(err) {
		// Older column names: keep the status, drop the times.
		rows = nil
		err = q(ctx, "SELECT database, view, toString(status) AS status FROM system.view_refreshes LIMIT "+strconv.Itoa(MaxObjects), nil, &rows)
	}
	if err != nil {
		if isUnknownTableError(err) {
			return nil, "" // no refreshable MVs on this server version
		}
		return nil, "system.view_refreshes not readable: refresh status unavailable"
	}
	return rows, ""
}

func loadHost(ctx context.Context, q Query) (string, bool, string) {
	var rows []struct {
		Host  string `json:"host"`
		Hosts Count  `json:"hosts"`
	}
	err := q(ctx, "SELECT hostName() AS host, (SELECT uniqExact(host_name) FROM system.clusters) AS hosts", nil, &rows)
	if err == nil && len(rows) == 1 {
		return rows[0].Host, rows[0].Hosts.V > 1, ""
	}
	rows = nil
	if err := q(ctx, "SELECT hostName() AS host", nil, &rows); err == nil && len(rows) == 1 {
		return rows[0].Host, false, "system.clusters not readable: cannot tell whether this server is one node of a cluster"
	}
	return "", false, "Unable to read the server host name"
}
