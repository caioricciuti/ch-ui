package pipelines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

// drainSpool is the source loop of the push sources (webhook, ingest). It
// claims the oldest sealed segment of the pipeline's stream, sends all its
// rows as one batch with the segment's dedup token, and waits: the runner
// acks the batch only after ClickHouse took it, and the ack deletes the
// segment. A failed write is retried by the runner with backoff, with the
// same token; when the pipeline stops first the segment is released and the
// next run replays it, again with the same token, so ClickHouse drops a
// replay of rows it already has.
//
// withData parses the first row of each batch into Record.Data, which the
// sink needs to infer columns when it creates an events table.
func drainSpool(ctx context.Context, cfg ConnectorConfig, out chan<- Batch, withData bool) error {
	hub := cfg.Hub
	if !hub.Available() {
		return ErrNoSpool
	}
	pipelineID := stringField(cfg.Fields, "pipeline_id", "")
	stats := hub.Stats(pipelineID)
	backoff := minBackoff
	for {
		seg, err := hub.sp.Claim(ctx, pipelineID)
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return nil
			case errors.Is(err, spool.ErrClosed):
				// The server is shutting down; the runner cancels us next.
				<-ctx.Done()
				return nil
			case errors.Is(err, spool.ErrStreamDropped):
				continue
			}
			cfg.Health.Fail("Ingest spool", err)
			if !sleepCtx(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = minBackoff

		recs := segmentRecords(seg, withData)
		done := make(chan struct{})
		n := int64(len(recs))
		batch := Batch{
			Records:    recs,
			SourceTS:   time.Now(),
			DedupToken: seg.DedupToken(),
			Ack: func() {
				if err := hub.sp.Ack(seg); err != nil {
					// The segment stays and is written again; the dedup
					// token makes that a no-op in ClickHouse.
					slog.Warn("Ingest spool ack failed", "pipeline", pipelineID, "segment", seg.ID, "error", err)
				}
				stats.Written(n, time.Now())
				close(done)
			},
		}
		if len(recs) == 0 {
			batch.Ack()
			continue
		}
		select {
		case out <- batch:
		case <-ctx.Done():
			_ = hub.sp.Release(seg)
			return nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			// Release after a concurrent Ack is a harmless ErrNotClaimed.
			_ = hub.sp.Release(seg)
			return nil
		}
	}
}

// segmentRecords splits a segment's JSONEachRow blocks into one record per
// row. The rows alias the segment's bytes.
func segmentRecords(seg *spool.Segment, withData bool) []Record {
	var recs []Record
	for _, block := range seg.Records() {
		for len(block) > 0 {
			line := block
			if i := bytes.IndexByte(block, '\n'); i >= 0 {
				line, block = block[:i], block[i+1:]
			} else {
				block = nil
			}
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			recs = append(recs, Record{RawJSON: line})
		}
	}
	if withData && len(recs) > 0 {
		var data map[string]interface{}
		if json.Unmarshal(recs[0].RawJSON, &data) == nil {
			recs[0].Data = data
		}
	}
	return recs
}

// encodeRows joins compact one-line JSON rows into a JSONEachRow block.
func encodeRows(records []Record) []byte {
	size := 0
	for _, r := range records {
		size += len(r.RawJSON) + 1
	}
	block := make([]byte, 0, size)
	for _, r := range records {
		block = append(block, r.RawJSON...)
		block = append(block, '\n')
	}
	return block
}
