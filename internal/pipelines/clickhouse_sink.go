package pipelines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ClickHouseSink writes batches to a ClickHouse table via the tunnel gateway.
type ClickHouseSink struct {
	gateway   *tunnel.Gateway
	db        *database.DB
	secretKey string

	// tableMu guards tableCreated. A failed auto-create is retried on the
	// next batch instead of being cached forever.
	tableMu      sync.Mutex
	tableCreated bool
	dlqCreated   bool
}

// insertTimeout bounds one INSERT or CREATE TABLE through the gateway.
const insertTimeout = 60 * time.Second

// Runtime sink fields the runner sets for an ingest pipeline with a logs or
// traces signal (never taken from a saved config): the table is created in
// the OpenTelemetry exporter layout with this TTL, and inserts skip unknown
// fields.
const (
	sinkOTelSignal  = "_otel_signal"
	sinkOTelTTLDays = "_otel_ttl_days"
)

// dedupWindow is set on the tables the sink creates (OpenTelemetry tables,
// and plain MergeTree engines of create_table), so a replayed batch (same
// insert_deduplication_token) is dropped on a plain MergeTree too. This is
// what lets a routed pipeline retry a whole batch while the sinks that
// already took it drop the replay. Replicated and Shared tables deduplicate
// without it.
const dedupWindow = 1000

// dedupsWithoutToken reports whether the sink's auto-created table keeps
// a deduplication window, so an insert without a token must turn
// deduplication off or identical batches (a database source without a
// watermark re-reading the same rows) would be dropped as duplicates.
func dedupsWithoutToken(fields map[string]interface{}) bool {
	if !boolField(fields, "create_table", false) || stringField(fields, sinkOTelSignal, "") != "" {
		return false
	}
	engine, err := tableEngine(stringField(fields, "create_table_engine", ""))
	return err == nil && !replicatedEngine(engine)
}

// replicatedEngine reports whether an engine deduplicates inserts on its own.
func replicatedEngine(engine string) bool {
	return strings.HasPrefix(engine, "Replicated") || strings.HasPrefix(engine, "Shared")
}

// NewClickHouseSink creates a new ClickHouse sink connector.
func NewClickHouseSink(gw *tunnel.Gateway, db *database.DB, secretKey string) *ClickHouseSink {
	return &ClickHouseSink{
		gateway:   gw,
		db:        db,
		secretKey: secretKey,
	}
}

func (s *ClickHouseSink) Type() string { return "sink_clickhouse" }

// Validate checks the sink configuration.
func (s *ClickHouseSink) Validate(cfg ConnectorConfig) error {
	db, _ := cfg.Fields["database"].(string)
	table, _ := cfg.Fields["table"].(string)
	if db == "" {
		return fmt.Errorf("database is required")
	}
	if table == "" {
		return fmt.Errorf("table is required")
	}
	if boolField(cfg.Fields, "create_table", false) {
		if _, err := tableEngine(stringField(cfg.Fields, "create_table_engine", "")); err != nil {
			return err
		}
		if err := validateExpr("ORDER BY", stringField(cfg.Fields, "create_table_order_by", "")); err != nil {
			return err
		}
		if err := validateExpr("PARTITION BY", stringField(cfg.Fields, "create_table_partition_by", "")); err != nil {
			return err
		}
		if _, err := schemaColumns(cfg.Fields); err != nil {
			return err
		}
	}
	if _, err := errorRatio(cfg.Fields); err != nil {
		return err
	}
	return nil
}

// errorRatio returns the optional max_error_ratio (0 when unset), which is
// sent as input_format_allow_errors_ratio.
func errorRatio(fields map[string]interface{}) (float64, error) {
	v, ok := fields["max_error_ratio"]
	if !ok || v == nil || v == "" {
		return 0, nil
	}
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, fmt.Errorf("max_error_ratio must be a number between 0 and 1")
		}
		f = parsed
	default:
		return 0, fmt.Errorf("max_error_ratio must be a number between 0 and 1")
	}
	if f < 0 || f > 1 {
		return 0, fmt.Errorf("max_error_ratio must be between 0 and 1 (got %g)", f)
	}
	return f, nil
}

// WriteBatch inserts a batch of records into the ClickHouse table using INSERT FORMAT JSONEachRow.
func (s *ClickHouseSink) WriteBatch(ctx context.Context, cfg ConnectorConfig, batch Batch) (int, error) {
	if len(batch.Records) == 0 {
		return 0, nil
	}

	// Auto-create table on first batch if configured
	if boolField(cfg.Fields, "create_table", false) {
		s.tableMu.Lock()
		if !s.tableCreated {
			if err := s.ensureTable(ctx, cfg, batch); err != nil {
				s.tableMu.Unlock()
				return 0, fmt.Errorf("ensure table: %w", err)
			}
			s.tableCreated = true
		}
		s.tableMu.Unlock()
	}

	db, _ := cfg.Fields["database"].(string)
	table, _ := cfg.Fields["table"].(string)

	// Build JSONEachRow payload: one compact JSON object per line. Raw bytes
	// are compacted (a pretty-printed object would span lines) and replaced by
	// the parsed data when they are not valid JSON.
	var sb bytes.Buffer
	for _, rec := range batch.Records {
		if len(rec.RawJSON) == 0 || json.Compact(&sb, rec.RawJSON) != nil {
			raw, err := json.Marshal(rec.Data)
			if err != nil {
				return 0, fmt.Errorf("marshal record: %w", err)
			}
			sb.Write(raw)
		}
		sb.WriteByte('\n')
	}

	query := fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow\n%s", quoteIdent(db), quoteIdent(table), sb.String())

	// Find credentials from the pipeline's connection
	connectionID, _ := cfg.Fields["connection_id"].(string)
	if connectionID == "" {
		return 0, fmt.Errorf("no connection_id in sink config")
	}

	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return 0, fmt.Errorf("find credentials: %w", err)
	}

	settings := map[string]string{}
	if batch.DedupToken != "" {
		settings["insert_deduplication_token"] = batch.DedupToken
	} else if dedupsWithoutToken(cfg.Fields) {
		settings["insert_deduplicate"] = "0"
	}
	if ratio, _ := errorRatio(cfg.Fields); ratio > 0 {
		settings["input_format_allow_errors_ratio"] = strconv.FormatFloat(ratio, 'f', -1, 64)
	}
	if stringField(cfg.Fields, sinkOTelSignal, "") != "" {
		settings["input_format_skip_unknown_fields"] = "1"
	}
	_, execErr := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, query, user, password, settings, insertTimeout)
	if execErr != nil {
		return 0, fmt.Errorf("execute insert: %w", execErr)
	}

	return len(batch.Records), nil
}

// ensureTable creates the target table if it doesn't exist, inferring schema from the first batch.
func (s *ClickHouseSink) ensureTable(ctx context.Context, cfg ConnectorConfig, batch Batch) error {
	db := stringField(cfg.Fields, "database", "default")
	table := stringField(cfg.Fields, "table", "")
	orderBy := strings.TrimSpace(stringField(cfg.Fields, "create_table_order_by", ""))
	partitionBy := strings.TrimSpace(stringField(cfg.Fields, "create_table_partition_by", ""))

	if table == "" {
		return fmt.Errorf("table name is required for auto-creation")
	}
	if signal := stringField(cfg.Fields, sinkOTelSignal, ""); signal != "" {
		return s.ensureOTelTable(ctx, cfg, signal, db, table)
	}
	engine, err := tableEngine(stringField(cfg.Fields, "create_table_engine", ""))
	if err != nil {
		return err
	}
	if err := validateExpr("ORDER BY", orderBy); err != nil {
		return err
	}
	if err := validateExpr("PARTITION BY", partitionBy); err != nil {
		return err
	}
	if orderBy == "" {
		orderBy = "tuple()"
	}

	// Columns saved in the sink config (inferred by ClickHouse from a
	// sample, see InferSchema) win; otherwise infer from the first record.
	saved, err := schemaColumns(cfg.Fields)
	if err != nil {
		return err
	}
	var cols []string
	if len(saved) > 0 {
		for _, c := range saved {
			cols = append(cols, fmt.Sprintf("%s %s", quoteIdent(c.Name), c.Type))
		}
	} else {
		if len(batch.Records) == 0 {
			return fmt.Errorf("cannot infer schema from empty batch")
		}
		data := batch.Records[0].Data
		if len(data) == 0 {
			return fmt.Errorf("cannot infer schema from empty record")
		}
		colNames := make([]string, 0, len(data))
		for k := range data {
			colNames = append(colNames, k)
		}
		sort.Strings(colNames)
		for _, name := range colNames {
			cols = append(cols, fmt.Sprintf("%s %s", quoteIdent(name), inferClickHouseType(data[name])))
		}
	}

	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (\n  %s\n) ENGINE = %s",
		quoteIdent(db), quoteIdent(table), strings.Join(cols, ",\n  "), engine)
	if partitionBy != "" {
		ddl += "\nPARTITION BY " + partitionBy
	}
	ddl += "\nORDER BY " + orderBy
	if !replicatedEngine(engine) {
		ddl += fmt.Sprintf("\nSETTINGS non_replicated_deduplication_window = %d", dedupWindow)
	}

	connectionID, _ := cfg.Fields["connection_id"].(string)
	if connectionID == "" {
		return fmt.Errorf("no connection_id in sink config")
	}

	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return fmt.Errorf("find credentials: %w", err)
	}

	_, execErr := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, ddl, user, password, nil, insertTimeout)
	if execErr != nil {
		return fmt.Errorf("execute CREATE TABLE: %w", execErr)
	}

	slog.Info("Auto-created ClickHouse table", "database", db, "table", table, "engine", engine, "columns", len(cols))
	return nil
}

// ensureOTelTable creates the exporter's logs table, or its traces table
// with the trace id lookup and the view that fills it.
func (s *ClickHouseSink) ensureOTelTable(ctx context.Context, cfg ConnectorConfig, signal, db, table string) error {
	ttl := intField(cfg.Fields, sinkOTelTTLDays, 0)
	var stmts []string
	switch signal {
	case SignalLogs:
		stmts = []string{otlp.LogsTableDDL(db, table, ttl, "")}
	case SignalTraces:
		stmts = otlp.TracesTableDDL(db, table, ttl, "")
	default:
		return fmt.Errorf("unknown OpenTelemetry signal %q", signal)
	}
	// The spans and lookup tables end in a SETTINGS clause; extend it.
	for i, stmt := range stmts {
		if !strings.HasPrefix(stmt, "CREATE MATERIALIZED VIEW") {
			stmts[i] = stmt + fmt.Sprintf(", non_replicated_deduplication_window = %d", dedupWindow)
		}
	}
	connectionID := stringField(cfg.Fields, "connection_id", "")
	if connectionID == "" {
		return fmt.Errorf("no connection_id in sink config")
	}
	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return fmt.Errorf("find credentials: %w", err)
	}
	for _, stmt := range stmts {
		if _, err := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, stmt, user, password, nil, insertTimeout); err != nil {
			return fmt.Errorf("execute CREATE: %w", err)
		}
	}
	slog.Info("Auto-created OpenTelemetry table", "database", db, "table", table, "signal", signal, "ttl_days", ttl)
	return nil
}

// inferClickHouseType maps a Go/JSON value to a ClickHouse column type.
func inferClickHouseType(v interface{}) string {
	switch v.(type) {
	case string:
		return "String"
	case float64:
		return "Float64"
	case bool:
		return "UInt8"
	case nil:
		return "Nullable(String)"
	default:
		return "String"
	}
}

// findCredentials resolves the configured background account or session credentials.
func (s *ClickHouseSink) findCredentials(connectionID string) (string, string, error) {
	return s.db.BackgroundCredentials(connectionID, "pipeline", s.secretKey)
}

// quoteIdent quotes a ClickHouse identifier with backticks, escaping
// backslashes and backticks inside it.
func quoteIdent(name string) string {
	r := strings.NewReplacer("\\", "\\\\", "`", "\\`")
	return "`" + r.Replace(name) + "`"
}

// allowedEngines are the table engines the sink may auto-create. They take no
// arguments, so the engine clause can never carry SQL.
var allowedEngines = map[string]bool{
	"MergeTree":                      true,
	"ReplacingMergeTree":             true,
	"SummingMergeTree":               true,
	"AggregatingMergeTree":           true,
	"ReplicatedMergeTree":            true,
	"ReplicatedReplacingMergeTree":   true,
	"ReplicatedSummingMergeTree":     true,
	"ReplicatedAggregatingMergeTree": true,
	"SharedMergeTree":                true,
	"SharedReplacingMergeTree":       true,
}

// tableEngine returns the engine clause for an allowed engine name ("" means
// MergeTree). "MergeTree()" is accepted as "MergeTree".
func tableEngine(name string) (string, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), "()")
	if name == "" {
		name = "MergeTree"
	}
	if !allowedEngines[name] {
		return "", fmt.Errorf("table engine %q is not supported for auto-created tables", name)
	}
	return name, nil
}

// validateExpr checks an ORDER BY or PARTITION BY expression: one expression
// with balanced brackets and quotes, and no statement separator or comment
// that could append SQL to the generated CREATE TABLE.
func validateExpr(what, expr string) error {
	if len(expr) > 500 {
		return fmt.Errorf("%s expression is too long", what)
	}
	depth := 0
	var bare strings.Builder // the expression with quoted parts removed
	var quote rune
	escaped := false
	prev := rune(0)
	for _, c := range expr {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == quote:
				quote = 0
			}
			prev = c
			continue
		}
		bare.WriteRune(c)
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
			if depth < 0 {
				return fmt.Errorf("%s has unbalanced brackets", what)
			}
		case ';':
			return fmt.Errorf("%s must not contain ';'", what)
		case '#':
			return fmt.Errorf("%s must not contain comments", what)
		case '-':
			if prev == '-' {
				return fmt.Errorf("%s must not contain comments", what)
			}
		case '*':
			if prev == '/' {
				return fmt.Errorf("%s must not contain comments", what)
			}
		case '\n', '\r':
			return fmt.Errorf("%s must be on one line", what)
		}
		prev = c
	}
	if quote != 0 || depth != 0 {
		return fmt.Errorf("%s has unbalanced quotes or brackets", what)
	}
	if m := clauseKeyword.FindString(bare.String()); m != "" {
		return fmt.Errorf("%s must be a single expression (found %q)", what, m)
	}
	return nil
}

// clauseKeyword matches keywords that would start another clause of the
// CREATE TABLE statement after the expression.
var clauseKeyword = regexp.MustCompile(`(?i)\b(AS|SELECT|SETTINGS|TTL|PRIMARY|SAMPLE|ENGINE|COMMENT|FORMAT|INTO|PARTITION|ORDER|EMPTY|WITH|FROM|UNION)\b`)
