package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// Receive setup: one call that makes everything a shipper needs to send
// OpenTelemetry logs or traces to CH-UI, so the Send data sheet does it in
// one step. The table is created with the caller's own ClickHouse account,
// the pipeline then writes with the connection's pipeline background account.

const (
	// receiveDDLTimeout bounds each CREATE of the receive table.
	receiveDDLTimeout = 30 * time.Second
	// receiveMaxTTLDays is the sink's bound on ttl_days (100 years).
	receiveMaxTTLDays = 36500
	receiveKeyName    = "Default key"
)

type receiveRequest struct {
	Signal   string `json:"signal"`
	Database string `json:"database"`
	Table    string `json:"table"`
	TTLDays  *int   `json:"ttl_days"`
}

// receivePipeline is the pipeline part of the receive answer.
type receivePipeline struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Signal string `json:"signal"`
}

// parse applies the defaults and validates the request. The identifiers go
// through telemetry.ValidIdent, the same check the source registered at the
// end makes, and are only ever quoted into SQL.
func (req *receiveRequest) parse() (ttl int, msg string) {
	req.Signal = strings.TrimSpace(req.Signal)
	req.Database = strings.TrimSpace(req.Database)
	req.Table = strings.TrimSpace(req.Table)
	switch req.Signal {
	case pipelines.SignalLogs, pipelines.SignalTraces:
	default:
		return 0, "Choose logs or traces as the signal to receive."
	}
	if req.Database == "" {
		req.Database = "default"
	}
	if req.Table == "" {
		req.Table = "otel_" + req.Signal
	}
	if !telemetry.ValidIdent(req.Database) {
		return 0, "The database name can only use letters, digits and underscores. Pick another name."
	}
	if !telemetry.ValidIdent(req.Table) {
		return 0, "The table name can only use letters, digits and underscores. Pick another name."
	}
	ttl = 30
	if req.TTLDays != nil {
		ttl = *req.TTLDays
	}
	if ttl < 0 || ttl > receiveMaxTTLDays {
		return 0, fmt.Sprintf("Keep data for 0 to %d days (0 keeps it forever).", receiveMaxTTLDays)
	}
	return ttl, ""
}

// receiveSource is the Telemetry source for the receive table, with the
// exporter's default mapping.
func receiveSource(signal, db, table, connID, actor string) *telemetry.Source {
	src := &telemetry.Source{ConnectionID: connID, Database: db, Table: table, Enabled: true, CreatedBy: actor}
	if signal == pipelines.SignalLogs {
		m := telemetry.DefaultLogsMapping()
		src.Kind, src.Name, src.Logs = telemetry.KindLogs, fmt.Sprintf("Logs (%s.%s)", db, table), &m
	} else {
		m := telemetry.DefaultTracesMapping()
		src.Kind, src.Name, src.Traces = telemetry.KindTraces, fmt.Sprintf("Traces (%s.%s)", db, table), &m
	}
	return src
}

// Receive sets up receiving OpenTelemetry logs or traces: it creates (or
// reuses) the table, an ingest pipeline into it, starts the pipeline, issues
// an ingest key and registers the Telemetry source. If a step after the
// pipeline exists fails, the pipeline and its key are removed again.
func (h *TelemetryHandler) Receive(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeCredsError(w, err)
		return
	}
	var req receiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	ttl, msg := req.parse()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	db, table, connID := req.Database, req.Table, session.ConnectionID
	actor := middleware.Actor(session)

	// Nothing is created when the pipeline could not run anyway.
	if h.Runner == nil || !h.Runner.Hub().Available() {
		slog.Error("Receive setup without an ingest spool", "connection", connID)
		writeError(w, http.StatusServiceUnavailable, "This server cannot receive data right now because its ingest spool is not available. Check the server logs and restart CH-UI.")
		return
	}
	bg, err := h.DB.GetBackgroundCredential(connID, "pipeline")
	if err != nil {
		slog.Error("Failed to load pipeline background account", "error", err, "connection", connID)
		writeError(w, http.StatusInternalServerError, "Could not check the pipeline background account. Try again.")
		return
	}
	if bg.Mode == "disabled" {
		writeError(w, http.StatusConflict, "Pipelines are turned off for background work on this connection. Turn them on in Admin, Connections, Background accounts, then try again.")
		return
	}

	src := receiveSource(req.Signal, db, table, connID, actor)
	tableCreated, ok := h.receiveTable(w, c, src, req.Signal, ttl)
	if !ok {
		return
	}

	name, err := h.receivePipelineName(connID, req.Signal)
	if err != nil {
		slog.Error("Failed to list pipelines", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not create the pipeline. Try again.")
		return
	}
	sourceJSON, _ := json.Marshal(map[string]interface{}{"signal": req.Signal, "ttl_days": ttl})
	sinkJSON, _ := json.Marshal(map[string]interface{}{"database": db, "table": table, "create_table": true})
	pipelineID, err := h.DB.CreatePipelineWithGraph(name, fmt.Sprintf("Receives OpenTelemetry %s into %s.%s.", req.Signal, db, table), connID, actor,
		func(id string) ([]database.PipelineNode, []database.PipelineEdge) {
			// The shape create_pipeline uses over MCP.
			sourceID, sinkID := id+"-source-1", id+"-sink-1"
			return []database.PipelineNode{
					{ID: sourceID, NodeType: "source_ingest", Label: "ingest source", PositionX: 80, PositionY: 160, ConfigEncrypted: string(sourceJSON)},
					{ID: sinkID, NodeType: "sink_clickhouse", Label: db + "." + table, PositionX: 480, PositionY: 160, ConfigEncrypted: string(sinkJSON)},
				}, []database.PipelineEdge{
					{ID: id + "-edge-1", SourceNodeID: sourceID, TargetNodeID: sinkID},
				}
		}, `{"x":0,"y":0,"zoom":1}`)
	if err != nil {
		slog.Error("Failed to create receive pipeline", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not create the pipeline. Try again.")
		return
	}

	// From here on a failure removes the pipeline (its keys go with it). A
	// table this call created stays: it is empty, harmless, and the next
	// attempt reuses it.
	started := false
	rollback := func() {
		if started {
			if err := h.Runner.StopPipeline(pipelineID); err != nil {
				slog.Warn("Failed to stop receive pipeline during rollback", "error", err, "pipeline", pipelineID)
			}
		}
		if err := h.DB.DeletePipeline(pipelineID); err != nil {
			slog.Error("Failed to delete receive pipeline during rollback", "error", err, "pipeline", pipelineID)
		}
		if err := h.Runner.DropSpool(pipelineID); err != nil {
			slog.Warn("Failed to drop receive pipeline spool during rollback", "error", err, "pipeline", pipelineID)
		}
	}

	start := h.startPipeline
	if start == nil {
		start = h.Runner.StartPipeline
	}
	if err := start(pipelineID); err != nil {
		slog.Error("Failed to start receive pipeline", "error", err, "pipeline", pipelineID)
		rollback()
		writeError(w, http.StatusInternalServerError, "The pipeline could not start, so nothing was kept. Try again, or check the server logs.")
		return
	}
	started = true

	newKey := h.newIngestKey
	if newKey == nil {
		newKey = ingest.GenerateKey
	}
	secret, hash, prefix, err := newKey()
	var key *database.IngestKey
	if err == nil {
		key, err = h.DB.CreateIngestKey(pipelineID, receiveKeyName, hash, prefix, actor)
	}
	if err != nil {
		slog.Error("Failed to create receive ingest key", "error", err, "pipeline", pipelineID)
		rollback()
		writeError(w, http.StatusInternalServerError, "Could not create an ingest key, so nothing was kept. Try again.")
		return
	}

	existing, err := h.existingSource(src)
	if err != nil {
		slog.Error("Failed to list telemetry sources", "error", err)
		rollback()
		writeError(w, http.StatusInternalServerError, "Could not register the Telemetry source, so nothing was kept. Try again.")
		return
	}
	saved := existing
	if saved == nil {
		id, err := h.DB.CreateTelemetrySource(src)
		if err == nil {
			saved, err = h.DB.GetTelemetrySource(id)
		}
		if err != nil || saved == nil {
			slog.Error("Failed to create telemetry source", "error", err)
			rollback()
			writeError(w, http.StatusInternalServerError, "Could not register the Telemetry source, so nothing was kept. Try again.")
			return
		}
	}

	pipeline, _ := h.DB.GetPipelineByID(pipelineID)
	status := "running"
	if pipeline != nil {
		status = pipeline.Status
	}
	h.auditReceive(r, session, pipelineID, name, key, saved, existing == nil, db, table)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"pipeline":      receivePipeline{ID: pipelineID, Name: name, Status: status, Signal: req.Signal},
		"key":           key,
		"secret":        secret,
		"source":        saved,
		"table_created": tableCreated,
		"database":      db,
		"table":         table,
	})
}

// receiveTable reuses the table when it has the exporter's shape, or creates
// it with the session's account. It reports whether this call created it;
// on failure it has written the response.
func (h *TelemetryHandler) receiveTable(w http.ResponseWriter, c chCreds, src *telemetry.Source, signal string, ttl int) (created, ok bool) {
	db, table := src.Database, src.Table
	describe, err := telemetry.DescribeSQL(db, table)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The table name can only use letters, digits and underscores. Pick another name.")
		return false, false
	}
	rows, err := h.exec(c, describe, telemetryMetaTimeout)
	switch code := clickhouseCode(err); {
	case err == nil:
		cols := telemetry.ColumnTypes(rows)
		trimMissing(src, cols)
		if missing := missingColumns(src, cols); len(missing) > 0 {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("%s.%s already exists but is not an OpenTelemetry %s table (missing %s). Pick another table name.", db, table, signal, strings.Join(missing, ", ")),
				"missing": missing,
			})
			return false, false
		}
		return false, true
	case code == 81: // UNKNOWN_DATABASE
		writeError(w, http.StatusBadRequest, fmt.Sprintf("The database %s does not exist. Create it first, or pick another database.", db))
		return false, false
	case code != 60: // anything but UNKNOWN_TABLE
		slog.Warn("Receive setup could not check the table", "error", err, "database", db, "table", table)
		status := clickhouseStatus(err)
		if status == http.StatusForbidden {
			writeError(w, status, fmt.Sprintf("Your ClickHouse user cannot read %s.%s. Ask an admin for SHOW COLUMNS on it, or pick another table.", db, table))
		} else {
			writeError(w, status, "Could not check the table in ClickHouse. Try again in a moment.")
		}
		return false, false
	}

	// For traces a failure after the spans table leaves it in place; the
	// statements are IF NOT EXISTS, so a retry finishes the job.
	for _, stmt := range pipelines.OTelTableStatements(signal, db, table, ttl) {
		if _, err := h.exec(c, stmt, receiveDDLTimeout); err != nil {
			slog.Warn("Receive setup could not create the table", "error", err, "database", db, "table", table)
			status := clickhouseStatus(err)
			if status == http.StatusForbidden {
				what := "CREATE TABLE"
				if signal == pipelines.SignalTraces {
					what = "CREATE TABLE and CREATE VIEW"
				}
				writeError(w, status, fmt.Sprintf("Your ClickHouse user cannot create tables in %s. Ask an admin for %s on it, or pick a database you can write to.", db, what))
			} else {
				writeError(w, status, fmt.Sprintf("ClickHouse could not create %s.%s. Check the server logs, or pick another table name.", db, table))
			}
			return false, false
		}
	}
	return true, true
}

// receivePipelineName is "Receive logs" (or traces), numbered when the
// connection already has a pipeline by that name.
func (h *TelemetryHandler) receivePipelineName(connID, signal string) (string, error) {
	all, err := h.DB.GetPipelines()
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for _, p := range all {
		if p.ConnectionID == connID {
			taken[p.Name] = true
		}
	}
	base := "Receive " + signal
	name := base
	for n := 2; taken[name]; n++ {
		name = fmt.Sprintf("%s %d", base, n)
	}
	return name, nil
}

// existingSource returns the connection's source of the same kind on the
// same table, or nil.
func (h *TelemetryHandler) existingSource(src *telemetry.Source) (*telemetry.Source, error) {
	sources, err := h.DB.ListTelemetrySources(src.ConnectionID)
	if err != nil {
		return nil, err
	}
	for i := range sources {
		s := &sources[i]
		if s.Kind == src.Kind && s.Database == src.Database && s.Table == src.Table {
			return s, nil
		}
	}
	return nil, nil
}

// auditReceive writes the entries creating each piece by hand would have
// written, so filtering the audit log by action still finds them.
func (h *TelemetryHandler) auditReceive(r *http.Request, session *middleware.SessionInfo, pipelineID, name string, key *database.IngestKey, src *telemetry.Source, sourceCreated bool, db, table string) {
	connID := session.ConnectionID
	for _, action := range []string{"pipeline.created", "pipeline.started"} {
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:         action,
			Username:       strPtr(middleware.Actor(session)),
			ClickhouseUser: &session.ClickhouseUser,
			ConnectionID:   &connID,
			Details:        strPtr(fmt.Sprintf("%s (receive setup into %s.%s)", name, db, table)),
		})
	}
	(&PipelinesHandler{DB: h.DB}).auditIngestKey(r, session, "ingest_key.created", &database.Pipeline{ID: pipelineID, Name: name, ConnectionID: connID}, key)
	if sourceCreated {
		h.audit("telemetry.source.created", session, fmt.Sprintf("%s %s (%s.%s)", src.Kind, src.Name, src.Database, src.Table))
	}
}
