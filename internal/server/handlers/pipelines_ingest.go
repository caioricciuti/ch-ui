package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// pipelineListItem is a pipeline in the list, with its source type and, for
// an ingest source, its signal, so the list needs no request per pipeline.
type pipelineListItem struct {
	database.Pipeline
	SourceType string `json:"source_type,omitempty"`
	Signal     string `json:"signal,omitempty"`
}

func newPipelineListItem(p database.Pipeline, source database.PipelineNode) pipelineListItem {
	item := pipelineListItem{Pipeline: p, SourceType: source.NodeType}
	if source.NodeType == "source_ingest" {
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(source.ConfigEncrypted), &cfg) == nil {
			item.Signal = pipelines.IngestSignal(cfg)
		}
	}
	return item
}

// maxIngestKeyName bounds a key's display name.
const maxIngestKeyName = 100

// ListIngestKeys returns a pipeline's ingest keys, revoked ones included.
// Secrets are never returned after creation.
func (h *PipelinesHandler) ListIngestKeys(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	keys, err := h.DB.ListIngestKeys(pipeline.ID)
	if err != nil {
		slog.Error("Failed to list ingest keys", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to list ingest keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"keys": keys})
}

// CreateIngestKey issues a key for the pipeline. The secret is in this
// response only.
func (h *PipelinesHandler) CreateIngestKey(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if len(name) > maxIngestKeyName {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Name must be at most %d characters", maxIngestKeyName))
		return
	}
	secret, hash, prefix, err := ingest.GenerateKey()
	if err != nil {
		slog.Error("Failed to generate ingest key", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create ingest key")
		return
	}
	key, err := h.DB.CreateIngestKey(pipeline.ID, name, hash, prefix, middleware.Actor(session))
	if err != nil {
		slog.Error("Failed to create ingest key", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to create ingest key")
		return
	}
	h.auditIngestKey(r, session, "ingest_key.created", pipeline, key)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"key": key, "secret": secret})
}

// RevokeIngestKey revokes a key; it stops working at once.
func (h *PipelinesHandler) RevokeIngestKey(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	keyID := chi.URLParam(r, "keyId")
	key, err := h.DB.GetIngestKey(pipeline.ID, keyID)
	if err != nil {
		slog.Error("Failed to load ingest key", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to revoke ingest key")
		return
	}
	if key == nil {
		writeError(w, http.StatusNotFound, "Ingest key not found")
		return
	}
	ok, err := h.DB.RevokeIngestKey(pipeline.ID, keyID)
	if err != nil {
		slog.Error("Failed to revoke ingest key", "error", err, "pipeline", pipeline.ID)
		writeError(w, http.StatusInternalServerError, "Failed to revoke ingest key")
		return
	}
	if ok {
		h.auditIngestKey(r, session, "ingest_key.revoked", pipeline, key)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *PipelinesHandler) auditIngestKey(r *http.Request, session *middleware.SessionInfo, action string, pipeline *database.Pipeline, key *database.IngestKey) {
	details := fmt.Sprintf("pipeline %q, key %q (chi_%s...)", pipeline.Name, key.Name, key.Prefix)
	ip := middleware.ClientIP(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   &pipeline.ConnectionID,
		Details:        &details,
		IPAddress:      &ip,
	})
}

// GetIngestStats returns the push counters of a webhook or ingest pipeline
// since the server started, and what is waiting in its spool.
func (h *PipelinesHandler) GetIngestStats(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	hub := h.Runner.Hub()
	if hub == nil {
		writeError(w, http.StatusServiceUnavailable, pipelines.ErrNoSpool.Error())
		return
	}
	view := hub.View(pipeline.ID, time.Now())
	view.Signal = h.pushSignal(pipeline.ID)
	if view.Signal == "" {
		writeError(w, http.StatusNotFound, "This pipeline does not receive pushed data")
		return
	}
	if pipeline.LastError != nil && *pipeline.LastError != "" {
		view.LastError = pipeline.LastError
	}
	writeJSON(w, http.StatusOK, view)
}

// pushSignal is the signal of a pipeline's source_ingest node, "webhook" for
// a webhook source, or "" for any other source.
func (h *PipelinesHandler) pushSignal(pipelineID string) string {
	nodes, _, err := h.DB.GetPipelineGraph(pipelineID)
	if err != nil {
		return ""
	}
	for _, n := range nodes {
		switch n.NodeType {
		case "source_webhook":
			return "webhook"
		case "source_ingest":
			var cfg map[string]interface{}
			if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) == nil {
				return pipelines.IngestSignal(cfg)
			}
		}
	}
	return ""
}

// IngestHandler serves GET /api/ingest/info.
type IngestHandler struct {
	Config *config.Config
}

// Info tells the UI where shippers send data.
func (h *IngestHandler) Info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"base_url":          IngestBaseURL(h.Config, r),
		"signals":           []string{pipelines.SignalLogs, pipelines.SignalTraces, pipelines.SignalEvents},
		"separate_listener": h.Config.IngestAddr != "",
	})
}

// IngestBaseURL is the URL shippers append /v1/<signal> to. It is APP_URL
// (or the request's host when APP_URL is the localhost default and the
// request came from elsewhere) plus /ingest; with --ingest-addr the port is
// the ingest listener's.
func IngestBaseURL(cfg *config.Config, r *http.Request) string {
	base, err := url.Parse(strings.TrimRight(cfg.AppURL, "/"))
	if err != nil || base.Host == "" {
		base = &url.URL{Scheme: "http", Host: r.Host}
	}
	if isLoopbackHost(base.Hostname()) && r.Host != "" && !isLoopbackHost(hostOnly(r.Host)) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = &url.URL{Scheme: scheme, Host: r.Host}
	}
	if cfg.IngestAddr != "" {
		if _, port, err := net.SplitHostPort(cfg.IngestAddr); err == nil && port != "" {
			base.Host = net.JoinHostPort(base.Hostname(), port)
			// The ingest listener has no path prefix of its own.
			base.Path = ""
		}
	}
	return strings.TrimRight(base.String(), "/") + "/ingest"
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
