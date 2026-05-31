package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/google/uuid"
)

func RegisterPipeline(r *Registry) {
	r.Register(getPipelineGraph)
	r.Register(configurePipeline)
	r.Register(startPipeline)
}

var allowedSourceTypes = map[string]bool{
	"source_kafka":    true,
	"source_webhook":  true,
	"source_database": true,
	"source_s3":       true,
}
var allowedSinkTypes = map[string]bool{
	"sink_clickhouse": true,
}

var getPipelineGraph = Tool{
	Name:        "get_pipeline_graph",
	Description: "Get a pipeline's current source/sink nodes and edges. ALWAYS call this before configure_pipeline so you know whether the pipeline is empty or already wired.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"pipeline_id"},
		"properties": map[string]any{
			"pipeline_id": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			PipelineID string `json:"pipeline_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.PipelineID = strings.TrimSpace(in.PipelineID)
		if in.PipelineID == "" {
			return nil, errors.New("pipeline_id is required")
		}
		nodes, edges, err := tctx.DB.GetPipelineGraph(in.PipelineID)
		if err != nil {
			return nil, fmt.Errorf("get pipeline graph: %w", err)
		}
		nodeOut := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			var cfg map[string]any
			if n.ConfigEncrypted != "" {
				_ = json.Unmarshal([]byte(n.ConfigEncrypted), &cfg)
			}
			nodeOut = append(nodeOut, map[string]any{
				"id":        n.ID,
				"node_type": n.NodeType,
				"label":     n.Label,
				"config":    redactSecrets(cfg),
			})
		}
		edgeOut := make([]map[string]any, 0, len(edges))
		for _, e := range edges {
			edgeOut = append(edgeOut, map[string]any{
				"id":             e.ID,
				"source_node_id": e.SourceNodeID,
				"target_node_id": e.TargetNodeID,
			})
		}
		return map[string]any{
			"pipeline_id": in.PipelineID,
			"nodes":       nodeOut,
			"edges":       edgeOut,
			"empty":       len(nodes) == 0,
		}, nil
	},
}

var configurePipeline = Tool{
	Name: "configure_pipeline",
	Description: `Wire a pipeline end-to-end in ONE call: creates a source node, a sink node, and the edge between them. Replaces any existing graph (call get_pipeline_graph first if you want to preserve).

Source node_type and config keys (REQUIRED for each):
- source_webhook: { auth_enabled?: bool, batch_size?: number, batch_timeout_ms?: number }
- source_kafka: { brokers: string, topic: string, consumer_group: string, sasl_mechanism?, sasl_username?, sasl_password?, use_tls?: bool, batch_size?, batch_timeout_ms? }
- source_database: { db_type: "postgres"|"mysql"|"sqlite", connection_string: string, query: string, poll_interval?, watermark_column?, batch_size? }
- source_s3: { endpoint?, region?, bucket: string, prefix?, access_key: string, secret_key: string, format: "json"|"ndjson"|"csv", poll_interval?, batch_size? }

Sink node_type and config keys:
- sink_clickhouse: { database: string, table: string, create_table?: bool, create_table_engine?, create_table_order_by? }

After configuring, propose start_pipeline in the same turn.`,
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"pipeline_id", "source", "sink"},
		"properties": map[string]any{
			"pipeline_id": map[string]any{"type": "string"},
			"source": map[string]any{
				"type":     "object",
				"required": []string{"node_type", "config"},
				"properties": map[string]any{
					"node_type": map[string]any{"type": "string", "enum": []string{"source_webhook", "source_kafka", "source_database", "source_s3"}},
					"label":     map[string]any{"type": "string"},
					"config":    map[string]any{"type": "object"},
				},
			},
			"sink": map[string]any{
				"type":     "object",
				"required": []string{"node_type", "config"},
				"properties": map[string]any{
					"node_type": map[string]any{"type": "string", "enum": []string{"sink_clickhouse"}},
					"label":     map[string]any{"type": "string"},
					"config":    map[string]any{"type": "object"},
				},
			},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			PipelineID string `json:"pipeline_id"`
			Source     struct {
				NodeType string                 `json:"node_type"`
				Label    string                 `json:"label"`
				Config   map[string]interface{} `json:"config"`
			} `json:"source"`
			Sink struct {
				NodeType string                 `json:"node_type"`
				Label    string                 `json:"label"`
				Config   map[string]interface{} `json:"config"`
			} `json:"sink"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.PipelineID = strings.TrimSpace(in.PipelineID)
		if in.PipelineID == "" {
			return nil, errors.New("pipeline_id is required")
		}
		if !allowedSourceTypes[in.Source.NodeType] {
			return nil, fmt.Errorf("invalid source node_type: %s. Allowed: source_webhook, source_kafka, source_database, source_s3", in.Source.NodeType)
		}
		if !allowedSinkTypes[in.Sink.NodeType] {
			return nil, fmt.Errorf("invalid sink node_type: %s. Allowed: sink_clickhouse", in.Sink.NodeType)
		}
		if missing := missingFields(in.Source.NodeType, in.Source.Config); len(missing) > 0 {
			return nil, fmt.Errorf("source_%s missing required config: %s", strings.TrimPrefix(in.Source.NodeType, "source_"), strings.Join(missing, ", "))
		}
		if missing := missingFields(in.Sink.NodeType, in.Sink.Config); len(missing) > 0 {
			return nil, fmt.Errorf("sink missing required config: %s", strings.Join(missing, ", "))
		}

		if _, err := tctx.DB.GetPipelineByID(in.PipelineID); err != nil {
			return nil, fmt.Errorf("pipeline lookup: %w", err)
		}

		var sinkNotes []string
		if in.Sink.NodeType == "sink_clickhouse" {
			sinkNotes = autoFillClickHouseSink(tctx, in.Sink.Config)
		}

		sourceID := uuid.NewString()
		sinkID := uuid.NewString()
		edgeID := uuid.NewString()

		sourceConfigJSON, _ := json.Marshal(in.Source.Config)
		sinkConfigJSON, _ := json.Marshal(in.Sink.Config)

		sourceLabel := in.Source.Label
		if sourceLabel == "" {
			sourceLabel = labelFromNodeType(in.Source.NodeType)
		}
		sinkLabel := in.Sink.Label
		if sinkLabel == "" {
			sinkLabel = labelFromNodeType(in.Sink.NodeType)
		}

		nodes := []database.PipelineNode{
			{
				ID:              sourceID,
				PipelineID:      in.PipelineID,
				NodeType:        in.Source.NodeType,
				Label:           sourceLabel,
				PositionX:       100,
				PositionY:       150,
				ConfigEncrypted: string(sourceConfigJSON),
			},
			{
				ID:              sinkID,
				PipelineID:      in.PipelineID,
				NodeType:        in.Sink.NodeType,
				Label:           sinkLabel,
				PositionX:       500,
				PositionY:       150,
				ConfigEncrypted: string(sinkConfigJSON),
			},
		}
		edges := []database.PipelineEdge{
			{
				ID:           edgeID,
				PipelineID:   in.PipelineID,
				SourceNodeID: sourceID,
				TargetNodeID: sinkID,
			},
		}
		if err := tctx.DB.SavePipelineGraph(in.PipelineID, nodes, edges, ""); err != nil {
			return nil, fmt.Errorf("save pipeline graph: %w", err)
		}
		result := map[string]any{
			"configured":     true,
			"pipeline_id":    in.PipelineID,
			"source_node_id": sourceID,
			"sink_node_id":   sinkID,
			"edge_id":        edgeID,
			"open_url":       tctx.AbsURL("/pipelines/" + in.PipelineID),
			"hint":           "Pipeline wired. Propose start_pipeline in the same turn so the user sees it ingesting.",
		}
		if len(sinkNotes) > 0 {
			result["notes"] = sinkNotes
		}
		return result, nil
	},
}

func autoFillClickHouseSink(tctx Context, cfg map[string]interface{}) []string {
	db, _ := cfg["database"].(string)
	tbl, _ := cfg["table"].(string)
	db = strings.TrimSpace(db)
	tbl = strings.TrimSpace(tbl)
	if db == "" || tbl == "" {
		return nil
	}
	exists := clickHouseTableExists(tctx, db, tbl)
	var notes []string
	if !exists {
		if _, ok := cfg["create_table"]; !ok {
			cfg["create_table"] = true
			notes = append(notes, fmt.Sprintf("auto-set create_table=true (%s.%s does not exist)", db, tbl))
		}
		if v, ok := cfg["create_table_engine"]; !ok || v == "" {
			cfg["create_table_engine"] = "MergeTree"
			notes = append(notes, "auto-set create_table_engine=MergeTree")
		}
		if v, ok := cfg["create_table_order_by"]; !ok || v == "" {
			cfg["create_table_order_by"] = "tuple()"
			notes = append(notes, "auto-set create_table_order_by=tuple()")
		}
	}
	return notes
}

func clickHouseTableExists(tctx Context, db, table string) bool {
	if tctx.Gateway == nil || tctx.ConnectionID == "" {
		return true
	}
	sql := fmt.Sprintf("SELECT count() AS n FROM system.tables WHERE database = '%s' AND name = '%s'",
		strings.ReplaceAll(db, "'", "''"), strings.ReplaceAll(table, "'", "''"))
	rows, err := runSelect(tctx, sql, 5*time.Second)
	if err != nil || len(rows) == 0 {
		return true
	}
	switch v := rows[0]["n"].(type) {
	case string:
		return v != "0"
	case float64:
		return v > 0
	}
	return true
}

var startPipeline = Tool{
	Name:             "start_pipeline",
	Description:      "Start a configured pipeline (begins ingesting from source → sink). Will fail if the pipeline graph is empty — configure_pipeline first.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"pipeline_id"},
		"properties": map[string]any{
			"pipeline_id": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			PipelineID string `json:"pipeline_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.PipelineID = strings.TrimSpace(in.PipelineID)
		if in.PipelineID == "" {
			return nil, errors.New("pipeline_id is required")
		}
		if tctx.StartPipeline == nil {
			return nil, errors.New("start_pipeline is not available in this context")
		}
		if err := tctx.StartPipeline(in.PipelineID); err != nil {
			return nil, fmt.Errorf("start pipeline: %w", err)
		}
		return map[string]any{
			"started":     true,
			"pipeline_id": in.PipelineID,
			"open_url":    tctx.AbsURL("/pipelines/" + in.PipelineID),
		}, nil
	},
}

var requiredFields = map[string][]string{
	"source_kafka":    {"brokers", "topic", "consumer_group"},
	"source_webhook":  {},
	"source_database": {"db_type", "connection_string", "query"},
	"source_s3":       {"bucket", "access_key", "secret_key", "format"},
	"sink_clickhouse": {"database", "table"},
}

func missingFields(nodeType string, config map[string]interface{}) []string {
	var missing []string
	for _, k := range requiredFields[nodeType] {
		v, ok := config[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			missing = append(missing, k)
		}
	}
	return missing
}

func labelFromNodeType(nodeType string) string {
	name := nodeType
	name = strings.TrimPrefix(name, "source_")
	name = strings.TrimPrefix(name, "sink_")
	if name == "" {
		return nodeType
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

var secretKeys = map[string]bool{
	"password":          true,
	"sasl_password":     true,
	"secret_key":        true,
	"connection_string": true,
	"api_key":           true,
	"auth_token":        true,
}

func redactSecrets(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if secretKeys[k] {
			out[k] = "***"
		} else {
			out[k] = v
		}
	}
	return out
}
