package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type mapConfig struct {
	Rename []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"rename"`
	Set []struct {
		Field string `json:"field"`
		Value any    `json:"value"`
	} `json:"set"`
	Copy []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"copy"`
	Drop            []string `json:"drop"`
	SeverityFrom    string   `json:"severity_from"`
	TimestampFrom   string   `json:"timestamp_from"`
	TimestampFormat string   `json:"timestamp_format"`
}

type pathPair struct{ from, to Path }

type setOp struct {
	field Path
	value any
}

// mapNode runs, in order: copy, rename, set, severity_from, timestamp_from,
// drop.
type mapNode struct {
	copies  []pathPair
	renames []pathPair
	sets    []setOp
	drops   []Path

	severity   *Path
	sevText    Path // SeverityText or severity
	sevNumber  *Path
	timestamp  *Path
	tsFormat   string
	tsLayout   string
	tsTarget   Path
	tsDateTime bool // write "<sec>.<ns>" for a DateTime64(9) column
}

func newMap(raw map[string]any, opts Options) (processor, error) {
	var cfg mapConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return nil, err
	}
	for _, l := range []struct {
		name string
		n    int
	}{{"copy", len(cfg.Copy)}, {"rename", len(cfg.Rename)}, {"set", len(cfg.Set)}, {"drop", len(cfg.Drop)}} {
		if l.n > maxListItems {
			return nil, fmt.Errorf("%s has more than %d entries", l.name, maxListItems)
		}
	}
	n := &mapNode{}
	pair := func(what string, from, to string) (pathPair, error) {
		f, err := ParsePath(from, opts)
		if err != nil {
			return pathPair{}, fmt.Errorf("%s from: %w", what, err)
		}
		t, err := ParsePath(to, opts)
		if err != nil {
			return pathPair{}, fmt.Errorf("%s to: %w", what, err)
		}
		return pathPair{f, t}, nil
	}
	for _, c := range cfg.Copy {
		p, err := pair("copy", c.From, c.To)
		if err != nil {
			return nil, err
		}
		n.copies = append(n.copies, p)
	}
	for _, r := range cfg.Rename {
		p, err := pair("rename", r.From, r.To)
		if err != nil {
			return nil, err
		}
		n.renames = append(n.renames, p)
	}
	for _, s := range cfg.Set {
		f, err := ParsePath(s.Field, opts)
		if err != nil {
			return nil, fmt.Errorf("set: %w", err)
		}
		if b, _ := json.Marshal(s.Value); len(b) > maxSetValueSize {
			return nil, fmt.Errorf("set %s: value is larger than %d bytes", s.Field, maxSetValueSize)
		}
		n.sets = append(n.sets, setOp{field: f, value: s.Value})
	}
	for _, d := range cfg.Drop {
		p, err := ParsePath(d, opts)
		if err != nil {
			return nil, fmt.Errorf("drop: %w", err)
		}
		n.drops = append(n.drops, p)
	}
	if strings.TrimSpace(cfg.SeverityFrom) != "" {
		p, err := ParsePath(cfg.SeverityFrom, opts)
		if err != nil {
			return nil, fmt.Errorf("severity_from: %w", err)
		}
		n.severity = &p
		if opts.logs() {
			n.sevText = Path{raw: "SeverityText", segs: []string{"SeverityText"}}
			n.sevNumber = &Path{raw: "SeverityNumber", segs: []string{"SeverityNumber"}}
		} else {
			n.sevText = Path{raw: "severity", segs: []string{"severity"}}
		}
	}
	if strings.TrimSpace(cfg.TimestampFrom) != "" {
		p, err := ParsePath(cfg.TimestampFrom, opts)
		if err != nil {
			return nil, fmt.Errorf("timestamp_from: %w", err)
		}
		n.timestamp = &p
		n.tsFormat = cfg.TimestampFormat
		switch {
		case n.tsFormat == "":
			n.tsFormat = "rfc3339"
		case n.tsFormat == "rfc3339", n.tsFormat == "unix", n.tsFormat == "unix_ms", n.tsFormat == "unix_ns":
		case strings.HasPrefix(n.tsFormat, "layout:"):
			n.tsLayout = strings.TrimPrefix(n.tsFormat, "layout:")
			if strings.TrimSpace(n.tsLayout) == "" || len(n.tsLayout) > 100 {
				return nil, fmt.Errorf("timestamp_format layout: is empty or too long")
			}
		default:
			return nil, fmt.Errorf("timestamp_format must be rfc3339, unix, unix_ms, unix_ns or layout:<Go layout>")
		}
		if opts.otel() {
			n.tsTarget = Path{raw: "Timestamp", segs: []string{"Timestamp"}}
			n.tsDateTime = true
		} else {
			n.tsTarget = Path{raw: "timestamp", segs: []string{"timestamp"}}
		}
	}
	return n, nil
}

func (n *mapNode) process(rec map[string]any) (bool, string, error) {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, c := range n.copies {
		if v, ok := c.from.get(rec); ok {
			note(c.to.set(rec, deepCopy(v)))
		}
	}
	for _, r := range n.renames {
		if v, ok := r.from.get(rec); ok {
			r.from.del(rec)
			note(r.to.set(rec, v))
		}
	}
	for _, s := range n.sets {
		note(s.field.set(rec, deepCopy(s.value)))
	}
	if n.severity != nil {
		if v, ok := n.severity.get(rec); ok {
			if text, num, ok := severityOf(Stringify(v)); ok {
				note(n.sevText.set(rec, text))
				if n.sevNumber != nil {
					note(n.sevNumber.set(rec, num))
				}
			}
		}
	}
	if n.timestamp != nil {
		if v, ok := n.timestamp.get(rec); ok && v != nil {
			t, err := parseTimestamp(v, n.tsFormat, n.tsLayout)
			if err != nil {
				note(fmt.Errorf("timestamp_from %s: %w", n.timestamp.raw, err))
			} else if n.tsDateTime {
				note(n.tsTarget.set(rec, dateTime64(t)))
			} else {
				note(n.tsTarget.set(rec, t.UTC().Format(time.RFC3339Nano)))
			}
		}
	}
	for _, d := range n.drops {
		d.del(rec)
	}
	return true, "", firstErr
}

// severityOf maps a level word to the OpenTelemetry severity text and
// number. Unknown words report false.
func severityOf(level string) (string, int, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "trace":
		return "TRACE", 1, true
	case "debug", "dbg":
		return "DEBUG", 5, true
	case "info", "information", "informational", "notice":
		return "INFO", 9, true
	case "warn", "warning":
		return "WARN", 13, true
	case "error", "err":
		return "ERROR", 17, true
	case "fatal", "critical", "crit", "alert", "emerg", "emergency", "panic":
		return "FATAL", 21, true
	}
	return "", 0, false
}

var errTimestamp = errors.New("not a timestamp in the configured format")

// parseTimestamp reads v in one of the formats. Numbers are read from their
// decimal text so no float rounding changes the instant.
func parseTimestamp(v any, format, layout string) (time.Time, error) {
	s := strings.TrimSpace(Stringify(v))
	if f, ok := v.(float64); ok {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	switch format {
	case "rfc3339":
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}, errTimestamp
		}
		return t, nil
	case "unix":
		return unixDecimal(s, 0)
	case "unix_ms":
		return unixDecimal(s, 3)
	case "unix_ns":
		return unixDecimal(s, 9)
	default:
		t, err := time.Parse(layout, s)
		if err != nil {
			return time.Time{}, errTimestamp
		}
		return t, nil
	}
}

// unixDecimal parses "<int>[.<frac>]" counted in 10^-scale seconds.
func unixDecimal(s string, scale int) (time.Time, error) {
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" || len(intPart) > 19 {
		return time.Time{}, errTimestamp
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return time.Time{}, errTimestamp
	}
	for _, c := range frac {
		if c < '0' || c > '9' {
			return time.Time{}, errTimestamp
		}
	}
	// Total nanoseconds digits after the unit: 9-scale.
	unitNS := int64(math.Pow10(9 - scale))
	sec := whole / int64(math.Pow10(scale))
	rem := whole % int64(math.Pow10(scale))
	ns := rem * unitNS
	if frac != "" && unitNS > 1 {
		digits := len(strconv.FormatInt(unitNS, 10)) - 1
		frac = (frac + "000000000")[:digits]
		f, _ := strconv.ParseInt(frac, 10, 64)
		ns += f
	}
	return time.Unix(sec, ns).UTC(), nil
}

// dateTime64 formats t as "<unix seconds>.<9 digits>", the absolute form the
// OpenTelemetry tables' DateTime64(9) columns read regardless of time zone.
func dateTime64(t time.Time) string {
	sec := t.Unix()
	if sec < 0 {
		return "0.000000000"
	}
	return fmt.Sprintf("%d.%09d", sec, t.Nanosecond())
}
