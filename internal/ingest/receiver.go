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

// Receiver serves the /ingest endpoints: OTLP and events under /v1, the
// Elasticsearch bulk API and the Splunk HTTP Event Collector.
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

// Routes registers POST /v1/logs, /v1/traces and /v1/events, the
// Elasticsearch bulk API under /es and the Splunk HEC endpoints under
// /services/collector on r. Mount it at /ingest, and on the separate
// listener also at the root so a bare host works as
// OTEL_EXPORTER_OTLP_ENDPOINT and as a HEC URL.
func (rv *Receiver) Routes(r chi.Router) {
	r.Post("/v1/logs", rv.handler(pipelines.SignalLogs))
	r.Post("/v1/traces", rv.handler(pipelines.SignalTraces))
	r.Post("/v1/events", rv.handler(pipelines.SignalEvents))
	r.Route("/es", rv.esRoutes)
	rv.hecRoutes(r)
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

// reject writes a rejection with the gRPC code that matches its kind.
func (p reply) reject(rj *rejection) {
	if rj.retry > 0 {
		setRetryAfter(p.w, rj.retry)
	}
	code := codeInvalidArgument
	switch rj.kind {
	case rejectAuth:
		code = codeUnauthenticated
	case rejectUnavailable:
		code = codeUnavailable
	case rejectSignal:
		code = codeNotFound
	case rejectBusy:
		code = codeResourceExhausted
	}
	p.fail(rj.status, code, rj.msg)
}

func setRetryAfter(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(max(int(d.Seconds()+0.5), 1)))
}

// rejectKind classifies a refused request. Every receiver API writes it in
// its own error format.
type rejectKind int

const (
	rejectAuth        rejectKind = iota // no key, unknown or revoked
	rejectUnavailable                   // pipeline stopped, spool full or failing
	rejectSignal                        // the key's pipeline takes another signal
	rejectBusy                          // rate limit or daily quota
	rejectInvalid                       // malformed, unsupported or too large
)

// rejection is a refused request: the HTTP status, its kind, a message safe
// to show the client, and a Retry-After when retry > 0.
type rejection struct {
	status int
	kind   rejectKind
	msg    string
	retry  time.Duration
}

const msgBadKey = "missing or invalid ingest key"

// admitted is a request that passed every check before decoding.
type admitted struct {
	key   *database.IngestKey
	ep    *pipelines.IngestEndpoint
	stats *pipelines.IngestStats
	body  []byte
	now   time.Time
}

// admit runs the checks every receiver shares, in this order: the key, the
// pipeline running, accepts (the signal), the rate limit, the quota, ctOK
// (the Content-Type), the body read and the quota charge. Rejections are
// counted in the pipeline's stats where they can be attributed.
func (rv *Receiver) admit(w http.ResponseWriter, r *http.Request, accepts func(signal string) *rejection, ctOK bool) (*admitted, *rejection) {
	now := rv.now()
	key := rv.authenticate(r)
	if key == nil {
		return nil, &rejection{status: http.StatusUnauthorized, kind: rejectAuth, msg: msgBadKey}
	}
	stats := rv.Hub.Stats(key.PipelineID)
	if key.RevokedAt != nil {
		stats.RejectedAuth.Add(1)
		return nil, &rejection{status: http.StatusUnauthorized, kind: rejectAuth, msg: msgBadKey}
	}
	ep := rv.Hub.Endpoint(key.PipelineID)
	if ep == nil {
		// Stopped (or not started yet): ask the shipper to retry, it keeps
		// its data. Spooled data waits for the next start.
		return nil, &rejection{status: http.StatusServiceUnavailable, kind: rejectUnavailable, msg: "the pipeline for this key is not running", retry: 30 * time.Second}
	}
	if rj := accepts(ep.Signal); rj != nil {
		return nil, rj
	}
	rv.touch(key, now)

	if !rv.lim.allow(key.ID, ep.RateLimitRPS, now) {
		stats.RejectedRateLimit.Add(1)
		return nil, &rejection{status: http.StatusTooManyRequests, kind: rejectBusy, msg: "rate limit exceeded", retry: time.Second}
	}
	quotaGone := &rejection{status: http.StatusTooManyRequests, kind: rejectBusy, msg: "daily quota exhausted, resets at 00:00 UTC", retry: untilMidnightUTC(now)}
	if !rv.lim.quotaLeft(ep.PipelineID, ep.DailyQuotaBytes, now) {
		stats.RejectedQuota.Add(1)
		return nil, quotaGone
	}
	if !ctOK {
		stats.RejectedMalformed.Add(1)
		return nil, &rejection{status: http.StatusUnsupportedMediaType, kind: rejectInvalid, msg: "unsupported content type"}
	}

	body, status, msg := rv.readBody(w, r)
	if status != 0 {
		switch status {
		case http.StatusRequestEntityTooLarge:
			stats.RejectedTooLarge.Add(1)
		default:
			stats.RejectedMalformed.Add(1)
		}
		return nil, &rejection{status: status, kind: rejectInvalid, msg: msg}
	}
	if !rv.lim.charge(ep.PipelineID, ep.DailyQuotaBytes, int64(len(body)), now) {
		stats.RejectedQuota.Add(1)
		return nil, quotaGone
	}
	return &admitted{key: key, ep: ep, stats: stats, body: body, now: now}, nil
}

// decodeFailed counts and classifies an error from decoding a body.
func decodeFailed(stats *pipelines.IngestStats, err error) *rejection {
	if errors.Is(err, otlp.ErrTooManyRecords) {
		stats.RejectedTooLarge.Add(1)
		return &rejection{status: http.StatusRequestEntityTooLarge, kind: rejectInvalid, msg: err.Error()}
	}
	stats.RejectedMalformed.Add(1)
	return &rejection{status: http.StatusBadRequest, kind: rejectInvalid, msg: err.Error()}
}

// store appends the block of n rows to the pipeline's spool and counts the
// request as accepted. A request with no rows is counted and not spooled.
func (rv *Receiver) store(in *admitted, block []byte, n int) *rejection {
	if n > 0 {
		if status, msg := pipelines.SpoolAppendStatus(rv.Hub.Append(in.ep.PipelineID, block), in.stats); status != http.StatusOK {
			if status == http.StatusServiceUnavailable {
				return &rejection{status: status, kind: rejectUnavailable, msg: msg, retry: 5 * time.Second}
			}
			return &rejection{status: status, kind: rejectInvalid, msg: msg}
		}
	}
	in.stats.Accepted(int64(n), int64(len(in.body)), in.now)
	return nil
}

func (rv *Receiver) handler(signal string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rv.serve(w, r, signal)
	}
}

func (rv *Receiver) serve(w http.ResponseWriter, r *http.Request, signal string) {
	ct := r.Header.Get("Content-Type")
	rep := reply{w: w, otlp: signal != pipelines.SignalEvents, contentType: ct}
	accepts := func(got string) *rejection {
		if got != signal {
			return &rejection{status: http.StatusNotFound, kind: rejectSignal, msg: "this key does not accept " + signal}
		}
		return nil
	}
	in, rj := rv.admit(w, r, accepts, acceptedContentType(signal, ct))
	if rj != nil {
		rep.reject(rj)
		return
	}
	block, n, err := rv.encode(signal, in.body, ct, in.now)
	if err != nil {
		rep.reject(decodeFailed(in.stats, err))
		return
	}
	if rj := rv.store(in, block, n); rj != nil {
		rep.reject(rj)
		return
	}

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
