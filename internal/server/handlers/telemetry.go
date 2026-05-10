package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

type TelemetryHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

func (h *TelemetryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/schema", h.DetectSchema)
	r.Post("/logs", h.QueryLogs)
	r.Get("/logs/services", h.ListLogServices)
	r.Post("/logs/histogram", h.LogHistogram)
	r.Get("/config", h.GetConfig)
	r.Put("/config", h.SaveConfig)
	return r
}

func sanitizeIdentifier(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return r
		}
		return -1
	}, s)
}

func (h *TelemetryHandler) execQuery(r *http.Request, sql string, timeout time.Duration) (*tunnel.QueryResult, error) {
	session := middleware.GetSession(r)
	if session == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials")
	}

	return h.Gateway.ExecuteQuery(session.ConnectionID, sql, session.ClickhouseUser, password, timeout)
}

// DetectSchema auto-detects OpenTelemetry tables in the connected ClickHouse.
func (h *TelemetryHandler) DetectSchema(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	sql := `SELECT database, name, engine, total_rows FROM system.tables WHERE name LIKE 'otel_%' ORDER BY database, name`

	result, err := h.execQuery(r, sql, 15*time.Second)
	if err != nil {
		slog.Warn("Telemetry schema detection failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tables": result.Data,
		"meta":   result.Meta,
	})
}

// QueryLogs searches the otel_logs table with filters.
func (h *TelemetryHandler) QueryLogs(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Database string   `json:"database"`
		Table    string   `json:"table"`
		TimeFrom string   `json:"timeFrom"`
		TimeTo   string   `json:"timeTo"`
		Severity []string `json:"severity"`
		Services []string `json:"services"`
		Search   string   `json:"search"`
		Limit    int      `json:"limit"`
		Offset   int      `json:"offset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	db := sanitizeIdentifier(body.Database)
	if db == "" {
		db = "default"
	}
	table := sanitizeIdentifier(body.Table)
	if table == "" {
		table = "otel_logs"
	}
	limit := body.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset := body.Offset
	if offset < 0 {
		offset = 0
	}

	var conditions []string
	if body.TimeFrom != "" {
		conditions = append(conditions, fmt.Sprintf("Timestamp >= '%s'", escapeString(body.TimeFrom)))
	}
	if body.TimeTo != "" {
		conditions = append(conditions, fmt.Sprintf("Timestamp <= '%s'", escapeString(body.TimeTo)))
	}
	if len(body.Severity) > 0 {
		quoted := make([]string, len(body.Severity))
		for i, s := range body.Severity {
			quoted[i] = fmt.Sprintf("'%s'", escapeString(s))
		}
		conditions = append(conditions, fmt.Sprintf("SeverityText IN (%s)", strings.Join(quoted, ",")))
	}
	if len(body.Services) > 0 {
		quoted := make([]string, len(body.Services))
		for i, s := range body.Services {
			quoted[i] = fmt.Sprintf("'%s'", escapeString(s))
		}
		conditions = append(conditions, fmt.Sprintf("ServiceName IN (%s)", strings.Join(quoted, ",")))
	}
	if body.Search != "" {
		conditions = append(conditions, fmt.Sprintf("Body ILIKE '%%%s%%'", escapeString(body.Search)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	sql := fmt.Sprintf(
		`SELECT Timestamp, TraceId, SpanId, SeverityText, SeverityNumber, ServiceName, Body, ResourceAttributes, LogAttributes FROM %s.%s %s ORDER BY Timestamp DESC LIMIT %d OFFSET %d`,
		db, table, where, limit, offset,
	)

	result, err := h.execQuery(r, sql, 30*time.Second)
	if err != nil {
		slog.Warn("Telemetry log query failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": result.Data,
		"meta": result.Meta,
	})
}

// ListLogServices returns distinct service names from the logs table.
func (h *TelemetryHandler) ListLogServices(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	db := sanitizeIdentifier(r.URL.Query().Get("database"))
	if db == "" {
		db = "default"
	}
	table := sanitizeIdentifier(r.URL.Query().Get("table"))
	if table == "" {
		table = "otel_logs"
	}

	sql := fmt.Sprintf(`SELECT DISTINCT ServiceName FROM %s.%s ORDER BY ServiceName`, db, table)

	result, err := h.execQuery(r, sql, 15*time.Second)
	if err != nil {
		slog.Warn("Telemetry services query failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": result.Data,
		"meta": result.Meta,
	})
}

// LogHistogram returns time-bucketed log counts for a volume chart.
func (h *TelemetryHandler) LogHistogram(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Database string   `json:"database"`
		Table    string   `json:"table"`
		TimeFrom string   `json:"timeFrom"`
		TimeTo   string   `json:"timeTo"`
		Severity []string `json:"severity"`
		Services []string `json:"services"`
		Search   string   `json:"search"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	db := sanitizeIdentifier(body.Database)
	if db == "" {
		db = "default"
	}
	table := sanitizeIdentifier(body.Table)
	if table == "" {
		table = "otel_logs"
	}

	bucketSeconds := 60
	if body.TimeFrom != "" && body.TimeTo != "" {
		from, err1 := time.Parse(time.RFC3339, body.TimeFrom)
		to, err2 := time.Parse(time.RFC3339, body.TimeTo)
		if err1 == nil && err2 == nil {
			rangeSeconds := int(to.Sub(from).Seconds())
			switch {
			case rangeSeconds <= 3600:
				bucketSeconds = 60
			case rangeSeconds <= 86400:
				bucketSeconds = 900
			case rangeSeconds <= 604800:
				bucketSeconds = 3600
			default:
				bucketSeconds = 86400
			}
		}
	}

	var conditions []string
	if body.TimeFrom != "" {
		conditions = append(conditions, fmt.Sprintf("Timestamp >= '%s'", escapeString(body.TimeFrom)))
	}
	if body.TimeTo != "" {
		conditions = append(conditions, fmt.Sprintf("Timestamp <= '%s'", escapeString(body.TimeTo)))
	}
	if len(body.Severity) > 0 {
		quoted := make([]string, len(body.Severity))
		for i, s := range body.Severity {
			quoted[i] = fmt.Sprintf("'%s'", escapeString(s))
		}
		conditions = append(conditions, fmt.Sprintf("SeverityText IN (%s)", strings.Join(quoted, ",")))
	}
	if len(body.Services) > 0 {
		quoted := make([]string, len(body.Services))
		for i, s := range body.Services {
			quoted[i] = fmt.Sprintf("'%s'", escapeString(s))
		}
		conditions = append(conditions, fmt.Sprintf("ServiceName IN (%s)", strings.Join(quoted, ",")))
	}
	if body.Search != "" {
		conditions = append(conditions, fmt.Sprintf("Body ILIKE '%%%s%%'", escapeString(body.Search)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	sql := fmt.Sprintf(
		`SELECT toStartOfInterval(Timestamp, INTERVAL %d second) as bucket_time, count() as count FROM %s.%s %s GROUP BY bucket_time ORDER BY bucket_time`,
		bucketSeconds, db, table, where,
	)

	result, err := h.execQuery(r, sql, 30*time.Second)
	if err != nil {
		slog.Warn("Telemetry histogram query failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": result.Data,
		"meta": result.Meta,
	})
}

// GetConfig returns the saved telemetry configuration.
func (h *TelemetryHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	cfg, err := h.DB.GetTelemetryConfig(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get config"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"config": cfg})
}

// SaveConfig persists the telemetry configuration.
func (h *TelemetryHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	if err := h.DB.SaveTelemetryConfig(session.ConnectionID, string(body)); err != nil {
		slog.Error("Failed to save telemetry config", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save config"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
