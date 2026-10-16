package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
)

// The Splunk HTTP Event Collector API at /ingest/services/collector. The
// event endpoint takes concatenated JSON event objects, the raw endpoint
// lines of text. A request is stored whole or not at all: a bad event fails
// the request and nothing of it is spooled.

// HEC status codes, as Splunk documents them.
const (
	hecSuccess          = 0
	hecTokenRequired    = 2
	hecInvalidAuth      = 3
	hecInvalidToken     = 4
	hecNoData           = 5
	hecInvalidFormat    = 6
	hecServerBusy       = 9
	hecEventRequired    = 12
	hecEventBlank       = 13
	hecBadIndexedFields = 15
	hecHealthy          = 17
	hecUnhealthy        = 18
)

// hecRoutes registers the collector endpoints on r.
func (rv *Receiver) hecRoutes(r chi.Router) {
	for _, p := range []string{"/services/collector", "/services/collector/event", "/services/collector/event/1.0"} {
		r.Post(p, rv.hecEvent)
	}
	for _, p := range []string{"/services/collector/raw", "/services/collector/raw/1.0"} {
		r.Post(p, rv.hecRaw)
	}
	for _, p := range []string{"/services/collector/health", "/services/collector/health/1.0"} {
		r.Get(p, rv.hecHealth)
	}
}

// hecError is a request the collector refuses, with its HEC code. event is
// the index of the offending event, or -1.
type hecError struct {
	code  int
	text  string
	event int
}

func (e *hecError) Error() string { return e.text }

func hecReply(w http.ResponseWriter, status, code int, text string, event int) {
	body := map[string]any{"text": text, "code": code}
	if event >= 0 {
		body["invalid-event-number"] = event
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// hecReject writes a rejection in HEC format. Auth failures tell a missing
// token from a bad one, as Splunk does; busy and unavailable are 503 "server
// is busy", which HEC clients retry.
func hecReject(w http.ResponseWriter, r *http.Request, rj *rejection) {
	if rj.retry > 0 {
		setRetryAfter(w, rj.retry)
	}
	switch rj.kind {
	case rejectAuth:
		switch {
		case presentedKey(r) != "":
			hecReply(w, http.StatusForbidden, hecInvalidToken, "Invalid token", -1)
		case r.Header.Get("Authorization") == "" && r.Header.Get("X-CHUI-Ingest-Key") == "":
			hecReply(w, http.StatusUnauthorized, hecTokenRequired, "Token is required", -1)
		default:
			hecReply(w, http.StatusUnauthorized, hecInvalidAuth, "Invalid authorization", -1)
		}
	case rejectUnavailable, rejectBusy:
		status := rj.status
		if status == http.StatusTooManyRequests {
			status = http.StatusServiceUnavailable
		}
		hecReply(w, status, hecServerBusy, "Server is busy: "+rj.msg, -1)
	default:
		hecReply(w, rj.status, hecInvalidFormat, "Invalid data format: "+rj.msg, -1)
	}
}

// hecDecodeFailed counts a decode error and writes it.
func hecDecodeFailed(w http.ResponseWriter, r *http.Request, in *admitted, err error) {
	rj := decodeFailed(in.stats, err)
	var he *hecError
	if errors.As(err, &he) {
		hecReply(w, http.StatusBadRequest, he.code, he.text, he.event)
		return
	}
	hecReject(w, r, rj)
}

func (rv *Receiver) hecEvent(w http.ResponseWriter, r *http.Request) {
	rv.hecServe(w, r, func(in *admitted) ([]byte, int, error) {
		return rv.hecEncodeEvents(in)
	})
}

func (rv *Receiver) hecRaw(w http.ResponseWriter, r *http.Request) {
	rv.hecServe(w, r, func(in *admitted) ([]byte, int, error) {
		return rv.hecEncodeRaw(in, r.URL.Query())
	})
}

func (rv *Receiver) hecServe(w http.ResponseWriter, r *http.Request, encode func(*admitted) ([]byte, int, error)) {
	in, rj := rv.admit(w, r, logsOrEvents("Splunk HEC API"), true)
	if rj != nil {
		hecReject(w, r, rj)
		return
	}
	block, n, err := encode(in)
	if err != nil {
		hecDecodeFailed(w, r, in, err)
		return
	}
	if rj := rv.store(in, block, n); rj != nil {
		hecReject(w, r, rj)
		return
	}
	hecReply(w, http.StatusOK, hecSuccess, "Success", -1)
}

// hecHealth answers healthy without a token, as Splunk does. With a token
// it also checks the token and that its pipeline runs, so a shipper's
// health check catches both.
func (rv *Receiver) hecHealth(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" && r.Header.Get("X-CHUI-Ingest-Key") == "" {
		hecReply(w, http.StatusOK, hecHealthy, "HEC is healthy", -1)
		return
	}
	key := rv.authenticate(r)
	if key == nil || key.RevokedAt != nil {
		hecReject(w, r, &rejection{status: http.StatusUnauthorized, kind: rejectAuth, msg: msgBadKey})
		return
	}
	if rv.Hub.Endpoint(key.PipelineID) == nil {
		hecReply(w, http.StatusServiceUnavailable, hecUnhealthy, "HEC is unhealthy: the pipeline for this token is not running", -1)
		return
	}
	hecReply(w, http.StatusOK, hecHealthy, "HEC is healthy", -1)
}

// hecMeta is the metadata of one event: from the event object's envelope,
// or the raw endpoint's query string.
type hecMeta struct {
	time   time.Time
	hasTS  bool
	host   string
	source string
	stype  string
	index  string
	fields map[string]any
	// raw keeps the envelope values as sent, for the events signal.
	raw map[string]any
}

var hecMetaKeys = []string{"time", "host", "source", "sourcetype", "index"}

func (rv *Receiver) hecEncodeEvents(in *admitted) ([]byte, int, error) {
	m := newDocMapper(rv.Limits, in.now)
	events := in.ep.Signal == pipelines.SignalEvents
	out := make([]byte, 0, len(in.body)*2)
	dec := json.NewDecoder(bytes.NewReader(in.body))
	dec.UseNumber()
	n := 0
	for ; ; n++ {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, &hecError{code: hecInvalidFormat, text: "Invalid data format", event: n}
		}
		if n >= m.lim.MaxRecords {
			return nil, 0, fmt.Errorf("%w: limit is %d", otlp.ErrTooManyRecords, m.lim.MaxRecords)
		}
		env, ok := v.(map[string]any)
		if !ok {
			return nil, 0, &hecError{code: hecInvalidFormat, text: "Invalid data format", event: n}
		}
		ev, ok := env["event"]
		if !ok || ev == nil {
			return nil, 0, &hecError{code: hecEventRequired, text: "Event field is required", event: n}
		}
		if s, isStr := ev.(string); isStr && strings.TrimSpace(s) == "" {
			return nil, 0, &hecError{code: hecEventBlank, text: "Event field cannot be blank", event: n}
		}
		meta := hecMeta{raw: map[string]any{}}
		for _, k := range hecMetaKeys {
			if mv, ok := env[k]; ok && mv != nil {
				meta.raw[k] = mv
			}
		}
		if tv, ok := env["time"]; ok && tv != nil {
			t, ok := parseTime(tv)
			if !ok {
				return nil, 0, &hecError{code: hecInvalidFormat, text: "Invalid data format: time must be epoch seconds", event: n}
			}
			meta.time, meta.hasTS = t, true
		}
		meta.host, _ = scalarString(env["host"])
		meta.source, _ = scalarString(env["source"])
		meta.stype, _ = scalarString(env["sourcetype"])
		meta.index, _ = scalarString(env["index"])
		if fv, ok := env["fields"]; ok && fv != nil {
			f, isMap := fv.(map[string]any)
			if !isMap {
				return nil, 0, &hecError{code: hecBadIndexedFields, text: "Error in handling indexed fields", event: n}
			}
			meta.fields = f
		}
		if out, err = hecAppend(out, m, events, ev, &meta); err != nil {
			return nil, 0, &hecError{code: hecInvalidFormat, text: "Invalid data format", event: n}
		}
	}
	if n == 0 {
		return nil, 0, &hecError{code: hecNoData, text: "No data", event: -1}
	}
	return out, n, nil
}

// hecEncodeRaw stores each non-empty line as one event, with the metadata
// from the query string (host, source, sourcetype, index, time). The
// token never comes from there.
func (rv *Receiver) hecEncodeRaw(in *admitted, q url.Values) ([]byte, int, error) {
	m := newDocMapper(rv.Limits, in.now)
	events := in.ep.Signal == pipelines.SignalEvents
	meta := hecMeta{raw: map[string]any{}}
	for _, k := range hecMetaKeys {
		if v := q.Get(k); v != "" {
			meta.raw[k] = v
		}
	}
	if tv := q.Get("time"); tv != "" {
		t, ok := parseEpoch(tv)
		if !ok {
			return nil, 0, &hecError{code: hecInvalidFormat, text: "Invalid data format: time must be epoch seconds", event: -1}
		}
		meta.time, meta.hasTS = t, true
		meta.raw["time"] = json.Number(tv)
	}
	meta.host, meta.source, meta.stype, meta.index = q.Get("host"), q.Get("source"), q.Get("sourcetype"), q.Get("index")
	out := make([]byte, 0, len(in.body)*3)
	n := 0
	for _, line := range bytes.Split(in.body, []byte{'\n'}) {
		line = bytes.TrimRight(line, "\r")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if n >= m.lim.MaxRecords {
			return nil, 0, fmt.Errorf("%w: limit is %d", otlp.ErrTooManyRecords, m.lim.MaxRecords)
		}
		var err error
		if out, err = hecAppend(out, m, events, string(line), &meta); err != nil {
			return nil, 0, &hecError{code: hecInvalidFormat, text: "Invalid data format", event: n}
		}
		n++
	}
	if n == 0 {
		return nil, 0, &hecError{code: hecNoData, text: "No data", event: -1}
	}
	return out, n, nil
}

// hecAppend encodes one event. For logs the event object (or a string as
// the body) is mapped like a log document, the envelope time and host
// override, fields and source, sourcetype and index become attributes. For
// events the event object is stored with the envelope merged in, keys of
// the event winning.
func hecAppend(out []byte, m *docMapper, events bool, ev any, meta *hecMeta) ([]byte, error) {
	doc, isMap := ev.(map[string]any)
	if !isMap {
		doc = map[string]any{"event": ev}
	}
	if events {
		for k, v := range meta.raw {
			if _, ok := doc[k]; !ok {
				doc[k] = v
			}
		}
		for k, v := range meta.fields {
			if _, ok := doc[k]; !ok {
				doc[k] = v
			}
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return out, err
		}
		return append(append(out, b...), '\n'), nil
	}
	extra := map[string]string{}
	for k, v := range meta.fields {
		extra[k] = valueString(v)
	}
	for k, v := range map[string]string{"splunk.source": meta.source, "splunk.sourcetype": meta.stype, "splunk.index": meta.index} {
		if v != "" {
			extra[k] = v
		}
	}
	row := m.logRow(doc, extra)
	if meta.hasTS {
		row.Timestamp = meta.time
	}
	if meta.host != "" {
		row.ResourceAttributes["host.name"] = truncateString(meta.host, m.lim.MaxValueBytes)
		if row.ServiceName == "" {
			row.ServiceName = truncateString(meta.host, m.lim.MaxValueBytes)
		}
	}
	return otlp.AppendLogJSON(out, &row), nil
}
