package ingest

import (
	"compress/gzip"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
)

// MaxBodyBytes bounds a request body after decompression. The compressed
// body is bounded by the same number.
const MaxBodyBytes = 32 << 20

// touchEvery throttles the last_used_at write per key.
const touchEvery = time.Minute

// gRPC status codes carried in OTLP error bodies.
const (
	codeInvalidArgument   int32 = 3
	codeNotFound          int32 = 5
	codeResourceExhausted int32 = 8
	codeUnavailable       int32 = 14
	codeUnauthenticated   int32 = 16
)

// Receiver serves the /ingest/v1 endpoints.
type Receiver struct {
	DB  *database.DB
	Hub *pipelines.Hub
	// Limits bound what one OTLP request may decode to; zero fields take
	// otlp.DefaultLimits.
	Limits otlp.Limits
	// MaxBody overrides MaxBodyBytes (tests).
	MaxBody int64

	now     func() time.Time
	lim     *limiter
	touchMu sync.Mutex
	touched map[string]time.Time
}

// NewReceiver returns a receiver for the pipelines in db, spooling into hub.
func NewReceiver(db *database.DB, hub *pipelines.Hub) *Receiver {
	return &Receiver{DB: db, Hub: hub, now: time.Now, lim: newLimiter(), touched: map[string]time.Time{}}
}

// Routes registers POST /v1/logs, /v1/traces and /v1/events on r. Mount it
// at /ingest, and on the separate listener also at the root so a bare host
// works as OTEL_EXPORTER_OTLP_ENDPOINT.
func (rv *Receiver) Routes(r chi.Router) {
	r.Post("/v1/logs", rv.handler(pipelines.SignalLogs))
	r.Post("/v1/traces", rv.handler(pipelines.SignalTraces))
	r.Post("/v1/events", rv.handler(pipelines.SignalEvents))
}

func (rv *Receiver) maxBody() int64 {
	if rv.MaxBody > 0 {
		return rv.MaxBody
	}
	return MaxBodyBytes
}

// reply writes errors in the endpoint's format: an OTLP google.rpc.Status
// for logs and traces, {"error": ...} for events.
type reply struct {
	w           http.ResponseWriter
	otlp        bool
	contentType string
}

func (p reply) fail(status int, code int32, msg string) {
	if p.otlp {
		body, ct := otlp.StatusResponse(p.contentType, code, msg)
		p.w.Header().Set("Content-Type", ct)
		p.w.WriteHeader(status)
		_, _ = p.w.Write(body)
		return
	}
	p.w.Header().Set("Content-Type", "application/json")
	p.w.WriteHeader(status)
	_ = json.NewEncoder(p.w).Encode(map[string]string{"error": msg})
}

func (p reply) retryAfter(d time.Duration) {
	p.w.Header().Set("Retry-After", strconv.Itoa(max(int(d.Seconds()+0.5), 1)))
}

func (rv *Receiver) handler(signal string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rv.serve(w, r, signal)
	}
}

func (rv *Receiver) serve(w http.ResponseWriter, r *http.Request, signal string) {
	now := rv.now()
	ct := r.Header.Get("Content-Type")
	rep := reply{w: w, otlp: signal != pipelines.SignalEvents, contentType: ct}

	key := rv.authenticate(r)
	if key == nil {
		rep.fail(http.StatusUnauthorized, codeUnauthenticated, "missing or invalid ingest key")
		return
	}
	stats := rv.Hub.Stats(key.PipelineID)
	if key.RevokedAt != nil {
		stats.RejectedAuth.Add(1)
		rep.fail(http.StatusUnauthorized, codeUnauthenticated, "missing or invalid ingest key")
		return
	}
	ep := rv.Hub.Endpoint(key.PipelineID)
	if ep == nil {
		// Stopped (or not started yet): ask the shipper to retry, it keeps
		// its data. Spooled data waits for the next start.
		rep.retryAfter(30 * time.Second)
		rep.fail(http.StatusServiceUnavailable, codeUnavailable, "the pipeline for this key is not running")
		return
	}
	if ep.Signal != signal {
		rep.fail(http.StatusNotFound, codeNotFound, "this key does not accept "+signal)
		return
	}
	rv.touch(key, now)

	if !rv.lim.allow(key.ID, ep.RateLimitRPS, now) {
		stats.RejectedRateLimit.Add(1)
		rep.retryAfter(time.Second)
		rep.fail(http.StatusTooManyRequests, codeResourceExhausted, "rate limit exceeded")
		return
	}
	if !rv.lim.quotaLeft(ep.PipelineID, ep.DailyQuotaBytes, now) {
		stats.RejectedQuota.Add(1)
		rep.retryAfter(untilMidnightUTC(now))
		rep.fail(http.StatusTooManyRequests, codeResourceExhausted, "daily quota exhausted, resets at 00:00 UTC")
		return
	}
	if !acceptedContentType(signal, ct) {
		stats.RejectedMalformed.Add(1)
		rep.fail(http.StatusUnsupportedMediaType, codeInvalidArgument, "unsupported content type")
		return
	}

	body, status, msg := rv.readBody(w, r)
	if status != 0 {
		switch status {
		case http.StatusRequestEntityTooLarge:
			stats.RejectedTooLarge.Add(1)
		default:
			stats.RejectedMalformed.Add(1)
		}
		rep.fail(status, codeInvalidArgument, msg)
		return
	}
	if !rv.lim.charge(ep.PipelineID, ep.DailyQuotaBytes, int64(len(body)), now) {
		stats.RejectedQuota.Add(1)
		rep.retryAfter(untilMidnightUTC(now))
		rep.fail(http.StatusTooManyRequests, codeResourceExhausted, "daily quota exhausted, resets at 00:00 UTC")
		return
	}

	block, n, err := rv.encode(signal, body, ct, now)
	if err != nil {
		if errors.Is(err, otlp.ErrTooManyRecords) {
			stats.RejectedTooLarge.Add(1)
			rep.fail(http.StatusRequestEntityTooLarge, codeInvalidArgument, err.Error())
			return
		}
		stats.RejectedMalformed.Add(1)
		rep.fail(http.StatusBadRequest, codeInvalidArgument, err.Error())
		return
	}
	if n > 0 {
		if status, msg := pipelines.SpoolAppendStatus(rv.Hub.Append(ep.PipelineID, block), stats); status != http.StatusOK {
			if status == http.StatusServiceUnavailable {
				rep.retryAfter(5 * time.Second)
				rep.fail(status, codeUnavailable, msg)
			} else {
				rep.fail(status, codeInvalidArgument, msg)
			}
			return
		}
	}
	stats.Accepted(int64(n), int64(len(body)), now)

	if !rep.otlp {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]int{"accepted": n})
		return
	}
	resp, rct := otlp.SuccessResponse(ct)
	w.Header().Set("Content-Type", rct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

// authenticate returns the key the request presents, revoked or not, or nil
// when it presents none or an unknown one.
func (rv *Receiver) authenticate(r *http.Request) *database.IngestKey {
	plain := presentedKey(r)
	if !wellFormed(plain) {
		return nil
	}
	hash := HashKey(plain)
	k, err := rv.DB.GetIngestKeyByHash(hash)
	if err != nil {
		slog.Error("Ingest key lookup failed", "error", err)
		return nil
	}
	// The lookup matched on the hash already; confirm in constant time.
	if k == nil || subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(hash)) != 1 {
		return nil
	}
	return k
}

// touch records the key's use at most once per touchEvery.
func (rv *Receiver) touch(k *database.IngestKey, now time.Time) {
	rv.touchMu.Lock()
	last, ok := rv.touched[k.ID]
	if ok && now.Sub(last) < touchEvery {
		rv.touchMu.Unlock()
		return
	}
	rv.touched[k.ID] = now
	if len(rv.touched) > 10000 {
		for id, t := range rv.touched {
			if now.Sub(t) >= touchEvery {
				delete(rv.touched, id)
			}
		}
	}
	rv.touchMu.Unlock()
	if err := rv.DB.TouchIngestKey(k.ID, now); err != nil {
		slog.Warn("Failed to record ingest key use", "key", k.ID, "error", err)
	}
}

func mediaType(ct string) string {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(ct))
	}
	return mt
}

// acceptedContentType checks the Content-Type before the body is read.
func acceptedContentType(signal, ct string) bool {
	if signal != pipelines.SignalEvents {
		_, err := otlp.ParseContentType(ct)
		return err == nil
	}
	switch mediaType(ct) {
	case "application/json", "application/x-ndjson", "text/plain":
		return true
	}
	return false
}

// readBody reads the body, decompressing gzip, with both the compressed and
// the decompressed size capped. A non-zero status means it failed.
func (rv *Receiver) readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	limit := rv.maxBody()
	tooLarge := "request body too large (max " + strconv.FormatInt(limit>>20, 10) + " MB decompressed)"
	src := io.Reader(http.MaxBytesReader(w, r.Body, limit))
	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(src)
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, http.StatusRequestEntityTooLarge, tooLarge
			}
			return nil, http.StatusBadRequest, "invalid gzip body"
		}
		defer zr.Close()
		src = zr
	default:
		return nil, http.StatusUnsupportedMediaType, "unsupported Content-Encoding " + strconv.Quote(enc)
	}
	// One byte over the limit tells a full body from a cut one.
	body, err := io.ReadAll(io.LimitReader(src, limit+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, http.StatusRequestEntityTooLarge, tooLarge
		}
		return nil, http.StatusBadRequest, "failed to read the request body"
	}
	if int64(len(body)) > limit {
		return nil, http.StatusRequestEntityTooLarge, tooLarge
	}
	return body, 0, ""
}

// encode decodes a body into the JSONEachRow block the drainer inserts as
// is, and returns the number of rows.
func (rv *Receiver) encode(signal string, body []byte, ct string, now time.Time) ([]byte, int, error) {
	switch signal {
	case pipelines.SignalLogs:
		rows, err := otlp.DecodeLogs(body, ct, rv.Limits)
		if err != nil {
			return nil, 0, err
		}
		out := make([]byte, 0, len(body)*2)
		for i := range rows {
			// Neither time_unix_nano nor observed_time_unix_nano: use the
			// receive time rather than 1970.
			if rows[i].Timestamp.UnixNano() == 0 {
				rows[i].Timestamp = now.UTC()
			}
			out = otlp.AppendLogJSON(out, &rows[i])
		}
		return out, len(rows), nil
	case pipelines.SignalTraces:
		rows, err := otlp.DecodeTraces(body, ct, rv.Limits)
		if err != nil {
			return nil, 0, err
		}
		out := make([]byte, 0, len(body)*2)
		for i := range rows {
			out = otlp.AppendSpanJSON(out, &rows[i])
		}
		return out, len(rows), nil
	default:
		return pipelines.EventsBlock(body)
	}
}
