package handlers

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
)

const (
	maxUploadBytes       = 25 * 1024 * 1024
	maxUploadPreviewRows = 20
)

type uploadDiscoveredColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Sample   string `json:"sample,omitempty"`
}

type parsedUploadDataset struct {
	Rows        []map[string]interface{}
	ColumnOrder []string
}

type uploadInsertColumn struct {
	Name string
	Type string
}

// DiscoverUploadSchema handles POST /upload/discover.
func (h *QueryHandler) DiscoverUploadSchema(w http.ResponseWriter, r *http.Request) {
	session := h.requireSchemaAdmin(w, r)
	if session == nil {
		return
	}

	filename, format, payload, err := readUploadFile(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dataset, err := parseUploadDataset(format, payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(dataset.Rows) == 0 {
		writeError(w, http.StatusBadRequest, "Uploaded file has no rows")
		return
	}

	columns := inferUploadColumns(dataset)
	preview := dataset.Rows
	if len(preview) > maxUploadPreviewRows {
		preview = preview[:maxUploadPreviewRows]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"filename": filename,
		"format":   format,
		"rows":     len(dataset.Rows),
		"columns":  columns,
		"preview":  preview,
	})
}

// IngestUpload handles POST /upload/ingest.
func (h *QueryHandler) IngestUpload(w http.ResponseWriter, r *http.Request) {
	session := h.requireSchemaAdmin(w, r)
	if session == nil {
		return
	}

	filename, format, payload, err := readUploadFile(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dataset, err := parseUploadDataset(format, payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(dataset.Rows) == 0 {
		writeError(w, http.StatusBadRequest, "Uploaded file has no rows")
		return
	}

	dbName := strings.TrimSpace(r.FormValue("database"))
	tableName := strings.TrimSpace(r.FormValue("table"))
	if err := validateSimpleObjectName(dbName, "database"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateSimpleObjectName(tableName, "table"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if isSystemDatabaseName(dbName) {
		writeError(w, http.StatusBadRequest, "Cannot upload into system databases")
		return
	}

	createTable := parseMultipartBool(r.FormValue("create_table"), false)
	columns, err := parseUploadColumnsForm(r.FormValue("columns"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(columns) == 0 {
		inferred := inferUploadColumns(dataset)
		columns = make([]uploadDiscoveredColumn, 0, len(inferred))
		for _, col := range inferred {
			columns = append(columns, uploadDiscoveredColumn{
				Name: col.Name,
				Type: col.Type,
			})
		}
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	createdTable := false
	createTableSQL := ""
	if createTable {
		createReq := createTableRequest{
			Database:    dbName,
			Name:        tableName,
			Engine:      strings.TrimSpace(r.FormValue("engine")),
			OnCluster:   strings.TrimSpace(r.FormValue("on_cluster")),
			IfNotExists: boolPtr(parseMultipartBool(r.FormValue("if_not_exists"), true)),
			OrderBy:     strings.TrimSpace(r.FormValue("order_by")),
			PartitionBy: strings.TrimSpace(r.FormValue("partition_by")),
			PrimaryKey:  strings.TrimSpace(r.FormValue("primary_key")),
			SampleBy:    strings.TrimSpace(r.FormValue("sample_by")),
			TTL:         strings.TrimSpace(r.FormValue("ttl")),
			Settings:    strings.TrimSpace(r.FormValue("settings")),
			Comment:     strings.TrimSpace(r.FormValue("comment")),
			Columns:     make([]createTableColumn, 0, len(columns)),
		}
		for _, col := range columns {
			createReq.Columns = append(createReq.Columns, createTableColumn{
				Name: col.Name,
				Type: col.Type,
			})
		}

		sql, err := buildCreateTableSQL(createReq)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		createTableSQL = sql

		if _, err := h.Gateway.ExecuteQuery(
			session.ConnectionID,
			sql,
			session.ClickhouseUser,
			password,
			45*time.Second,
		); err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("%s\n\nCreate table command:\n%s", err.Error(), truncateUploadCommand(sql, 3000)))
			return
		}
		createdTable = true
	}

	insertColumns, err := h.resolveInsertColumns(session, password, dbName, tableName, columns, createTable)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(insertColumns) == 0 {
		writeError(w, http.StatusBadRequest, "No columns available for insert")
		return
	}

	rowsInserted, insertPreviewSQL, err := h.insertJSONEachRowBatches(session, password, dbName, tableName, insertColumns, dataset.Rows)
	if err != nil {
		message := err.Error()
		if insertPreviewSQL != "" {
			message += "\n\nInsert command preview:\n" + truncateUploadCommand(insertPreviewSQL, 3000)
		}
		writeError(w, http.StatusBadGateway, message)
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "schema.upload.ingest",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("file=%s format=%s target=%s.%s rows=%d created_table=%t", filename, format, dbName, tableName, rowsInserted, createdTable)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"database":      dbName,
		"table":         tableName,
		"rows_inserted": rowsInserted,
		"created_table": createdTable,
		"commands": map[string]string{
			"create_table": createTableSQL,
			"insert":       insertPreviewSQL,
		},
	})
}

func readUploadFile(w http.ResponseWriter, r *http.Request) (filename, format string, payload []byte, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1024*64)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", "", nil, fmt.Errorf("invalid upload form: %w", err)
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return "", "", nil, errors.New("file is required")
	}
	defer file.Close()

	filename = strings.TrimSpace(header.Filename)
	if filename == "" {
		filename = "upload"
	}

	format, err = detectUploadFormat(filename, r.FormValue("format"))
	if err != nil {
		return "", "", nil, err
	}

	reader := io.LimitReader(file, maxUploadBytes+1)
	payload, err = io.ReadAll(reader)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to read uploaded file: %w", err)
	}
	if len(payload) == 0 {
		return "", "", nil, errors.New("uploaded file is empty")
	}
	if len(payload) > maxUploadBytes {
		return "", "", nil, fmt.Errorf("file exceeds %d MB limit", maxUploadBytes/(1024*1024))
	}

	return filename, format, payload, nil
}

func detectUploadFormat(filename string, explicit string) (string, error) {
	if explicit != "" {
		switch strings.ToLower(strings.TrimSpace(explicit)) {
		case "csv", "parquet", "json", "jsonl":
			return strings.ToLower(strings.TrimSpace(explicit)), nil
		default:
			return "", errors.New("unsupported format: use csv, parquet, json, or jsonl")
		}
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
		return "csv", nil
	case ".parquet":
		return "parquet", nil
	case ".json":
		return "json", nil
	case ".jsonl":
		return "jsonl", nil
	default:
		return "", errors.New("unsupported file type: only csv, parquet, json, and jsonl are allowed")
	}
}

func parseUploadDataset(format string, payload []byte) (parsedUploadDataset, error) {
	switch format {
	case "csv":
		return parseCSVDataset(payload)
	case "json":
		return parseJSONDataset(payload)
	case "jsonl":
		return parseJSONLinesDataset(payload)
	case "parquet":
		return parseParquetDataset(payload)
	default:
		return parsedUploadDataset{}, errors.New("unsupported file type: only csv, parquet, json, and jsonl are allowed")
	}
}

func parseCSVDataset(payload []byte) (parsedUploadDataset, error) {
	reader := csv.NewReader(bytes.NewReader(payload))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false

	header, err := reader.Read()
	if err != nil {
		return parsedUploadDataset{}, errors.New("invalid csv file: missing header row")
	}
	headers := normalizeCSVHeaders(header)
	if len(headers) == 0 {
		return parsedUploadDataset{}, errors.New("invalid csv file: no columns found")
	}

	rows := make([]map[string]interface{}, 0, 512)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return parsedUploadDataset{}, fmt.Errorf("invalid csv row: %w", err)
		}

		row := make(map[string]interface{}, len(headers))
		for i, col := range headers {
			if i >= len(record) {
				row[col] = nil
				continue
			}
			value := strings.TrimSpace(record[i])
			if value == "" {
				row[col] = nil
			} else {
				row[col] = value
			}
		}
		rows = append(rows, row)
	}

	return parsedUploadDataset{
		Rows:        rows,
		ColumnOrder: headers,
	}, nil
}

func parseJSONDataset(payload []byte) (parsedUploadDataset, error) {
	var raw interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return parsedUploadDataset{}, fmt.Errorf("invalid json file: %w", err)
	}

	rows := make([]map[string]interface{}, 0, 512)
	switch value := raw.(type) {
	case []interface{}:
		for _, item := range value {
			rows = append(rows, normalizeRowFromAny(item))
		}
	case map[string]interface{}:
		if dataField, ok := value["data"]; ok {
			if arr, ok := dataField.([]interface{}); ok {
				for _, item := range arr {
					rows = append(rows, normalizeRowFromAny(item))
				}
			} else {
				rows = append(rows, normalizeRowFromAny(value))
			}
		} else {
			rows = append(rows, normalizeRowFromAny(value))
		}
	default:
		rows = append(rows, normalizeRowFromAny(value))
	}

	return parsedUploadDataset{
		Rows: rows,
	}, nil
}

func parseJSONLinesDataset(payload []byte) (parsedUploadDataset, error) {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	rows := make([]map[string]interface{}, 0, 512)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return parsedUploadDataset{}, fmt.Errorf("invalid jsonl at line %d: %w", lineNo, err)
		}
		rows = append(rows, normalizeRowFromAny(raw))
	}
	if err := scanner.Err(); err != nil {
		return parsedUploadDataset{}, fmt.Errorf("failed to read jsonl: %w", err)
	}

	return parsedUploadDataset{
		Rows: rows,
	}, nil
}

func parseParquetDataset(payload []byte) (parsedUploadDataset, error) {
	tmp, err := os.CreateTemp("", "ch-ui-upload-*.parquet")
	if err != nil {
		return parsedUploadDataset{}, fmt.Errorf("failed to create temp file for parquet: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return parsedUploadDataset{}, fmt.Errorf("failed to write parquet temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return parsedUploadDataset{}, fmt.Errorf("failed to finalize parquet temp file: %w", err)
	}

	fr, err := local.NewLocalFileReader(tmpPath)
	if err != nil {
		return parsedUploadDataset{}, fmt.Errorf("failed to open parquet file: %w", err)
	}
	defer fr.Close()

	pr, err := reader.NewParquetReader(fr, new(interface{}), 1)
	if err != nil {
		return parsedUploadDataset{}, fmt.Errorf("failed to read parquet schema: %w", err)
	}
	defer pr.ReadStop()

	totalRows := int(pr.GetNumRows())
	if totalRows == 0 {
		return parsedUploadDataset{Rows: []map[string]interface{}{}}, nil
	}

	rows := make([]map[string]interface{}, 0, totalRows)
	batchSize := 512
	for readCount := 0; readCount < totalRows; {
		toRead := batchSize
		if totalRows-readCount < toRead {
			toRead = totalRows - readCount
		}
		batch := make([]interface{}, toRead)
		if err := pr.Read(&batch); err != nil {
			return parsedUploadDataset{}, fmt.Errorf("failed to read parquet rows: %w", err)
		}

		for _, item := range batch {
			rows = append(rows, normalizeRowFromAny(item))
		}
		readCount += len(batch)
	}

	return parsedUploadDataset{
		Rows: rows,
	}, nil
}

func normalizeCSVHeaders(header []string) []string {
	normalized := make([]string, 0, len(header))
	used := map[string]int{}
	for idx, raw := range header {
		name := strings.TrimSpace(raw)
		if idx == 0 {
			name = strings.TrimPrefix(name, "\uFEFF")
		}
		if name == "" {
			name = fmt.Sprintf("column_%d", idx+1)
		}

		base := name
		counter := used[base]
		if counter > 0 {
			name = fmt.Sprintf("%s_%d", base, counter+1)
		}
		used[base] = counter + 1
		normalized = append(normalized, name)
	}
	return normalized
}

func normalizeRowFromAny(raw interface{}) map[string]interface{} {
	switch value := raw.(type) {
	case map[string]interface{}:
		row := make(map[string]interface{}, len(value))
		for k, v := range value {
			row[strings.TrimSpace(k)] = normalizeUploadValue(v)
		}
		return row
	case map[interface{}]interface{}:
		row := make(map[string]interface{}, len(value))
		for k, v := range value {
			row[fmt.Sprint(k)] = normalizeUploadValue(v)
		}
		return row
	default:
		// Fallback for struct or scalar payloads.
		asMap := map[string]interface{}{}
		rawJSON, err := json.Marshal(raw)
		if err == nil {
			if json.Unmarshal(rawJSON, &asMap) == nil && len(asMap) > 0 {
				row := make(map[string]interface{}, len(asMap))
				for k, v := range asMap {
					row[strings.TrimSpace(k)] = normalizeUploadValue(v)
				}
				return row
			}
		}
		return map[string]interface{}{"value": normalizeUploadValue(raw)}
	}
}

func normalizeUploadValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		return v
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		return trimmed
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case float32:
		return float64(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		if v > math.MaxInt64 {
			return fmt.Sprint(v)
		}
		return int64(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case []interface{}, map[string]interface{}:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		if len(raw) > 0 && raw[0] == '{' {
			return string(raw)
		}
		var scalar interface{}
		if json.Unmarshal(raw, &scalar) == nil {
			return normalizeUploadValue(scalar)
		}
		return string(raw)
	}
}

func inferUploadColumns(dataset parsedUploadDataset) []uploadDiscoveredColumn {
	order := make([]string, 0, len(dataset.ColumnOrder))
	seen := map[string]struct{}{}
	for _, name := range dataset.ColumnOrder {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		order = append(order, trimmed)
	}

	extras := make([]string, 0)
	for _, row := range dataset.Rows {
		for key := range row {
			trimmed := strings.TrimSpace(key)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			extras = append(extras, trimmed)
		}
	}
	sort.Strings(extras)
	order = append(order, extras...)

	columns := make([]uploadDiscoveredColumn, 0, len(order))
	for _, name := range order {
		values := make([]interface{}, 0, len(dataset.Rows))
		sample := ""
		for _, row := range dataset.Rows {
			v, ok := row[name]
			if !ok {
				values = append(values, nil)
				continue
			}
			values = append(values, v)
			if sample == "" && v != nil {
				sample = fmt.Sprint(v)
			}
		}
		baseType, nullable := inferUploadColumnType(values)
		columnType := baseType
		if nullable {
			columnType = fmt.Sprintf("Nullable(%s)", baseType)
		}
		columns = append(columns, uploadDiscoveredColumn{
			Name:     name,
			Type:     columnType,
			Nullable: nullable,
			Sample:   sample,
		})
	}
	return columns
}

func inferUploadColumnType(values []interface{}) (baseType string, nullable bool) {
	allBool := true
	allInt := true
	allFloat := true
	allDate := true
	allDateTime := true
	hasValue := false

	for _, raw := range values {
		if raw == nil {
			nullable = true
			continue
		}

		switch v := raw.(type) {
		case bool:
			hasValue = true
			allInt = false
			allFloat = false
			allDate = false
			allDateTime = false
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			hasValue = true
			allBool = false
			allDate = false
			allDateTime = false
		case float32:
			hasValue = true
			allBool = false
			allDate = false
			allDateTime = false
			if math.Trunc(float64(v)) != float64(v) {
				allInt = false
			}
		case float64:
			hasValue = true
			allBool = false
			allDate = false
			allDateTime = false
			if math.Trunc(v) != v {
				allInt = false
			}
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				nullable = true
				continue
			}
			hasValue = true
			if _, err := strconv.ParseBool(trimmed); err != nil && trimmed != "0" && trimmed != "1" {
				allBool = false
			}
			if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
				allInt = false
			}
			if _, err := strconv.ParseFloat(trimmed, 64); err != nil {
				allFloat = false
			}
			if _, err := time.Parse("2006-01-02", trimmed); err != nil {
				allDate = false
			}
			if !isDateTimeString(trimmed) {
				allDateTime = false
			}
		default:
			hasValue = true
			allBool = false
			allInt = false
			allFloat = false
			allDate = false
			allDateTime = false
		}
	}

	if !hasValue {
		return "String", true
	}
	switch {
	case allBool:
		return "Bool", nullable
	case allInt:
		return "Int64", nullable
	case allFloat:
		return "Float64", nullable
	case allDateTime:
		return "DateTime", nullable
	case allDate:
		return "Date", nullable
	default:
		return "String", nullable
	}
}

func isDateTimeString(value string) bool {
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000",
	}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func parseUploadColumnsForm(raw string) ([]uploadDiscoveredColumn, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var cols []uploadDiscoveredColumn
	if err := json.Unmarshal([]byte(trimmed), &cols); err != nil {
		return nil, errors.New("invalid columns payload")
	}
	result := make([]uploadDiscoveredColumn, 0, len(cols))
	for idx, col := range cols {
		name := strings.TrimSpace(col.Name)
		colType := strings.TrimSpace(col.Type)
		if err := validateSimpleObjectName(name, fmt.Sprintf("column #%d", idx+1)); err != nil {
			return nil, err
		}
		if colType == "" || isUnsafeSQLFragment(colType) {
			return nil, fmt.Errorf("invalid type for column %q", name)
		}
		result = append(result, uploadDiscoveredColumn{
			Name: name,
			Type: colType,
		})
	}
	return result, nil
}

func parseMultipartBool(raw string, defaultValue bool) bool {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return defaultValue
	}
	switch trimmed {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func buildCreateTableSQL(req createTableRequest) (string, error) {
	dbName := strings.TrimSpace(req.Database)
	tableName := strings.TrimSpace(req.Name)
	if err := validateSimpleObjectName(dbName, "database"); err != nil {
		return "", err
	}
	if err := validateSimpleObjectName(tableName, "table"); err != nil {
		return "", err
	}
	if isSystemDatabaseName(dbName) {
		return "", errors.New("cannot create tables in system databases")
	}
	if len(req.Columns) == 0 {
		return "", errors.New("at least one column is required")
	}

	engine := strings.TrimSpace(req.Engine)
	if engine == "" {
		engine = "MergeTree"
	}
	if isUnsafeSQLFragment(engine) {
		return "", errors.New("invalid engine expression")
	}

	cluster := strings.TrimSpace(req.OnCluster)
	if cluster != "" && isUnsafeSQLFragment(cluster) {
		return "", errors.New("invalid cluster name")
	}

	columnsSQL := make([]string, 0, len(req.Columns))
	for i, col := range req.Columns {
		colName := strings.TrimSpace(col.Name)
		colType := strings.TrimSpace(col.Type)
		if err := validateSimpleObjectName(colName, fmt.Sprintf("column #%d", i+1)); err != nil {
			return "", err
		}
		if colType == "" || isUnsafeSQLFragment(colType) {
			return "", fmt.Errorf("invalid type for column %q", colName)
		}

		part := escapeIdentifier(colName) + " " + colType
		if def := strings.TrimSpace(col.DefaultExpression); def != "" {
			if isUnsafeSQLFragment(def) {
				return "", fmt.Errorf("invalid default expression for column %q", colName)
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
			return "", fmt.Errorf("invalid %s expression", expr.name)
		}
	}

	if strings.Contains(strings.ToLower(engine), "mergetree") && orderBy == "" {
		orderBy = "tuple()"
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

	return sqlBuilder.String(), nil
}

func (h *QueryHandler) resolveInsertColumns(
	session *middleware.SessionInfo,
	password string,
	databaseName string,
	tableName string,
	discovered []uploadDiscoveredColumn,
	createTable bool,
) ([]uploadInsertColumn, error) {
	if createTable {
		cols := make([]uploadInsertColumn, 0, len(discovered))
		for _, col := range discovered {
			name := strings.TrimSpace(col.Name)
			colType := strings.TrimSpace(col.Type)
			if name == "" {
				continue
			}
			if colType == "" {
				colType = "String"
			}
			cols = append(cols, uploadInsertColumn{
				Name: name,
				Type: colType,
			})
		}
		return cols, nil
	}

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		fmt.Sprintf("DESCRIBE TABLE %s.%s", escapeIdentifier(databaseName), escapeIdentifier(tableName)),
		session.ClickhouseUser,
		password,
		20*time.Second,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read table schema: %w", err)
	}

	rows := decodeRows(result.Data)
	tableCols := make([]uploadInsertColumn, 0, len(rows))
	for _, row := range rows {
		rawName, ok := row["name"]
		if !ok {
			continue
		}
		colName := strings.TrimSpace(fmt.Sprint(rawName))
		if colName == "" {
			continue
		}
		colType := strings.TrimSpace(fmt.Sprint(row["type"]))
		if colType == "" {
			colType = "String"
		}
		tableCols = append(tableCols, uploadInsertColumn{
			Name: colName,
			Type: colType,
		})
	}
	if len(tableCols) == 0 {
		return nil, errors.New("target table has no columns")
	}

	byLower := make(map[string]uploadInsertColumn, len(tableCols))
	for _, col := range tableCols {
		byLower[strings.ToLower(col.Name)] = col
	}

	selected := make([]uploadInsertColumn, 0, len(discovered))
	used := map[string]struct{}{}
	for _, col := range discovered {
		name := strings.TrimSpace(col.Name)
		if name == "" {
			continue
		}
		var matched uploadInsertColumn
		for _, tableCol := range tableCols {
			if tableCol.Name == name {
				matched = tableCol
				break
			}
		}
		if matched.Name == "" {
			if value, ok := byLower[strings.ToLower(name)]; ok {
				matched = value
			}
		}
		if matched.Name == "" {
			continue
		}
		if _, exists := used[matched.Name]; exists {
			continue
		}
		used[matched.Name] = struct{}{}
		selected = append(selected, matched)
	}

	if len(selected) == 0 {
		return nil, errors.New("no matching columns between uploaded file and target table")
	}

	return selected, nil
}

func (h *QueryHandler) insertJSONEachRowBatches(
	session *middleware.SessionInfo,
	password string,
	databaseName string,
	tableName string,
	targetColumns []uploadInsertColumn,
	rows []map[string]interface{},
) (int, string, error) {
	if len(rows) == 0 {
		return 0, "", nil
	}
	const batchSize = 500
	inserted := 0
	insertPreview := ""

	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		query, rowCount, err := buildJSONEachRowInsertQuery(databaseName, tableName, targetColumns, rows[start:end], start)
		if err != nil {
			return inserted, insertPreview, err
		}
		if rowCount == 0 {
			continue
		}
		if insertPreview == "" {
			insertPreview = query
		}

		if _, err := h.Gateway.ExecuteQuery(
			session.ConnectionID,
			query,
			session.ClickhouseUser,
			password,
			90*time.Second,
		); err != nil {
			return inserted, insertPreview, fmt.Errorf("insert failed after %d rows: %s", inserted, humanizeUploadInsertError(err.Error()))
		}
		inserted += rowCount
	}

	return inserted, insertPreview, nil
}

func buildJSONEachRowInsertQuery(
	databaseName string,
	tableName string,
	targetColumns []uploadInsertColumn,
	sourceRows []map[string]interface{},
	baseRowOffset int,
) (string, int, error) {
	if len(targetColumns) == 0 {
		return "", 0, errors.New("insert requires at least one column")
	}

	seen := map[string]struct{}{}
	columns := make([]uploadInsertColumn, 0, len(targetColumns))
	for _, col := range targetColumns {
		name := strings.TrimSpace(col.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		typ := strings.TrimSpace(col.Type)
		if typ == "" {
			typ = "String"
		}
		columns = append(columns, uploadInsertColumn{Name: name, Type: typ})
	}
	if len(columns) == 0 {
		return "", 0, errors.New("insert requires at least one valid column")
	}

	var builder strings.Builder
	builder.WriteString("INSERT INTO ")
	builder.WriteString(escapeIdentifier(databaseName))
	builder.WriteString(".")
	builder.WriteString(escapeIdentifier(tableName))
	builder.WriteString(" (")
	for idx, col := range columns {
		if idx > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(escapeIdentifier(col.Name))
	}
	builder.WriteString(") FORMAT JSONEachRow\n")

	inserted := 0
	for rowIdx, source := range sourceRows {
		row := make(map[string]interface{}, len(columns))
		hasData := false
		for _, col := range columns {
			val, ok := source[col.Name]
			if !ok {
				row[col.Name] = nil
				continue
			}
			coerced, err := coerceUploadValueForType(val, col.Type)
			if err != nil {
				return "", inserted, fmt.Errorf("row %d column %q: %w", baseRowOffset+rowIdx+1, col.Name, err)
			}
			row[col.Name] = coerced
			if coerced != nil {
				hasData = true
			}
		}
		if !hasData {
			continue
		}
		line, err := json.Marshal(row)
		if err != nil {
			return "", inserted, fmt.Errorf("failed to encode row for insert: %w", err)
		}
		builder.Write(line)
		builder.WriteByte('\n')
		inserted++
	}

	return builder.String(), inserted, nil
}

func humanizeUploadInsertError(message string) string {
	msg := strings.TrimSpace(message)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "cannot parse input") {
		return msg + " Hint: adjust discovered column types or normalize date/time formats before upload."
	}
	return msg
}

func truncateUploadCommand(sql string, limit int) string {
	if limit <= 0 || len(sql) <= limit {
		return sql
	}
	return sql[:limit] + "\n... (truncated)"
}

func coerceUploadValueForType(value interface{}, typeExpr string) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	baseType := normalizeClickHouseType(typeExpr)
	if baseType == "" {
		baseType = "STRING"
	}

	switch {
	case strings.Contains(baseType, "BOOL"):
		parsed, err := parseBoolUploadValue(value)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	case strings.Contains(baseType, "INT"):
		parsed, err := parseIntUploadValue(value)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	case strings.Contains(baseType, "FLOAT") || strings.Contains(baseType, "DECIMAL"):
		parsed, err := parseFloatUploadValue(value)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	case strings.Contains(baseType, "DATE") && strings.Contains(baseType, "TIME"):
		parsed, err := parseDateTimeUploadValue(value)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	case strings.HasPrefix(baseType, "DATE"):
		parsed, err := parseDateUploadValue(value)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	default:
		return normalizeUploadValue(value), nil
	}
}

func normalizeClickHouseType(typeExpr string) string {
	trimmed := strings.TrimSpace(typeExpr)
	if trimmed == "" {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	changed := true
	for changed {
		changed = false
		for _, wrapper := range []string{"NULLABLE(", "LOWCARDINALITY("} {
			if strings.HasPrefix(upper, wrapper) && strings.HasSuffix(upper, ")") {
				upper = strings.TrimSuffix(strings.TrimPrefix(upper, wrapper), ")")
				changed = true
			}
		}
	}
	return strings.TrimSpace(upper)
}

func parseBoolUploadValue(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(v))
		switch trimmed {
		case "1", "true", "yes", "on":
			return true, nil
		case "0", "false", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("cannot parse %q as Bool", v)
		}
	case int64:
		return v != 0, nil
	case float64:
		return v != 0, nil
	default:
		normalized := normalizeUploadValue(value)
		if s, ok := normalized.(string); ok {
			return parseBoolUploadValue(s)
		}
		return false, fmt.Errorf("cannot parse %T as Bool", value)
	}
}

func parseIntUploadValue(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("cannot parse non-integer %v as Int64", v)
		}
		return int64(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, errors.New("empty value")
		}
		i, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as Int64", v)
		}
		return i, nil
	default:
		normalized := normalizeUploadValue(value)
		switch nv := normalized.(type) {
		case int64:
			return nv, nil
		case float64:
			if math.Trunc(nv) != nv {
				return 0, fmt.Errorf("cannot parse non-integer %v as Int64", nv)
			}
			return int64(nv), nil
		case string:
			return parseIntUploadValue(nv)
		default:
			return 0, fmt.Errorf("cannot parse %T as Int64", value)
		}
	}
}

func parseFloatUploadValue(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, errors.New("empty value")
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as Float64", v)
		}
		return f, nil
	default:
		normalized := normalizeUploadValue(value)
		switch nv := normalized.(type) {
		case float64:
			return nv, nil
		case int64:
			return float64(nv), nil
		case string:
			return parseFloatUploadValue(nv)
		default:
			return 0, fmt.Errorf("cannot parse %T as Float64", value)
		}
	}
}

func parseDateUploadValue(value interface{}) (string, error) {
	t, err := parseFlexibleTime(value)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02"), nil
}

func parseDateTimeUploadValue(value interface{}) (string, error) {
	t, err := parseFlexibleTime(value)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02 15:04:05"), nil
}

func parseFlexibleTime(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return time.Time{}, errors.New("empty time value")
		}
		layouts := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02 15:04:05",
			"2006-01-02 15:04:05.000",
			"2006-01-02T15:04:05",
			"2006-01-02T15:04:05.000",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, trimmed); err == nil {
				return t.UTC(), nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse %q as Date/DateTime", v)
	default:
		normalized := normalizeUploadValue(value)
		if s, ok := normalized.(string); ok {
			return parseFlexibleTime(s)
		}
		return time.Time{}, fmt.Errorf("cannot parse %T as Date/DateTime", value)
	}
}
