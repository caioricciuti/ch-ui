package pipelines

import (
	"context"
	"sync/atomic"
	"time"
)

// Record represents a single data record flowing through the pipeline.
type Record struct {
	Data    map[string]interface{} // Column name -> value
	RawJSON []byte                 // Original bytes for pass-through; must be one valid JSON object
	// Position, when set, names where the record came from (Kafka
	// topic/partition/offset, S3 key and line, database watermark). It is
	// written to the dead-letter table with a rejected record.
	Position string
}

// Checkpoint is a source position to persist once its batch is written.
type Checkpoint struct {
	Key      string
	Position string
}

// Batch is a slice of records ready for INSERT.
type Batch struct {
	Records  []Record
	SourceTS time.Time
	// Ack, if non-nil, is called by the runner only AFTER the batch has been
	// durably written to the sink. Sources advance their position (Kafka
	// offsets, database watermark, processed S3 files) from Ack, so a failed
	// or interrupted write re-delivers the records: at-least-once delivery.
	Ack func()
	// DedupToken, when set, is sent as insert_deduplication_token. It is
	// derived from a stable source position so that a retried or re-read
	// batch produces the same token and ClickHouse can drop the duplicate
	// (Replicated tables, or tables with non_replicated_deduplication_window).
	DedupToken string
	// Checkpoint, when set, is saved by the runner after the batch is
	// written, right before Ack, so a restart resumes after this batch.
	Checkpoint *Checkpoint
}

// ConnectorConfig is the parsed config for a connector node.
type ConnectorConfig struct {
	NodeType string                 `json:"node_type"`
	Fields   map[string]interface{} `json:"fields"`
	// Health, set by the runner, lets a source report connection or poll
	// errors while it keeps retrying. It may be nil (tests, validation).
	Health *Health `json:"-"`
	// Checkpoints are the positions saved by earlier runs, by key. Set by
	// the runner at start; nil means none.
	Checkpoints map[string]string `json:"-"`
	// Hub, set by the runner, is the ingest spool the push sources (webhook,
	// ingest) drain. It may be nil.
	Hub *Hub `json:"-"`
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
	DLQRows       atomic.Int64
	LastBatchAt   atomic.Value // time.Time
}
