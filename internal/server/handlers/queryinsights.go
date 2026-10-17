// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/queryinsights"
	"github.com/caioricciuti/ch-ui/internal/querylogactor"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// QueryInsightsHandler serves the PRO Query Insights endpoints: live analytics
// over system.query_log (latency, volume, slow/heavy patterns, errors, users,
// hot tables).
type QueryInsightsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

const (
	queryInsightsTimeout = 30 * time.Second
	// Cluster resolution is a tiny system.clusters lookup; a hung tunnel must
	// not consume the whole request budget before the real query even runs.
	clusterResolveTimeout = 8 * time.Second
)

// insightSections maps URL section names to their query builders.
var insightSections = map[string]func(cluster string, rng queryinsights.Range, f queryinsights.Filters) string{
	"summary":  queryinsights.SummaryQuery,
	"volume":   queryinsights.VolumeQuery,
	"latency":  queryinsights.LatencyQuery,
	"slow":     queryinsights.SlowQueriesQuery,
	"memory":   queryinsights.MemoryQuery,
	"frequent": queryinsights.FrequentQuery,
	"errors":   queryinsights.ErrorsQuery,
	"users":    queryinsights.UsersQuery,
	"tables":   queryinsights.TablesQuery,
}

// Routes returns a chi.Router with all query-insights routes mounted.
func (h *QueryInsightsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{section}", h.getSection)
	return r
}

// session mirrors the cluster-health handler: validates the request, checks
// the tunnel, and decrypts the ClickHouse password.
func (h *QueryInsightsHandler) session(w http.ResponseWriter, r *http.Request) (chSession, bool) {
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
		slog.Error("Query insights: failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return chSession{}, false
	}
	return chSession{connID: sess.ConnectionID, user: sess.ClickhouseUser, password: password}, true
}

// exec runs a statement tagged with the insights log_comment so the analytics
// can exclude exactly our own introspection queries from the numbers.
func (h *QueryInsightsHandler) exec(cs chSession, sql string, timeout time.Duration) ([]map[string]interface{}, error) {
	settings := map[string]string{"log_comment": queryinsights.LogComment}
	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(context.Background(), cs.connID, sql, cs.user, cs.password, settings, timeout)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return decodeRows(result.Data), nil
}

// resolveCluster picks the cluster containing the connected node (same logic
// as cluster health); "" means single node.
func (h *QueryInsightsHandler) resolveCluster(r *http.Request, cs chSession) string {
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

// personAccounts lists the connection's per-person SSO users as actor
// accounts, so query_log rows those users ran outside CH-UI are labeled by
// email like the tagged ones. A lookup failure only loses the relabeling.
func personAccounts(db *database.DB, connID string) []querylogactor.Account {
	if db == nil {
		return nil
	}
	users, err := db.ListSSOClickHouseUsers(connID)
	if err != nil {
		slog.Warn("Query log attribution: failed to load SSO users", "connection", connID, "error", err)
		return nil
	}
	accounts := make([]querylogactor.Account, 0, len(users))
	for _, u := range users {
		accounts = append(accounts, querylogactor.Account{User: u.CHUser, Person: u.Email})
	}
	return accounts
}

// isQueryLogUnavailable detects the errors ClickHouse raises when query_log is
// disabled or a column this section needs doesn't exist on this version.
func isQueryLogUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "query_log") &&
		(strings.Contains(msg, "unknown_table") || strings.Contains(msg, "unknown table expression identifier") || strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "does not exist")) {
		return true
	}
	// Missing columns on older versions (normalized_query_hash, tables) or a
	// missing helper function (errorCodeToName) make a section unsupported
	// rather than broken.
	return strings.Contains(msg, "unknown_identifier") ||
		strings.Contains(msg, "missing columns") ||
		strings.Contains(msg, "unknown function")
}

// getSection serves GET /{section}?range=&cluster= for every insights section.
// All sections soft-fail with supported:false when query_log (or a column the
// section needs) is unavailable on the deployment.
func (h *QueryInsightsHandler) getSection(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	build, ok := insightSections[section]
	if !ok {
		writeError(w, http.StatusNotFound, "Unknown insights section")
		return
	}

	cs, ok := h.session(w, r)
	if !ok {
		return
	}

	rng, ok := queryinsights.RangeSpec(strings.TrimSpace(r.URL.Query().Get("range")))
	if !ok {
		rng = queryinsights.DefaultRange
	}
	cluster := h.resolveCluster(r, cs)

	q := r.URL.Query()
	minMS, _ := strconv.Atoi(q.Get("min_ms"))
	filters := queryinsights.Filters{
		User:          strings.TrimSpace(q.Get("user")),
		QueryKind:     strings.TrimSpace(q.Get("kind")),
		Search:        strings.TrimSpace(q.Get("search")),
		Table:         strings.TrimSpace(q.Get("table")),
		MinDurationMS: minMS,
		Accounts:      personAccounts(h.DB, cs.connID),
	}

	rows, err := h.exec(cs, build(cluster, rng, filters), queryInsightsTimeout)
	degraded := false
	// Remote nodes may deny access to query_log; retry on the local node.
	// A timeout means load, not access denial — piling a second heavy scan on
	// a struggling server would make things worse, so don't retry those.
	if err != nil && cluster != "" && !strings.Contains(err.Error(), "query timeout") {
		if rows2, err2 := h.exec(cs, build("", rng, filters), queryInsightsTimeout); err2 == nil {
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
		slog.Warn("Query insights section failed", "section", section, "error", err, "connection", cs.connID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": cluster, "is_cluster": cluster != "", "supported": true,
		"degraded": degraded, "range": rng.Name, "data": rows,
	})
}
