package pipelines

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

// ErrNoSpool is returned by spooled sources and receivers when the server
// runs without an ingest spool (it could not be opened).
var ErrNoSpool = errors.New("the ingest spool is not available on this server")

// Hub connects the push receivers (/ingest and the webhook) to the running
// pipelines. Receivers Append to the pipeline's spool stream and answer the
// client only after Append returned; the pipeline's source drains the stream
// into ClickHouse. One stream per pipeline id.
type Hub struct {
	sp *spool.Spool

	mu        sync.Mutex
	endpoints map[string]*IngestEndpoint
	stats     map[string]*IngestStats
}

// IngestEndpoint is a running pipeline that accepts pushed data.
type IngestEndpoint struct {
	PipelineID string
	// Signal is "logs", "traces" or "events" for source_ingest, "webhook"
	// for source_webhook.
	Signal string
	// RateLimitRPS is requests per second per key (source_ingest only).
	RateLimitRPS int
	// DailyQuotaBytes is decompressed request bytes per UTC day for the
	// pipeline; 0 is unlimited.
	DailyQuotaBytes int64
}

// NewHub wraps an open spool. sp may be nil: every Append then fails with
// ErrNoSpool and spooled sources refuse to start.
func NewHub(sp *spool.Spool) *Hub {
	return &Hub{sp: sp, endpoints: map[string]*IngestEndpoint{}, stats: map[string]*IngestStats{}}
}

// Available reports whether the hub has a spool.
func (h *Hub) Available() bool { return h != nil && h.sp != nil }

// Append writes one record (a JSONEachRow block) to the pipeline's stream.
// When it returns nil the record is durable.
func (h *Hub) Append(pipelineID string, block []byte) error {
	if !h.Available() {
		return ErrNoSpool
	}
	return h.sp.Append(pipelineID, block)
}

// Endpoint returns the running endpoint of a pipeline, or nil.
func (h *Hub) Endpoint(pipelineID string) *IngestEndpoint {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.endpoints[pipelineID]
}

// Register makes a pipeline's endpoint accept pushed data. The running
// source calls it; Unregister with the same pointer undoes it.
func (h *Hub) Register(ep *IngestEndpoint) {
	h.mu.Lock()
	h.endpoints[ep.PipelineID] = ep
	h.mu.Unlock()
}

// Unregister removes ep if it is still the pipeline's endpoint.
func (h *Hub) Unregister(ep *IngestEndpoint) {
	h.mu.Lock()
	if h.endpoints[ep.PipelineID] == ep {
		delete(h.endpoints, ep.PipelineID)
	}
	h.mu.Unlock()
}

// Stats returns the in-memory counters of a pipeline, creating them.
func (h *Hub) Stats(pipelineID string) *IngestStats {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.stats[pipelineID]
	if s == nil {
		s = &IngestStats{}
		h.stats[pipelineID] = s
	}
	return s
}

// SpoolStats reports the unacked data of a pipeline's stream.
func (h *Hub) SpoolStats(pipelineID string) spool.Stats {
	if !h.Available() {
		return spool.Stats{}
	}
	return h.sp.Stats(pipelineID)
}

// DropStream deletes a pipeline's spooled data and counters. Called when the
// pipeline is deleted.
func (h *Hub) DropStream(pipelineID string) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	delete(h.stats, pipelineID)
	h.mu.Unlock()
	if h.sp == nil {
		return nil
	}
	return h.sp.DropStream(pipelineID)
}

// SpoolTotal reports the unacked data of every stream.
func (h *Hub) SpoolTotal() spool.Stats {
	if !h.Available() {
		return spool.Stats{}
	}
	return h.sp.TotalStats()
}

// Close closes the spool. Call it after the runner stopped.
func (h *Hub) Close() error {
	if !h.Available() {
		return nil
	}
	return h.sp.Close()
}

// IngestStats are a pipeline's push counters since process start.
type IngestStats struct {
	Requests atomic.Int64
	Records  atomic.Int64
	Bytes    atomic.Int64

	RejectedAuth      atomic.Int64
	RejectedRateLimit atomic.Int64
	RejectedQuota     atomic.Int64
	RejectedTooLarge  atomic.Int64
	RejectedMalformed atomic.Int64
	RejectedSpoolFull atomic.Int64

	WrittenRows    atomic.Int64
	WrittenBatches atomic.Int64
	lastWriteNs    atomic.Int64
	lastAcceptNs   atomic.Int64

	rateMu sync.Mutex
	rate   [rateWindow]rateBucket
}

const rateWindow = 60

type rateBucket struct {
	sec     int64
	records int64
	bytes   int64
}

// Accepted counts one request that was spooled.
func (s *IngestStats) Accepted(records, bytes int64, now time.Time) {
	s.Requests.Add(1)
	s.Records.Add(records)
	s.Bytes.Add(bytes)
	s.lastAcceptNs.Store(now.UnixNano())
	sec := now.Unix()
	s.rateMu.Lock()
	b := &s.rate[sec%rateWindow]
	if b.sec != sec {
		*b = rateBucket{sec: sec}
	}
	b.records += records
	b.bytes += bytes
	s.rateMu.Unlock()
}

// LastAccepted is when a request was last spooled, zero before the first
// one since process start.
func (s *IngestStats) LastAccepted() time.Time {
	if ns := s.lastAcceptNs.Load(); ns > 0 {
		return time.Unix(0, ns)
	}
	return time.Time{}
}

// RejectedOther sums the rejections other than auth: quota, rate limit,
// too large, malformed and spool full.
func (s *IngestStats) RejectedOther() int64 {
	return s.RejectedRateLimit.Load() + s.RejectedQuota.Load() + s.RejectedTooLarge.Load() +
		s.RejectedMalformed.Load() + s.RejectedSpoolFull.Load()
}

// Written counts one batch inserted into ClickHouse.
func (s *IngestStats) Written(rows int64, now time.Time) {
	s.WrittenRows.Add(rows)
	s.WrittenBatches.Add(1)
	s.lastWriteNs.Store(now.UnixNano())
}

// rate1m returns records and bytes per second over the last minute.
func (s *IngestStats) rate1m(now time.Time) (float64, float64) {
	cutoff := now.Unix() - rateWindow
	var recs, bytes int64
	s.rateMu.Lock()
	for _, b := range s.rate {
		if b.sec > cutoff {
			recs += b.records
			bytes += b.bytes
		}
	}
	s.rateMu.Unlock()
	return float64(recs) / rateWindow, float64(bytes) / rateWindow
}

// IngestStatsView is the JSON shape of GET /api/pipelines/{id}/ingest-stats.
type IngestStatsView struct {
	Signal   string `json:"signal"`
	Received struct {
		Requests int64 `json:"requests"`
		Records  int64 `json:"records"`
		Bytes    int64 `json:"bytes"`
	} `json:"received"`
	Rejected struct {
		Auth      int64 `json:"auth"`
		RateLimit int64 `json:"rate_limit"`
		Quota     int64 `json:"quota"`
		TooLarge  int64 `json:"too_large"`
		Malformed int64 `json:"malformed"`
		SpoolFull int64 `json:"spool_full"`
	} `json:"rejected"`
	Written struct {
		Rows        int64   `json:"rows"`
		Batches     int64   `json:"batches"`
		LastWriteAt *string `json:"last_write_at"`
	} `json:"written"`
	Spool struct {
		Bytes       int64 `json:"bytes"`
		Segments    int   `json:"segments"`
		OldestAgeMs int64 `json:"oldest_age_ms"`
	} `json:"spool"`
	Rate struct {
		RecordsPerSec1m float64 `json:"records_per_sec_1m"`
		BytesPerSec1m   float64 `json:"bytes_per_sec_1m"`
	} `json:"rate"`
	LastError *string `json:"last_error"`
}

// View renders a pipeline's counters and spool usage. Signal and LastError
// are left for the caller, which knows the pipeline.
func (h *Hub) View(pipelineID string, now time.Time) IngestStatsView {
	var v IngestStatsView
	s := h.Stats(pipelineID)
	v.Received.Requests = s.Requests.Load()
	v.Received.Records = s.Records.Load()
	v.Received.Bytes = s.Bytes.Load()
	v.Rejected.Auth = s.RejectedAuth.Load()
	v.Rejected.RateLimit = s.RejectedRateLimit.Load()
	v.Rejected.Quota = s.RejectedQuota.Load()
	v.Rejected.TooLarge = s.RejectedTooLarge.Load()
	v.Rejected.Malformed = s.RejectedMalformed.Load()
	v.Rejected.SpoolFull = s.RejectedSpoolFull.Load()
	v.Written.Rows = s.WrittenRows.Load()
	v.Written.Batches = s.WrittenBatches.Load()
	if ns := s.lastWriteNs.Load(); ns > 0 {
		t := time.Unix(0, ns).UTC().Format(time.RFC3339)
		v.Written.LastWriteAt = &t
	}
	st := h.SpoolStats(pipelineID)
	v.Spool.Bytes = st.Bytes
	v.Spool.Segments = st.Segments
	v.Spool.OldestAgeMs = st.OldestAge.Milliseconds()
	v.Rate.RecordsPerSec1m, v.Rate.BytesPerSec1m = s.rate1m(now)
	return v
}
