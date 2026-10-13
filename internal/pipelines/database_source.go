package pipelines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// DatabaseSource polls a PostgreSQL, MySQL or SQLite database for new rows.
type DatabaseSource struct{}

func (d *DatabaseSource) Type() string { return "source_database" }

// dbTypeOf returns the configured db_type, postgres when unset.
func dbTypeOf(fields map[string]interface{}) string {
	if t := strings.TrimSpace(stringField(fields, "db_type", "")); t != "" {
		return t
	}
	return "postgres"
}

// Validate checks database source configuration.
func (d *DatabaseSource) Validate(cfg ConnectorConfig) error {
	dbType := dbTypeOf(cfg.Fields)
	connStr := stringField(cfg.Fields, "connection_string", "")
	query := stringField(cfg.Fields, "query", "")

	if dbType != "postgres" && dbType != "mysql" && dbType != "sqlite" {
		return fmt.Errorf("db_type must be 'postgres', 'mysql', or 'sqlite'")
	}
	if connStr == "" {
		return fmt.Errorf("connection_string is required")
	}
	if query == "" {
		return fmt.Errorf("query is required")
	}
	return validateCommon(cfg.Fields)
}

// IsSQLiteSource reports whether a node config is a SQLite database source.
// Such sources read files on the CH-UI host, so only admins may save or
// start them.
func IsSQLiteSource(nodeType string, fields map[string]interface{}) bool {
	return nodeType == "source_database" && dbTypeOf(fields) == "sqlite"
}

// CheckSQLitePath refuses a SQLite connection string that points inside
// CH-UI's own data directory (its database, keys and backups live there).
func CheckSQLitePath(connStr, dataDir string) error {
	p := strings.TrimSpace(connStr)
	p = strings.TrimPrefix(p, "file:")
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == ":memory:" {
		return nil
	}
	if dataDir == "" {
		return nil
	}
	target, err := canonicalPath(p)
	if err != nil {
		return fmt.Errorf("sqlite path: %w", err)
	}
	dir, err := canonicalPath(dataDir)
	if err != nil {
		return fmt.Errorf("data directory: %w", err)
	}
	if target == dir || strings.HasPrefix(target, dir+string(filepath.Separator)) {
		return errors.New("a SQLite source cannot read files inside CH-UI's data directory")
	}
	return nil
}

// canonicalPath returns an absolute path with symlinks resolved as far as the
// path exists.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	// Resolve the existing parent so a not-yet-existing file under a
	// symlinked directory is still compared correctly.
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return abs, nil
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

// Start polls the source database and sends batches to the output channel.
// With a watermark column, the watermark advances only when the runner acks
// a batch (after it is written), and each poll waits for its batches to be
// acked, so a failed write is re-read instead of skipped.
func (d *DatabaseSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	dbType := dbTypeOf(cfg.Fields)
	connStr := stringField(cfg.Fields, "connection_string", "")
	query := stringField(cfg.Fields, "query", "")
	pollIntervalSec := max(intField(cfg.Fields, "poll_interval", 60), 1)
	watermarkCol := stringField(cfg.Fields, "watermark_column", "")
	batchSize := max(intField(cfg.Fields, "batch_size", 1000), 1)
	pipelineID := stringField(cfg.Fields, "pipeline_id", "")

	db, err := sql.Open(dbType, connStr)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("Database source started", "type", dbType, "poll_interval", pollIntervalSec)

	var (
		wmMu      sync.Mutex
		watermark interface{} // last watermark of a written batch
	)
	// Resume from the watermark saved after the last written batch.
	if watermarkCol != "" {
		if saved := cfg.Checkpoints[watermarkCheckpointKey]; saved != "" {
			if v, err := decodeWatermark(saved); err == nil {
				watermark = v
				slog.Info("Database source resuming from saved watermark", "pipeline", pipelineID, "watermark", watermarkString(v))
			} else {
				slog.Warn("Ignoring unreadable saved watermark", "pipeline", pipelineID, "error", err)
			}
		}
	}
	committed := func() interface{} {
		wmMu.Lock()
		defer wmMu.Unlock()
		return watermark
	}

	poll := func() error {
		start := committed()
		var rows *sql.Rows
		var queryErr error
		if watermarkCol != "" {
			// Before the first written batch the watermark is NULL, so a
			// query like "WHERE id > COALESCE($1, 0)" reads from the start.
			rows, queryErr = db.QueryContext(ctx, query, start)
		} else {
			rows, queryErr = db.QueryContext(ctx, query)
		}
		if queryErr != nil {
			return fmt.Errorf("query: %w", queryErr)
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return fmt.Errorf("get columns: %w", err)
		}

		var acks []chan struct{}
		seq := 0
		emit := func(buf []Record, last interface{}) bool {
			seq++
			acked := make(chan struct{})
			var once sync.Once
			batch := Batch{
				Records:  buf,
				SourceTS: time.Now(),
				Ack: func() {
					once.Do(func() {
						if watermarkCol != "" && last != nil {
							wmMu.Lock()
							watermark = last
							wmMu.Unlock()
						}
						close(acked)
					})
				},
			}
			if watermarkCol != "" {
				batch.DedupToken = dedupToken("db", pipelineID, watermarkString(start), seq, watermarkString(last), len(buf))
				if last != nil {
					if pos, err := encodeWatermark(last); err == nil {
						batch.Checkpoint = &Checkpoint{Key: watermarkCheckpointKey, Position: pos}
					}
				}
			}
			select {
			case out <- batch:
				acks = append(acks, acked)
				return true
			case <-ctx.Done():
				return false
			}
		}

		var buf []Record
		var last interface{}
		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}
			if err := rows.Scan(valuePtrs...); err != nil {
				return fmt.Errorf("scan row: %w", err)
			}

			data := make(map[string]interface{}, len(columns))
			for i, col := range columns {
				val := values[i]
				// Convert []byte to string for JSON compatibility
				if b, ok := val.([]byte); ok {
					data[col] = string(b)
				} else {
					data[col] = val
				}
			}
			pos := ""
			if watermarkCol != "" {
				if wv, ok := data[watermarkCol]; ok {
					last = wv
					pos = watermarkCol + "=" + watermarkString(wv)
				}
			}

			raw, err := json.Marshal(data)
			if err != nil {
				return fmt.Errorf("encode row: %w", err)
			}
			buf = append(buf, Record{Data: data, RawJSON: raw, Position: pos})
			if len(buf) >= batchSize {
				if !emit(buf, last) {
					return nil
				}
				buf = nil
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows iteration: %w", err)
		}
		if len(buf) > 0 && !emit(buf, last) {
			return nil
		}

		// The next poll starts from the committed watermark, so wait until
		// everything read in this poll has been written.
		for _, a := range acks {
			select {
			case <-a:
			case <-ctx.Done():
				return nil
			}
		}
		return nil
	}

	runPoll := func() {
		if err := poll(); err != nil && ctx.Err() == nil {
			cfg.Health.Fail("Database source", err)
			return
		}
		if ctx.Err() == nil {
			cfg.Health.OK()
		}
	}

	runPoll()
	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runPoll()
		}
	}
}

// watermarkString renders a watermark value for a dedup token.
func watermarkString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

// watermarkCheckpointKey is the checkpoint key of a database source.
const watermarkCheckpointKey = "watermark"

// savedWatermark keeps the Go type of a watermark so a resumed poll binds
// the same kind of value (an integer stays an integer).
type savedWatermark struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func encodeWatermark(v interface{}) (string, error) {
	var w savedWatermark
	switch t := v.(type) {
	case int64:
		w = savedWatermark{"int", strconv.FormatInt(t, 10)}
	case int:
		w = savedWatermark{"int", strconv.Itoa(t)}
	case int32:
		w = savedWatermark{"int", strconv.FormatInt(int64(t), 10)}
	case uint64:
		w = savedWatermark{"uint", strconv.FormatUint(t, 10)}
	case float64:
		w = savedWatermark{"float", strconv.FormatFloat(t, 'g', -1, 64)}
	case float32:
		w = savedWatermark{"float", strconv.FormatFloat(float64(t), 'g', -1, 32)}
	case bool:
		w = savedWatermark{"bool", strconv.FormatBool(t)}
	case time.Time:
		w = savedWatermark{"time", t.Format(time.RFC3339Nano)}
	case string:
		w = savedWatermark{"string", t}
	case []byte:
		w = savedWatermark{"string", string(t)}
	default:
		w = savedWatermark{"string", fmt.Sprint(t)}
	}
	b, err := json.Marshal(w)
	return string(b), err
}

func decodeWatermark(s string) (interface{}, error) {
	var w savedWatermark
	if err := json.Unmarshal([]byte(s), &w); err != nil {
		return nil, err
	}
	switch w.Kind {
	case "int":
		return strconv.ParseInt(w.Value, 10, 64)
	case "uint":
		return strconv.ParseUint(w.Value, 10, 64)
	case "float":
		return strconv.ParseFloat(w.Value, 64)
	case "bool":
		return strconv.ParseBool(w.Value)
	case "time":
		return time.Parse(time.RFC3339Nano, w.Value)
	case "string":
		return w.Value, nil
	}
	return nil, fmt.Errorf("unknown watermark kind %q", w.Kind)
}

// Sample runs the query once (wrapped in LIMIT) and returns up to limit rows.
// A saved watermark is bound as on a resumed poll.
func (d *DatabaseSource) Sample(ctx context.Context, cfg ConnectorConfig, limit int) (SampleResult, error) {
	dbType := dbTypeOf(cfg.Fields)
	query := strings.TrimRight(strings.TrimSpace(stringField(cfg.Fields, "query", "")), "; \n\t")
	watermarkCol := stringField(cfg.Fields, "watermark_column", "")

	db, err := sql.Open(dbType, stringField(cfg.Fields, "connection_string", ""))
	if err != nil {
		return SampleResult{}, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	wrapped := fmt.Sprintf("SELECT * FROM (%s) AS chui_sample LIMIT %d", query, limit)
	var rows *sql.Rows
	note := "First rows of the query (LIMIT " + strconv.Itoa(limit) + ")."
	if saved := cfg.Checkpoints[watermarkCheckpointKey]; watermarkCol != "" && saved != "" {
		wm, derr := decodeWatermark(saved)
		if derr != nil {
			return SampleResult{}, fmt.Errorf("saved watermark: %w", derr)
		}
		rows, err = db.QueryContext(ctx, wrapped, wm)
		note = fmt.Sprintf("Next rows after the saved watermark %s = %s (LIMIT %d).", watermarkCol, watermarkString(wm), limit)
	} else if watermarkCol != "" {
		rows, err = db.QueryContext(ctx, wrapped, nil)
		note = fmt.Sprintf("First rows with no saved watermark, the parameter bound as NULL (LIMIT %d).", limit)
	} else {
		rows, err = db.QueryContext(ctx, wrapped)
	}
	if err != nil {
		return SampleResult{}, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return SampleResult{}, err
	}
	var out []map[string]interface{}
	for rows.Next() && len(out) < limit {
		values := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return SampleResult{}, fmt.Errorf("scan row: %w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, c := range columns {
			if b, ok := values[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = values[i]
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return SampleResult{}, err
	}
	return SampleResult{Records: out, Note: note}, nil
}
