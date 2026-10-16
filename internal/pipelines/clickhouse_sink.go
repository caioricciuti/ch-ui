package pipelines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
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
	// columns are the target table's column types by name, read once for
	// a wide events sink or schema evolution; nil until read.
	columns map[string]string
	// jsonType caches whether the server has the JSON type.
	jsonType *bool
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

// Runtime sink fields the runner sets once a run exists, so the sink can
// write schema changes to the run log and the audit log.
const (
	sinkRunID      = "_run_id"
	sinkPipelineID = "_pipeline_id"
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
	if !boolField(fields, "create_table", false) || otelPreset(sinkPreset(fields)) {
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
	if err := validatePresetFields(cfg.Fields); err != nil {
		return err
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

// WriteBatch inserts a batch of records into the ClickHouse table using
// INSERT FORMAT JSONEachRow. Before the insert it creates the table
// (create_table) and adds columns for new keys (add_new_columns); a wide
// events sink maps every record onto its columns.
func (s *ClickHouseSink) WriteBatch(ctx context.Context, cfg ConnectorConfig, batch Batch) (int, error) {
	if len(batch.Records) == 0 {
		return 0, nil
	}
	db := stringField(cfg.Fields, "database", "default")
	table := stringField(cfg.Fields, "table", "")
	connectionID, _ := cfg.Fields["connection_id"].(string)
	if connectionID == "" {
		return 0, fmt.Errorf("no connection_id in sink config")
	}
	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return 0, fmt.Errorf("find credentials: %w", err)
	}
	t := sinkTarget{s: s, cfg: cfg, conn: connectionID, user: user, password: password, db: db, table: table}
	ctx = queryCtx(ctx, pipelineIDOf(cfg))

	preset := sinkPreset(cfg.Fields)
	evolve := boolField(cfg.Fields, "add_new_columns", false) && !otelPreset(preset)
	var rows []map[string]interface{} // decoded records, when needed
	decoded := func() []map[string]interface{} {
		if rows == nil {
			rows = make([]map[string]interface{}, len(batch.Records))
			for i, rec := range batch.Records {
				rows[i] = decodeRecord(rec)
			}
		}
		return rows
	}

	// Table creation, column lookup and schema evolution run under one
	// lock; a failed step is retried on the next batch.
	s.tableMu.Lock()
	var cols map[string]string
	err = func() error {
		if boolField(cfg.Fields, "create_table", false) && !s.tableCreated {
			if err := t.ensureTable(ctx, preset, decoded); err != nil {
				return fmt.Errorf("ensure table: %w", err)
			}
			s.tableCreated = true
		}
		if preset != PresetWideEvents && !evolve {
			return nil
		}
		if s.columns == nil {
			c, err := t.tableColumns(ctx)
			if err != nil {
				return fmt.Errorf("read table columns: %w", err)
			}
			if len(c) == 0 {
				return nil // no table yet: the insert reports it, the next batch looks again
			}
			s.columns = c
		}
		if evolve {
			if err := t.evolve(ctx, preset, decoded); err != nil {
				return fmt.Errorf("add new columns: %w", err)
			}
		}
		cols = s.columns
		return nil
	}()
	s.tableMu.Unlock()
	if err != nil {
		return 0, err
	}

	// Build the JSONEachRow payload: one compact JSON object per line. Raw
	// bytes are compacted (a pretty-printed object would span lines) and
	// replaced by the parsed data when they are not valid JSON.
	var sb bytes.Buffer
	if preset == PresetWideEvents {
		roles, _ := wideMapping(cfg.Fields)
		_, hasRaw := cols["raw"]
		target := wideTarget{attrs: attrsEncoding(cols["attributes"]), raw: boolField(cfg.Fields, "keep_raw", false) && hasRaw}
		now := time.Now()
		for _, rec := range batch.Records {
			b, err := marshalCompact(wideRow(rec, roles, target, now))
			if err != nil {
				return 0, fmt.Errorf("marshal record: %w", err)
			}
			sb.Write(b)
			sb.WriteByte('\n')
		}
	} else {
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
	}

	query := fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow\n%s", quoteIdent(db), quoteIdent(table), sb.String())

	settings := map[string]string{}
	if batch.DedupToken != "" {
		settings["insert_deduplication_token"] = batch.DedupToken
	} else if dedupsWithoutToken(cfg.Fields) {
		settings["insert_deduplicate"] = "0"
	}
	if ratio, _ := errorRatio(cfg.Fields); ratio > 0 {
		settings["input_format_allow_errors_ratio"] = strconv.FormatFloat(ratio, 'f', -1, 64)
	}
	if otelPreset(preset) {
		settings["input_format_skip_unknown_fields"] = "1"
	}
	// Inferred DateTime64 columns and the wide events timestamp take ISO
	// 8601 strings with a zone, which the basic parser refuses.
	if preset == PresetWideEvents || evolve || boolField(cfg.Fields, "create_table", false) && !otelPreset(preset) {
		settings["date_time_input_format"] = "best_effort"
	}
	_, execErr := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, query, user, password, settings, insertTimeout)
	if execErr != nil {
		return 0, fmt.Errorf("execute insert: %w", execErr)
	}

	return len(batch.Records), nil
}

// sinkTarget is one write's view of the sink: its config, credentials and
// table.
type sinkTarget struct {
	s                    *ClickHouseSink
	cfg                  ConnectorConfig
	conn, user, password string
	db, table            string
}

func (t sinkTarget) exec(ctx context.Context, sql string, settings map[string]string) (*tunnel.QueryResult, error) {
	return t.s.gateway.ExecuteQueryWithSettingsCtx(ctx, t.conn, sql, t.user, t.password, settings, insertTimeout)
}

// pipelineIDOf is the id of the pipeline a sink config belongs to, set by
// the runner at start; "" for a config outside a run (validation, checks).
func pipelineIDOf(cfg ConnectorConfig) string {
	return stringField(cfg.Fields, sinkPipelineID, "")
}

// queryCtx tags the sink's queries for system.query_log. A context the
// caller already tagged (a person checking a sink from the UI) keeps its
// surface and person and gains the pipeline as its object; a background
// run is tagged as the pipeline's own work.
func queryCtx(ctx context.Context, pipelineID string) context.Context {
	if _, ok := tunnel.TagFrom(ctx); ok {
		return tunnel.WithObjectTag(ctx, "pipeline", pipelineID)
	}
	return tunnel.WithTag(ctx, tunnel.QueryTag{Surface: "pipeline"}.WithObject("pipeline", pipelineID))
}

// jsonType reports whether the server has the JSON type, asking once per
// sink. The caller holds tableMu.
func (t sinkTarget) jsonType(ctx context.Context) (bool, error) {
	if t.s.jsonType != nil {
		return *t.s.jsonType, nil
	}
	res, err := t.exec(ctx, "SELECT version() AS v", nil)
	if err != nil {
		return false, fmt.Errorf("read server version: %w", err)
	}
	// An unreadable answer means an old or unusual server: no JSON type.
	var rows []map[string]interface{}
	v := ""
	if json.Unmarshal(res.Data, &rows) == nil && len(rows) > 0 {
		v, _ = rows[0]["v"].(string)
	}
	ok := jsonTypeSupported(v)
	t.s.jsonType = &ok
	return ok, nil
}

// tableColumns returns the target table's column types by name (empty when
// the table does not exist).
func (t sinkTarget) tableColumns(ctx context.Context) (map[string]string, error) {
	res, err := t.exec(ctx, "SELECT name, type FROM system.columns WHERE database = {db:String} AND table = {t:String}",
		map[string]string{"param_db": t.db, "param_t": t.table})
	if err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	if len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &rows); err != nil {
			return nil, err
		}
	}
	cols := make(map[string]string, len(rows))
	for _, r := range rows {
		name, _ := r["name"].(string)
		typ, _ := r["type"].(string)
		if name != "" {
			cols[name] = typ
		}
	}
	return cols, nil
}

// evolve adds the columns the batch needs and the table lacks, with ALTER
// TABLE ... ADD COLUMN IF NOT EXISTS. A custom table gets a column per new
// top-level key, typed from the batch; a wide events table gets the layout
// columns it is missing (raw after keep_raw is turned on), since other keys
// go to attributes. Existing columns are never changed or dropped. Each
// addition is written to the run log and the audit log. The caller holds
// tableMu and has loaded s.columns.
func (t sinkTarget) evolve(ctx context.Context, preset string, decoded func() []map[string]interface{}) error {
	existing := t.s.columns
	if len(existing) == 0 {
		return nil // no table (yet): the insert reports it
	}
	var add []SchemaColumn
	if preset == PresetWideEvents {
		if _, ok := existing["raw"]; ok || !boolField(t.cfg.Fields, "keep_raw", false) {
			return nil
		}
		jsonType, err := t.jsonType(ctx)
		if err != nil {
			return err
		}
		for _, c := range wideColumns(jsonType, true) {
			if _, ok := existing[c.Name]; !ok {
				add = append(add, c)
			}
		}
	} else {
		rows := decoded()
		missing := map[string]bool{}
		for _, r := range rows {
			for k := range r {
				if _, ok := existing[k]; !ok && k != "" {
					missing[k] = true
				}
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if len(existing)+len(missing) > maxSchemaColumns {
			return fmt.Errorf("the table would have more than %d columns", maxSchemaColumns)
		}
		jsonType, err := t.jsonType(ctx)
		if err != nil {
			return err
		}
		sample := make([]map[string]interface{}, 0, min(len(rows), InferSampleLimit))
		for _, r := range rows {
			only := map[string]interface{}{}
			for k, v := range r {
				if missing[k] {
					only[k] = v
				}
			}
			if len(only) > 0 {
				sample = append(sample, only)
			}
			if len(sample) == InferSampleLimit {
				break
			}
		}
		add = InferColumns(sample, jsonType)
	}
	if len(add) == 0 {
		return nil
	}
	clauses := make([]string, len(add))
	for i, c := range add {
		clauses[i] = "ADD COLUMN IF NOT EXISTS " + quoteIdent(c.Name) + " " + c.Type
	}
	alter := fmt.Sprintf("ALTER TABLE %s.%s %s", quoteIdent(t.db), quoteIdent(t.table), strings.Join(clauses, ", "))
	if _, err := t.exec(ctx, alter, nil); err != nil {
		return fmt.Errorf("execute ALTER TABLE: %w", err)
	}
	runID := stringField(t.cfg.Fields, sinkRunID, "")
	pipelineID := stringField(t.cfg.Fields, sinkPipelineID, "")
	for _, c := range add {
		t.s.columns[c.Name] = c.Type
		msg := fmt.Sprintf("Added column %s %s to %s.%s for a new field", c.Name, c.Type, t.db, t.table)
		if runID != "" {
			t.s.db.CreatePipelineRunLog(runID, "info", msg)
		}
		details := msg
		if pipelineID != "" {
			details = "Pipeline " + pipelineID + ": " + msg
		}
		conn := t.conn
		if err := t.s.db.CreateAuditLog(database.AuditLogParams{Action: "pipeline.column_added", ConnectionID: &conn, Details: &details}); err != nil {
			slog.Warn("Failed to audit an added pipeline column", "error", err)
		}
	}
	slog.Info("Added columns to a pipeline table", "database", t.db, "table", t.table, "columns", len(add))
	return nil
}

// ensureTable creates the target table if it doesn't exist, in the sink's
// preset layout. A custom table without saved columns infers them from
// the first records of the batch.
func (t sinkTarget) ensureTable(ctx context.Context, preset string, decoded func() []map[string]interface{}) error {
	db, table := t.db, t.table
	if table == "" {
		return fmt.Errorf("table name is required for auto-creation")
	}
	if otelPreset(preset) {
		return t.ensureOTelTable(ctx, preset)
	}
	engine, err := tableEngine(stringField(t.cfg.Fields, "create_table_engine", ""))
	if err != nil {
		return err
	}
	if preset == PresetWideEvents {
		jsonType, err := t.jsonType(ctx)
		if err != nil {
			return err
		}
		ttl, err := sinkTTLDays(t.cfg.Fields)
		if err != nil {
			return err
		}
		ddl := wideEventsDDL(db, table, engine, jsonType, boolField(t.cfg.Fields, "keep_raw", false), ttl)
		if _, err := t.exec(ctx, ddl, nil); err != nil {
			return fmt.Errorf("execute CREATE TABLE: %w", err)
		}
		slog.Info("Auto-created wide events table", "database", db, "table", table, "engine", engine, "json_type", jsonType)
		return nil
	}

	orderBy := strings.TrimSpace(stringField(t.cfg.Fields, "create_table_order_by", ""))
	partitionBy := strings.TrimSpace(stringField(t.cfg.Fields, "create_table_partition_by", ""))
	if err := validateExpr("ORDER BY", orderBy); err != nil {
		return err
	}
	if err := validateExpr("PARTITION BY", partitionBy); err != nil {
		return err
	}
	if orderBy == "" {
		orderBy = "tuple()"
	}

	// Columns saved in the sink config (inferred from a sample in the
	// editor and maybe edited) win; otherwise infer from the batch.
	columns, err := schemaColumns(t.cfg.Fields)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		jsonType, err := t.jsonType(ctx)
		if err != nil {
			return err
		}
		rows := decoded()
		if len(rows) > InferSampleLimit {
			rows = rows[:InferSampleLimit]
		}
		columns = InferColumns(rows, jsonType)
		if len(columns) == 0 {
			return fmt.Errorf("cannot infer schema from empty records")
		}
	}
	cols := make([]string, len(columns))
	for i, c := range columns {
		cols[i] = fmt.Sprintf("%s %s", quoteIdent(c.Name), c.Type)
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
	if _, err := t.exec(ctx, ddl, nil); err != nil {
		return fmt.Errorf("execute CREATE TABLE: %w", err)
	}

	slog.Info("Auto-created ClickHouse table", "database", db, "table", table, "engine", engine, "columns", len(cols))
	return nil
}

// ensureOTelTable creates the exporter's logs table, or its traces table
// with the trace id lookup and the view that fills it. An ingest source's
// TTL wins over the sink's ttl_days.
func (t sinkTarget) ensureOTelTable(ctx context.Context, preset string) error {
	db, table := t.db, t.table
	ttl := intField(t.cfg.Fields, sinkOTelTTLDays, -1)
	if ttl < 0 {
		var err error
		if ttl, err = sinkTTLDays(t.cfg.Fields); err != nil {
			return err
		}
	}
	var stmts []string
	switch preset {
	case PresetOTelLogs:
		stmts = []string{otlp.LogsTableDDL(db, table, ttl, "")}
	default:
		stmts = otlp.TracesTableDDL(db, table, ttl, "")
	}
	// The spans and lookup tables end in a SETTINGS clause; extend it.
	for i, stmt := range stmts {
		if !strings.HasPrefix(stmt, "CREATE MATERIALIZED VIEW") {
			stmts[i] = stmt + fmt.Sprintf(", non_replicated_deduplication_window = %d", dedupWindow)
		}
	}
	for _, stmt := range stmts {
		if _, err := t.exec(ctx, stmt, nil); err != nil {
			return fmt.Errorf("execute CREATE: %w", err)
		}
	}
	slog.Info("Auto-created OpenTelemetry table", "database", db, "table", table, "layout", preset, "ttl_days", ttl)
	return nil
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
