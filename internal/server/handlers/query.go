package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

const maxQueryTimeout = 5 * time.Minute

// QueryHandler handles SQL query execution and schema exploration endpoints.
type QueryHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

// Routes registers all query-related routes on the given chi.Router.
func (h *QueryHandler) Routes(r chi.Router) {
	r.Post("/", h.ExecuteQuery)
	r.Post("/run", h.ExecuteQuery)
	r.Post("/stream", h.StreamQuery)
	r.Post("/sample", h.SampleQuery)
	r.Post("/explorer-data", h.ExplorerData)
	r.Post("/format", h.FormatSQL)
	r.Post("/explain", h.ExplainQuery)
	r.Post("/plan", h.QueryPlan)
	r.Post("/profile", h.QueryProfile)
	r.Get("/databases", h.ListDatabases)
	r.Get("/tables", h.ListTables)
	r.Get("/columns", h.ListColumns)
	r.Get("/data-types", h.ListDataTypes)
	r.Get("/clusters", h.ListClusters)
	r.Post("/schema/database", h.CreateDatabase)
	r.Post("/schema/database/drop", h.DropDatabase)
	r.Post("/schema/table", h.CreateTable)
	r.Post("/schema/table/drop", h.DropTable)
	r.Post("/upload/discover", h.DiscoverUploadSchema)
	r.Post("/upload/ingest", h.IngestUpload)
	r.Get("/host-info", h.GetHostInfo)
	r.Get("/completions", h.ListCompletions)
}

// --- Request / Response types ---

type executeQueryRequest struct {
	Query   string `json:"query"`
	Timeout int    `json:"timeout"` // seconds
}

type executeQueryResponse struct {
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data,omitempty"`
	Meta       json.RawMessage `json:"meta,omitempty"`
	Statistics json.RawMessage `json:"statistics,omitempty"`
	Rows       int             `json:"rows"`
	ElapsedMS  int64           `json:"elapsed_ms"`
}

type formatRequest struct {
	Query string `json:"query"`
}

type formatResponse struct {
	Formatted string `json:"formatted"`
}

type explainRequest struct {
	Query string `json:"query"`
}

type sampleRequest struct {
	Query    string `json:"query"`
	PerShard int    `json:"per_shard"`
	ShardBy  string `json:"shard_by"`
	Timeout  int    `json:"timeout"`
}

type planNode struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id,omitempty"`
	Level    int     `json:"level"`
	Label    string  `json:"label"`
}

type createDatabaseRequest struct {
	Name        string `json:"name"`
	Engine      string `json:"engine"`
	OnCluster   string `json:"on_cluster"`
	IfNotExists *bool  `json:"if_not_exists"`
}

type dropDatabaseRequest struct {
	Name      string `json:"name"`
	OnCluster string `json:"on_cluster"`
	IfExists  *bool  `json:"if_exists"`
	Sync      bool   `json:"sync"`
}

type createTableColumn struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	DefaultExpression string `json:"default_expression"`
	Comment           string `json:"comment"`
}

type createTableRequest struct {
	Database    string              `json:"database"`
	Name        string              `json:"name"`
	Engine      string              `json:"engine"`
	OnCluster   string              `json:"on_cluster"`
	IfNotExists *bool               `json:"if_not_exists"`
	Columns     []createTableColumn `json:"columns"`
	OrderBy     string              `json:"order_by"`
	PartitionBy string              `json:"partition_by"`
	PrimaryKey  string              `json:"primary_key"`
	SampleBy    string              `json:"sample_by"`
	TTL         string              `json:"ttl"`
	Settings    string              `json:"settings"`
	Comment     string              `json:"comment"`
}

type dropTableRequest struct {
	Database  string `json:"database"`
	Name      string `json:"name"`
	OnCluster string `json:"on_cluster"`
	IfExists  *bool  `json:"if_exists"`
	Sync      bool   `json:"sync"`
}

// --- Handlers ---

// ExecuteQuery handles POST / and POST /run.
func (h *QueryHandler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req executeQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	// Determine timeout
	timeout := 30 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	if timeout > maxQueryTimeout {
		timeout = maxQueryTimeout
	}

	// Decrypt password
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	// Execute query via tunnel
	start := time.Now()
	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
		timeout,
	)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		slog.Warn("Query execution failed", "error", err, "connection", session.ConnectionID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Count rows from data
	rows := countRows(result.Data)

	// Audit log (truncate query preview to 100 chars)
	preview := query
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	go func() {
		ip := r.RemoteAddr
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:       "query.execute",
			Username:     strPtr(session.ClickhouseUser),
			ConnectionID: strPtr(session.ConnectionID),
			Details:      strPtr(preview),
			IPAddress:    strPtr(ip),
		})
	}()

	resp := executeQueryResponse{
		Success:    true,
		Data:       result.Data,
		Meta:       result.Meta,
		Statistics: result.Stats,
		Rows:       rows,
		ElapsedMS:  elapsed,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// FormatSQL handles POST /format.
func (h *QueryHandler) FormatSQL(w http.ResponseWriter, r *http.Request) {
	var req formatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	formatted := formatSQL(query)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(formatResponse{Formatted: formatted})
}

// ExplainQuery handles POST /explain.
func (h *QueryHandler) ExplainQuery(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req explainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	explainSQL := "EXPLAIN " + query

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		explainSQL,
		session.ClickhouseUser,
		password,
		30*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    result.Data,
		"meta":    result.Meta,
	})
}

// QueryPlan handles POST /plan and returns a parsed plan tree for visualization.
func (h *QueryHandler) QueryPlan(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req explainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	candidates := []struct {
		source string
		sql    string
	}{
		{source: "plan", sql: "EXPLAIN PLAN " + query},
		{source: "ast", sql: "EXPLAIN AST " + query},
		{source: "generic", sql: "EXPLAIN " + query},
	}

	var lastErr error
	for _, candidate := range candidates {
		result, execErr := h.Gateway.ExecuteQuery(
			session.ConnectionID,
			candidate.sql,
			session.ClickhouseUser,
			password,
			45*time.Second,
		)
		if execErr != nil {
			lastErr = execErr
			continue
		}

		lines := extractExplainLines(result.Data)
		if len(lines) == 0 {
			continue
		}

		nodes := buildPlanTree(lines)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"source":  candidate.source,
			"lines":   lines,
			"nodes":   nodes,
		})
		return
	}

	if lastErr != nil {
		writeError(w, http.StatusBadGateway, lastErr.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "No plan information returned by ClickHouse")
}

// SampleQuery handles POST /sample and returns first N rows per shard when available.
func (h *QueryHandler) SampleQuery(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req sampleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}
	if !isReadOnlyQuery(query) {
		writeError(w, http.StatusBadRequest, "Sampling only supports read-only SELECT/WITH queries")
		return
	}

	perShard := req.PerShard
	if perShard <= 0 {
		perShard = 25
	}
	if perShard > 500 {
		perShard = 500
	}

	shardBy := strings.TrimSpace(req.ShardBy)
	if shardBy == "" {
		shardBy = "_shard_num"
	}
	timeout := 30 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	if timeout > maxQueryTimeout {
		timeout = maxQueryTimeout
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	base := stripFormatClause(stripTrailingSemicolon(query))
	perShardSQL := fmt.Sprintf(
		"SELECT * FROM (%s) AS __ch_ui_sample LIMIT %d BY %s",
		base,
		perShard,
		escapeIdentifier(shardBy),
	)

	start := time.Now()
	result, runErr := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		perShardSQL,
		session.ClickhouseUser,
		password,
		timeout,
	)
	elapsed := time.Since(start).Milliseconds()

	samplingMode := "per_shard"
	warning := ""

	if runErr != nil && shouldFallbackToGlobalSample(runErr.Error()) {
		// Fallback for local/non-distributed queries where _shard_num is unavailable.
		fallbackSQL := fmt.Sprintf(
			"SELECT * FROM (%s) AS __ch_ui_sample LIMIT %d",
			base,
			perShard,
		)

		start = time.Now()
		result, runErr = h.Gateway.ExecuteQuery(
			session.ConnectionID,
			fallbackSQL,
			session.ClickhouseUser,
			password,
			timeout,
		)
		elapsed = time.Since(start).Milliseconds()
		samplingMode = "global"
		warning = "Shard virtual column not available for this query; returned global sample instead."
	}

	if runErr != nil {
		writeError(w, http.StatusBadGateway, runErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"data":          result.Data,
		"meta":          result.Meta,
		"statistics":    result.Stats,
		"rows":          countRows(result.Data),
		"elapsed_ms":    elapsed,
		"sampling_mode": samplingMode,
		"warning":       warning,
	})
}

// QueryProfile handles POST /profile and returns latest query_log metrics for the exact SQL.
func (h *QueryHandler) QueryProfile(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req explainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	escapedQuery := escapeLiteral(stripTrailingSemicolon(query))
	escapedUser := escapeLiteral(session.ClickhouseUser)

	profileSQL := fmt.Sprintf(`SELECT
  query_duration_ms,
  read_rows,
  read_bytes,
  result_rows,
  result_bytes,
  memory_usage,
  ProfileEvents['SelectedRows'] AS selected_rows,
  ProfileEvents['SelectedBytes'] AS selected_bytes,
  ProfileEvents['SelectedMarks'] AS selected_marks
FROM system.query_log
WHERE type = 'QueryFinish'
  AND query = '%s'
  AND user = '%s'
ORDER BY event_time DESC
LIMIT 1`, escapedQuery, escapedUser)

	result, execErr := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		profileSQL,
		session.ClickhouseUser,
		password,
		10*time.Second,
	)
	if execErr != nil {
		// query_log may be unavailable depending on ClickHouse config/version.
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"available": false,
			"reason":    "system.query_log is unavailable for this connection",
		})
		return
	}

	rows := decodeRows(result.Data)
	if len(rows) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"available": false,
			"reason":    "No query profile row found yet (query_log flush can be delayed)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"available": true,
		"profile":   rows[0],
	})
}

// StreamQuery handles POST /stream — streaming query execution via NDJSON chunked response.
func (h *QueryHandler) StreamQuery(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req executeQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	requestID, stream, err := h.Gateway.ExecuteStreamQuery(
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer h.Gateway.CleanupStream(session.ConnectionID, requestID)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	ctx := r.Context()

	// Wait for meta or error
	select {
	case meta := <-stream.MetaCh:
		enc.Encode(map[string]interface{}{"type": "meta", "meta": meta})
		flusher.Flush()
	case err := <-stream.ErrorCh:
		enc.Encode(map[string]interface{}{"type": "error", "error": err.Error()})
		flusher.Flush()
		return
	case <-ctx.Done():
		return
	}

	// Read chunks until channel is closed or client disconnects
	seq := 0
	for {
		select {
		case chunk, ok := <-stream.ChunkCh:
			if !ok {
				goto streamDone
			}
			enc.Encode(map[string]interface{}{"type": "chunk", "data": chunk, "seq": seq})
			flusher.Flush()
			seq++
		case <-ctx.Done():
			return
		}
	}

streamDone:
	// ChunkCh closed — read final done or error
	select {
	case donePayload := <-stream.DoneCh:
		var done tunnel.StreamDone
		json.Unmarshal(donePayload, &done)
		enc.Encode(map[string]interface{}{
			"type":       "done",
			"statistics": done.Statistics,
			"total_rows": done.TotalRows,
		})
		flusher.Flush()
	case err := <-stream.ErrorCh:
		enc.Encode(map[string]interface{}{"type": "error", "error": err.Error()})
		flusher.Flush()
	case <-ctx.Done():
		return
	}

	// Audit log
	preview := query
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	go func() {
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:       "query.stream",
			Username:     strPtr(session.ClickhouseUser),
			ConnectionID: strPtr(session.ConnectionID),
			Details:      strPtr(preview),
			IPAddress:    strPtr(r.RemoteAddr),
		})
	}()
}

// ExplorerData handles POST /explorer-data — server-side paginated data browsing.
func (h *QueryHandler) ExplorerData(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req struct {
		Database   string `json:"database"`
		Table      string `json:"table"`
		Page       int    `json:"page"`
		PageSize   int    `json:"page_size"`
		SortColumn string `json:"sort_column"`
		SortDir    string `json:"sort_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Database == "" || req.Table == "" {
		writeError(w, http.StatusBadRequest, "database and table are required")
		return
	}
	if req.PageSize <= 0 || req.PageSize > 1000 {
		req.PageSize = 100
	}
	if req.Page < 0 {
		req.Page = 0
	}

	sortDir := "ASC"
	if strings.EqualFold(req.SortDir, "desc") {
		sortDir = "DESC"
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	// Build data query with LIMIT/OFFSET
	offset := req.Page * req.PageSize
	dataSQL := fmt.Sprintf("SELECT * FROM %s.%s", escapeIdentifier(req.Database), escapeIdentifier(req.Table))
	if req.SortColumn != "" {
		dataSQL += fmt.Sprintf(" ORDER BY %s %s", escapeIdentifier(req.SortColumn), sortDir)
	}
	dataSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", req.PageSize, offset)

	// Build count query
	countSQL := fmt.Sprintf("SELECT count() FROM %s.%s", escapeIdentifier(req.Database), escapeIdentifier(req.Table))

	// Execute data query (JSONCompact — positional arrays, smaller payload)
	dataRaw, err := h.Gateway.ExecuteQueryWithFormat(
		session.ConnectionID, dataSQL, session.ClickhouseUser, password, "JSONCompact", 30*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Execute count query
	countRaw, err := h.Gateway.ExecuteQueryWithFormat(
		session.ConnectionID, countSQL, session.ClickhouseUser, password, "JSONCompact", 30*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Parse JSONCompact data result
	var dataCompact struct {
		Meta json.RawMessage `json:"meta"`
		Data json.RawMessage `json:"data"`
		Rows int             `json:"rows"`
	}
	json.Unmarshal(dataRaw, &dataCompact)

	// Parse count result — JSONCompact: {"data":[[12345]]}
	var totalRows int64
	var countCompact struct {
		Data [][]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(countRaw, &countCompact); err == nil &&
		len(countCompact.Data) > 0 && len(countCompact.Data[0]) > 0 {
		var v interface{}
		if json.Unmarshal(countCompact.Data[0][0], &v) == nil {
			switch n := v.(type) {
			case float64:
				totalRows = int64(n)
			case string:
				fmt.Sscanf(n, "%d", &totalRows)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"meta":       dataCompact.Meta,
		"data":       dataCompact.Data,
		"rows":       dataCompact.Rows,
		"total_rows": totalRows,
		"page":       req.Page,
		"page_size":  req.PageSize,
	})
}

// ListDatabases handles GET /databases.
func (h *QueryHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		"SHOW DATABASES",
		session.ClickhouseUser,
		password,
		30*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	names := extractNames(result.Data)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"databases": names,
	})
}

// ListTables handles GET /tables?database=X.
func (h *QueryHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	db := r.URL.Query().Get("database")
	if db == "" {
		writeError(w, http.StatusBadRequest, "database query parameter is required")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	query := fmt.Sprintf("SHOW TABLES FROM %s", escapeIdentifier(db))

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
		30*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	names := extractNames(result.Data)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"tables":  names,
	})
}

// ListColumns handles GET /columns?database=X&table=Y.
func (h *QueryHandler) ListColumns(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	dbName := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	if dbName == "" || table == "" {
		writeError(w, http.StatusBadRequest, "database and table query parameters are required")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	query := fmt.Sprintf("DESCRIBE TABLE %s.%s", escapeIdentifier(dbName), escapeIdentifier(table))

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
		30*time.Second,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"columns": result.Data,
		"meta":    result.Meta,
	})
}

// ListDataTypes handles GET /data-types.
func (h *QueryHandler) ListDataTypes(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		"SELECT name FROM system.data_type_families WHERE is_parametric = 0 ORDER BY name",
		session.ClickhouseUser,
		password,
		15*time.Second,
	)
	if err != nil {
		// Fallback for older ClickHouse versions where is_parametric might not exist.
		slog.Warn("Failed to list non-parametric data types; trying fallback query", "error", err, "connection", session.ConnectionID)
		result, err = h.Gateway.ExecuteQuery(
			session.ConnectionID,
			"SELECT name FROM system.data_type_families ORDER BY name",
			session.ClickhouseUser,
			password,
			15*time.Second,
		)
		if err != nil {
			slog.Warn("Failed to list data types; returning empty list", "error", err, "connection", session.ConnectionID)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":    true,
				"data_types": []string{},
			})
			return
		}
	}

	rawTypes := extractNames(result.Data)
	uniq := make(map[string]struct{}, len(rawTypes))
	types := make([]string, 0, len(rawTypes))
	for _, t := range rawTypes {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, exists := uniq[t]; exists {
			continue
		}
		uniq[t] = struct{}{}
		types = append(types, t)
	}
	sort.Strings(types)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"data_types": types,
	})
}

// ListClusters handles GET /clusters.
func (h *QueryHandler) ListClusters(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		"SELECT DISTINCT cluster FROM system.clusters WHERE cluster != '' ORDER BY cluster",
		session.ClickhouseUser,
		password,
		15*time.Second,
	)
	if err != nil {
		// Some deployments/users cannot read system.clusters; return an empty list instead of hard failing UI.
		slog.Warn("Failed to list clusters; returning empty list", "error", err, "connection", session.ConnectionID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"clusters": []string{},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"clusters": extractNames(result.Data),
	})
}

// CreateDatabase handles POST /schema/database.
func (h *QueryHandler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	session := h.requireSchemaAdmin(w, r)
	if session == nil {
		return
	}

	var req createDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if err := validateSimpleObjectName(name, "database"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if isSystemDatabaseName(name) {
		writeError(w, http.StatusBadRequest, "Cannot create reserved system database")
		return
	}

	engine := strings.TrimSpace(req.Engine)
	if engine == "" {
		engine = "Atomic"
	}
	if isUnsafeSQLFragment(engine) {
		writeError(w, http.StatusBadRequest, "Invalid engine expression")
		return
	}

	cluster := strings.TrimSpace(req.OnCluster)
	if cluster != "" && isUnsafeSQLFragment(cluster) {
		writeError(w, http.StatusBadRequest, "Invalid cluster name")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	ifNotExists := req.IfNotExists == nil || *req.IfNotExists

	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("CREATE DATABASE ")
	if ifNotExists {
		sqlBuilder.WriteString("IF NOT EXISTS ")
	}
	sqlBuilder.WriteString(escapeIdentifier(name))
	if cluster != "" {
		sqlBuilder.WriteString(" ON CLUSTER ")
		sqlBuilder.WriteString(escapeIdentifier(cluster))
	}
	sqlBuilder.WriteString(" ENGINE = ")
	sqlBuilder.WriteString(engine)

	sql := sqlBuilder.String()
	if _, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		sql,
		session.ClickhouseUser,
		password,
		45*time.Second,
	); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s\n\nCommand:\n%s", err.Error(), sql))
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "schema.database.create",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("database=%s engine=%s cluster=%s", name, engine, cluster)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"database": name,
	})
}

// DropDatabase handles POST /schema/database/drop.
func (h *QueryHandler) DropDatabase(w http.ResponseWriter, r *http.Request) {
	session := h.requireSchemaAdmin(w, r)
	if session == nil {
		return
	}

	var req dropDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if err := validateSimpleObjectName(name, "database"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if isSystemDatabaseName(name) {
		writeError(w, http.StatusBadRequest, "Cannot drop system database")
		return
	}
	cluster := strings.TrimSpace(req.OnCluster)
	if cluster != "" && isUnsafeSQLFragment(cluster) {
		writeError(w, http.StatusBadRequest, "Invalid cluster name")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	ifExists := req.IfExists == nil || *req.IfExists

	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("DROP DATABASE ")
	if ifExists {
		sqlBuilder.WriteString("IF EXISTS ")
	}
	sqlBuilder.WriteString(escapeIdentifier(name))
	if cluster != "" {
		sqlBuilder.WriteString(" ON CLUSTER ")
		sqlBuilder.WriteString(escapeIdentifier(cluster))
	}
	if req.Sync {
		sqlBuilder.WriteString(" SYNC")
	}

	sql := sqlBuilder.String()
	if _, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		sql,
		session.ClickhouseUser,
		password,
		45*time.Second,
	); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "schema.database.drop",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("database=%s cluster=%s sync=%t", name, cluster, req.Sync)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"database": name,
	})
}

// CreateTable handles POST /schema/table.
func (h *QueryHandler) CreateTable(w http.ResponseWriter, r *http.Request) {
	session := h.requireSchemaAdmin(w, r)
	if session == nil {
		return
	}

	var req createTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	dbName := strings.TrimSpace(req.Database)
	tableName := strings.TrimSpace(req.Name)
	if err := validateSimpleObjectName(dbName, "database"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateSimpleObjectName(tableName, "table"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if isSystemDatabaseName(dbName) {
		writeError(w, http.StatusBadRequest, "Cannot create tables in system databases")
		return
	}

	if len(req.Columns) == 0 {
		writeError(w, http.StatusBadRequest, "At least one column is required")
		return
	}

	engine := strings.TrimSpace(req.Engine)
	if engine == "" {
		engine = "MergeTree"
	}
	if isUnsafeSQLFragment(engine) {
		writeError(w, http.StatusBadRequest, "Invalid engine expression")
		return
	}

	cluster := strings.TrimSpace(req.OnCluster)
	if cluster != "" && isUnsafeSQLFragment(cluster) {
		writeError(w, http.StatusBadRequest, "Invalid cluster name")
		return
	}

	columnsSQL := make([]string, 0, len(req.Columns))
	for i, col := range req.Columns {
		colName := strings.TrimSpace(col.Name)
		colType := strings.TrimSpace(col.Type)
		if err := validateSimpleObjectName(colName, fmt.Sprintf("column #%d", i+1)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if colType == "" || isUnsafeSQLFragment(colType) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid type for column %q", colName))
			return
		}

		part := escapeIdentifier(colName) + " " + colType
		if def := strings.TrimSpace(col.DefaultExpression); def != "" {
			if isUnsafeSQLFragment(def) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid default expression for column %q", colName))
				return
			}
			part += " DEFAULT " + def
		}
		if comment := strings.TrimSpace(col.Comment); comment != "" {
			part += " COMMENT '" + escapeLiteral(comment) + "'"
		}
		columnsSQL = append(columnsSQL, part)
	}

	orderBy := strings.TrimSpace(req.OrderBy)
	partitionBy := strings.TrimSpace(req.PartitionBy)
	primaryKey := strings.TrimSpace(req.PrimaryKey)
	sampleBy := strings.TrimSpace(req.SampleBy)
	ttl := strings.TrimSpace(req.TTL)
	settings := strings.TrimSpace(req.Settings)
	comment := strings.TrimSpace(req.Comment)

	expressions := []struct {
		name  string
		value string
	}{
		{name: "order_by", value: orderBy},
		{name: "partition_by", value: partitionBy},
		{name: "primary_key", value: primaryKey},
		{name: "sample_by", value: sampleBy},
		{name: "ttl", value: ttl},
		{name: "settings", value: settings},
	}
	for _, expr := range expressions {
		if expr.value != "" && isUnsafeSQLFragment(expr.value) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid %s expression", expr.name))
			return
		}
	}

	if strings.Contains(strings.ToLower(engine), "mergetree") && orderBy == "" {
		orderBy = "tuple()"
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	ifNotExists := req.IfNotExists == nil || *req.IfNotExists

	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("CREATE TABLE ")
	if ifNotExists {
		sqlBuilder.WriteString("IF NOT EXISTS ")
	}
	sqlBuilder.WriteString(escapeIdentifier(dbName))
	sqlBuilder.WriteString(".")
	sqlBuilder.WriteString(escapeIdentifier(tableName))
	if cluster != "" {
		sqlBuilder.WriteString(" ON CLUSTER ")
		sqlBuilder.WriteString(escapeIdentifier(cluster))
	}
	sqlBuilder.WriteString(" (\n  ")
	sqlBuilder.WriteString(strings.Join(columnsSQL, ",\n  "))
	sqlBuilder.WriteString("\n)")
	sqlBuilder.WriteString("\nENGINE = ")
	sqlBuilder.WriteString(engine)
	if partitionBy != "" {
		sqlBuilder.WriteString("\nPARTITION BY ")
		sqlBuilder.WriteString(partitionBy)
	}
	if orderBy != "" {
		sqlBuilder.WriteString("\nORDER BY ")
		sqlBuilder.WriteString(orderBy)
	}
	if primaryKey != "" {
		sqlBuilder.WriteString("\nPRIMARY KEY ")
		sqlBuilder.WriteString(primaryKey)
	}
	if sampleBy != "" {
		sqlBuilder.WriteString("\nSAMPLE BY ")
		sqlBuilder.WriteString(sampleBy)
	}
	if ttl != "" {
		sqlBuilder.WriteString("\nTTL ")
		sqlBuilder.WriteString(ttl)
	}
	if settings != "" {
		sqlBuilder.WriteString("\nSETTINGS ")
		sqlBuilder.WriteString(settings)
	}
	if comment != "" {
		sqlBuilder.WriteString("\nCOMMENT '")
		sqlBuilder.WriteString(escapeLiteral(comment))
		sqlBuilder.WriteString("'")
	}

	sql := sqlBuilder.String()
	if _, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		sql,
		session.ClickhouseUser,
		password,
		45*time.Second,
	); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "schema.table.create",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("table=%s.%s engine=%s cluster=%s", dbName, tableName, engine, cluster)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"database": dbName,
		"table":    tableName,
		"command":  sql,
	})
}

// DropTable handles POST /schema/table/drop.
func (h *QueryHandler) DropTable(w http.ResponseWriter, r *http.Request) {
	session := h.requireSchemaAdmin(w, r)
	if session == nil {
		return
	}

	var req dropTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	dbName := strings.TrimSpace(req.Database)
	tableName := strings.TrimSpace(req.Name)
	if err := validateSimpleObjectName(dbName, "database"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateSimpleObjectName(tableName, "table"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if isSystemDatabaseName(dbName) {
		writeError(w, http.StatusBadRequest, "Cannot drop tables from system databases")
		return
	}

	cluster := strings.TrimSpace(req.OnCluster)
	if cluster != "" && isUnsafeSQLFragment(cluster) {
		writeError(w, http.StatusBadRequest, "Invalid cluster name")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	ifExists := req.IfExists == nil || *req.IfExists

	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("DROP TABLE ")
	if ifExists {
		sqlBuilder.WriteString("IF EXISTS ")
	}
	sqlBuilder.WriteString(escapeIdentifier(dbName))
	sqlBuilder.WriteString(".")
	sqlBuilder.WriteString(escapeIdentifier(tableName))
	if cluster != "" {
		sqlBuilder.WriteString(" ON CLUSTER ")
		sqlBuilder.WriteString(escapeIdentifier(cluster))
	}
	if req.Sync {
		sqlBuilder.WriteString(" SYNC")
	}

	sql := sqlBuilder.String()
	if _, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		sql,
		session.ClickhouseUser,
		password,
		45*time.Second,
	); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "schema.table.drop",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("table=%s.%s cluster=%s sync=%t", dbName, tableName, cluster, req.Sync)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"database": dbName,
		"table":    tableName,
	})
}

// GetHostInfo handles GET /host-info.
func (h *QueryHandler) GetHostInfo(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	info, err := h.DB.GetConnectionHostInfo(session.ConnectionID)
	if err != nil {
		slog.Error("Failed to get host info", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve host info")
		return
	}

	if info == nil {
		writeError(w, http.StatusNotFound, "Host info not available")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"host_info": info,
	})
}

// ListCompletions handles GET /completions — returns ClickHouse functions and keywords for autocomplete.
func (h *QueryHandler) ListCompletions(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	// Fetch functions
	fnResult, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		"SELECT name FROM system.functions",
		session.ClickhouseUser,
		password,
		15*time.Second,
	)

	functions := []string{}
	if err == nil {
		functions = extractNames(fnResult.Data)
	}

	// Fetch keywords
	kwResult, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		"SELECT keyword FROM system.keywords",
		session.ClickhouseUser,
		password,
		15*time.Second,
	)

	keywords := []string{}
	if err == nil {
		keywords = extractNames(kwResult.Data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"functions": functions,
		"keywords":  keywords,
	})
}

// --- Helpers ---

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// escapeIdentifier wraps a SQL identifier in backticks and escapes any inner backticks.
func escapeIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, "`", "``")
	return "`" + escaped + "`"
}

// escapeLiteral escapes single quotes and backslashes for SQL string literals.
func escapeLiteral(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "\\'")
}

func stripTrailingSemicolon(query string) string {
	return strings.TrimRight(query, " \n\t;")
}

func stripFormatClause(query string) string {
	re := regexp.MustCompile(`(?is)\s+FORMAT\s+\w+\s*$`)
	return strings.TrimSpace(re.ReplaceAllString(query, ""))
}

func isReadOnlyQuery(query string) bool {
	re := regexp.MustCompile(`(?is)^\s*(SELECT|WITH|SHOW|DESC|DESCRIBE|EXPLAIN)\b`)
	return re.MatchString(query)
}

func (h *QueryHandler) requireSchemaAdmin(w http.ResponseWriter, r *http.Request) *middleware.SessionInfo {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil
	}

	isAdmin, err := h.DB.IsUserRole(session.ClickhouseUser, "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Role check failed")
		return nil
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "Admin role required for schema changes")
		return nil
	}
	return session
}

func validateSimpleObjectName(name string, label string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s name is required", label)
	}
	if strings.Contains(name, ".") {
		return fmt.Errorf("%s name cannot contain '.'", label)
	}
	if strings.ContainsAny(name, "\x00\r\n\t") {
		return fmt.Errorf("%s name contains invalid control characters", label)
	}
	return nil
}

func isUnsafeSQLFragment(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" {
		return false
	}
	return strings.Contains(v, ";") ||
		strings.Contains(v, "--") ||
		strings.Contains(v, "/*") ||
		strings.Contains(v, "*/")
}

func isSystemDatabaseName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "system", "information_schema":
		return true
	default:
		return false
	}
}

func shouldFallbackToGlobalSample(message string) bool {
	msg := strings.ToLower(message)
	return strings.Contains(msg, "_shard_num") ||
		strings.Contains(msg, "unknown identifier") ||
		strings.Contains(msg, "unknown column")
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

func decodeRows(data json.RawMessage) []map[string]interface{} {
	if len(data) == 0 {
		return nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(data, &rows); err == nil {
		return rows
	}

	// Fallback for positional arrays (rare on this path)
	var arrRows [][]interface{}
	if err := json.Unmarshal(data, &arrRows); err == nil {
		normalized := make([]map[string]interface{}, 0, len(arrRows))
		for _, row := range arrRows {
			obj := map[string]interface{}{"value": row}
			normalized = append(normalized, obj)
		}
		return normalized
	}
	return nil
}

func extractExplainLines(data json.RawMessage) []string {
	rows := decodeRows(data)
	if len(rows) == 0 {
		return nil
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		// Common column names in EXPLAIN output first.
		priority := []string{"explain", "plan", "explain_plan", "explain_ast", "value"}
		picked := ""
		for _, key := range priority {
			if v, ok := row[key]; ok {
				picked = fmt.Sprint(v)
				break
			}
		}
		if picked == "" {
			keys := make([]string, 0, len(row))
			for k := range row {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			picked = fmt.Sprint(row[keys[0]])
		}
		picked = strings.TrimSpace(picked)
		if picked != "" {
			lines = append(lines, picked)
		}
	}
	return lines
}

func buildPlanTree(lines []string) []planNode {
	nodes := make([]planNode, 0, len(lines))
	stack := make([]string, 0, 16)

	for i, line := range lines {
		level := planLineLevel(line)
		label := cleanPlanLabel(line)
		if label == "" {
			continue
		}
		if level < 0 {
			level = 0
		}
		if level > len(stack) {
			level = len(stack)
		}

		id := fmt.Sprintf("n%d", i+1)
		var parentID *string
		if level > 0 && level-1 < len(stack) {
			parent := stack[level-1]
			parentID = &parent
		}

		if level == len(stack) {
			stack = append(stack, id)
		} else {
			stack[level] = id
			stack = stack[:level+1]
		}

		nodes = append(nodes, planNode{
			ID:       id,
			ParentID: parentID,
			Level:    level,
			Label:    label,
		})
	}
	return nodes
}

func planLineLevel(line string) int {
	level := 0
	runes := []rune(line)
	for i := 0; i < len(runes); {
		if i+1 < len(runes) && runes[i] == ' ' && runes[i+1] == ' ' {
			level++
			i += 2
			continue
		}
		if i+1 < len(runes) && runes[i] == '│' && runes[i+1] == ' ' {
			level++
			i += 2
			continue
		}
		if runes[i] == ' ' || runes[i] == '│' {
			i++
			continue
		}
		break
	}
	return level
}

func cleanPlanLabel(line string) string {
	label := strings.TrimSpace(line)
	label = strings.TrimLeft(label, "│ ")
	label = strings.TrimPrefix(label, "└─")
	label = strings.TrimPrefix(label, "├─")
	label = strings.TrimPrefix(label, "─")
	return strings.TrimSpace(label)
}

// countRows attempts to determine the number of rows in a JSON data payload.
// The data is expected to be a JSON array.
func countRows(data json.RawMessage) int {
	if len(data) == 0 {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return 0
	}
	return len(arr)
}

// extractNames extracts the first string value from each object in a JSON array.
// Used for SHOW DATABASES and SHOW TABLES results where each row has a single column.
func extractNames(data json.RawMessage) []string {
	if len(data) == 0 {
		return []string{}
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(data, &rows); err != nil {
		return []string{}
	}

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		// Take the first (and typically only) value from the row
		for _, v := range row {
			if s, ok := v.(string); ok {
				names = append(names, s)
				break
			}
		}
	}
	return names
}

// formatSQL performs basic SQL formatting: uppercases keywords and adds newlines
// before major clauses.
func formatSQL(sql string) string {
	// Uppercase SQL keywords
	keywords := []string{
		"SELECT", "FROM", "WHERE", "AND", "OR", "ORDER BY", "GROUP BY",
		"HAVING", "LIMIT", "OFFSET", "JOIN", "LEFT JOIN", "RIGHT JOIN",
		"INNER JOIN", "OUTER JOIN", "FULL JOIN", "CROSS JOIN",
		"ON", "AS", "IN", "NOT", "NULL", "IS", "BETWEEN", "LIKE",
		"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE",
		"CREATE", "TABLE", "ALTER", "DROP", "INDEX",
		"DISTINCT", "UNION", "ALL", "EXISTS", "CASE", "WHEN", "THEN",
		"ELSE", "END", "ASC", "DESC", "WITH", "FORMAT",
	}

	result := sql

	// Replace keywords with uppercase versions (word-boundary aware)
	for _, kw := range keywords {
		pattern := `(?i)\b` + regexp.QuoteMeta(kw) + `\b`
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, kw)
	}

	// Add newlines before major clauses
	clauses := []string{
		"SELECT", "FROM", "WHERE", "ORDER BY", "GROUP BY", "HAVING",
		"LIMIT", "LEFT JOIN", "RIGHT JOIN", "INNER JOIN", "OUTER JOIN",
		"FULL JOIN", "CROSS JOIN", "JOIN", "UNION",
	}
	for _, clause := range clauses {
		pattern := `(?m)\s+` + regexp.QuoteMeta(clause) + `\b`
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, "\n"+clause)
	}

	return strings.TrimSpace(result)
}
