package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TelemetryHandler serves observability sources and the logs explorer.
// Every query runs on the session's connection with the session's
// ClickHouse credentials; identifiers only ever come from verified source
// mappings and literals are escaped in internal/telemetry.
type TelemetryHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config

	cacheMu sync.Mutex
	schemas map[string]schemaEntry
}

type schemaEntry struct {
	columns map[string]string
	at      time.Time
}

const (
	schemaCacheTTL          = 5 * time.Minute
	telemetrySearchTimeout  = 30 * time.Second
	telemetrySummaryTimeout = 20 * time.Second
	telemetryMetaTimeout    = 15 * time.Second
)

// Routes mounts the telemetry API.
func (h *TelemetryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	writer := middleware.RequireWriter()

	r.Get("/sources", h.ListSources)
	r.With(writer).Post("/sources", h.CreateSource)
	r.With(writer).Post("/sources/detect", h.DetectSources)
	r.With(writer).Put("/sources/{id}", h.UpdateSource)
	r.With(writer).Delete("/sources/{id}", h.DeleteSource)
	r.Post("/sources/{id}/test", h.TestSource)

	r.Post("/logs/search", h.SearchLogs)
	r.Post("/logs/histogram", h.LogsHistogram)
	r.Post("/logs/facets", h.LogsFacets)
	r.Post("/logs/context", h.LogsContext)
	r.Get("/logs/by-trace/{traceId}", h.LogsByTrace)

	// Saved searches stay community: the Logs section itself uses them.
	r.Get("/saved-searches", h.ListSavedSearches)
	r.With(writer).Post("/saved-searches", h.CreateSavedSearch)
	r.With(writer).Put("/saved-searches/{id}", h.UpdateSavedSearch)
	r.With(writer).Delete("/saved-searches/{id}", h.DeleteSavedSearch)

	// Traces, metrics, the service map and monitors are Pro, matching the
	// other Operate depth (Cluster Health, Query Insights, Cost Center,
	// Governance). Logs and sources are the community on-ramp.
	r.Group(func(pro chi.Router) {
		pro.Use(middleware.RequirePro(h.Config))

		// Phase 2: traces (telemetry_traces.go)
		pro.Post("/traces/search", h.SearchTraces)
		pro.Post("/traces/histogram", h.TracesHistogram)
		pro.Post("/traces/facets", h.TracesFacets)
		pro.Get("/traces/{traceId}", h.GetTrace)

		// Phase 3: metrics (telemetry_metrics.go)
		pro.Get("/metrics/catalog", h.MetricsCatalog)
		pro.Post("/metrics/query", h.MetricsQuery)
		pro.Get("/metrics/attributes", h.MetricsAttributes)

		// Phase 4: monitors (telemetry_saved.go)
		pro.Get("/monitors", h.ListMonitors)
		pro.With(writer).Post("/monitors", h.CreateMonitor)
		pro.With(writer).Put("/monitors/{id}", h.UpdateMonitor)
		pro.With(writer).Delete("/monitors/{id}", h.DeleteMonitor)
		pro.With(writer).Post("/monitors/{id}/run", h.RunMonitor)

		// Phase 4: service map (telemetry_servicemap.go)
		pro.Post("/service-map", h.ServiceMap)
	})

	// The single-table config of the first Telemetry page is gone; the old
	// client must fail loudly rather than render an empty explorer.
	gone := func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusGone, "Telemetry config moved to /api/telemetry/sources")
	}
	r.Get("/config", gone)
	r.Put("/config", gone)
	return r
}

// ── Query plumbing ─────────────────────────────────────────────────

type chCreds struct {
	connID, user, password string
}

func (h *TelemetryHandler) creds(r *http.Request) (chCreds, *middleware.SessionInfo, error) {
	session := middleware.GetSession(r)
	if session == nil {
		return chCreds{}, nil, fmt.Errorf("not authenticated")
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		return chCreds{}, nil, fmt.Errorf("failed to decrypt credentials")
	}
	return chCreds{connID: session.ConnectionID, user: session.ClickhouseUser, password: password}, session, nil
}

func (h *TelemetryHandler) exec(c chCreds, sqlText string, timeout time.Duration) ([]map[string]interface{}, error) {
	result, err := h.Gateway.ExecuteQuery(c.connID, sqlText, c.user, c.password, timeout)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return decodeRows(result.Data), nil
}

// describe returns name -> type for a table.
func (h *TelemetryHandler) describe(c chCreds, db, table string) (map[string]string, error) {
	if !telemetry.ValidIdent(db) || !telemetry.ValidIdent(table) {
		return nil, fmt.Errorf("invalid table name")
	}
	rows, err := h.exec(c, fmt.Sprintf("DESCRIBE TABLE %s.%s", telemetry.Quote(db), telemetry.Quote(table)), telemetryMetaTimeout)
	if err != nil {
		return nil, err
	}
	cols := make(map[string]string, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		typ, _ := row["type"].(string)
		if name != "" {
			cols[name] = typ
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s.%s has no columns or does not exist", db, table)
	}
	return cols, nil
}

// columnsFor returns the cached DESCRIBE for a source's table.
func (h *TelemetryHandler) columnsFor(c chCreds, src *telemetry.Source) (map[string]string, error) {
	h.cacheMu.Lock()
	if h.schemas == nil {
		h.schemas = map[string]schemaEntry{}
	}
	e, ok := h.schemas[src.ID]
	h.cacheMu.Unlock()
	if ok && time.Since(e.at) < schemaCacheTTL {
		return e.columns, nil
	}
	cols, err := h.describe(c, src.Database, src.Table)
	if err != nil {
		return nil, err
	}
	h.cacheMu.Lock()
	h.schemas[src.ID] = schemaEntry{columns: cols, at: time.Now()}
	h.cacheMu.Unlock()
	return cols, nil
}

func (h *TelemetryHandler) invalidate(id string) {
	h.cacheMu.Lock()
	delete(h.schemas, id)
	h.cacheMu.Unlock()
}

// missingColumns lists mapped columns absent from the table.
func missingColumns(src *telemetry.Source, cols map[string]string) []string {
	var missing []string
	for _, c := range src.RequiredColumns() {
		if _, ok := cols[c]; !ok {
			missing = append(missing, c)
		}
	}
	return missing
}

// verifySource runs DESCRIBE for every table of a source and returns the
// missing mapped columns (metrics: missing tables).
func (h *TelemetryHandler) verifySource(c chCreds, src *telemetry.Source) (map[string]string, []string, error) {
	if src.Kind == telemetry.KindMetrics {
		var missing []string
		for _, t := range []string{src.Tables.Gauge, src.Tables.Sum, src.Tables.Histogram, src.Tables.ExponentialHistogram, src.Tables.Summary} {
			if t == "" {
				continue
			}
			if _, err := h.describe(c, src.Database, t); err != nil {
				missing = append(missing, t)
			}
		}
		return nil, missing, nil
	}
	cols, err := h.describe(c, src.Database, src.Table)
	if err != nil {
		return nil, nil, err
	}
	return cols, missingColumns(src, cols), nil
}

// loadSource fetches a source and checks it belongs to the session.
func (h *TelemetryHandler) loadSource(w http.ResponseWriter, id, connID string) *telemetry.Source {
	src, err := h.DB.GetTelemetrySource(id)
	if err != nil {
		slog.Error("Failed to load telemetry source", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to load source")
		return nil
	}
	if src == nil || src.ConnectionID != connID {
		writeError(w, http.StatusNotFound, "Source not found")
		return nil
	}
	return src
}

func (h *TelemetryHandler) audit(action string, session *middleware.SessionInfo, details string) {
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       action,
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(details),
	})
}

// ── Sources ────────────────────────────────────────────────────────

// ListSources returns the sources of the session connection.
func (h *TelemetryHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	sources, err := h.DB.ListTelemetrySources(session.ConnectionID)
	if err != nil {
		slog.Error("Failed to list telemetry sources", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "sources": sources})
}

func decodeSource(r *http.Request) (*telemetry.Source, error) {
	var src telemetry.Source
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		return nil, fmt.Errorf("Invalid JSON body")
	}
	src.Name = strings.TrimSpace(src.Name)
	src.Database = strings.TrimSpace(src.Database)
	src.Table = strings.TrimSpace(src.Table)
	if src.Kind == telemetry.KindMetrics && src.Table == "" {
		src.Table = "" // metrics use Tables
	}
	return &src, nil
}

// writeVerification answers a create/update whose mapping does not match
// the table.
func writeVerification(w http.ResponseWriter, missing []string, kind telemetry.Kind) {
	what := "columns"
	if kind == telemetry.KindMetrics {
		what = "tables"
	}
	writeJSON(w, http.StatusBadRequest, map[string]interface{}{
		"success": false,
		"error":   fmt.Sprintf("Missing %s: %s", what, strings.Join(missing, ", ")),
		"missing": missing,
	})
}

// CreateSource validates, verifies against ClickHouse, and stores a source.
func (h *TelemetryHandler) CreateSource(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	src, err := decodeSource(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	src.ConnectionID = session.ConnectionID
	src.CreatedBy = session.ClickhouseUser
	if err := src.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, missing, err := h.verifySource(c, src)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(missing) > 0 {
		writeVerification(w, missing, src.Kind)
		return
	}
	id, err := h.DB.CreateTelemetrySource(src)
	if err != nil {
		slog.Error("Failed to create telemetry source", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create source")
		return
	}
	h.audit("telemetry.source.created", session, fmt.Sprintf("%s %s (%s.%s)", src.Kind, src.Name, src.Database, src.Table))
	saved, _ := h.DB.GetTelemetrySource(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "source": saved})
}

// UpdateSource replaces a source's editable fields.
func (h *TelemetryHandler) UpdateSource(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id := chi.URLParam(r, "id")
	existing := h.loadSource(w, id, session.ConnectionID)
	if existing == nil {
		return
	}
	src, err := decodeSource(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	src.ID = existing.ID
	src.ConnectionID = existing.ConnectionID
	src.CreatedBy = existing.CreatedBy
	if err := src.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, missing, err := h.verifySource(c, src)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(missing) > 0 {
		writeVerification(w, missing, src.Kind)
		return
	}
	if err := h.DB.UpdateTelemetrySource(src); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "Source not found")
			return
		}
		slog.Error("Failed to update telemetry source", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update source")
		return
	}
	h.invalidate(id)
	h.audit("telemetry.source.updated", session, fmt.Sprintf("%s %s (%s.%s)", src.Kind, src.Name, src.Database, src.Table))
	saved, _ := h.DB.GetTelemetrySource(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "source": saved})
}

// DeleteSource removes a source.
func (h *TelemetryHandler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing := h.loadSource(w, id, session.ConnectionID)
	if existing == nil {
		return
	}
	if err := h.DB.DeleteTelemetrySource(id); err != nil {
		slog.Error("Failed to delete telemetry source", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete source")
		return
	}
	h.invalidate(id)
	h.audit("telemetry.source.deleted", session, fmt.Sprintf("%s %s", existing.Kind, existing.Name))
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// DetectSources scans the connection for OpenTelemetry-shaped tables and
// proposes sources. Nothing is saved.
func (h *TelemetryHandler) DetectSources(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rows, err := h.exec(c, `SELECT database, name FROM system.tables WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') AND (name LIKE 'otel%' OR name LIKE '%logs%' OR name LIKE '%traces%' OR name LIKE '%spans%' OR name LIKE '%metrics%') ORDER BY database, name`, telemetryMetaTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	proposals := make([]telemetry.Source, 0)
	metricsByDB := map[string]*telemetry.MetricsTables{}
	var dbOrder []string
	for _, row := range rows {
		db, _ := row["database"].(string)
		name, _ := row["name"].(string)
		if !telemetry.ValidIdent(db) || !telemetry.ValidIdent(name) {
			continue
		}
		lower := strings.ToLower(name)
		// Metric tables are recognised by the exporter's suffixes.
		if strings.Contains(lower, "metrics") {
			mt := metricsByDB[db]
			if mt == nil {
				mt = &telemetry.MetricsTables{}
				metricsByDB[db] = mt
				dbOrder = append(dbOrder, db)
			}
			switch {
			case strings.HasSuffix(lower, "_exponential_histogram"):
				mt.ExponentialHistogram = name
			case strings.HasSuffix(lower, "_histogram"):
				mt.Histogram = name
			case strings.HasSuffix(lower, "_gauge"):
				mt.Gauge = name
			case strings.HasSuffix(lower, "_sum"):
				mt.Sum = name
			case strings.HasSuffix(lower, "_summary"):
				mt.Summary = name
			}
			continue
		}
		cols, err := h.describe(c, db, name)
		if err != nil {
			continue
		}
		has := func(names ...string) bool {
			for _, n := range names {
				if _, ok := cols[n]; !ok {
					return false
				}
			}
			return true
		}
		switch {
		case has("SpanId", "ParentSpanId", "Duration", "Timestamp"):
			m := telemetry.DefaultTracesMapping()
			src := telemetry.Source{Kind: telemetry.KindTraces, Name: fmt.Sprintf("Traces (%s.%s)", db, name), Database: db, Table: name, Traces: &m, Enabled: true}
			trimMissing(&src, cols)
			proposals = append(proposals, src)
		case has("Body", "Timestamp", "SeverityText"):
			m := telemetry.DefaultLogsMapping()
			src := telemetry.Source{Kind: telemetry.KindLogs, Name: fmt.Sprintf("Logs (%s.%s)", db, name), Database: db, Table: name, Logs: &m, Enabled: true}
			trimMissing(&src, cols)
			proposals = append(proposals, src)
		}
	}
	for _, db := range dbOrder {
		mt := metricsByDB[db]
		if mt.Gauge == "" && mt.Sum == "" && mt.Histogram == "" && mt.ExponentialHistogram == "" && mt.Summary == "" {
			continue
		}
		proposals = append(proposals, telemetry.Source{Kind: telemetry.KindMetrics, Name: fmt.Sprintf("Metrics (%s)", db), Database: db, Tables: mt, Enabled: true})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "proposals": proposals})
}

// trimMissing blanks optional mapped columns the table does not have, so a
// proposal is immediately valid.
func trimMissing(src *telemetry.Source, cols map[string]string) {
	blank := func(p *string) {
		if *p != "" {
			if _, ok := cols[*p]; !ok {
				*p = ""
			}
		}
	}
	if src.Logs != nil {
		m := src.Logs
		for _, p := range []*string{&m.SeverityNumber, &m.TraceID, &m.SpanID, &m.ResourceAttributes, &m.ScopeAttributes, &m.LogAttributes, &m.ScopeName, &m.EventName} {
			blank(p)
		}
	}
	if src.Traces != nil {
		m := src.Traces
		for _, p := range []*string{&m.SpanKind, &m.StatusCode, &m.StatusMessage, &m.ResourceAttributes, &m.SpanAttributes} {
			blank(p)
		}
		// Events and Links name a Nested prefix, not a column: DESCRIBE
		// reports Events.Name, so blank() would drop a perfectly good
		// mapping. Keep the prefix when any sub-column is there.
		if !telemetry.HasNestedPrefix(cols, m.Events) {
			m.Events = ""
		}
		if !telemetry.HasNestedPrefix(cols, m.Links) {
			m.Links = ""
		}
	}
}

// TestSource checks a stored source against ClickHouse.
func (h *TelemetryHandler) TestSource(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	src := h.loadSource(w, chi.URLParam(r, "id"), session.ConnectionID)
	if src == nil {
		return
	}
	out := map[string]interface{}{"success": true, "ok": false, "columns": map[string]string{}, "missing": []string{}, "row_count_1h": 0, "latest": nil}
	cols, missing, err := h.verifySource(c, src)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if cols != nil {
		out["columns"] = cols
	}
	if missing == nil {
		missing = []string{}
	}
	out["missing"] = missing
	if len(missing) > 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var tsCol, table string
	switch src.Kind {
	case telemetry.KindLogs:
		tsCol, table = src.Logs.Timestamp, src.Table
	case telemetry.KindTraces:
		tsCol, table = src.Traces.Timestamp, src.Table
	case telemetry.KindMetrics:
		tsCol = "TimeUnix"
		for _, t := range []string{src.Tables.Gauge, src.Tables.Sum, src.Tables.Histogram, src.Tables.ExponentialHistogram, src.Tables.Summary} {
			if t != "" {
				table = t
				break
			}
		}
	}
	if tsCol != "" && table != "" {
		q := fmt.Sprintf("SELECT count() AS c, formatDateTime(max(%s), '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS latest FROM %s.%s WHERE %s >= now() - INTERVAL 1 HOUR %s",
			telemetry.Quote(tsCol), telemetry.Quote(src.Database), telemetry.Quote(table), telemetry.Quote(tsCol), telemetry.QuerySettings)
		rows, err := h.exec(c, q, telemetrySummaryTimeout)
		if err != nil {
			out["error"] = err.Error()
			writeJSON(w, http.StatusOK, out)
			return
		}
		if len(rows) > 0 {
			out["row_count_1h"] = toInt64(rows[0]["c"])
			if l, ok := rows[0]["latest"].(string); ok && toInt64(rows[0]["c"]) > 0 {
				out["latest"] = l
			}
		}
	}
	out["ok"] = true
	writeJSON(w, http.StatusOK, out)
}

// ── Logs ───────────────────────────────────────────────────────────

type logsRequest struct {
	SourceID  string   `json:"source_id"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Q         string   `json:"q"`
	Severity  []string `json:"severity"`
	Services  []string `json:"services"`
	Limit     int      `json:"limit"`
	Order     string   `json:"order"`
	Cursor    string   `json:"cursor"`
	Keys      []string `json:"keys"`
	Timestamp string   `json:"timestamp_ns"`
	Service   string   `json:"service"`
	Before    int      `json:"before"`
	After     int      `json:"after"`
}

func (r logsRequest) params() telemetry.LogsParams {
	return telemetry.LogsParams{From: r.From, To: r.To, Q: r.Q, Severity: r.Severity, Services: r.Services}
}

// logsSetup decodes the body, loads the source and its columns.
func (h *TelemetryHandler) logsSetup(w http.ResponseWriter, r *http.Request, requireRange bool) (chCreds, *telemetry.Source, map[string]string, logsRequest, bool) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	var body logsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	if body.SourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	if requireRange && (body.From == "" || body.To == "") {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	src := h.loadSource(w, body.SourceID, session.ConnectionID)
	if src == nil {
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	if src.Kind != telemetry.KindLogs || src.Logs == nil {
		writeError(w, http.StatusBadRequest, "Source is not a logs source")
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	cols, err := h.columnsFor(c, src)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return chCreds{}, nil, nil, logsRequest{}, false
	}
	return c, src, cols, body, true
}

// shapeLogRows normalizes gateway rows into the contract row shape.
func shapeLogRows(rows []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]interface{}{
			"timestamp":       str(row["timestamp"]),
			"timestamp_ns":    str(row["timestamp_ns"]),
			"severity":        str(row["severity"]),
			"severity_number": toInt64(row["severity_number"]),
			"service":         str(row["service"]),
			"body":            str(row["body"]),
			"trace_id":        str(row["trace_id"]),
			"span_id":         str(row["span_id"]),
			"resource":        stringMap(row["resource"]),
			"scope":           stringMap(row["scope"]),
			"attributes":      stringMap(row["attributes"]),
			"scope_name":      str(row["scope_name"]),
			"event_name":      str(row["event_name"]),
		})
	}
	return out
}

func str(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return fmt.Sprintf("%.0f", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// stringMap accepts a JSON object (Map column or the exporter's JSON
// column) or a JSON string of one. Nested objects, which is how a JSON
// column stores dotted OTel keys, are flattened back to dotted keys.
func stringMap(v interface{}) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]interface{}:
		flattenInto(out, "", t)
	case string:
		var m map[string]interface{}
		if json.Unmarshal([]byte(t), &m) == nil {
			flattenInto(out, "", m)
		}
	}
	return out
}

func flattenInto(out map[string]string, prefix string, m map[string]interface{}) {
	for k, val := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if nested, ok := val.(map[string]interface{}); ok {
			flattenInto(out, key, nested)
			continue
		}
		out[key] = str(val)
	}
}

// SearchLogs runs the explorer query with keyset paging.
func (h *TelemetryHandler) SearchLogs(w http.ResponseWriter, r *http.Request) {
	c, src, cols, body, ok := h.logsSetup(w, r, true)
	if !ok {
		return
	}
	limit := body.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	var cursor *telemetry.Cursor
	if body.Cursor != "" {
		cur, err := telemetry.DecodeCursor(body.Cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &cur
	}
	sqlText, err := telemetry.SearchSQL(src, cols, body.params(), body.Order, limit+1, cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	rows, err := h.exec(c, sqlText, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry log search failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var next interface{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = telemetry.EncodeCursor(telemetry.Cursor{TimestampNs: str(last["timestamp_ns"]), TraceID: str(last["trace_id"]), SpanID: str(last["span_id"])})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"rows":        shapeLogRows(rows),
		"next_cursor": next,
		"took_ms":     time.Since(started).Milliseconds(),
	})
}

// LogsHistogram returns severity-stacked counts per time bucket.
func (h *TelemetryHandler) LogsHistogram(w http.ResponseWriter, r *http.Request) {
	c, src, cols, body, ok := h.logsSetup(w, r, true)
	if !ok {
		return
	}
	bucket := telemetry.BucketSeconds(body.From, body.To)
	sqlText, err := telemetry.HistogramSQL(src, cols, body.params(), bucket)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
	if err != nil {
		slog.Warn("Telemetry histogram failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	type bucketOut struct {
		T      string           `json:"t"`
		Counts map[string]int64 `json:"counts"`
	}
	var order []string
	byT := map[string]*bucketOut{}
	for _, row := range rows {
		t := str(row["t"])
		b := byT[t]
		if b == nil {
			b = &bucketOut{T: t, Counts: map[string]int64{}}
			byT[t] = b
			order = append(order, t)
		}
		sev := str(row["severity"])
		if sev == "" {
			sev = "UNKNOWN"
		}
		b.Counts[sev] += toInt64(row["c"])
	}
	buckets := make([]bucketOut, 0, len(order))
	for _, t := range order {
		buckets = append(buckets, *byT[t])
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "bucket_seconds": bucket, "buckets": buckets})
}

// LogsFacets returns severity, service and attribute distributions.
func (h *TelemetryHandler) LogsFacets(w http.ResponseWriter, r *http.Request) {
	c, src, cols, body, ok := h.logsSetup(w, r, true)
	if !ok {
		return
	}
	queries, err := telemetry.BuildFacetsSQL(src, cols, body.params(), body.Keys)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	type facet struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	valueFacets := func(sqlText string) []facet {
		out := []facet{}
		if sqlText == "" {
			return out
		}
		rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
		if err != nil {
			slog.Warn("Telemetry facet query failed", "error", err)
			return out
		}
		for _, row := range rows {
			out = append(out, facet{Value: str(row["value"]), Count: toInt64(row["c"])})
		}
		return out
	}
	type keyFacet struct {
		Key    string `json:"key"`
		Count  int64  `json:"count"`
		Source string `json:"source"`
	}
	keys := []keyFacet{}
	if queries.Keys != "" {
		rows, err := h.exec(c, queries.Keys, telemetrySummaryTimeout)
		if err != nil {
			slog.Warn("Telemetry facet keys failed", "error", err)
		}
		for _, row := range rows {
			keys = append(keys, keyFacet{Key: str(row["k"]), Count: toInt64(row["c"]), Source: str(row["src"])})
		}
	}
	values := map[string][]facet{}
	names := make([]string, 0, len(queries.Values))
	for k := range queries.Values {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		values[k] = valueFacets(queries.Values[k])
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"severity":       valueFacets(queries.Severity),
		"services":       valueFacets(queries.Services),
		"attribute_keys": keys,
		"attributes":     values,
	})
}

// LogsContext returns rows around an anchor row, same service.
func (h *TelemetryHandler) LogsContext(w http.ResponseWriter, r *http.Request) {
	c, src, _, body, ok := h.logsSetup(w, r, false)
	if !ok {
		return
	}
	ts := strings.TrimSpace(body.Timestamp)
	if ts == "" || len(ts) > 20 {
		writeError(w, http.StatusBadRequest, "timestamp_ns is required")
		return
	}
	for _, ch := range ts {
		if ch < '0' || ch > '9' {
			writeError(w, http.StatusBadRequest, "timestamp_ns must be digits")
			return
		}
	}
	before, after := body.Before, body.After
	if before <= 0 || before > 500 {
		before = 50
	}
	if after <= 0 || after > 500 {
		after = 50
	}
	beforeSQL, afterSQL := telemetry.ContextSQL(src, ts, body.Service, before, after)
	beforeRows, err := h.exec(c, beforeSQL, telemetrySearchTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	afterRows, err := h.exec(c, afterSQL, telemetrySearchTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// before rows come newest-first; flip so the whole list is ascending.
	for i, j := 0, len(beforeRows)-1; i < j; i, j = i+1, j-1 {
		beforeRows[i], beforeRows[j] = beforeRows[j], beforeRows[i]
	}
	rows := append(beforeRows, afterRows...)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"rows":         shapeLogRows(rows),
		"anchor_index": len(beforeRows),
	})
}

// LogsByTrace lists every log line of a trace.
func (h *TelemetryHandler) LogsByTrace(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	traceID := chi.URLParam(r, "traceId")
	if traceID == "" || len(traceID) > 64 {
		writeError(w, http.StatusBadRequest, "trace id is required")
		return
	}
	src := h.loadSource(w, r.URL.Query().Get("source_id"), session.ConnectionID)
	if src == nil {
		return
	}
	if src.Kind != telemetry.KindLogs || src.Logs == nil {
		writeError(w, http.StatusBadRequest, "Source is not a logs source")
		return
	}
	sqlText, err := telemetry.ByTraceSQL(src, traceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySearchTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rows": shapeLogRows(rows)})
}
