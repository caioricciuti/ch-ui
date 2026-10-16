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
	g, err := lineage.Load(r.Context(), h.queryAs(cs), dbs)
	if err != nil {
		var tooLarge lineage.TooLargeError
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

// queryAs binds lineageQuery to the session's ClickHouse account.
func (h *LineageHandler) queryAs(cs chSession) lineage.Query {
	return func(ctx context.Context, sql string, params map[string]string, dst any) error {
		return h.lineageQuery(ctx, cs, sql, params, dst)
	}
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
