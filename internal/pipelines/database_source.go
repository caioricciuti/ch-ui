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
	committed := func() interface{} {
		wmMu.Lock()
		defer wmMu.Unlock()
		return watermark
	}

	poll := func() error {
		start := committed()
		var rows *sql.Rows
		var queryErr error
		if watermarkCol != "" && start != nil {
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
			if watermarkCol != "" {
				if wv, ok := data[watermarkCol]; ok {
					last = wv
				}
			}

			raw, err := json.Marshal(data)
			if err != nil {
				return fmt.Errorf("encode row: %w", err)
			}
			buf = append(buf, Record{Data: data, RawJSON: raw})
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
