package dashboards

import (
	"regexp"
	"strings"
	"time"
)

// Options of a DateTime column come back in ClickHouse's default text form,
// "2026-01-01 10:00:00" in the column's time zone. That is the canonical
// value: substituted as a quoted literal, ClickHouse reads it back in the
// same zone, while an ISO instant ("2026-01-01T09:00:00Z") is refused
// ("Cannot convert string"). Panel queries return DateTime values as ISO
// instants, so a value clicked in a panel is matched to its option by
// instant (see Option.Instant).

// dateTimeType matches DateTime and DateTime64 type names, wrapped in
// Nullable or LowCardinality or not, and captures the time zone if the type
// names one.
var dateTimeType = regexp.MustCompile(`^(?:(?:Nullable|LowCardinality)\()*DateTime(?:64)?(?:\((?:\s*\d+\s*,?)?\s*(?:'([^']*)')?\s*\))?\)*$`)

// DateTimeZone reports whether chType is a DateTime or DateTime64 type and
// returns the time zone it names; empty means the session time zone.
func DateTimeZone(chType string) (string, bool) {
	m := dateTimeType.FindStringSubmatch(strings.TrimSpace(chType))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// SetInstants sets the Instant of every option whose value is a ClickHouse
// date and time: text read in loc, or already an ISO instant (an options
// query that sets date_time_output_format itself).
func SetInstants(opts []Option, loc *time.Location) {
	for i := range opts {
		if t, ok := parseDateTimeText(opts[i].Value, loc); ok {
			opts[i].Instant = t.UTC().Format(time.RFC3339Nano)
		}
	}
}

func parseDateTimeText(s string, loc *time.Location) (time.Time, bool) {
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, loc); err == nil {
		return t, true
	}
	return parseInstant(s)
}

// parseInstant reads an ISO 8601 instant with a zone ("Z" or an offset) and
// any number of fractional digits: what panel queries return for a DateTime
// with date_time_output_format=iso, and what a browser's toISOString writes.
func parseInstant(s string) (time.Time, bool) {
	if len(s) < len("2006-01-02T15:04:05Z") || (s[10] != 'T' && s[10] != 't') {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// canonicalValues replaces every value that is not an option but is the
// instant of one with that option's value, keeping the order and dropping
// repeats. Values that match nothing are kept as they are.
func canonicalValues(values []string, opts []Option) []string {
	exact := make(map[string]bool, len(opts))
	for _, o := range opts {
		exact[o.Value] = true
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		if !exact[v] {
			if t, ok := parseInstant(v); ok {
				for _, o := range opts {
					if ot, ok := parseInstant(o.Instant); ok && ot.Equal(t) {
						v = o.Value
						break
					}
				}
			}
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func anyInstant(values []string) bool {
	for _, v := range values {
		if _, ok := parseInstant(v); ok {
			return true
		}
	}
	return false
}
