// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/schemacompare"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

type SchemaCompareHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

type schemaEndpoint struct {
	ConnectionID string `json:"connection_id"`
	Database     string `json:"database"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
}

func (h *SchemaCompareHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/connections", h.Connections)
	r.Post("/compare", h.Compare)
	return r
}

func (h *SchemaCompareHandler) Connections(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	conns, err := h.DB.GetConnections()
	if err != nil {
		writeError(w, 500, "Failed to load connections")
		return
	}
	list := make([]map[string]any, 0, len(conns))
	for _, conn := range conns {
		list = append(list, map[string]any{"id": conn.ID, "name": conn.Name, "online": h.Gateway != nil && h.Gateway.IsTunnelOnline(conn.ID), "uses_session": conn.ID == session.ConnectionID})
	}
	writeJSON(w, 200, map[string]any{"connections": list})
}

func (h *SchemaCompareHandler) credentials(session *middleware.SessionInfo, endpoint schemaEndpoint) (chSession, error) {
	if session == nil {
		return chSession{}, errors.New("Not authenticated")
	}
	if endpoint.ConnectionID == "" || strings.TrimSpace(endpoint.Database) == "" || len(endpoint.Database) > 1024 || strings.IndexByte(endpoint.Database, 0) >= 0 {
		return chSession{}, errors.New("A connection and database are required for each side")
	}
	if endpoint.Username != "" {
		return chSession{connID: endpoint.ConnectionID, user: endpoint.Username, password: endpoint.Password}, nil
	}
	// Matching ClickHouse usernames on two connections do not prove identity.
	// Never search active sessions or fall back to a background service account.
	if endpoint.ConnectionID != session.ConnectionID {
		return chSession{}, errors.New("Supply your ClickHouse username and password for the other connection")
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		return chSession{}, errors.New("Unable to read your session credentials; sign in again")
	}
	return chSession{connID: session.ConnectionID, user: session.ClickhouseUser, password: password}, nil
}

func (h *SchemaCompareHandler) Compare(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, 401, "Not authenticated")
		return
	}
	var request struct {
		Source schemaEndpoint `json:"source"`
		Target schemaEndpoint `json:"target"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, 400, "Invalid schema comparison request")
		return
	}
	sourceCredentials, err := h.credentials(session, request.Source)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	targetCredentials, err := h.credentials(session, request.Target)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	for _, endpoint := range []schemaEndpoint{request.Source, request.Target} {
		conn, err := h.DB.GetConnectionByID(endpoint.ConnectionID)
		if err != nil {
			writeError(w, 500, "Failed to load connection")
			return
		}
		if conn == nil {
			writeError(w, 404, "Connection not found")
			return
		}
		if h.Gateway == nil || !h.Gateway.IsTunnelOnline(conn.ID) {
			writeError(w, 503, "Both connections must be online to compare schemas")
			return
		}
	}
	source, err := h.snapshot(r.Context(), sourceCredentials, request.Source.Database)
	if err != nil {
		writeError(w, 502, "Source: "+err.Error())
		return
	}
	target, err := h.snapshot(r.Context(), targetCredentials, request.Target.Database)
	if err != nil {
		writeError(w, 502, "Target: "+err.Error())
		return
	}
	result := schemacompare.Compare(source, target, request.Target.Database)
	writeJSON(w, 200, map[string]any{
		"source":      map[string]string{"connection_id": request.Source.ConnectionID, "database": request.Source.Database},
		"target":      map[string]string{"connection_id": request.Target.ConnectionID, "database": request.Target.Database},
		"captured_at": time.Now().UTC().Format(time.RFC3339), "result": result,
		"scope": "Only objects visible to the supplied accounts are compared. Engine arguments, table settings, grants, dictionaries and data are excluded. Review all SQL and dependencies before applying changes.",
	})
}

func (h *SchemaCompareHandler) snapshot(ctx context.Context, credentials chSession, db string) ([]schemacompare.Table, error) {
	exec := func(sql string, dst any) error {
		result, err := h.Gateway.ExecuteQueryWithSettingsCtx(ctx, credentials.connID, sql, credentials.user, credentials.password, map[string]string{
			"readonly": "1", "max_execution_time": "15", "max_result_rows": "10001", "max_result_bytes": "16777216", "result_overflow_mode": "throw", "output_format_json_quote_64bit_integers": "0", "param_database": db,
		}, 20*time.Second)
		// Avoid returning remote SQL/connection errors which may contain secrets.
		if err != nil {
			return errors.New("Unable to read metadata; check credentials, database visibility and SELECT access to system.databases, system.tables and system.columns")
		}
		if result == nil || !strings.HasPrefix(strings.TrimSpace(string(result.Data)), "[") || json.Unmarshal(result.Data, dst) != nil {
			return errors.New("ClickHouse returned invalid schema metadata")
		}
		return nil
	}
	var databases []struct {
		Name string `json:"name"`
	}
	if err := exec("SELECT name FROM system.databases WHERE name = {database:String}", &databases); err != nil {
		return nil, err
	}
	if len(databases) != 1 {
		return nil, errors.New("Database does not exist or is not visible to this account")
	}
	var tables []struct {
		schemacompare.Table
		CreateQuery string `json:"create_table_query"`
	}
	if err := exec("SELECT name, engine, sorting_key, partition_key, primary_key, sampling_key, create_table_query FROM system.tables WHERE database = {database:String} AND is_temporary = 0 ORDER BY name LIMIT 10001", &tables); err != nil {
		return nil, err
	}
	if len(tables) > 10000 {
		return nil, errors.New("Database exceeds the 10,000 table comparison limit; compare a smaller database")
	}
	var columns []schemacompare.Column
	if err := exec("SELECT table, name, type, position, default_kind, default_expression, compression_codec FROM system.columns WHERE database = {database:String} ORDER BY table, position LIMIT 10001", &columns); err != nil {
		return nil, err
	}
	if len(columns) > 10000 {
		return nil, errors.New("Database exceeds the 10,000 column comparison limit; compare a smaller database")
	}
	byTable := make(map[string][]schemacompare.Column)
	for _, column := range columns {
		byTable[column.Table] = append(byTable[column.Table], column)
	}
	out := make([]schemacompare.Table, 0, len(tables))
	for _, row := range tables {
		if row.Name == "" || row.Engine == "" {
			return nil, fmt.Errorf("Incomplete table metadata; refresh the comparison")
		}
		row.Table.CreateSQL = row.CreateQuery
		row.Table.Columns = byTable[row.Name]
		out = append(out, row.Table)
	}
	return out, nil
}
