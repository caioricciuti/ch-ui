// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti. See LICENSE.BSL.

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/performance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

type PerformanceHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

func (h *PerformanceHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/regressions", h.regressions)
	r.Get("/investigations", h.list)
	r.Get("/investigations/{id}", h.detail)
	// Background snapshots use a delegated service account. Keep those reads
	// admin-only; interactive regressions always use the caller's own grants.
	r.With(middleware.RequireAdmin(h.DB)).Get("/monitor", h.monitor)
	r.With(middleware.RequireWriter()).Post("/investigations", h.create)
	r.With(middleware.RequireWriter()).Put("/investigations/{id}", h.update)
	r.With(middleware.RequireWriter()).Post("/investigations/{id}/compare", h.compare)
	r.With(middleware.RequireAdmin(h.DB)).Put("/monitor", h.setMonitor)
	return r
}

func performanceSession(w http.ResponseWriter, r *http.Request) *middleware.SessionInfo {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
	}
	return session
}

func performanceActor(session *middleware.SessionInfo) string {
	if session.AuthSubject != "" {
		return session.AuthSubject
	}
	return session.ClickhouseUser
}

func (h *PerformanceHandler) executor(w http.ResponseWriter, r *http.Request) (performance.Executor, bool) {
	insights := QueryInsightsHandler{DB: h.DB, Gateway: h.Gateway, Config: h.Config}
	cs, ok := insights.session(w, r)
	if !ok {
		return nil, false
	}
	return func(sql string) ([]map[string]interface{}, error) {
		result, err := h.Gateway.ExecuteQueryWithSettings(cs.connID, sql, cs.user, cs.password,
			map[string]string{"log_comment": performance.LogComment, "readonly": "1", "max_execution_time": "25", "max_memory_usage": "536870912", "max_threads": "2"}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("no query response")
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal(result.Data, &rows); err != nil {
			return nil, fmt.Errorf("invalid query response: %w", err)
		}
		return rows, nil
	}, true
}

func (h *PerformanceHandler) regressions(w http.ResponseWriter, r *http.Request) {
	exec, ok := h.executor(w, r)
	if !ok {
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	if _, ok := performance.Duration(rng); !ok {
		writeError(w, 400, "range must be 1h, 6h, 24h, or 7d")
		return
	}
	// The connected node is an explicit, repeatable coverage boundary. An
	// optional cluster can be used by API clients; failures never fall back.
	cluster := r.URL.Query().Get("cluster")
	if cluster != "" && !performance.ValidCluster(cluster) {
		writeError(w, 400, "Invalid cluster name")
		return
	}
	report, err := performance.Collect(exec, cluster, time.Now(), rng)
	if err != nil {
		if isQueryLogUnavailable(err) {
			writeJSON(w, 200, performance.Report{Range: rng, Cluster: cluster, Supported: false, MinSamples: performance.MinSamples,
				Coverage:    "Query log or required columns are unavailable. Enable system.query_log and grant SELECT access; this comparison needs normalized_query_hash, current_database, memory_usage, read_bytes and ProfileEvents.",
				Regressions: []performance.Pattern{}, Patterns: []performance.Pattern{}})
			return
		}
		writeError(w, http.StatusBadGateway, "Could not compare query log: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *PerformanceHandler) list(w http.ResponseWriter, r *http.Request) {
	sess := performanceSession(w, r)
	if sess == nil {
		return
	}
	items, err := h.DB.ListPerformanceInvestigations(sess.ConnectionID)
	if err != nil {
		h.dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]interface{}{"investigations": items})
}

func (h *PerformanceHandler) load(w http.ResponseWriter, r *http.Request) *database.PerformanceInvestigation {
	sess := performanceSession(w, r)
	if sess == nil {
		return nil
	}
	item, err := h.DB.GetPerformanceInvestigation(sess.ConnectionID, chi.URLParam(r, "id"))
	if err != nil {
		h.dbError(w, err)
		return nil
	}
	if item == nil {
		writeError(w, 404, "Investigation not found")
	}
	return item
}

func (h *PerformanceHandler) detail(w http.ResponseWriter, r *http.Request) {
	item := h.load(w, r)
	if item == nil {
		return
	}
	h.writeDetail(w, item)
}

func (h *PerformanceHandler) writeDetail(w http.ResponseWriter, item *database.PerformanceInvestigation) {
	events, err := h.DB.ListPerformanceEvents(item.ConnectionID, item.ID)
	if err != nil {
		h.dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]interface{}{"investigation": item, "events": events})
}

type performanceInput struct {
	Title    string `json:"title"`
	Owner    string `json:"owner"`
	Status   string `json:"status"`
	Note     string `json:"note"`
	Hash     string `json:"hash"`
	Database string `json:"database"`
	Range    string `json:"range"`
	Cluster  string `json:"cluster"`
	Revision int64  `json:"revision"`
}

func readPerformanceInput(w http.ResponseWriter, r *http.Request) (performanceInput, bool) {
	var in performanceInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeError(w, 400, "Invalid investigation JSON")
		return in, false
	}
	in.Title, in.Owner, in.Note = strings.TrimSpace(in.Title), strings.TrimSpace(in.Owner), strings.TrimSpace(in.Note)
	if in.Title == "" || len(in.Title) > 200 || len(in.Owner) > 320 || len(in.Note) > 8000 || len(in.Database) > 1024 {
		writeError(w, 400, "A title (up to 200 characters) is required; owner is limited to 320 and notes to 8000 characters")
		return in, false
	}
	return in, true
}

func (h *PerformanceHandler) create(w http.ResponseWriter, r *http.Request) {
	sess := performanceSession(w, r)
	if sess == nil {
		return
	}
	in, ok := readPerformanceInput(w, r)
	if !ok {
		return
	}
	if !performance.ValidHash(in.Hash) {
		writeError(w, 400, "A valid query hash is required")
		return
	}
	if _, ok := performance.Duration(in.Range); !ok {
		writeError(w, 400, "Select a valid baseline range")
		return
	}
	if in.Cluster != "" && !performance.ValidCluster(in.Cluster) {
		writeError(w, 400, "Invalid cluster name")
		return
	}
	exec, ok := h.executor(w, r)
	if !ok {
		return
	}
	snapshot, sample, err := performance.Capture(exec, in.Cluster, time.Now(), in.Range, in.Hash, in.Database)
	if err != nil {
		writeError(w, 502, "Could not capture baseline: "+err.Error())
		return
	}
	if snapshot.Metrics.Runs < performance.MinSamples {
		writeError(w, 409, fmt.Sprintf("Baseline needs at least %d successful logged executions; found %d", performance.MinSamples, snapshot.Metrics.Runs))
		return
	}
	item := &database.PerformanceInvestigation{ConnectionID: sess.ConnectionID, Title: in.Title, Owner: in.Owner, QueryHash: in.Hash,
		Database: in.Database, SampleQuery: sample, Baseline: snapshot, CreatedBy: performanceActor(sess)}
	if err := h.DB.CreatePerformanceInvestigation(item, in.Note); err != nil {
		h.dbError(w, err)
		return
	}
	h.writeDetail(w, item)
}

func (h *PerformanceHandler) update(w http.ResponseWriter, r *http.Request) {
	item := h.load(w, r)
	if item == nil {
		return
	}
	in, ok := readPerformanceInput(w, r)
	if !ok {
		return
	}
	if in.Status != "open" && in.Status != "monitoring" && in.Status != "resolved" {
		writeError(w, 400, "Status must be open, monitoring or resolved")
		return
	}
	item.Title, item.Owner, item.Status, item.Revision = in.Title, in.Owner, in.Status, in.Revision
	if err := h.DB.UpdatePerformanceInvestigation(item, performanceActor(middleware.GetSession(r)), in.Note); err != nil {
		if errors.Is(err, database.ErrPerformanceConflict) {
			writeError(w, 409, err.Error())
			return
		}
		h.dbError(w, err)
		return
	}
	h.detail(w, r)
}

func (h *PerformanceHandler) compare(w http.ResponseWriter, r *http.Request) {
	item := h.load(w, r)
	if item == nil {
		return
	}
	// Rewritten SQL can have a new normalized hash. An explicitly selected
	// successor pattern remains visible in the immutable comparison evidence.
	var target struct {
		Hash     string `json:"hash"`
		Database string `json:"database"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil && err != io.EOF {
		writeError(w, 400, "Invalid comparison target")
		return
	}
	if target.Hash == "" {
		target.Hash, target.Database = item.QueryHash, item.Database
	}
	if !performance.ValidHash(target.Hash) || len(target.Database) > 1024 {
		writeError(w, 400, "Invalid comparison pattern")
		return
	}
	_, window, err := performance.Windows(time.Now(), item.Baseline.Range)
	if err != nil {
		h.dbError(w, err)
		return
	}
	if window.Start.Before(item.Baseline.Window.End) {
		available := item.Baseline.Window.End.Add(item.Baseline.Window.End.Sub(item.Baseline.Window.Start)).Add(time.Minute)
		writeError(w, 409, "A full non-overlapping comparison window will be available after "+available.Format(time.RFC3339))
		return
	}
	exec, ok := h.executor(w, r)
	if !ok {
		return
	}
	current, _, err := performance.Capture(exec, item.Baseline.Cluster, time.Now(), item.Baseline.Range, target.Hash, target.Database)
	if err != nil {
		writeError(w, 502, "Could not capture comparison: "+err.Error())
		return
	}
	comparison, err := performance.Compare(item.Baseline, current)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if err := h.DB.AddPerformanceComparison(item.ConnectionID, item.ID, performanceActor(middleware.GetSession(r)), comparison); err != nil {
		h.dbError(w, err)
		return
	}
	h.detail(w, r)
}

func (h *PerformanceHandler) monitor(w http.ResponseWriter, r *http.Request) {
	sess := performanceSession(w, r)
	if sess == nil {
		return
	}
	state, err := h.DB.GetPerformanceMonitor(sess.ConnectionID)
	if err != nil {
		h.dbError(w, err)
		return
	}
	writeJSON(w, 200, state)
}

func (h *PerformanceHandler) setMonitor(w http.ResponseWriter, r *http.Request) {
	sess := performanceSession(w, r)
	if sess == nil {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		writeError(w, 400, "Invalid monitor settings")
		return
	}
	if in.Enabled {
		credential, err := h.DB.GetBackgroundCredential(sess.ConnectionID, "performance")
		if err != nil {
			h.dbError(w, err)
			return
		}
		if credential.Mode != "service_account" {
			writeError(w, 409, "Configure a dedicated Performance service account in Connections → Background accounts before enabling hourly scans")
			return
		}
	}
	if err := h.DB.SetPerformanceMonitor(sess.ConnectionID, in.Enabled); err != nil {
		h.dbError(w, err)
		return
	}
	h.monitor(w, r)
}

func (h *PerformanceHandler) dbError(w http.ResponseWriter, err error) {
	slog.Error("Performance storage failed", "error", err)
	writeError(w, 500, "Could not access performance investigations")
}
