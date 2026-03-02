package pipelines

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// DatabaseSource polls a PostgreSQL or MySQL database for new rows.
type DatabaseSource struct{}

func (d *DatabaseSource) Type() string { return "source_database" }

// Validate checks database source configuration.
func (d *DatabaseSource) Validate(cfg ConnectorConfig) error {
	dbType := stringField(cfg.Fields, "db_type", "")
	connStr := stringField(cfg.Fields, "connection_string", "")
	query := stringField(cfg.Fields, "query", "")

	if dbType == "" {
		return fmt.Errorf("db_type is required (postgres, mysql, or sqlite)")
	}
	if dbType != "postgres" && dbType != "mysql" && dbType != "sqlite" {
		return fmt.Errorf("db_type must be 'postgres', 'mysql', or 'sqlite'")
	}
	if connStr == "" {
		return fmt.Errorf("connection_string is required")
	}
	if query == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

// Start begins polling the source database and sends batches to the output channel.
func (d *DatabaseSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	dbType := stringField(cfg.Fields, "db_type", "")
	connStr := stringField(cfg.Fields, "connection_string", "")
	query := stringField(cfg.Fields, "query", "")
	pollIntervalSec := intField(cfg.Fields, "poll_interval", 60)
	watermarkCol := stringField(cfg.Fields, "watermark_column", "")
	batchSize := intField(cfg.Fields, "batch_size", 1000)

	// Map db_type to driver name
	driver := dbType
	if dbType == "postgres" {
		driver = "postgres"
	}

	db, err := sql.Open(driver, connStr)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	slog.Info("Database source started", "type", dbType, "poll_interval", pollIntervalSec)

	var watermark interface{}
	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()

	poll := func() error {
		var rows *sql.Rows
		var queryErr error

		if watermarkCol != "" && watermark != nil {
			rows, queryErr = db.QueryContext(ctx, query, watermark)
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

		var buf []Record
		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				slog.Warn("Database source row scan error", "error", err)
				continue
			}

			data := make(map[string]interface{})
			for i, col := range columns {
				val := values[i]
				// Convert []byte to string for JSON compatibility
				if b, ok := val.([]byte); ok {
					data[col] = string(b)
				} else {
					data[col] = val
				}
			}

			// Update watermark
			if watermarkCol != "" {
				if wv, ok := data[watermarkCol]; ok {
					watermark = wv
				}
			}

			raw, _ := json.Marshal(data)
			buf = append(buf, Record{
				Data:    data,
				RawJSON: raw,
			})

			if len(buf) >= batchSize {
				select {
				case out <- Batch{Records: buf, SourceTS: time.Now()}:
				case <-ctx.Done():
					return nil
				}
				buf = nil
			}
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows iteration: %w", err)
		}

		// Flush remaining
		if len(buf) > 0 {
			select {
			case out <- Batch{Records: buf, SourceTS: time.Now()}:
			case <-ctx.Done():
				return nil
			}
		}

		return nil
	}

	// First poll
	if err := poll(); err != nil {
		slog.Error("Database source poll error", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := poll(); err != nil {
				slog.Error("Database source poll error", "error", err)
			}
		}
	}
}
