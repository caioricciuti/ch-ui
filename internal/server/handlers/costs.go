// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/costs"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// CostsHandler serves the PRO Cost Center: showback/chargeback analytics
// over system.query_log and system.parts, priced with per-connection rates.
type CostsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

const costsTimeout = 30 * time.Second

// costSections maps URL section names to their query builders. Storage
// ignores the range (parts are a snapshot) but shares the signature.
var costSections = map[string]func(cluster string, rng costs.Range, cfg costs.Config) string{
	"summary": costs.SummaryQuery,
	"trend":   costs.TrendQuery,
	"users":   costs.UsersQuery,
	"queries": costs.QueriesQuery,
	"storage": func(cluster string, _ costs.Range, cfg costs.Config) string {
		return costs.StorageQuery(cluster, cfg)
	},
}

// teamMappedSections get a "team" field attached to every row from the
// user→team rules, so the UI can group by cost center.
var teamMappedSections = map[string]bool{"trend": true, "users": true}

func (h *CostsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/config", h.getConfig)
	r.Put("/config", h.saveConfig)
	r.Get("/{section}", h.getSection)
	return r
}

func (h *CostsHandler) session(w http.ResponseWriter, r *http.Request) (chSession, bool) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return chSession{}, false
	}
	if !h.Gateway.IsTunnelOnline(sess.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is offline")
		return chSession{}, false
	}
	password, err := crypto.Decrypt(sess.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Cost center: failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return chSession{}, false
	}
	return chSession{connID: sess.ConnectionID, user: sess.ClickhouseUser, password: password}, true
}

func (h *CostsHandler) exec(cs chSession, sql string, timeout time.Duration) ([]map[string]interface{}, error) {
	settings := map[string]string{"log_comment": costs.LogComment}
	result, err := h.Gateway.ExecuteQueryWithSettings(cs.connID, sql, cs.user, cs.password, settings, timeout)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return decodeRows(result.Data), nil
}

func (h *CostsHandler) resolveCluster(r *http.Request, cs chSession) string {
	if override := strings.TrimSpace(r.URL.Query().Get("cluster")); override != "" {
		if clusterhealth.IsValidClusterName(override) {
			return override
		}
		return ""
	}
	rows, err := h.exec(cs, clusterhealth.ResolveClusterQuery, clusterResolveTimeout)
	if err != nil || len(rows) == 0 {
		return ""
	}
	name, _ := rows[0]["cluster"].(string)
	if clusterhealth.IsValidClusterName(name) {
		return name
	}
	return ""
}

// loadConfig returns the connection's saved cost model, or defaults when
// nothing was saved yet (the page must work out of the box).
func (h *CostsHandler) loadConfig(connectionID string) (costs.Config, bool) {
	stored, _ := h.DB.GetCostsConfig(connectionID)
	if stored == nil || stored.ConfigJSON == "" {
		return costs.DefaultConfig(), true
	}
	var cfg costs.Config
	if err := json.Unmarshal([]byte(stored.ConfigJSON), &cfg); err != nil {
		slog.Warn("Cost center: stored config is invalid, using defaults", "error", err)
		return costs.DefaultConfig(), true
	}
	return cfg.Sanitize(), false
}

// getConfig serves GET /config: the saved cost model, or defaults flagged
// with is_default so the UI can prompt for real rates.
func (h *CostsHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	cfg, isDefault := h.loadConfig(sess.ConnectionID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"config": cfg, "is_default": isDefault})
}

// saveConfig persists the cost model after sanitizing it.
func (h *CostsHandler) saveConfig(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var cfg costs.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	cfg = cfg.Sanitize()
	raw, err := json.Marshal(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to encode config")
		return
	}
	if err := h.DB.SaveCostsConfig(sess.ConnectionID, string(raw)); err != nil {
		slog.Error("Cost center: failed to save config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to save config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"config": cfg, "is_default": false})
}

// getSection serves GET /{section}?range=&cluster=. Same soft-fail contract
// as query insights: supported:false when query_log (or a column a section
// needs, like the ProfileEvents map) is unavailable.
func (h *CostsHandler) getSection(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	build, ok := costSections[section]
	if !ok {
		writeError(w, http.StatusNotFound, "Unknown costs section")
		return
	}

	cs, ok := h.session(w, r)
	if !ok {
		return
	}

	rng, ok := costs.RangeSpec(strings.TrimSpace(r.URL.Query().Get("range")))
	if !ok {
		rng = costs.DefaultRange
	}
	cluster := h.resolveCluster(r, cs)
	cfg, _ := h.loadConfig(cs.connID)

	rows, err := h.exec(cs, build(cluster, rng, cfg), costsTimeout)
	degraded := false
	// Remote nodes may deny system table access; retry on the local node.
	// Timeouts mean load, not denial — don't pile on a second heavy scan.
	if err != nil && cluster != "" && !strings.Contains(err.Error(), "query timeout") {
		if rows2, err2 := h.exec(cs, build("", rng, cfg), costsTimeout); err2 == nil {
			rows, err, degraded = rows2, nil, true
		}
	}
	if err != nil {
		if isQueryLogUnavailable(err) {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"cluster": cluster, "is_cluster": cluster != "", "supported": false,
				"range": rng.Name, "data": []interface{}{},
			})
			return
		}
		slog.Warn("Cost center section failed", "section", section, "error", err, "connection", cs.connID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	if teamMappedSections[section] {
		for _, row := range rows {
			user, _ := row["user"].(string)
			row["team"] = costs.MapTeam(user, cfg.Teams)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": cluster, "is_cluster": cluster != "", "supported": true,
		"degraded": degraded, "range": rng.Name, "data": rows,
		"currency": cfg.Currency,
	})
}
