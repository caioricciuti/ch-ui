package ingest

import "encoding/binary"

// pb is a test-only protobuf encoder for building OTLP export requests.
type pb []byte

func (p pb) tag(num, wire int) pb { return pb(binary.AppendUvarint(p, uint64(num<<3|wire))) }

func (p pb) varint(num int, v uint64) pb { return pb(binary.AppendUvarint(p.tag(num, 0), v)) }

func (p pb) fixed64(num int, v uint64) pb {
	return pb(binary.LittleEndian.AppendUint64(p.tag(num, 1), v))
}

func (p pb) bytes(num int, v []byte) pb {
	b := binary.AppendUvarint(p.tag(num, 2), uint64(len(v)))
	return pb(append(b, v...))
}

func (p pb) str(num int, s string) pb { return p.bytes(num, []byte(s)) }

func (p pb) msg(num int, m pb) pb { return p.bytes(num, m) }

// kv is a KeyValue with a string AnyValue.
func kv(key, value string) pb {
	return pb(nil).str(1, key).msg(2, pb(nil).str(1, value))
}

func serviceResource(service string) pb { return pb(nil).msg(1, kv("service.name", service)) }

// otlpLogsProto is an ExportLogsServiceRequest with one record per body.
// tsNano 0 leaves both timestamps unset.
func otlpLogsProto(service string, tsNano uint64, traceID []byte, bodies ...string) []byte {
	var scope pb
	scope = scope.msg(1, pb(nil).str(1, "test-scope"))
	for _, b := range bodies {
		var rec pb
		if tsNano != 0 {
			rec = rec.fixed64(1, tsNano)
		}
		rec = rec.varint(2, 9).str(3, "INFO").msg(5, pb(nil).str(1, b)).msg(6, kv("k", "v"))
		if traceID != nil {
			rec = rec.bytes(9, traceID)
		}
		scope = scope.msg(2, rec)
	}
	rl := pb(nil).msg(1, serviceResource(service)).msg(2, scope)
	return pb(nil).msg(1, rl)
}

// otlpTraceProto is an ExportTraceServiceRequest with a root span and one
// child in the same trace.
func otlpTraceProto(service string, traceID []byte, startNano uint64) []byte {
	root := pb(nil).bytes(1, traceID).bytes(2, []byte{1, 1, 1, 1, 1, 1, 1, 1}).str(5, "GET /").
		varint(6, 2).fixed64(7, startNano).fixed64(8, startNano+5_000_000).msg(9, kv("http.method", "GET"))
	child := pb(nil).bytes(1, traceID).bytes(2, []byte{2, 2, 2, 2, 2, 2, 2, 2}).bytes(4, []byte{1, 1, 1, 1, 1, 1, 1, 1}).
		str(5, "db query").varint(6, 3).fixed64(7, startNano+1_000_000).fixed64(8, startNano+3_000_000).
		msg(15, pb(nil).str(2, "boom").varint(3, 2))
	scope := pb(nil).msg(1, pb(nil).str(1, "test-scope")).msg(2, root).msg(2, child)
	rs := pb(nil).msg(1, serviceResource(service)).msg(2, scope)
	return pb(nil).msg(1, rs)
}
