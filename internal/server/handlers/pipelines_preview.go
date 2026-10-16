package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// pipelineTestResponse is a test result plus the schema inferred from the
// rows each sink would write: Schema for the first sink, Schemas by sink
// node id.
type pipelineTestResponse struct {
	*pipelines.TestResult
	Schema      *pipelines.InferredSchema            `json:"schema,omitempty"`
	Schemas     map[string]*pipelines.InferredSchema `json:"schemas,omitempty"`
	SchemaError string                               `json:"schema_error,omitempty"`
}

// TestPipeline reads a sample from the saved pipeline's source, checks its
// sink and infers a table schema from the sample. Nothing is written and no
// source position moves.
func (h *PipelinesHandler) TestPipeline(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	if !h.sqliteAllowed(w, pipeline.ID, session) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	res, err := h.Runner.TestPipeline(ctx, pipeline.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := pipelineTestResponse{TestResult: res}
	hasRows := false
	for _, rows := range res.SinkRecords {
		hasRows = hasRows || len(rows) > 0
	}
	if hasRows {
		password, err := crypto.Decrypt(session.EncryptedPassword, h.secretKey())
		if err != nil {
			resp.SchemaError = "Could not read your session credentials"
		} else if jsonType, err := pipelines.ServerJSONType(ctx, h.Gateway, session.ConnectionID, session.ClickhouseUser, password); err != nil {
			resp.SchemaError = err.Error()
		} else {
			resp.Schemas = map[string]*pipelines.InferredSchema{}
			for _, id := range res.SinkOrder {
				if rows := res.SinkRecords[id]; len(rows) > 0 {
					s := pipelines.InferSchemaFrom(rows, jsonType)
					resp.Schemas[id] = &s
					if resp.Schema == nil {
						resp.Schema = &s
					}
				}
			}
			if resp.Schema == nil {
				resp.SchemaError = "The transforms kept no sample rows to infer a schema from"
			}
		}
	} else if len(res.Records) > 0 {
		resp.SchemaError = "The transforms kept no sample rows to infer a schema from"
	}
	writeJSON(w, http.StatusOK, resp)
}

// ResetPosition forgets the saved source positions so the next start reads
// the source from the beginning. Kafka keeps its consumer group offsets.
func (h *PipelinesHandler) ResetPosition(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	if pipeline.Status == "running" || pipeline.Status == "starting" {
		writeError(w, http.StatusConflict, "Stop the pipeline before resetting its position")
		return
	}
	n, err := h.DB.DeletePipelineCheckpoints(pipeline.ID)
	if err != nil {
		slog.Error("Failed to reset pipeline position", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to reset the position")
		return
	}
	details := fmt.Sprintf("%s (%d checkpoints cleared)", pipeline.Name, n)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "pipeline.position_reset",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        &details,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"cleared": n})
}

// ListCheckpoints returns the saved source positions of a pipeline.
func (h *PipelinesHandler) ListCheckpoints(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	list, err := h.DB.GetPipelineCheckpoints(pipeline.ID)
	if err != nil {
		slog.Error("Failed to list pipeline checkpoints", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to list checkpoints")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"checkpoints": list})
}

// sqliteAllowed refuses a non-admin acting on a pipeline with a SQLite
// source (it reads files on this server). It writes the response when not.
func (h *PipelinesHandler) sqliteAllowed(w http.ResponseWriter, pipelineID string, session *middleware.SessionInfo) bool {
	nodes, _, err := h.DB.GetPipelineGraph(pipelineID)
	if err != nil {
		return true // prepare reports the load error
	}
	for _, n := range nodes {
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) == nil && pipelines.IsSQLiteSource(n.NodeType, cfg) && session.UserRole != "admin" {
			writeError(w, http.StatusForbidden, "Only admins can use a pipeline with a SQLite source")
			return false
		}
	}
	return true
}

// dlqTarget returns the database and dead-letter table of the pipeline's
// sink, or empty strings when it has none.
func (h *PipelinesHandler) dlqTarget(pipelineID string) (string, string) {
	nodes, _, err := h.DB.GetPipelineGraph(pipelineID)
	if err != nil {
		return "", ""
	}
	for _, n := range nodes {
		if n.NodeType != "sink_clickhouse" {
			continue
		}
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) != nil {
			return "", ""
		}
		db, _ := cfg["database"].(string)
		table, _ := cfg["table"].(string)
		if db == "" {
			db = "default"
		}
		if table == "" {
			return "", ""
		}
		return db, table + "_dlq"
	}
	return "", ""
}
