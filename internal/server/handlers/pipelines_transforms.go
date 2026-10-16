package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
)

// Bounds of a transform preview request.
const (
	maxPreviewBody    = 1 << 20
	maxPreviewRecords = 100
)

// previewTransformsResponse is the input and what each node made of it.
type previewTransformsResponse struct {
	Input []map[string]any `json:"input"`
	Steps []transform.Step `json:"steps"`
}

// PreviewTransforms runs the nodes in the request (unsaved configs, in chain
// order) on the request's records, or on the last records a running
// pipeline saw, or on a sample read from its source. Nothing is written.
func (h *PipelinesHandler) PreviewTransforms(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPreviewBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read the request")
		return
	}
	if len(body) > maxPreviewBody {
		writeError(w, http.StatusRequestEntityTooLarge, "Preview request is larger than 1 MB")
		return
	}
	var req struct {
		Nodes   []transform.NodeSpec `json:"nodes"`
		Records []map[string]any     `json:"records"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if len(req.Records) > maxPreviewRecords {
		writeError(w, http.StatusBadRequest, "At most 100 records can be previewed")
		return
	}
	for _, rec := range req.Records {
		if rec == nil {
			writeError(w, http.StatusBadRequest, "Every record must be a JSON object")
			return
		}
	}
	if err := transform.RequirePro(req.Nodes, h.proAllowed()); err != nil {
		writeNodeError(w, http.StatusPaymentRequired, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	opts, _, err := h.Runner.PreviewSource(ctx, pipeline.ID, false)
	if err != nil {
		slog.Warn("Transform preview could not load the pipeline", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to load the pipeline")
		return
	}
	chain, err := transform.Compile(req.Nodes, opts)
	if err != nil {
		writeNodeError(w, http.StatusBadRequest, err)
		return
	}

	records := req.Records
	if len(records) == 0 {
		records = h.Runner.RecentRecords(pipeline.ID)
	}
	if len(records) == 0 {
		if !h.sqliteAllowed(w, pipeline.ID, session) {
			return
		}
		if _, records, err = h.Runner.PreviewSource(ctx, pipeline.ID, true); err != nil {
			slog.Warn("Transform preview could not sample the source", "error", err, "pipeline", pipeline.ID)
		}
	}
	if len(records) == 0 {
		writeError(w, http.StatusBadRequest, "No sample records yet; send some data or paste records")
		return
	}
	if len(records) > maxPreviewRecords {
		records = records[:maxPreviewRecords]
	}
	writeJSON(w, http.StatusOK, previewTransformsResponse{Input: records, Steps: chain.Preview(records)})
}

// proAllowed reports whether Pro transforms may be previewed: an active
// license or the read-only grace window (a preview changes nothing).
func (h *PipelinesHandler) proAllowed() bool {
	return h.Config != nil && h.Config.ProAccess() != config.ProNone
}

// writeNodeError writes an error and, for a node error, the node's id. A
// Pro refusal carries only "Sampling needs CH-UI Pro"; node_id names the
// node.
func writeNodeError(w http.ResponseWriter, status int, err error) {
	resp := map[string]any{"success": false, "error": err.Error()}
	var ne *transform.NodeError
	if errors.As(err, &ne) {
		resp["node_id"] = ne.NodeID
		if ne.Pro {
			resp["error"] = ne.Msg
		}
	}
	writeJSON(w, status, resp)
}

// validateTransformNodes checks the config of every transform node of a
// graph being saved. Pro nodes save on any edition, so a downgrade keeps the
// config; starting the pipeline needs Pro.
func validateTransformNodes(nodes []graphNode) error {
	var sourceType string
	var sourceCfg map[string]any
	for _, n := range nodes {
		if pipelines.IsSourceType(n.NodeType) {
			sourceType, sourceCfg = n.NodeType, n.Config
			break
		}
	}
	opts := pipelines.TransformOptions(sourceType, sourceCfg)
	for _, n := range nodes {
		if !transform.IsTransform(n.NodeType) {
			continue
		}
		if err := transform.Validate(transform.NodeSpec{ID: n.ID, Type: n.NodeType, Label: n.Label, Config: n.Config}, opts); err != nil {
			return err
		}
	}
	return nil
}
