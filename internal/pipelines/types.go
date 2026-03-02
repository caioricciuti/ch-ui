package pipelines

import (
	"context"
	"sync/atomic"
	"time"
)

// Record represents a single data record flowing through the pipeline.
type Record struct {
	Data    map[string]interface{} // Column name -> value
	RawJSON []byte                 // Original bytes for pass-through
}

// Batch is a slice of records ready for INSERT.
type Batch struct {
	Records  []Record
	SourceTS time.Time
}

// ConnectorConfig is the parsed config for a connector node.
type ConnectorConfig struct {
	NodeType string                 `json:"node_type"`
	Fields   map[string]interface{} `json:"fields"`
}

// SourceConnector is the interface all source connectors implement.
type SourceConnector interface {
	// Validate checks configuration before pipeline start.
	Validate(cfg ConnectorConfig) error

	// Start begins reading data. It sends batches to the output channel.
	// It blocks until ctx is cancelled or an unrecoverable error occurs.
	Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error

	// Type returns the connector type identifier.
	Type() string
}

// SinkConnector writes batches to the destination.
type SinkConnector interface {
	Validate(cfg ConnectorConfig) error
	WriteBatch(ctx context.Context, cfg ConnectorConfig, batch Batch) (rowsWritten int, err error)
	Type() string
}

// Metrics tracks pipeline execution metrics (thread-safe via atomic).
type Metrics struct {
	RowsIngested  atomic.Int64
	BytesIngested atomic.Int64
	BatchesSent   atomic.Int64
	ErrorsCount   atomic.Int64
	LastBatchAt   atomic.Value // time.Time
}
