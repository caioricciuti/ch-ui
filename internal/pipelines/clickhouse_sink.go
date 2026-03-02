package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ClickHouseSink writes batches to a ClickHouse table via the tunnel gateway.
type ClickHouseSink struct {
	gateway   *tunnel.Gateway
	db        *database.DB
	secretKey string

	tableOnce sync.Once
	tableErr  error
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
	return nil
}

// WriteBatch inserts a batch of records into the ClickHouse table using INSERT FORMAT JSONEachRow.
func (s *ClickHouseSink) WriteBatch(ctx context.Context, cfg ConnectorConfig, batch Batch) (int, error) {
	if len(batch.Records) == 0 {
		return 0, nil
	}

	// Auto-create table on first batch if configured
	if boolField(cfg.Fields, "create_table", false) {
		s.tableOnce.Do(func() {
			s.tableErr = s.ensureTable(ctx, cfg, batch)
		})
		if s.tableErr != nil {
			return 0, fmt.Errorf("ensure table: %w", s.tableErr)
		}
	}

	db, _ := cfg.Fields["database"].(string)
	table, _ := cfg.Fields["table"].(string)

	// Build JSONEachRow payload
	var sb strings.Builder
	for _, rec := range batch.Records {
		if len(rec.RawJSON) > 0 {
			sb.Write(rec.RawJSON)
		} else {
			raw, err := json.Marshal(rec.Data)
			if err != nil {
				return 0, fmt.Errorf("marshal record: %w", err)
			}
			sb.Write(raw)
		}
		sb.WriteByte('\n')
	}

	query := fmt.Sprintf("INSERT INTO `%s`.`%s` FORMAT JSONEachRow\n%s", db, table, sb.String())

	// Find credentials from the pipeline's connection
	connectionID, _ := cfg.Fields["connection_id"].(string)
	if connectionID == "" {
		return 0, fmt.Errorf("no connection_id in sink config")
	}

	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return 0, fmt.Errorf("find credentials: %w", err)
	}

	_, execErr := s.gateway.ExecuteQuery(connectionID, query, user, password, 30*time.Second)
	if execErr != nil {
		return 0, fmt.Errorf("execute insert: %w", execErr)
	}

	return len(batch.Records), nil
}

// ensureTable creates the target table if it doesn't exist, inferring schema from the first batch.
func (s *ClickHouseSink) ensureTable(ctx context.Context, cfg ConnectorConfig, batch Batch) error {
	db := stringField(cfg.Fields, "database", "default")
	table := stringField(cfg.Fields, "table", "")
	engine := stringField(cfg.Fields, "create_table_engine", "MergeTree")
	orderBy := stringField(cfg.Fields, "create_table_order_by", "tuple()")

	if table == "" {
		return fmt.Errorf("table name is required for auto-creation")
	}
	if orderBy == "" {
		orderBy = "tuple()"
	}

	// Infer columns from first record
	if len(batch.Records) == 0 {
		return fmt.Errorf("cannot infer schema from empty batch")
	}

	data := batch.Records[0].Data
	if len(data) == 0 {
		return fmt.Errorf("cannot infer schema from empty record")
	}

	// Collect column names sorted for deterministic output
	colNames := make([]string, 0, len(data))
	for k := range data {
		colNames = append(colNames, k)
	}
	sort.Strings(colNames)

	// Build column definitions
	var cols []string
	for _, name := range colNames {
		chType := inferClickHouseType(data[name])
		cols = append(cols, fmt.Sprintf("`%s` %s", name, chType))
	}

	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`.`%s` (\n  %s\n) ENGINE = %s\nORDER BY %s",
		db, table, strings.Join(cols, ",\n  "), engine, orderBy)

	connectionID, _ := cfg.Fields["connection_id"].(string)
	if connectionID == "" {
		return fmt.Errorf("no connection_id in sink config")
	}

	user, password, err := s.findCredentials(connectionID)
	if err != nil {
		return fmt.Errorf("find credentials: %w", err)
	}

	_, execErr := s.gateway.ExecuteQuery(connectionID, ddl, user, password, 30*time.Second)
	if execErr != nil {
		return fmt.Errorf("execute CREATE TABLE: %w", execErr)
	}

	slog.Info("Auto-created ClickHouse table", "database", db, "table", table, "engine", engine, "columns", len(cols))
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

// findCredentials retrieves ClickHouse credentials from active sessions.
func (s *ClickHouseSink) findCredentials(connectionID string) (string, string, error) {
	sessions, err := s.db.GetActiveSessionsByConnection(connectionID, 3)
	if err != nil {
		return "", "", fmt.Errorf("failed to load sessions: %w", err)
	}

	for _, sess := range sessions {
		password, err := crypto.Decrypt(sess.EncryptedPassword, s.secretKey)
		if err != nil {
			continue
		}
		return sess.ClickhouseUser, password, nil
	}

	return "", "", fmt.Errorf("no active sessions with valid credentials for connection %s", connectionID)
}
