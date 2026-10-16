package transform

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// Redaction kinds.
const (
	KindEmail       = "email"
	KindIPv4        = "ipv4"
	KindIPv6        = "ipv6"
	KindCreditCard  = "credit_card"
	KindJWT         = "jwt"
	KindBearerToken = "bearer_token"
	KindAWSKey      = "aws_key"
	KindCustom      = "custom"
)

// defaultReplacement is used when replacement is empty.
const defaultReplacement = "[REDACTED:{kind}]"

// matcher finds one kind of sensitive value. When group > 0 only that
// submatch is replaced (the token after "Bearer "). valid, when set,
// rejects a candidate s[start:end] (the Luhn check for card numbers).
type matcher struct {
	kind  string
	re    *regexp.Regexp
	group int
	valid func(s string, start, end int) bool
	// hint, when set, is a cheap test a string must pass before the
	// regular expression runs; most log text has no match at all.
	hint func(s string) bool
}

// builtinMatchers, in the order they run: tokens first, so a JWT inside a
// bearer header is reported as the bearer token, and IPv6 before IPv4 so
// ::ffff:10.0.0.1 is one match.
var builtinMatchers = []matcher{
	{kind: KindBearerToken, re: regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9\-._~+/]+=*)`), group: 1, hint: func(s string) bool { return strings.Contains(s, "earer") || strings.Contains(s, "EARER") }},
	{kind: KindJWT, re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`), hint: func(s string) bool { return strings.Contains(s, "eyJ") }},
	{kind: KindAWSKey, re: regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16}\b`), hint: func(s string) bool { return len(s) >= 20 && strings.ContainsAny(s, "AK") }},
	{kind: KindEmail, re: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), hint: func(s string) bool { return strings.IndexByte(s, '@') >= 0 }},
	{kind: KindCreditCard, re: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`), valid: luhnValid, hint: func(s string) bool { return countDigits(s) >= 13 }},
	{kind: KindIPv6, re: regexp.MustCompile(`[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3})?`), valid: ipv6Valid, hint: func(s string) bool { return strings.Count(s, ":") >= 2 }},
	{kind: KindIPv4, re: regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b`), hint: func(s string) bool { return strings.Count(s, ".") >= 3 }},
}

func countDigits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			n++
		}
	}
	return n
}

type redactConfig struct {
	Kinds  []string `json:"kinds"`
	Custom []struct {
		Name    string `json:"name"`
		Pattern string `json:"pattern"`
	} `json:"custom"`
	Fields      []string `json:"fields"`
	Replacement string   `json:"replacement"`
}

type redactNode struct {
	matchers    []matcher
	fields      []Path // empty: every string in the record
	replacement string
}

func newRedact(raw map[string]any, opts Options) (processor, error) {
	var cfg redactConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return nil, err
	}
	if len(cfg.Kinds) == 0 {
		return nil, fmt.Errorf("choose at least one kind to redact")
	}
	want := map[string]bool{}
	for _, k := range cfg.Kinds {
		switch k {
		case KindEmail, KindIPv4, KindIPv6, KindCreditCard, KindJWT, KindBearerToken, KindAWSKey, KindCustom:
			want[k] = true
		default:
			return nil, fmt.Errorf("unknown kind %q", k)
		}
	}
	n := &redactNode{replacement: cfg.Replacement}
	if n.replacement == "" {
		n.replacement = defaultReplacement
	}
	if len(n.replacement) > maxReplacement {
		return nil, fmt.Errorf("replacement is longer than %d characters", maxReplacement)
	}
	for _, m := range builtinMatchers {
		if want[m.kind] {
			n.matchers = append(n.matchers, m)
		}
	}
	if want[KindCustom] {
		if len(cfg.Custom) == 0 {
			return nil, fmt.Errorf("custom needs at least one pattern")
		}
		if len(cfg.Custom) > maxCustomKinds {
			return nil, fmt.Errorf("at most %d custom patterns", maxCustomKinds)
		}
		for i, c := range cfg.Custom {
			name := strings.TrimSpace(c.Name)
			if name == "" {
				name = KindCustom
			}
			if len(name) > 64 {
				return nil, fmt.Errorf("custom pattern %d: name is longer than 64 characters", i+1)
			}
			if strings.TrimSpace(c.Pattern) == "" {
				return nil, fmt.Errorf("custom pattern %d (%s) is empty", i+1, name)
			}
			if len(c.Pattern) > maxPatternLen {
				return nil, fmt.Errorf("custom pattern %d (%s) is longer than %d characters", i+1, name, maxPatternLen)
			}
			re, err := regexp.Compile(c.Pattern)
			if err != nil {
				return nil, fmt.Errorf("custom pattern %d (%s): %v", i+1, name, err)
			}
			n.matchers = append(n.matchers, matcher{kind: name, re: re})
		}
	}
	if len(cfg.Fields) > maxListItems {
		return nil, fmt.Errorf("fields has more than %d entries", maxListItems)
	}
	for _, f := range cfg.Fields {
		if strings.TrimSpace(f) == "" {
			continue
		}
		p, err := ParsePath(f, opts)
		if err != nil {
			return nil, fmt.Errorf("fields: %w", err)
		}
		n.fields = append(n.fields, p)
	}
	return n, nil
}

func (n *redactNode) process(rec map[string]any) (bool, string, error) {
	if len(n.fields) == 0 {
		n.walkMap(rec)
		return true, "", nil
	}
	for _, p := range n.fields {
		v, ok := p.get(rec)
		if !ok {
			continue
		}
		if s, isStr := v.(string); isStr {
			if r, changed := n.redact(s); changed {
				_ = p.set(rec, r)
			}
			continue
		}
		n.walk(v)
	}
	return true, "", nil
}

func (n *redactNode) walkMap(m map[string]any) {
	for k, v := range m {
		if s, ok := v.(string); ok {
			if r, changed := n.redact(s); changed {
				m[k] = r
			}
			continue
		}
		n.walk(v)
	}
}

func (n *redactNode) walk(v any) {
	switch x := v.(type) {
	case map[string]any:
		n.walkMap(x)
	case []any:
		for i, e := range x {
			if s, ok := e.(string); ok {
				if r, changed := n.redact(s); changed {
					x[i] = r
				}
				continue
			}
			n.walk(e)
		}
	}
}

// redact replaces every match of every matcher in s.
func (n *redactNode) redact(s string) (string, bool) {
	changed := false
	for _, m := range n.matchers {
		if r, ok := m.replace(s, n.replacement); ok {
			s, changed = r, true
		}
	}
	return s, changed
}

func (m matcher) replace(s, replacement string) (string, bool) {
	if m.hint != nil && !m.hint(s) {
		return s, false
	}
	locs := m.re.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s, false
	}
	repl := strings.ReplaceAll(replacement, "{kind}", m.kind)
	var b strings.Builder
	last := 0
	changed := false
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		if m.group > 0 {
			start, end = loc[2*m.group], loc[2*m.group+1]
		}
		if start < 0 || start == end {
			continue
		}
		if m.valid != nil && !m.valid(s, start, end) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(repl)
		last = end
		changed = true
	}
	if !changed {
		return s, false
	}
	b.WriteString(s[last:])
	return b.String(), true
}

// luhnValid reports whether the digits of the match (13 to 19 of them,
// spaces and dashes ignored) pass the Luhn check.
func luhnValid(s string, start, end int) bool {
	s = s[start:end]
	sum, n := 0, 0
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c == ' ' || c == '-' {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if n%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		n++
	}
	return n >= 13 && n <= 19 && sum%10 == 0
}

// ipv6Valid accepts a match that parses as an IPv6 address, has a hex
// digit and is not glued to a word ("std::vector" is not an address).
func ipv6Valid(s string, start, end int) bool {
	if (start > 0 && isAlnum(s[start-1])) || (end < len(s) && (isAlnum(s[end]) || s[end] == ':')) {
		return false
	}
	m := s[start:end]
	if strings.Count(m, ":") < 2 || !strings.ContainsAny(m, "0123456789abcdefABCDEF") {
		return false
	}
	a, err := netip.ParseAddr(m)
	return err == nil && a.Is6()
}
