package otlp

import "strconv"

// Field numbers from the collector service protos and
// google/rpc/status.proto.
const (
	fExportResponsePartialSuccess = 1 // Export{Logs,Trace}ServiceResponse.partial_success
	fPartialSuccessRejected       = 1 // rejected_log_records / rejected_spans (int64)
	fPartialSuccessErrorMessage   = 2 // Export{Logs,Trace}PartialSuccess.error_message

	fRPCStatusCode    = 1 // google.rpc.Status.code (int32)
	fRPCStatusMessage = 2 // google.rpc.Status.message
)

const (
	contentTypeProtobuf = "application/x-protobuf"
	contentTypeJSON     = "application/json"
)

// responseEncoding picks the response encoding from the request's
// Content-Type, defaulting to protobuf as OTLP/HTTP does.
func responseEncoding(contentType string) (Encoding, string) {
	if enc, err := ParseContentType(contentType); err == nil && enc == EncodingJSON {
		return EncodingJSON, contentTypeJSON
	}
	return EncodingProtobuf, contentTypeProtobuf
}

// SuccessResponse is a full-success Export{Logs,Trace}ServiceResponse in the
// request's encoding: an empty message, so no bytes for protobuf and {} for
// JSON. It returns the body and its Content-Type.
func SuccessResponse(contentType string) ([]byte, string) {
	enc, ct := responseEncoding(contentType)
	if enc == EncodingJSON {
		return []byte("{}"), ct
	}
	return []byte{}, ct
}

// PartialSuccessLogsResponse reports rejected log records with a message.
func PartialSuccessLogsResponse(contentType string, rejected int64, message string) ([]byte, string) {
	return partialSuccess(contentType, "rejectedLogRecords", rejected, message)
}

// PartialSuccessTracesResponse reports rejected spans with a message.
func PartialSuccessTracesResponse(contentType string, rejected int64, message string) ([]byte, string) {
	return partialSuccess(contentType, "rejectedSpans", rejected, message)
}

func partialSuccess(contentType, rejectedName string, rejected int64, message string) ([]byte, string) {
	enc, ct := responseEncoding(contentType)
	if enc == EncodingJSON {
		// int64 is a string in OTLP JSON.
		b := []byte(`{"partialSuccess":{"` + rejectedName + `":"`)
		b = strconv.AppendInt(b, rejected, 10)
		b = append(b, `","errorMessage":`...)
		b = appendJSONString(b, message)
		return append(b, "}}"...), ct
	}
	var inner []byte
	if rejected != 0 {
		inner = appendTag(inner, fPartialSuccessRejected, wireVarint)
		inner = appendVarint(inner, uint64(rejected))
	}
	if message != "" {
		inner = appendBytesField(inner, fPartialSuccessErrorMessage, []byte(message))
	}
	return appendBytesField(nil, fExportResponsePartialSuccess, inner), ct
}

// StatusResponse is the google.rpc.Status body OTLP/HTTP sends with a 4xx
// or 5xx. code is a gRPC status code (3 InvalidArgument, 8
// ResourceExhausted, 14 Unavailable and so on).
func StatusResponse(contentType string, code int32, message string) ([]byte, string) {
	enc, ct := responseEncoding(contentType)
	if enc == EncodingJSON {
		b := []byte(`{"code":`)
		b = strconv.AppendInt(b, int64(code), 10)
		b = append(b, `,"message":`...)
		b = appendJSONString(b, message)
		return append(b, '}'), ct
	}
	var b []byte
	if code != 0 {
		b = appendTag(b, fRPCStatusCode, wireVarint)
		// int32 fields encode negatives sign-extended to 64 bits.
		b = appendVarint(b, uint64(int64(code)))
	}
	if message != "" {
		b = appendBytesField(b, fRPCStatusMessage, []byte(message))
	}
	if b == nil {
		b = []byte{}
	}
	return b, ct
}
