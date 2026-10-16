package pipelines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// dlqSuffix names the dead-letter table next to the target table.
const dlqSuffix = "_dlq"

// DeadLetterTable returns the database and table rejected rows go to.
func (s *ClickHouseSink) DeadLetterTable(cfg ConnectorConfig) (string, string) {
	return stringField(cfg.Fields, "database", "default"), stringField(cfg.Fields, "table", "") + dlqSuffix
}

// WriteDeadLetters inserts refused records into <table>_dlq, creating it on
// first use.
func (s *ClickHouseSink) WriteDeadLetters(ctx context.Context, cfg ConnectorConfig, pipelineID string, letters []DeadLetter, dedupToken string) error {
	if len(letters) == 0 {
		return nil
	}
	connectionID := stringField(cfg.Fields, "connection_id", "")
	if connectionID == "" {
		return fmt.Errorf("no connection_id in sink config")
	}
	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return fmt.Errorf("find credentials: %w", err)
	}
	db, table := s.DeadLetterTable(cfg)
	ctx = queryCtx(ctx, pipelineID)

	s.tableMu.Lock()
	if !s.dlqCreated {
		ddl := DeadLetterDDL(db, table)
		if _, err := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, ddl, user, password, nil, insertTimeout); err != nil {
			s.tableMu.Unlock()
			return fmt.Errorf("create dead-letter table: %w", err)
		}
		s.dlqCreated = true
		slog.Info("Dead-letter table ready", "database", db, "table", table)
	}
	s.tableMu.Unlock()

	var sb bytes.Buffer
	for _, l := range letters {
		row, err := json.Marshal(map[string]string{
			"pipeline_id":     pipelineID,
			"raw":             l.Raw,
			"error":           l.Error,
			"source_position": l.Position,
		})
		if err != nil {
			return err
		}
		sb.Write(row)
		sb.WriteByte('\n')
	}
	query := fmt.Sprintf("INSERT INTO %s.%s (pipeline_id, raw, error, source_position) FORMAT JSONEachRow\n%s", quoteIdent(db), quoteIdent(table), sb.String())
	settings := map[string]string{}
	if dedupToken != "" {
		settings["insert_deduplication_token"] = dedupToken
	}
	if _, err := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, query, user, password, settings, insertTimeout); err != nil {
		return fmt.Errorf("insert dead-letter rows: %w", err)
	}
	return nil
}

// DeadLetterDDL is the CREATE TABLE of a dead-letter table. ts is filled by
// ClickHouse on insert.
func DeadLetterDDL(db, table string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (\n  `ts` DateTime64(3) DEFAULT now64(3),\n  `pipeline_id` String,\n  `raw` String,\n  `error` String,\n  `source_position` String\n) ENGINE = MergeTree\nORDER BY ts",
		quoteIdent(db), quoteIdent(table))
}

// SinkCheck is one result of checking a sink before running it.
type SinkCheck struct {
	// NodeID names the sink when a pipeline has several.
	NodeID string `json:"node_id,omitempty"`
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn or fail
	Detail string `json:"detail"`
}

// checkTimeout bounds each query of a sink check.
const checkTimeout = 10 * time.Second

// Check verifies, with the credentials the pipeline writes with, that the
// target database exists, the table exists or can be created, and INSERT is
// granted. It never changes anything.
func (s *ClickHouseSink) Check(ctx context.Context, cfg ConnectorConfig) []SinkCheck {
	db := stringField(cfg.Fields, "database", "default")
	table := stringField(cfg.Fields, "table", "")
	connectionID := stringField(cfg.Fields, "connection_id", "")
	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return []SinkCheck{{Name: "Credentials", Status: "fail", Detail: "The pipeline account could not be resolved: " + err.Error()}}
	}
	who := user
	if who == "" {
		who = "the pipeline account"
	}
	checks := []SinkCheck{{Name: "Credentials", Status: "ok", Detail: "Writes run as " + who}}
	ctx = queryCtx(ctx, pipelineIDOf(cfg))

	run := func(sql string, params map[string]string) ([]map[string]interface{}, error) {
		res, err := s.gateway.ExecuteQueryWithSettingsCtx(ctx, connectionID, sql, user, password, params, checkTimeout)
		if err != nil {
			return nil, err
		}
		var rows []map[string]interface{}
		if len(res.Data) > 0 {
			if err := json.Unmarshal(res.Data, &rows); err != nil {
				return nil, err
			}
		}
		return rows, nil
	}
	exists := func(sql string, params map[string]string) (bool, error) {
		rows, err := run(sql, params)
		if err != nil {
			return false, err
		}
		return len(rows) > 0 && firstNumber(rows[0]) > 0, nil
	}
	// CHECK GRANT takes no FORMAT clause; the agent leaves it off and the
	// HTTP default_format (JSON) shapes the result.
	grant := func(privilege, tbl string) (bool, error) {
		return exists(fmt.Sprintf("CHECK GRANT %s ON %s.%s", privilege, quoteIdent(db), quoteIdent(tbl)), nil)
	}

	dbOK, err := exists("SELECT count() AS n FROM system.databases WHERE name = {db:String}", map[string]string{"param_db": db})
	switch {
	case err != nil:
		return append(checks, SinkCheck{Name: "Database", Status: "fail", Detail: err.Error()})
	case !dbOK:
		return append(checks, SinkCheck{Name: "Database", Status: "fail", Detail: fmt.Sprintf("Database %s does not exist or is not visible to %s", db, who)})
	default:
		checks = append(checks, SinkCheck{Name: "Database", Status: "ok", Detail: fmt.Sprintf("Database %s exists", db)})
	}

	params := map[string]string{"param_db": db, "param_t": table}
	tableOK, err := exists("SELECT count() AS n FROM system.tables WHERE database = {db:String} AND name = {t:String}", params)
	if err != nil {
		return append(checks, SinkCheck{Name: "Table", Status: "fail", Detail: err.Error()})
	}
	if tableOK {
		checks = append(checks, SinkCheck{Name: "Table", Status: "ok", Detail: fmt.Sprintf("Table %s.%s exists", db, table)})
	} else if boolField(cfg.Fields, "create_table", false) {
		ok, err := grant("CREATE TABLE", table)
		switch {
		case err != nil:
			checks = append(checks, SinkCheck{Name: "Table", Status: "warn", Detail: "Table does not exist yet; could not verify the CREATE TABLE grant: " + err.Error()})
		case ok:
			checks = append(checks, SinkCheck{Name: "Table", Status: "ok", Detail: "Table does not exist yet and will be created on the first batch"})
		default:
			checks = append(checks, SinkCheck{Name: "Table", Status: "fail", Detail: fmt.Sprintf("Table does not exist and %s lacks CREATE TABLE on %s.%s", who, db, table)})
		}
	} else {
		checks = append(checks, SinkCheck{Name: "Table", Status: "fail", Detail: fmt.Sprintf("Table %s.%s does not exist and Create Table If Not Exists is off", db, table)})
	}

	if ok, err := grant("INSERT", table); err != nil {
		checks = append(checks, SinkCheck{Name: "Insert grant", Status: "warn", Detail: "Could not verify the INSERT grant: " + err.Error()})
	} else if ok {
		checks = append(checks, SinkCheck{Name: "Insert grant", Status: "ok", Detail: fmt.Sprintf("%s may INSERT into %s.%s", who, db, table)})
	} else {
		checks = append(checks, SinkCheck{Name: "Insert grant", Status: "fail", Detail: fmt.Sprintf("%s lacks INSERT on %s.%s", who, db, table)})
	}

	if boolField(cfg.Fields, "dead_letter", true) {
		_, dlq := s.DeadLetterTable(cfg)
		createOK, err1 := grant("CREATE TABLE", dlq)
		insertOK, err2 := grant("INSERT", dlq)
		switch {
		case err1 != nil || err2 != nil:
			checks = append(checks, SinkCheck{Name: "Dead-letter table", Status: "warn", Detail: "Could not verify grants on " + db + "." + dlq})
		case insertOK && createOK:
			checks = append(checks, SinkCheck{Name: "Dead-letter table", Status: "ok", Detail: fmt.Sprintf("Refused rows go to %s.%s", db, dlq)})
		default:
			checks = append(checks, SinkCheck{Name: "Dead-letter table", Status: "warn", Detail: fmt.Sprintf("%s needs CREATE TABLE and INSERT on %s.%s, or refused rows keep the pipeline retrying", who, db, dlq)})
		}
	}
	return checks
}

// firstNumber reads the first column of a result row as a number. ClickHouse
// quotes 64-bit integers in JSON.
func firstNumber(row map[string]interface{}) float64 {
	for _, v := range row {
		switch n := v.(type) {
		case float64:
			return n
		case string:
			f, _ := strconv.ParseFloat(n, 64)
			return f
		case bool:
			if n {
				return 1
			}
		}
	}
	return 0
}
