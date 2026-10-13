package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DeadLetter is one record ClickHouse refused, with why and where it came from.
type DeadLetter struct {
	Raw      string `json:"raw"`
	Error    string `json:"error"`
	Position string `json:"source_position"`
}

// DeadLetterSink is a sink that can store refused records in a dead-letter
// table next to its target.
type DeadLetterSink interface {
	WriteDeadLetters(ctx context.Context, cfg ConnectorConfig, pipelineID string, letters []DeadLetter, dedupToken string) error
	// DeadLetterTable returns the database and table rejected rows go to.
	DeadLetterTable(cfg ConnectorConfig) (string, string)
}

// dataErrorCodes are ClickHouse error codes caused by the inserted data
// itself: retrying the same rows fails the same way. Everything else
// (network, timeouts, memory, permissions) is retried as before.
var dataErrorCodes = map[int]bool{
	6:   true, // CANNOT_PARSE_TEXT
	25:  true, // CANNOT_PARSE_ESCAPE_SEQUENCE
	26:  true, // CANNOT_PARSE_QUOTED_STRING
	27:  true, // CANNOT_PARSE_INPUT_ASSERTION_FAILED
	32:  true, // ATTEMPT_TO_READ_AFTER_EOF
	33:  true, // CANNOT_READ_ALL_DATA
	38:  true, // CANNOT_PARSE_DATE
	41:  true, // CANNOT_PARSE_DATETIME
	53:  true, // TYPE_MISMATCH
	69:  true, // ARGUMENT_OUT_OF_BOUND
	70:  true, // CANNOT_CONVERT_TYPE
	72:  true, // CANNOT_PARSE_NUMBER
	117: true, // INCORRECT_DATA
	131: true, // TOO_LARGE_STRING_SIZE
	190: true, // SIZES_OF_ARRAYS_DONT_MATCH
	349: true, // CANNOT_INSERT_NULL_IN_ORDINARY_COLUMN
	457: true, // VALUE_IS_OUT_OF_RANGE_OF_DATA_TYPE
	467: true, // CANNOT_PARSE_BOOL
	469: true, // VIOLATED_CONSTRAINT
	691: true, // UNKNOWN_ELEMENT_OF_ENUM
}

var chErrorCode = regexp.MustCompile(`Code: (\d+)\.`)

// IsDataError reports whether err is ClickHouse refusing the inserted rows
// (a parse or type error), as opposed to a connection, timeout or server
// error that a retry can fix.
func IsDataError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	m := chErrorCode.FindStringSubmatch(err.Error())
	if m == nil {
		return false
	}
	code, convErr := strconv.Atoi(m[1])
	return convErr == nil && dataErrorCodes[code]
}

// Bounds for isolating bad rows in a refused batch: once maxBisectWrites
// sub-inserts were made, or a range was split maxBisectDepth times, a range
// that fails goes to the dead-letter table whole instead of being split.
// Ranges already queued are still tried once, so a batch costs at most
// about maxBisectWrites+maxBisectDepth inserts.
const (
	maxBisectDepth  = 12
	maxBisectWrites = 256
)

// isoRange is a slice [lo, hi) of a batch still to be written. dead ranges
// go to the dead-letter table with reason.
type isoRange struct {
	lo, hi int
	depth  int
	dead   bool
	reason string
}

// isolation is the progress of splitting one refused batch. It survives
// retries of a transient error so rows already written are not written again.
type isolation struct {
	ranges []isoRange
	writes int
	rows   int
	dlq    int
}

func newIsolation(n int, err error) *isolation {
	iso := &isolation{}
	iso.ranges = iso.split(isoRange{lo: 0, hi: n}, err)
	return iso
}

// split replaces a refused range by its halves, or marks it dead when it is
// one row or a bound is reached.
func (iso *isolation) split(rg isoRange, err error) []isoRange {
	if rg.hi-rg.lo <= 1 || rg.depth >= maxBisectDepth || iso.writes >= maxBisectWrites {
		reason := shortError(err)
		if rg.hi-rg.lo > 1 {
			reason = "isolation limit reached; this range was refused as a whole: " + reason
		}
		return []isoRange{{lo: rg.lo, hi: rg.hi, depth: rg.depth, dead: true, reason: reason}}
	}
	mid := rg.lo + (rg.hi-rg.lo)/2
	return []isoRange{{lo: rg.lo, hi: mid, depth: rg.depth + 1}, {lo: mid, hi: rg.hi, depth: rg.depth + 1}}
}

// run writes the remaining ranges in order. It returns a transient error
// (and keeps its progress) or nil when every row is written or dead-lettered.
func (iso *isolation) run(ctx context.Context, sink SinkConnector, dls DeadLetterSink, cfg ConnectorConfig, pipelineID string, batch Batch) error {
	for len(iso.ranges) > 0 {
		rg := iso.ranges[0]
		recs := batch.Records[rg.lo:rg.hi]
		token := subToken(batch.DedupToken, rg.lo, rg.hi)
		if rg.dead {
			letters := make([]DeadLetter, len(recs))
			for i, rec := range recs {
				letters[i] = deadLetter(rec, rg.reason, batch)
			}
			if err := dls.WriteDeadLetters(ctx, cfg, pipelineID, letters, subToken(token, -1, -1)); err != nil {
				return fmt.Errorf("write dead-letter rows: %w", err)
			}
			iso.dlq += len(recs)
			iso.ranges = iso.ranges[1:]
			continue
		}
		iso.writes++
		n, err := sink.WriteBatch(ctx, cfg, Batch{Records: recs, SourceTS: batch.SourceTS, DedupToken: token})
		switch {
		case err == nil:
			iso.rows += n
			iso.ranges = iso.ranges[1:]
		case IsDataError(err):
			iso.ranges = append(iso.split(rg, err), iso.ranges[1:]...)
		default:
			return err
		}
	}
	return nil
}

// subToken derives the dedup token of a slice of a batch, so a retried or
// re-read batch splits into the same tokens.
func subToken(token string, lo, hi int) string {
	if token == "" {
		return ""
	}
	return dedupToken(token, lo, hi)
}

func deadLetter(rec Record, reason string, batch Batch) DeadLetter {
	raw := string(rec.RawJSON)
	if raw == "" {
		if b, err := json.Marshal(rec.Data); err == nil {
			raw = string(b)
		}
	}
	pos := rec.Position
	if pos == "" && batch.Checkpoint != nil {
		pos = batch.Checkpoint.Position
	}
	return DeadLetter{Raw: raw, Error: reason, Position: pos}
}

// shortError is the ClickHouse message without wrapping prefixes, bounded.
func shortError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "Code: "); i > 0 {
		msg = msg[i:]
	}
	msg = strings.TrimSpace(msg)
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	return msg
}
