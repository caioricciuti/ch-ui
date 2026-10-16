package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Ingest signals a source_ingest node accepts.
const (
	SignalLogs   = "logs"
	SignalTraces = "traces"
	SignalEvents = "events"
)

// DefaultIngestRateLimitRPS is the per-key request rate when rate_limit_rps
// is 0 or absent.
const DefaultIngestRateLimitRPS = 200

// IngestSource receives data pushed to /ingest/v1/{logs,traces,events} with
// an ingest key. The receivers spool it; this source drains the spool.
type IngestSource struct{}

func (s *IngestSource) Type() string { return "source_ingest" }

// IngestSignal returns a source_ingest config's signal.
func IngestSignal(fields map[string]interface{}) string {
	return stringField(fields, "signal", "")
}

// PushSignal is what a pushed-to source node receives: the signal of a
// source_ingest node, "webhook" for a source_webhook node, "" for any other.
func PushSignal(n database.PipelineNode) string {
	switch n.NodeType {
	case "source_webhook":
		return "webhook"
	case "source_ingest":
		var cfg map[string]interface{}
		if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) == nil {
			return IngestSignal(cfg)
		}
	}
	return ""
}

// Validate checks the signal and the limits.
func (s *IngestSource) Validate(cfg ConnectorConfig) error {
	switch IngestSignal(cfg.Fields) {
	case SignalLogs, SignalTraces, SignalEvents:
	default:
		return fmt.Errorf("signal must be logs, traces or events")
	}
	for _, f := range []string{"ttl_days", "rate_limit_rps", "daily_quota_mb", "stall_after_minutes"} {
		if err := checkMin(cfg.Fields, f, 0, f); err != nil {
			return err
		}
	}
	return nil
}

// Sample reads nothing: pushed data is only in the spool until written.
func (s *IngestSource) Sample(_ context.Context, cfg ConnectorConfig, _ int) (SampleResult, error) {
	signal := IngestSignal(cfg.Fields)
	return SampleResult{Note: "An ingest source has nothing to read: shippers push to /ingest/v1/" + signal + " with an ingest key. Create a key, run the pipeline, and watch the live counters."}, nil
}

// Start registers the pipeline with the receivers and drains its spool
// stream until ctx is cancelled. Data spooled while the pipeline was stopped
// stays and is written now.
func (s *IngestSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	if !cfg.Hub.Available() {
		return ErrNoSpool
	}
	signal := IngestSignal(cfg.Fields)
	rps := intField(cfg.Fields, "rate_limit_rps", 0)
	if rps <= 0 {
		rps = DefaultIngestRateLimitRPS
	}
	ep := &IngestEndpoint{
		PipelineID:      stringField(cfg.Fields, "pipeline_id", ""),
		Signal:          signal,
		RateLimitRPS:    rps,
		DailyQuotaBytes: int64(max(intField(cfg.Fields, "daily_quota_mb", 0), 0)) << 20,
	}
	cfg.Hub.Register(ep)
	defer cfg.Hub.Unregister(ep)
	slog.Info("Ingest source started", "pipeline", ep.PipelineID, "signal", signal)
	return drainSpool(ctx, cfg, out, signal == SignalEvents)
}
