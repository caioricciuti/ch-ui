package pipelines

import (
	"encoding/json"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Status is a pipeline's stored state plus, while it runs, its live
// counters. The Pipelines page and the MCP pipeline_status tool both render
// it.
func (r *Runner) Status(p *database.Pipeline) map[string]interface{} {
	resp := map[string]interface{}{
		"pipeline_id": p.ID,
		"status":      p.Status,
		"last_error":  p.LastError,
	}
	if n, err := r.db.GetPipelineDLQRows(p.ID); err == nil {
		resp["dlq_rows"] = n
	}
	if nodes, _, err := r.db.GetPipelineGraph(p.ID); err == nil {
		if db, table := DLQTarget(nodes); table != "" {
			resp["dlq_database"] = db
			resp["dlq_table"] = table
		}
	}

	// Add live metrics if pipeline is running
	if metrics := r.GetRunningMetrics(p.ID); metrics != nil {
		resp["rows_ingested"] = metrics.RowsIngested.Load()
		resp["bytes_ingested"] = metrics.BytesIngested.Load()
		resp["batches_sent"] = metrics.BatchesSent.Load()
		resp["errors_count"] = metrics.ErrorsCount.Load()
		if t, ok := metrics.LastBatchAt.Load().(time.Time); ok && !t.IsZero() {
			resp["last_batch_at"] = t.UTC().Format(time.RFC3339)
		}
		if counters := r.TransformCounters(p.ID); counters != nil {
			resp["transforms"] = counters
		}
		if sinks := r.SinkStatuses(p.ID); sinks != nil {
			resp["sinks"] = sinks
		}
	}
	return resp
}

// DLQTarget returns the database and dead-letter table of the pipeline's
// sink, or empty strings when it has none.
func DLQTarget(nodes []database.PipelineNode) (string, string) {
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

// SinkDatabases lists the ClickHouse databases the pipeline's sinks write
// to ("default" when a sink names none), in node order, without repeats.
// ok is false when a sink config cannot be read.
func SinkDatabases(nodes []database.PipelineNode) (dbs []string, ok bool) {
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.NodeType != "sink_clickhouse" {
			continue
		}
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) != nil {
			return nil, false
		}
		db, _ := cfg["database"].(string)
		if db == "" {
			db = "default"
		}
		if !seen[db] {
			seen[db] = true
			dbs = append(dbs, db)
		}
	}
	return dbs, true
}

// HasSQLiteSource reports whether any node reads a SQLite file on this
// server.
func HasSQLiteSource(nodes []database.PipelineNode) bool {
	for _, n := range nodes {
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) == nil && IsSQLiteSource(n.NodeType, cfg) {
			return true
		}
	}
	return false
}
