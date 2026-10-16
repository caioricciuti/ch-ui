package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Parse formats.
const (
	FormatJSON   = "json"
	FormatLogfmt = "logfmt"
	FormatRegex  = "regex"
	FormatNginx  = "nginx"
	FormatApache = "apache"
	FormatSyslog = "syslog"
)

// maxParseInput bounds the field a parse node reads. Longer values fail to
// parse instead of costing unbounded time.
const maxParseInput = 1 << 20

type parseConfig struct {
	Field        string  `json:"field"`
	Format       string  `json:"format"`
	Pattern      string  `json:"pattern"`
	Target       *string `json:"target"`
	Prefix       string  `json:"prefix"`
	OnError      string  `json:"on_error"`
	RemoveSource bool    `json:"remove_source"`
}

type parseNode struct {
	field   Path
	format  string
	re      *regexp.Regexp
	names   []string // re's named groups, by group index
	target  *Path    // nil: the record root
	prefix  string
	drop    bool
	remove  bool
	mapCol  bool // target is an OpenTelemetry map column
	parseFn func(string) (map[string]any, error)
}

func newParse(raw map[string]any, opts Options) (processor, error) {
	var cfg parseConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return nil, err
	}
	n := &parseNode{format: cfg.Format, prefix: cfg.Prefix, remove: cfg.RemoveSource}
	if strings.TrimSpace(cfg.Field) == "" && opts.otel() {
		cfg.Field = "Body"
	}
	field, err := ParsePath(cfg.Field, opts)
	if err != nil {
		return nil, fmt.Errorf("field: %w", err)
	}
	n.field = field
	switch cfg.OnError {
	case "", "keep":
	case "drop":
		n.drop = true
	default:
		return nil, fmt.Errorf("on_error must be keep or drop")
	}
	if len(cfg.Prefix) > 64 {
		return nil, fmt.Errorf("prefix is longer than 64 characters")
	}

	// An absent target is LogAttributes on a logs pipeline; "" is the root.
	target := ""
	if cfg.Target != nil {
		target = strings.TrimSpace(*cfg.Target)
	} else if opts.logs() {
		target = "LogAttributes"
	}
	if target != "" {
		t, err := ParsePath(target, opts)
		if err != nil {
			return nil, fmt.Errorf("target: %w", err)
		}
		if t.mapCol && len(t.segs) != 1 {
			return nil, fmt.Errorf("target %s is a map key, not an object", target)
		}
		n.target = &t
		n.mapCol = t.mapCol
	}

	switch cfg.Format {
	case FormatJSON:
		n.parseFn = parseJSONObject
	case FormatLogfmt:
		n.parseFn = parseLogfmt
	case FormatNginx, FormatApache:
		n.parseFn = parseCombined
	case FormatSyslog:
		n.parseFn = parseSyslog
	case FormatRegex:
		if strings.TrimSpace(cfg.Pattern) == "" {
			return nil, fmt.Errorf("pattern is required for the regex format")
		}
		if len(cfg.Pattern) > maxPatternLen {
			return nil, fmt.Errorf("pattern is longer than %d characters", maxPatternLen)
		}
		re, err := regexp.Compile(cfg.Pattern)
		if err != nil {
			return nil, fmt.Errorf("pattern: %v", err)
		}
		named := 0
		for _, name := range re.SubexpNames() {
			if name != "" {
				named++
			}
		}
		if named == 0 {
			return nil, fmt.Errorf("pattern needs at least one named group, like (?P<status>\\d+)")
		}
		n.re, n.names = re, re.SubexpNames()
		n.parseFn = n.parseRegex
	case "":
		return nil, fmt.Errorf("format is required")
	default:
		return nil, fmt.Errorf("unknown format %q (use json, logfmt, regex, nginx, apache or syslog)", cfg.Format)
	}
	return n, nil
}

var (
	errParseMissing   = errors.New("field is missing")
	errParseTooLong   = errors.New("field is longer than 1 MB")
	errParseNoMatch   = errors.New("pattern did not match")
	errParseNotObject = errors.New("not a JSON object")
)

func (n *parseNode) process(rec map[string]any) (bool, string, error) {
	v, ok := n.field.get(rec)
	if !ok || v == nil {
		return n.fail(errParseMissing)
	}
	var fields map[string]any
	if m, isMap := v.(map[string]any); isMap && n.format == FormatJSON {
		fields = m
	} else {
		s := Stringify(v)
		if len(s) > maxParseInput {
			return n.fail(errParseTooLong)
		}
		var err error
		if fields, err = n.parseFn(s); err != nil {
			return n.fail(err)
		}
	}
	dst, err := n.target.container(rec)
	if err != nil {
		return n.fail(err)
	}
	for k, val := range fields {
		if n.mapCol {
			val = Stringify(val)
		}
		dst[n.prefix+k] = val
	}
	if n.remove {
		n.field.del(rec)
	}
	return true, "", nil
}

func (n *parseNode) fail(err error) (bool, string, error) {
	err = fmt.Errorf("parse %s: %w", n.field.raw, err)
	if n.drop {
		return false, err.Error(), err
	}
	return true, "", err
}

func parseJSONObject(s string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, errParseNotObject
	}
	if dec.More() {
		return nil, errParseNotObject
	}
	return m, nil
}

func (n *parseNode) parseRegex(s string) (map[string]any, error) {
	m := n.re.FindStringSubmatchIndex(s)
	if m == nil {
		return nil, errParseNoMatch
	}
	out := map[string]any{}
	for i, name := range n.names {
		if name == "" || m[2*i] < 0 {
			continue
		}
		out[name] = s[m[2*i]:m[2*i+1]]
	}
	return out, nil
}

// parseLogfmt reads key=value pairs separated by spaces. Values may be
// double-quoted with Go/JSON escapes; a bare key is "true". At least one
// key=value pair is needed.
func parseLogfmt(s string) (map[string]any, error) {
	out := map[string]any{}
	pairs := 0
	i := 0
	for i < len(s) {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		for i < len(s) && s[i] != '=' && !isSpace(s[i]) && s[i] != '"' {
			i++
		}
		key := s[start:i]
		if key == "" {
			if s[i] == '"' {
				return nil, errors.New("not logfmt: quote without a key")
			}
			// "=value" with no key
			return nil, errors.New("not logfmt: value without a key")
		}
		if i >= len(s) || isSpace(s[i]) {
			out[key] = "true"
			continue
		}
		if s[i] == '"' {
			return nil, errors.New("not logfmt: quote inside a key")
		}
		i++ // '='
		if i < len(s) && s[i] == '"' {
			end := i + 1
			for end < len(s) && s[end] != '"' {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(s) {
				return nil, errors.New("not logfmt: unterminated quoted value")
			}
			quoted := s[i : end+1]
			val, err := strconv.Unquote(quoted)
			if err != nil {
				val = quoted[1 : len(quoted)-1]
			}
			out[key] = val
			i = end + 1
		} else {
			vs := i
			for i < len(s) && !isSpace(s[i]) {
				i++
			}
			out[key] = s[vs:i]
		}
		pairs++
	}
	if pairs == 0 {
		return nil, errors.New("not logfmt: no key=value pairs")
	}
	return out, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// combinedLog is the NCSA combined log format nginx and Apache write by
// default; the referer and user agent are optional (common log format).
var combinedLog = regexp.MustCompile(`^(\S+) (\S+) (\S+) \[([^\]]+)\] "((?:[^"\\]|\\.)*)" (\d{3}) (\d+|-)(?: "((?:[^"\\]|\\.)*)" "((?:[^"\\]|\\.)*)")?`)

// parseCombined returns client_ip, ident, remote_user, time_local, request
// (and method, path, protocol when it splits), status, body_bytes_sent,
// http_referer and http_user_agent. "-" values are left out.
func parseCombined(s string) (map[string]any, error) {
	m := combinedLog.FindStringSubmatch(s)
	if m == nil {
		return nil, errors.New("not a combined access log line")
	}
	out := map[string]any{}
	put := func(k, v string) {
		if v != "" && v != "-" {
			out[k] = v
		}
	}
	put("client_ip", m[1])
	put("ident", m[2])
	put("remote_user", m[3])
	put("time_local", m[4])
	req := unescapeLogQuoted(m[5])
	put("request", req)
	if parts := strings.Fields(req); len(parts) == 3 {
		put("method", parts[0])
		put("path", parts[1])
		put("protocol", parts[2])
	}
	put("status", m[6])
	put("body_bytes_sent", m[7])
	put("http_referer", unescapeLogQuoted(m[8]))
	put("http_user_agent", unescapeLogQuoted(m[9]))
	return out, nil
}

// unescapeLogQuoted undoes the \" and \\ escapes access logs use.
func unescapeLogQuoted(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var syslogSeverities = [8]string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}

var syslogMonths = map[string]bool{"Jan": true, "Feb": true, "Mar": true, "Apr": true, "May": true, "Jun": true, "Jul": true, "Aug": true, "Sep": true, "Oct": true, "Nov": true, "Dec": true}

// parseSyslog reads an RFC 5424 or RFC 3164 message. It returns priority,
// facility, severity (keyword, e.g. "err"), timestamp, hostname, app_name,
// proc_id, msg_id, structured_data and message; nil values ("-") are left
// out.
func parseSyslog(s string) (map[string]any, error) {
	if len(s) < 3 || s[0] != '<' {
		return nil, errors.New("not syslog: no <priority>")
	}
	end := strings.IndexByte(s[:min(len(s), 5)], '>')
	if end < 2 {
		return nil, errors.New("not syslog: bad <priority>")
	}
	pri, err := strconv.Atoi(s[1:end])
	if err != nil || pri < 0 || pri > 191 {
		return nil, errors.New("not syslog: bad <priority>")
	}
	out := map[string]any{
		"priority": strconv.Itoa(pri),
		"facility": strconv.Itoa(pri / 8),
		"severity": syslogSeverities[pri%8],
	}
	rest := s[end+1:]
	if len(rest) >= 2 && rest[0] >= '1' && rest[0] <= '9' && (rest[1] == ' ' || (len(rest) > 2 && rest[1] >= '0' && rest[1] <= '9' && rest[2] == ' ')) {
		return parse5424(rest, out)
	}
	parse3164(rest, out)
	return out, nil
}

func parse5424(s string, out map[string]any) (map[string]any, error) {
	// VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID
	names := [...]string{"version", "timestamp", "hostname", "app_name", "proc_id", "msg_id"}
	for _, name := range names {
		i := strings.IndexByte(s, ' ')
		if i <= 0 {
			return nil, errors.New("not syslog: RFC 5424 header is incomplete")
		}
		if tok := s[:i]; tok != "-" {
			out[name] = tok
		}
		s = s[i+1:]
	}
	// STRUCTURED-DATA: "-" or one or more [id param="value" ...] elements.
	switch {
	case strings.HasPrefix(s, "-"):
		s = s[1:]
	case strings.HasPrefix(s, "["):
		i := 0
		for i < len(s) && s[i] == '[' {
			j := i + 1
			inQuote := false
			for j < len(s) {
				c := s[j]
				if c == '\\' && inQuote {
					j += 2
					continue
				}
				if c == '"' {
					inQuote = !inQuote
				} else if c == ']' && !inQuote {
					break
				}
				j++
			}
			if j >= len(s) {
				return nil, errors.New("not syslog: unterminated structured data")
			}
			i = j + 1
		}
		out["structured_data"] = s[:i]
		s = s[i:]
	default:
		return nil, errors.New("not syslog: missing structured data")
	}
	if strings.HasPrefix(s, " ") {
		msg := strings.TrimPrefix(s[1:], "\ufeff")
		if msg != "" {
			out["message"] = msg
		}
	} else if s != "" {
		return nil, errors.New("not syslog: text after structured data")
	}
	return out, nil
}

// parse3164 is lenient, as relays are: a missing timestamp or hostname
// leaves the rest as the message.
func parse3164(s string, out map[string]any) {
	// "Mmm dd hh:mm:ss " is 16 bytes.
	if len(s) >= 16 && syslogMonths[s[:3]] && s[3] == ' ' && s[6] == ' ' && s[9] == ':' && s[12] == ':' && s[15] == ' ' {
		out["timestamp"] = s[:15]
		s = s[16:]
		if i := strings.IndexByte(s, ' '); i > 0 && !strings.ContainsAny(s[:i], ":[") {
			out["hostname"] = s[:i]
			s = s[i+1:]
		}
	}
	// TAG[pid]: message
	j := 0
	for j < len(s) && j < 48 && (isAlnum(s[j]) || s[j] == '-' || s[j] == '_' || s[j] == '.' || s[j] == '/') {
		j++
	}
	if j > 0 && j < len(s) && (s[j] == ':' || s[j] == '[') {
		tag := s[:j]
		r := s[j:]
		pid := ""
		if r[0] == '[' {
			if k := strings.IndexByte(r, ']'); k > 0 {
				pid, r = r[1:k], r[k+1:]
			} else {
				r = ""
				tag = ""
			}
		}
		if tag != "" && strings.HasPrefix(r, ":") {
			out["app_name"] = tag
			if pid != "" {
				out["proc_id"] = pid
			}
			s = strings.TrimPrefix(r[1:], " ")
		}
	}
	if s != "" {
		out["message"] = s
	}
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
