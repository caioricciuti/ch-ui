package ingest

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
)

// The Elasticsearch bulk API, mounted at /ingest/es, so a shipper's
// Elasticsearch output works with hosts set to https://<ch-ui>/ingest/es.
// POST /_bulk takes index and create actions; the handshake endpoints the
// shippers call at startup answer minimal valid bodies.

// esVersion is the version GET / reports. Filebeat and Logstash 8 need an 8.x
// cluster; Vector and Fluent Bit only read the major.
const esVersion = "8.19.0"

const esName = "ch-ui"

// esRoutes registers the Elasticsearch endpoints on r.
func (rv *Receiver) esRoutes(r chi.Router) {
	r.Get("/", rv.esMeta(esInfo))
	r.Head("/", rv.esMeta(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for _, p := range []string{"/_bulk", "/{index}/_bulk"} {
		r.Post(p, rv.esBulk)
		r.Put(p, rv.esBulk)
	}
	r.Get("/_license", rv.esMeta(esLicense))
	r.Get("/_xpack", rv.esMeta(esXpack))
	r.Get("/_cluster/health", rv.esMeta(esHealth))
	r.Get("/_cluster/health/*", rv.esMeta(esHealth))
	r.Get("/_nodes", rv.esMeta(esNodes))
	r.Get("/_nodes/*", rv.esMeta(esNodes))
	for prefix, get := range map[string]func(name string) any{
		"/_index_template": esIndexTemplate,
		"/_template":       esLegacyTemplate,
		"/_ilm/policy":     esILMPolicy,
		"/_data_stream":    esDataStream,
	} {
		p := prefix + "/{name}"
		r.Get(p, rv.esMeta(func(w http.ResponseWriter, r *http.Request) { esJSON(w, http.StatusOK, get(esParam(r, "name"))) }))
		r.Head(p, rv.esMeta(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		r.Put(p, rv.esMeta(esAcknowledged))
		r.Post(p, rv.esMeta(esAcknowledged))
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		esHeaders(w)
		esError(w, http.StatusNotFound, "resource_not_found_exception", "ch-ui does not serve "+r.Method+" "+r.URL.Path+"; it takes the bulk API and the startup handshake")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		esHeaders(w)
		esError(w, http.StatusMethodNotAllowed, "illegal_argument_exception", "method "+r.Method+" is not allowed on "+r.URL.Path)
	})
}

// esHeaders sets the product header Elasticsearch 7.14+ clients check
// before they talk to a cluster.
func esHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Elastic-Product", "Elasticsearch")
}

func esParam(r *http.Request, name string) string {
	v := chi.URLParam(r, name)
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

func esJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// esError writes an Elasticsearch error body.
func esError(w http.ResponseWriter, status int, typ, reason string) {
	cause := map[string]any{"type": typ, "reason": reason}
	esJSON(w, status, map[string]any{
		"error":  map[string]any{"root_cause": []any{cause}, "type": typ, "reason": reason},
		"status": status,
	})
}

// esReject writes a rejection as an Elasticsearch error.
func esReject(w http.ResponseWriter, rj *rejection) {
	if rj.retry > 0 {
		setRetryAfter(w, rj.retry)
	}
	typ := "illegal_argument_exception"
	switch rj.kind {
	case rejectAuth:
		typ = "security_exception"
		w.Header().Add("WWW-Authenticate", `Basic realm="security", charset="UTF-8"`)
		w.Header().Add("WWW-Authenticate", "ApiKey")
	case rejectUnavailable:
		typ = "unavailable_shards_exception"
	case rejectBusy:
		typ = "es_rejected_execution_exception"
	}
	esError(w, rj.status, typ, rj.msg)
}

// esMeta wraps a handshake endpoint: it needs a valid key, of any signal,
// and the pipeline need not be running.
func (rv *Receiver) esMeta(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		esHeaders(w)
		key := rv.authenticate(r)
		if key == nil || key.RevokedAt != nil {
			if key != nil {
				rv.Hub.Stats(key.PipelineID).RejectedAuth.Add(1)
			}
			esReject(w, &rejection{status: http.StatusUnauthorized, kind: rejectAuth, msg: msgBadKey})
			return
		}
		h(w, r)
	}
}

func esInfo(w http.ResponseWriter, _ *http.Request) {
	esJSON(w, http.StatusOK, map[string]any{
		"name":         esName,
		"cluster_name": esName,
		"cluster_uuid": "ch-ui-ingest",
		"version": map[string]any{
			"number":                              esVersion,
			"build_flavor":                        "default",
			"build_type":                          "docker",
			"build_hash":                          "ch-ui",
			"build_date":                          "2025-07-24T00:00:00.000Z",
			"build_snapshot":                      false,
			"lucene_version":                      "9.12.2",
			"minimum_wire_compatibility_version":  "7.17.0",
			"minimum_index_compatibility_version": "7.0.0",
		},
		"tagline": "You Know, for Search",
	})
}

func esLicenseBody() map[string]any {
	return map[string]any{
		"uid":                  "00000000-0000-0000-0000-000000000000",
		"type":                 "basic",
		"mode":                 "basic",
		"status":               "active",
		"issue_date":           "2025-01-01T00:00:00.000Z",
		"issue_date_in_millis": int64(1735689600000),
		"max_nodes":            1000,
		"max_resource_units":   nil,
		"issued_to":            esName,
		"issuer":               "elasticsearch",
		"start_date_in_millis": -1,
	}
}

func esLicense(w http.ResponseWriter, _ *http.Request) {
	esJSON(w, http.StatusOK, map[string]any{"license": esLicenseBody()})
}

// esXpack reports ILM as unavailable, so Logstash's ilm_enabled => auto
// turns it off.
func esXpack(w http.ResponseWriter, _ *http.Request) {
	lic := esLicenseBody()
	esJSON(w, http.StatusOK, map[string]any{
		"build":   map[string]any{"hash": "ch-ui", "date": "2025-07-24T00:00:00.000Z"},
		"license": map[string]any{"uid": lic["uid"], "type": lic["type"], "mode": lic["mode"], "status": lic["status"]},
		"features": map[string]any{
			"ilm":      map[string]any{"available": false, "enabled": false},
			"security": map[string]any{"available": true, "enabled": true},
		},
		"tagline": "You know, for X",
	})
}

func esHealth(w http.ResponseWriter, _ *http.Request) {
	esJSON(w, http.StatusOK, map[string]any{
		"cluster_name":                     esName,
		"status":                           "green",
		"timed_out":                        false,
		"number_of_nodes":                  1,
		"number_of_data_nodes":             1,
		"active_primary_shards":            0,
		"active_shards":                    0,
		"relocating_shards":                0,
		"initializing_shards":              0,
		"unassigned_shards":                0,
		"delayed_unassigned_shards":        0,
		"number_of_pending_tasks":          0,
		"number_of_in_flight_fetch":        0,
		"task_max_waiting_in_queue_millis": 0,
		"active_shards_percent_as_number":  100.0,
	})
}

// esNodes lists no nodes: there is nothing to sniff. Shippers must keep
// sniffing off, as is their default.
func esNodes(w http.ResponseWriter, _ *http.Request) {
	esJSON(w, http.StatusOK, map[string]any{
		"_nodes":       map[string]any{"total": 0, "successful": 0, "failed": 0},
		"cluster_name": esName,
		"nodes":        map[string]any{},
	})
}

// Templates, ILM policies and data streams are answered as existing, so a
// shipper that checks before it creates skips the creation, and a creation
// is acknowledged. Nothing is stored: CH-UI's table layout comes from the
// pipeline.

func esIndexTemplate(name string) any {
	return map[string]any{"index_templates": []any{map[string]any{
		"name":           name,
		"index_template": map[string]any{"index_patterns": []string{name + "*"}, "composed_of": []string{}, "template": map[string]any{}},
	}}}
}

func esLegacyTemplate(name string) any {
	return map[string]any{name: map[string]any{
		"order": 0, "index_patterns": []string{name + "*"}, "settings": map[string]any{}, "mappings": map[string]any{}, "aliases": map[string]any{},
	}}
}

func esILMPolicy(name string) any {
	return map[string]any{name: map[string]any{
		"version":       1,
		"modified_date": "2025-01-01T00:00:00.000Z",
		"policy":        map[string]any{"phases": map[string]any{"hot": map[string]any{"min_age": "0ms", "actions": map[string]any{}}}},
	}}
}

func esDataStream(name string) any {
	return map[string]any{"data_streams": []any{map[string]any{
		"name":            name,
		"timestamp_field": map[string]any{"name": "@timestamp"},
		"indices":         []any{},
		"generation":      1,
		"status":          "GREEN",
		"template":        name,
	}}}
}

func esAcknowledged(w http.ResponseWriter, _ *http.Request) {
	esJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

// esContentType accepts what the bulk clients send, and no header at all.
func esContentType(ct string) bool {
	switch mt := mediaType(ct); {
	case mt == "", mt == "application/json", mt == "application/x-ndjson", mt == "text/plain":
		return true
	case strings.HasPrefix(mt, "application/vnd.elasticsearch+"):
		return true
	}
	return false
}

// logsOrEvents is the signal check of the Elasticsearch and Splunk APIs:
// they carry logs or events, never traces.
func logsOrEvents(api string) func(string) *rejection {
	return func(signal string) *rejection {
		if signal == pipelines.SignalLogs || signal == pipelines.SignalEvents {
			return nil
		}
		return &rejection{status: http.StatusBadRequest, kind: rejectSignal, msg: "this ingest key belongs to a " + signal + " pipeline; the " + api + " takes logs and events keys, send traces as OTLP to /ingest/v1/traces"}
	}
}

func (rv *Receiver) esBulk(w http.ResponseWriter, r *http.Request) {
	esHeaders(w)
	started := time.Now()
	in, rj := rv.admit(w, r, logsOrEvents("Elasticsearch bulk API"), esContentType(r.Header.Get("Content-Type")))
	if rj != nil {
		esReject(w, rj)
		return
	}
	res, err := rv.esDecode(in, esParam(r, "index"))
	if err != nil {
		esReject(w, decodeFailed(in.stats, err))
		return
	}
	if rj := rv.store(in, res.block, res.n); rj != nil {
		esReject(w, rj)
		return
	}
	if res.errors {
		in.stats.RejectedMalformed.Add(1)
	}
	items := make([]any, len(res.items))
	for i, it := range res.items {
		items[i] = map[string]any{it.action: it.fields}
	}
	resp := map[string]any{"took": time.Since(started).Milliseconds(), "errors": res.errors, "items": items}
	esJSON(w, http.StatusOK, esFilterPath(resp, r.URL.Query().Get("filter_path")))
}

// bulkItem is one entry of a bulk response.
type bulkItem struct {
	action string
	fields map[string]any
}

type bulkResult struct {
	block  []byte
	n      int
	items  []bulkItem
	errors bool
}

// esDecode reads action/metadata and document line pairs. A broken action
// line fails the request, as in Elasticsearch; a broken or refused document
// fails only its item. delete has no document line.
func (rv *Receiver) esDecode(in *admitted, pathIndex string) (*bulkResult, error) {
	m := newDocMapper(rv.Limits, in.now)
	events := in.ep.Signal == pipelines.SignalEvents
	res := &bulkResult{block: make([]byte, 0, len(in.body)*2)}
	lines := bytes.Split(in.body, []byte{'\n'})
	seq := 0
	for i := 0; i < len(lines); i++ {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		lineNo := i + 1
		if len(res.items) >= m.lim.MaxRecords {
			return nil, fmt.Errorf("%w: limit is %d", otlp.ErrTooManyRecords, m.lim.MaxRecords)
		}
		action, meta, err := parseBulkAction(line, lineNo)
		if err != nil {
			return nil, err
		}
		index := pathIndex
		if s, ok := meta["_index"].(string); ok && s != "" {
			index = s
		}
		id, _ := scalarString(meta["_id"])
		item := bulkItem{action: action, fields: map[string]any{"_index": index}}
		if id != "" {
			item.fields["_id"] = id
		}
		fail := func(status int, typ, reason string) {
			item.fields["status"] = status
			item.fields["error"] = map[string]any{"type": typ, "reason": reason}
			res.items = append(res.items, item)
			res.errors = true
		}
		if action == "delete" {
			fail(http.StatusBadRequest, "action_request_validation_exception", "ch-ui ingest takes only index and create actions, not delete")
			continue
		}
		var doc []byte
		hasDoc := i+1 < len(lines)
		if hasDoc {
			i++
			doc = bytes.TrimSpace(lines[i])
		}
		switch {
		case action == "update":
			fail(http.StatusBadRequest, "action_request_validation_exception", "ch-ui ingest takes only index and create actions, not update")
			continue
		case !hasDoc:
			fail(http.StatusBadRequest, "action_request_validation_exception", fmt.Sprintf("the %s action on line [%d] has no document line", action, lineNo))
			continue
		case index == "":
			fail(http.StatusBadRequest, "action_request_validation_exception", "index is missing")
			continue
		}
		obj, err := decodeObject(doc)
		if err != nil {
			fail(http.StatusBadRequest, "document_parsing_exception", fmt.Sprintf("[%d:1] failed to parse: %v", i+1, err))
			continue
		}
		if id == "" {
			id = esNewID()
			item.fields["_id"] = id
		}
		if events {
			var compact bytes.Buffer
			if err := json.Compact(&compact, doc); err != nil {
				fail(http.StatusBadRequest, "document_parsing_exception", fmt.Sprintf("[%d:1] failed to parse: %v", i+1, err))
				continue
			}
			res.block = append(append(res.block, compact.Bytes()...), '\n')
		} else {
			row := m.logRow(obj, map[string]string{"es.index": index})
			res.block = otlp.AppendLogJSON(res.block, &row)
		}
		res.n++
		item.fields["_version"] = 1
		item.fields["result"] = "created"
		item.fields["_shards"] = map[string]any{"total": 1, "successful": 1, "failed": 0}
		item.fields["_seq_no"] = seq
		item.fields["_primary_term"] = 1
		item.fields["status"] = http.StatusCreated
		seq++
		res.items = append(res.items, item)
	}
	if len(res.items) == 0 {
		return nil, fmt.Errorf("Validation Failed: 1: no requests added;")
	}
	return res, nil
}

// parseBulkAction reads {"<action>": {metadata}}.
func parseBulkAction(line []byte, lineNo int) (string, map[string]any, error) {
	obj, err := decodeObject(line)
	if err != nil {
		return "", nil, fmt.Errorf("Malformed action/metadata line [%d]: %v", lineNo, err)
	}
	if len(obj) != 1 {
		return "", nil, fmt.Errorf("Malformed action/metadata line [%d], expected a single action, found %d fields", lineNo, len(obj))
	}
	for action, v := range obj {
		switch action {
		case "index", "create", "update", "delete":
		default:
			return "", nil, fmt.Errorf("Malformed action/metadata line [%d], expected one of [create, delete, index, update] but found [%s]", lineNo, truncateString(action, 64))
		}
		meta, ok := v.(map[string]any)
		if !ok {
			return "", nil, fmt.Errorf("Malformed action/metadata line [%d], expected START_OBJECT", lineNo)
		}
		return action, meta, nil
	}
	return "", nil, nil
}

// esNewID returns a 20 character id like Elasticsearch's auto ids.
func esNewID() string {
	var b [15]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// esFilterPath applies the filter_path forms bulk clients send: top-level
// names (took, errors, items) and items.<action|*>.<field|*>. Exclusions
// and other forms leave the response whole. Items left empty are dropped,
// as Elasticsearch does.
func esFilterPath(resp map[string]any, filter string) map[string]any {
	if strings.TrimSpace(filter) == "" {
		return resp
	}
	top := map[string]bool{}
	type itemRule struct{ action, field string }
	var rules []itemRule
	for _, f := range strings.Split(filter, ",") {
		f = strings.TrimSpace(f)
		switch parts := strings.Split(f, "."); {
		case f == "*":
			return resp
		case strings.HasPrefix(f, "-"):
			return resp
		case len(parts) == 1:
			top[f] = true
		case parts[0] == "items" && len(parts) == 3:
			rules = append(rules, itemRule{parts[1], parts[2]})
		default:
			return resp
		}
	}
	out := map[string]any{}
	for k := range top {
		if v, ok := resp[k]; ok {
			out[k] = v
		}
	}
	if top["items"] || len(rules) == 0 {
		return out
	}
	var items []any
	for _, it := range resp["items"].([]any) {
		for action, v := range it.(map[string]any) {
			fields := v.(map[string]any)
			kept := map[string]any{}
			for _, rl := range rules {
				if rl.action != "*" && rl.action != action {
					continue
				}
				for k, fv := range fields {
					if rl.field == "*" || rl.field == k {
						kept[k] = fv
					}
				}
			}
			if len(kept) > 0 {
				items = append(items, map[string]any{action: kept})
			}
		}
	}
	if len(items) > 0 {
		out["items"] = items
	}
	return out
}
