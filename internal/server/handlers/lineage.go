package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/lineage"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// LineageHandler serves the data lineage graph of the session's connection,
// read with the session user's ClickHouse account. Nothing runs until it is
// called; results are cached for 30 s per connection, user and databases.
type LineageHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config

	cacheOnce sync.Once
	cache     *lineageCache
}

const (
	lineageCacheTTL       = 30 * time.Second
	lineageCacheEntries   = 256
	lineageMaxObjects     = 50000
	lineageMaxDatabases   = 200
	lineageQueryTimeout   = 30 * time.Second
	lineageMaxNameLength  = 1024
	lineageDatabasesParam = "databases"
)

func (h *LineageHandler) Routes(r chi.Router) {
	r.Get("/", h.Graph)
	r.Get("/node", h.Node)
}

func (h *LineageHandler) graphCache() *lineageCache {
	h.cacheOnce.Do(func() {
		if h.cache == nil {
			h.cache = &lineageCache{entries: map[string]lineageEntry{}, now: time.Now}
		}
	})
	return h.cache
}

// credentials returns the session's ClickHouse access, or writes the error.
func (h *LineageHandler) credentials(w http.ResponseWriter, r *http.Request) (chSession, bool) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return chSession{}, false
	}
	if h.Gateway == nil || !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is offline")
		return chSession{}, false
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Lineage: failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return chSession{}, false
	}
	return chSession{connID: session.ConnectionID, user: session.ClickhouseUser, password: password}, true
}

// Graph handles GET /api/lineage?databases=a,b&refresh=1.
func (h *LineageHandler) Graph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if middleware.GetSession(r) == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	dbs, err := parseLineageDatabases(r.URL.Query().Get(lineageDatabasesParam))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cs, ok := h.credentials(w, r)
	if !ok {
		return
	}
	key := cs.connID + "\x00" + cs.user + "\x00" + strings.Join(dbs, "\x00")
	cache := h.graphCache()
	if r.URL.Query().Get("refresh") != "1" {
		if g, ok := cache.get(key); ok {
			g.Meta.Cached = true
			writeJSON(w, http.StatusOK, g)
			return
		}
	}
	g, err := h.load(r.Context(), cs, dbs)
	if err != nil {
		var tooLarge lineageTooLargeError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusUnprocessableEntity, tooLarge.Error())
			return
		}
		slog.Warn("Lineage: failed to read system.tables", "error", err, "connection", cs.connID)
		writeError(w, http.StatusBadGateway, "Unable to read system.tables with your ClickHouse account")
		return
	}
	cache.put(key, g)
	writeJSON(w, http.StatusOK, g)
}

// parseLineageDatabases splits the databases parameter into a sorted,
// de-duplicated list. The names are sent as a bound parameter, never as SQL.
func parseLineageDatabases(raw string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		if len(name) > lineageMaxNameLength || strings.IndexByte(name, 0) >= 0 {
			return nil, errors.New("Invalid database name")
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) > lineageMaxDatabases {
		return nil, errors.New("Too many databases; choose at most " + strconv.Itoa(lineageMaxDatabases))
	}
	sort.Strings(out)
	return out, nil
}

type lineageTooLargeError struct{}

func (lineageTooLargeError) Error() string {
	return "More than " + strconv.Itoa(lineageMaxObjects) + " objects are visible; choose databases to narrow the graph"
}

// lineageQuery runs one metadata query as the session user and decodes its rows.
func (h *LineageHandler) lineageQuery(ctx context.Context, cs chSession, sql string, params map[string]string, dst any) error {
	settings := map[string]string{
		"readonly":           "1",
		"max_execution_time": "25",
		"output_format_json_quote_64bit_integers": "0",
	}
	for k, v := range params {
		settings[k] = v
	}
	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(ctx, cs.connID, sql, cs.user, cs.password, settings, lineageQueryTimeout)
	if err != nil {
		return err
	}
	if result == nil || !bytes.HasPrefix(bytes.TrimSpace(result.Data), []byte("[")) {
		return errors.New("ClickHouse returned invalid metadata")
	}
	return json.Unmarshal(result.Data, dst)
}

// lineageTablesSQL selects every visible object. Definitions (ddl,
// as_select) are read for every view, MV and dictionary in all databases, so
// a reader in another database still links to a selected table. With
// databases, other rows skip engine_full unless their engine routes data or
// reads from outside. strict drops the other rows.
func lineageTablesSQL(filtered, strict, asSelect bool) string {
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
		" LIMIT " + strconv.Itoa(lineageMaxObjects+1)
}

func lineageArrayParam(values []string) string {
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

// load reads metadata and builds the graph. system.tables is required; the
// other system tables are optional and degrade to warnings.
func (h *LineageHandler) load(ctx context.Context, cs chSession, dbs []string) (lineage.Graph, error) {
	params := map[string]string{}
	if len(dbs) > 0 {
		params["param_databases"] = lineageArrayParam(dbs)
	}
	var (
		wg             sync.WaitGroup
		dicts          []lineage.DictionaryRow
		dictErr        error
		refreshes      []lineage.RefreshRow
		refreshWarning string
		host           string
		multiNode      bool
		hostWarning    string
		tables         []lineage.TableRow
		tablesErr      error
		tablesWarnings []string
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		tables, tablesWarnings, tablesErr = h.loadTables(ctx, cs, dbs, params)
	}()
	go func() {
		defer wg.Done()
		sql := "SELECT database, name, source, toString(status) AS status FROM system.dictionaries"
		if len(dbs) > 0 {
			sql += " WHERE has({databases:Array(String)}, database)"
		}
		dictErr = h.lineageQuery(ctx, cs, sql+" LIMIT "+strconv.Itoa(lineageMaxObjects), params, &dicts)
	}()
	go func() {
		defer wg.Done()
		refreshes, refreshWarning = h.loadRefreshes(ctx, cs)
	}()
	go func() {
		defer wg.Done()
		host, multiNode, hostWarning = h.loadHost(ctx, cs)
	}()
	wg.Wait()
	if tablesErr != nil {
		return lineage.Graph{}, tablesErr
	}
	if dictErr != nil {
		dicts = nil
	}

	g := lineage.Build(lineage.Input{Tables: tables, Dictionaries: dicts, Refreshes: refreshes, Databases: dbs})
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

func (h *LineageHandler) loadTables(ctx context.Context, cs chSession, dbs []string, params map[string]string) ([]lineage.TableRow, []string, error) {
	filtered := len(dbs) > 0
	var warnings []string
	asSelect := true
	run := func(strict bool) ([]lineage.TableRow, error) {
		var rows []lineage.TableRow
		err := h.lineageQuery(ctx, cs, lineageTablesSQL(filtered, strict, asSelect), params, &rows)
		if err != nil && asSelect && isUnknownColumnError(err) && strings.Contains(err.Error(), "as_select") {
			// Servers before as_select: definitions are parsed from the DDL.
			asSelect = false
			rows = nil
			err = h.lineageQuery(ctx, cs, lineageTablesSQL(filtered, strict, asSelect), params, &rows)
		}
		return rows, err
	}
	rows, err := run(false)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > lineageMaxObjects {
		if !filtered {
			return nil, nil, lineageTooLargeError{}
		}
		if rows, err = run(true); err != nil {
			return nil, nil, err
		}
		if len(rows) > lineageMaxObjects {
			return nil, nil, lineageTooLargeError{}
		}
		warnings = append(warnings, "More than "+strconv.Itoa(lineageMaxObjects)+" objects are visible: objects outside the selected databases are not read, so references to them show as missing")
	}
	return rows, warnings, nil
}

func (h *LineageHandler) loadRefreshes(ctx context.Context, cs chSession) ([]lineage.RefreshRow, string) {
	var rows []lineage.RefreshRow
	err := h.lineageQuery(ctx, cs, "SELECT database, view, toString(status) AS status, toUnixTimestamp(last_success_time) AS last_success, toUnixTimestamp(next_refresh_time) AS next FROM system.view_refreshes LIMIT "+strconv.Itoa(lineageMaxObjects), nil, &rows)
	if err != nil && !isUnknownTableError(err) && isUnknownColumnError(err) {
		// Older column names: keep the status, drop the times.
		rows = nil
		err = h.lineageQuery(ctx, cs, "SELECT database, view, toString(status) AS status FROM system.view_refreshes LIMIT "+strconv.Itoa(lineageMaxObjects), nil, &rows)
	}
	if err != nil {
		if isUnknownTableError(err) {
			return nil, "" // no refreshable MVs on this server version
		}
		return nil, "system.view_refreshes not readable: refresh status unavailable"
	}
	return rows, ""
}

func (h *LineageHandler) loadHost(ctx context.Context, cs chSession) (string, bool, string) {
	var rows []struct {
		Host  string        `json:"host"`
		Hosts lineage.Count `json:"hosts"`
	}
	err := h.lineageQuery(ctx, cs, "SELECT hostName() AS host, (SELECT uniqExact(host_name) FROM system.clusters) AS hosts", nil, &rows)
	if err == nil && len(rows) == 1 {
		return rows[0].Host, rows[0].Hosts.V > 1, ""
	}
	rows = nil
	if err := h.lineageQuery(ctx, cs, "SELECT hostName() AS host", nil, &rows); err == nil && len(rows) == 1 {
		return rows[0].Host, false, "system.clusters not readable: cannot tell whether this server is one node of a cluster"
	}
	return "", false, "Unable to read the server host name"
}

// Node handles GET /api/lineage/node?id=db.name and returns one object with
// its definition. database and name may be given instead of id when a
// database name contains a dot.
func (h *LineageHandler) Node(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if middleware.GetSession(r) == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	q := r.URL.Query()
	db, name := q.Get("database"), q.Get("name")
	if db == "" && name == "" {
		db, name, _ = strings.Cut(q.Get("id"), ".")
	}
	if db == "" || name == "" || len(db) > lineageMaxNameLength || len(name) > lineageMaxNameLength || strings.IndexByte(db+name, 0) >= 0 {
		writeError(w, http.StatusBadRequest, "id must be database.name")
		return
	}
	cs, ok := h.credentials(w, r)
	if !ok {
		return
	}
	var rows []lineage.TableRow
	sql := "SELECT database, name, toString(uuid) AS uuid, engine, engine_full, create_table_query AS ddl, total_rows, total_bytes " +
		"FROM system.tables WHERE database = {database:String} AND (name = {name:String} OR name = concat('.inner.', {name:String}) " +
		"OR name IN (SELECT concat('.inner_id.', toString(uuid)) FROM system.tables WHERE database = {database:String} AND name = {name:String})) LIMIT 3"
	err := h.lineageQuery(r.Context(), cs, sql, map[string]string{"param_database": db, "param_name": name}, &rows)
	if err != nil {
		slog.Warn("Lineage: failed to read object", "error", err, "connection", cs.connID)
		writeError(w, http.StatusBadGateway, "Unable to read system.tables with your ClickHouse account")
		return
	}
	var main *lineage.TableRow
	var inner *lineage.TableRow
	for i := range rows {
		if rows[i].Name == name {
			main = &rows[i]
		} else {
			inner = &rows[i]
		}
	}
	if main == nil {
		writeError(w, http.StatusNotFound, "Object not found or not visible to your ClickHouse account")
		return
	}
	kind := lineage.Classify(*main, false)
	count, size := main.TotalRows, main.TotalBytes
	if inner != nil && (kind == lineage.KindMV || kind == lineage.KindRefreshableMV) {
		count, size = inner.TotalRows, inner.TotalBytes
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          main.Database + "." + main.Name,
		"database":    main.Database,
		"name":        main.Name,
		"kind":        kind,
		"engine":      main.Engine,
		"engine_full": main.EngineFull,
		"ddl":         main.DDL,
		"rows":        count,
		"bytes":       size,
	})
}

// lineageCache keeps built graphs for lineageCacheTTL. Entries are never
// mutated after put; a hit returns a copy whose Meta the caller may change.
type lineageCache struct {
	mu      sync.Mutex
	entries map[string]lineageEntry
	now     func() time.Time
}

type lineageEntry struct {
	graph   lineage.Graph
	expires time.Time
}

func (c *lineageCache) get(key string) (lineage.Graph, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		return lineage.Graph{}, false
	}
	return e.graph, true
}

func (c *lineageCache) put(key string, g lineage.Graph) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.entries) >= lineageCacheEntries {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= lineageCacheEntries {
			clear(c.entries)
		}
	}
	c.entries[key] = lineageEntry{graph: g, expires: now.Add(lineageCacheTTL)}
}
