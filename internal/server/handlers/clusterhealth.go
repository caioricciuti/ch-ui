package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// ClusterHealthHandler serves the PRO Cluster Health monitoring endpoints:
// live, fanned-out snapshots of cluster Operations & Database health.
type ClusterHealthHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Store   *clusterhealth.Store
}

const clusterHealthQueryTimeout = 20 * time.Second

// Routes returns a chi.Router with all cluster-health routes mounted.
func (h *ClusterHealthHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/summary", h.GetSummary)
	r.Get("/replication", h.liveHandler(clusterhealth.ReplicationQuery, false))
	r.Get("/replication-queue", h.liveHandler(clusterhealth.ReplicationQueueQuery, false))
	r.Get("/merges", h.liveHandler(clusterhealth.MergesQuery, false))
	r.Get("/mutations", h.liveHandler(clusterhealth.MutationsQuery, false))
	r.Get("/parts", h.liveHandler(clusterhealth.PartsPressureQuery, false))
	r.Get("/disks", h.liveHandler(clusterhealth.DisksQuery, false))
	// Keeper & backups may be unsupported on a given deployment; soft-fail them.
	r.Get("/keeper", h.liveHandler(clusterhealth.KeeperQuery, true))
	r.Get("/backups", h.liveHandler(clusterhealth.BackupsQuery, true))
	r.Get("/long-queries", h.getLongQueries)

	// History & settings (require the Store; wired in Phase 2).
	r.Get("/history", h.GetHistory)
	r.Get("/settings", h.GetSettings)
	r.With(middleware.RequireAdmin(h.DB)).Put("/settings", h.UpdateSettings)

	return r
}

// chSession bundles the per-request ClickHouse access the handler needs.
type chSession struct {
	connID   string
	user     string
	password string
}

// session validates the request, ensures the tunnel is online, and decrypts the
// ClickHouse password. It writes the error response itself and returns ok=false
// when the caller should stop.
func (h *ClusterHealthHandler) session(w http.ResponseWriter, r *http.Request) (chSession, bool) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return chSession{}, false
	}
	if !h.Gateway.IsTunnelOnline(sess.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return chSession{}, false
	}
	password, err := crypto.Decrypt(sess.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Cluster health: failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return chSession{}, false
	}
	return chSession{connID: sess.ConnectionID, user: sess.ClickhouseUser, password: password}, true
}

// exec runs a single SQL statement and decodes the result rows.
func (h *ClusterHealthHandler) exec(cs chSession, sql string) ([]map[string]interface{}, error) {
	result, err := h.Gateway.ExecuteQuery(cs.connID, sql, cs.user, cs.password, clusterHealthQueryTimeout)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return decodeRows(result.Data), nil
}

// resolveCluster determines which cluster to fan out across. An explicit and
// valid ?cluster= override wins; otherwise it picks the cluster containing the
// connected node. Returns "" when the deployment is a single node (queries then
// fall back to the local node).
func (h *ClusterHealthHandler) resolveCluster(r *http.Request, cs chSession) string {
	if override := strings.TrimSpace(r.URL.Query().Get("cluster")); override != "" {
		if clusterhealth.IsValidClusterName(override) {
			return override
		}
		return "" // invalid override → degrade to single node rather than error
	}
	rows, err := h.exec(cs, clusterhealth.ResolveClusterQuery)
	if err != nil || len(rows) == 0 {
		return ""
	}
	name, _ := rows[0]["cluster"].(string)
	if clusterhealth.IsValidClusterName(name) {
		return name
	}
	return ""
}

// liveHandler builds a drill-down endpoint for a fanned-out query. softFail marks
// queries against tables that may not exist on every deployment (Keeper, backups):
// those return supported:false with an empty list instead of an error. When a
// fan-out query fails on a real cluster, it retries scoped to the local node and
// flags the response as degraded.
func (h *ClusterHealthHandler) liveHandler(build func(cluster string) string, softFail bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cs, ok := h.session(w, r)
		if !ok {
			return
		}
		cluster := h.resolveCluster(r, cs)

		rows, err := h.exec(cs, build(cluster))
		degraded := false
		if err != nil && cluster != "" {
			// Retry on the local node only (e.g. remote nodes deny access).
			if rows2, err2 := h.exec(cs, build("")); err2 == nil {
				rows, err, degraded = rows2, nil, true
			}
		}
		if err != nil {
			if softFail {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"cluster": cluster, "is_cluster": cluster != "", "supported": false, "data": []interface{}{},
				})
				return
			}
			slog.Warn("Cluster health query failed", "error", err, "connection", cs.connID)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []map[string]interface{}{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"cluster": cluster, "is_cluster": cluster != "", "supported": true,
			"degraded": degraded, "data": rows,
		})
	}
}

// getLongQueries is a dedicated handler because its query takes the configured
// long-query threshold.
func (h *ClusterHealthHandler) getLongQueries(w http.ResponseWriter, r *http.Request) {
	cs, ok := h.session(w, r)
	if !ok {
		return
	}
	cluster := h.resolveCluster(r, cs)
	threshold := h.thresholdFor(cs.connID)

	build := func(c string) string { return clusterhealth.LongQueriesQuery(c, threshold) }
	rows, err := h.exec(cs, build(cluster))
	degraded := false
	if err != nil && cluster != "" {
		if rows2, err2 := h.exec(cs, build("")); err2 == nil {
			rows, err, degraded = rows2, nil, true
		}
	}
	if err != nil {
		slog.Warn("Cluster health long-queries failed", "error", err, "connection", cs.connID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": cluster, "is_cluster": cluster != "", "supported": true,
		"degraded": degraded, "threshold_seconds": threshold, "data": rows,
	})
}

// GetSummary returns the live per-node health rollup plus parts limits — the data
// behind the page's headline tiles and status badges.
func (h *ClusterHealthHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	cs, ok := h.session(w, r)
	if !ok {
		return
	}
	cluster := h.resolveCluster(r, cs)
	threshold := h.thresholdFor(cs.connID)

	exec := clusterhealth.ExecFunc(func(sql string) ([]map[string]interface{}, error) { return h.exec(cs, sql) })
	capturedAt := time.Now().UTC().Format(time.RFC3339)

	samples, limits, err := clusterhealth.CollectSamples(exec, cluster, threshold, capturedAt)
	degraded := false
	if err != nil && cluster != "" {
		// Replication agg failed cluster-wide; retry scoped to the local node.
		if s2, l2, err2 := clusterhealth.CollectSamples(exec, "", threshold, capturedAt); err2 == nil {
			samples, limits, degraded = s2, l2, true
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster":           cluster,
		"is_cluster":        cluster != "",
		"degraded":          degraded,
		"captured_at":       capturedAt,
		"threshold_seconds": threshold,
		"parts_limits":      limits,
		"nodes":             samples,
	})
}

// thresholdFor returns the configured long-query threshold for a connection,
// falling back to the default when the Store is unavailable or unset.
func (h *ClusterHealthHandler) thresholdFor(connID string) int {
	if h.Store == nil {
		return clusterhealth.DefaultSettings(connID).LongQueryThresholdSecs
	}
	s, err := h.Store.GetSettings(connID)
	if err != nil {
		return clusterhealth.DefaultSettings(connID).LongQueryThresholdSecs
	}
	return s.LongQueryThresholdSecs
}

// GetHistory returns stored time-series samples for charting. The ?range= param
// accepts a Go-style duration suffix (e.g. 1h, 6h, 24h, 168h) or 7d-style days;
// it defaults to the last 6 hours.
func (h *ClusterHealthHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if h.Store == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []interface{}{}})
		return
	}
	window := parseRange(r.URL.Query().Get("range"), 6*time.Hour)
	since := time.Now().UTC().Add(-window).Format(time.RFC3339)
	samples, err := h.Store.GetHistory(sess.ConnectionID, since)
	if err != nil {
		slog.Warn("Cluster health history failed", "error", err, "connection", sess.ConnectionID)
		writeError(w, http.StatusInternalServerError, "Failed to load history")
		return
	}
	if samples == nil {
		samples = []clusterhealth.HistorySample{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"since": since, "data": samples})
}

// GetSettings returns the per-connection monitoring settings (or defaults).
func (h *ClusterHealthHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if h.Store == nil {
		writeJSON(w, http.StatusOK, clusterhealth.DefaultSettings(sess.ConnectionID))
		return
	}
	settings, err := h.Store.GetSettings(sess.ConnectionID)
	if err != nil {
		slog.Warn("Cluster health settings load failed", "error", err, "connection", sess.ConnectionID)
		writeError(w, http.StatusInternalServerError, "Failed to load settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// UpdateSettings persists monitoring settings for the connection (admin only).
func (h *ClusterHealthHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if h.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "Cluster health storage unavailable")
		return
	}
	var in clusterhealth.Settings
	if err := decodeBody(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	in.ConnectionID = sess.ConnectionID
	saved, err := h.Store.UpsertSettings(in)
	if err != nil {
		slog.Warn("Cluster health settings save failed", "error", err, "connection", sess.ConnectionID)
		writeError(w, http.StatusInternalServerError, "Failed to save settings")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// parseRange converts a range string into a duration, accepting both Go durations
// (1h, 30m) and a Nd day suffix (7d). Falls back to def on empty/invalid input.
func parseRange(s string, def time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if strings.HasSuffix(s, "d") {
		if days, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
		return def
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return def
}

// decodeBody is a small helper for JSON request bodies.
func decodeBody(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
