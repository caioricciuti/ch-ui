// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileVersion is the chui_access version this build writes and reads.
const FileVersion = 1

// FileSource says where an access file was exported from. It is
// informational; import never reads it for anything but the audit log.
type FileSource struct {
	Connection string `json:"connection,omitempty"`
	Host       string `json:"host,omitempty"`
	ExportedAt string `json:"exported_at,omitempty"`
	// Incomplete lists the snapshot sections that could not be read.
	Incomplete []string `json:"incomplete,omitempty"`
}

// AccessFile is a parsed access file.
type AccessFile struct {
	Source FileSource
	State  *State
	// Logins are the users' authentication types and hosts, by user name:
	// hints for creating a user the target lacks. Never a secret.
	Logins map[string]UserLogin
}

// FileError is a problem in an access file at Path (roles.analyst.profile,
// grants.analyst[2].on). Line is 0 when unknown.
type FileError struct {
	Path string
	Line int
	Msg  string
}

func (e *FileError) Error() string {
	s := e.Msg
	if e.Path != "" {
		s = e.Path + ": " + s
	}
	if e.Line > 0 {
		s += " (line " + strconv.Itoa(e.Line) + ")"
	}
	return s
}

// ── Writing ─────────────────────────────────────────────────────────

// MarshalFileYAML writes st as an access file, with the login hints of its
// users. header becomes the comment at the top of the document.
func MarshalFileYAML(st *State, logins map[string]UserLogin, src FileSource, header string) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode, HeadComment: header, Content: []*yaml.Node{fileNode(st, logins, src)}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalFileJSON writes st as an access file in JSON, the same schema as
// the YAML form.
func MarshalFileJSON(st *State, logins map[string]UserLogin, src FileSource) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSONNode(&buf, fileNode(st, logins, src)); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func fileNode(st *State, logins map[string]UserLogin, src FileSource) *yaml.Node {
	st = st.Canonical()
	root := ymap(false)
	root.Content = append(root.Content, ystr("chui_access"), yint(FileVersion))
	{
		m := ymap(false)
		addStr(m, "connection", src.Connection)
		addStr(m, "host", src.Host)
		addStr(m, "exported_at", src.ExportedAt)
		if len(src.Incomplete) > 0 {
			m.Content = append(m.Content, ystr("incomplete"), ystrs(false, src.Incomplete))
		}
		if len(m.Content) > 0 {
			root.Content = append(root.Content, ystr("source"), m)
		}
	}

	roleNames, userNames := map[string]bool{}, map[string]bool{}
	for _, r := range st.Roles {
		roleNames[r.Name] = true
	}
	for _, u := range st.Users {
		userNames[u.Name] = true
	}

	if len(st.Roles) > 0 {
		m := ymap(false)
		for _, r := range st.Roles {
			e := ymap(true)
			switch len(r.Profiles) {
			case 0:
			case 1:
				e.Content = append(e.Content, ystr("profile"), ystr(r.Profiles[0]))
			default:
				e.Content = append(e.Content, ystr("profiles"), ystrs(true, r.Profiles))
			}
			m.Content = append(m.Content, ystr(r.Name), e)
		}
		root.Content = append(root.Content, ystr("roles"), m)
	}

	if len(st.Users) > 0 {
		m := ymap(false)
		for _, u := range st.Users {
			e := ymap(true)
			switch {
			case u.DefaultAll:
				e.Content = append(e.Content, ystr("default_roles"), ystr("all"))
				if len(u.DefaultExcept) > 0 {
					e.Content = append(e.Content, ystr("except"), ystrs(true, u.DefaultExcept))
				}
			case len(u.DefaultRoles) == 0:
				e.Content = append(e.Content, ystr("default_roles"), ystr("none"))
			default:
				e.Content = append(e.Content, ystr("default_roles"), ystrs(true, u.DefaultRoles))
			}
			if l, ok := logins[u.Name]; ok {
				addStr(e, "auth", l.Auth)
				if len(l.Hosts) > 0 {
					hosts := make([]string, len(l.Hosts))
					for i, h := range l.Hosts {
						hosts[i] = FormatHost(h)
					}
					e.Content = append(e.Content, ystr("hosts"), ystrs(true, hosts))
				}
			}
			m.Content = append(m.Content, ystr(u.Name), e)
		}
		root.Content = append(root.Content, ystr("users"), m)
	}

	if len(st.Grants) > 0 {
		m := ymap(false)
		for _, g := range st.Grants {
			list := yseq(false)
			for _, p := range mergeColumnGrants(g.Privileges) {
				e := ymap(true)
				e.Content = append(e.Content, ystr("access"), ystrs(true, p.Access), ykey("on"), ystr(formatOn(p.Database, p.Table)))
				if len(p.columns) > 0 {
					e.Content = append(e.Content, ystr("columns"), ystrs(true, p.columns))
				}
				if p.Revoke {
					e.Content = append(e.Content, ystr("revoke"), ybool(true))
				}
				if p.GrantOption {
					e.Content = append(e.Content, ystr("grant_option"), ybool(true))
				}
				list.Content = append(list.Content, e)
			}
			m.Content = append(m.Content, ystr(granteeKey(g.target(), roleNames, userNames)), list)
		}
		root.Content = append(root.Content, ystr("grants"), m)
	}

	if len(st.RoleGrants) > 0 {
		m := ymap(false)
		var cur *yaml.Node
		var curKey string
		for _, g := range st.RoleGrants {
			key := granteeKey(g.target(), roleNames, userNames)
			if cur == nil || key != curKey {
				cur, curKey = yseq(true), key
				m.Content = append(m.Content, ystr(key), cur)
			}
			if g.AdminOption {
				cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
					ystr("role"), ystr(g.Role), ystr("admin_option"), ybool(true),
				}})
			} else {
				cur.Content = append(cur.Content, ystr(g.Role))
			}
		}
		root.Content = append(root.Content, ystr("role_grants"), m)
	}

	if len(st.RowPolicies) > 0 {
		list := yseq(false)
		for _, p := range st.RowPolicies {
			e := ymap(true)
			e.Content = append(e.Content, ystr("name"), ystr(p.Name), ykey("on"), ystr(formatOn(p.Database, p.Table)), ystr("using"), ystr(p.Using))
			if p.Restrictive {
				e.Content = append(e.Content, ystr("restrictive"), ybool(true))
			}
			addAssignees(e, p.To)
			list.Content = append(list.Content, e)
		}
		root.Content = append(root.Content, ystr("row_policies"), list)
	}

	if len(st.Quotas) > 0 {
		m := ymap(false)
		for _, q := range st.Quotas {
			e := ymap(false)
			addStr(e, "keyed_by", q.KeyedBy)
			if len(q.Intervals) > 0 {
				ivs := yseq(false)
				for _, iv := range q.Intervals {
					ie := ymap(true)
					ie.Content = append(ie.Content, ystr("every"), ystr(strconv.FormatUint(iv.Duration, 10)+" "+iv.Unit))
					if iv.Randomized {
						ie.Content = append(ie.Content, ystr("randomized"), ybool(true))
					}
					if len(iv.Limits) > 0 {
						lm := ymap(true)
						for _, res := range sortedLimitKeys(iv.Limits) {
							lm.Content = append(lm.Content, ystr(res), yint(iv.Limits[res]))
						}
						ie.Content = append(ie.Content, ystr("limits"), lm)
					}
					ivs.Content = append(ivs.Content, ie)
				}
				e.Content = append(e.Content, ystr("intervals"), ivs)
			}
			addAssignees(e, q.To)
			m.Content = append(m.Content, ystr(q.Name), e)
		}
		root.Content = append(root.Content, ystr("quotas"), m)
	}

	if len(st.SettingsProfiles) > 0 {
		m := ymap(false)
		for _, p := range st.SettingsProfiles {
			e := ymap(false)
			if len(p.Inherit) > 0 {
				e.Content = append(e.Content, ystr("inherit"), ystrs(true, p.Inherit))
			}
			if len(p.Settings) > 0 {
				list := yseq(false)
				for _, s := range p.Settings {
					se := ymap(true)
					se.Content = append(se.Content, ystr("name"), ystr(s.Name))
					for _, f := range []struct {
						key string
						v   *string
					}{{"value", s.Value}, {"min", s.Min}, {"max", s.Max}} {
						if f.v != nil {
							se.Content = append(se.Content, ystr(f.key), ystr(*f.v))
						}
					}
					addStr(se, "writability", s.Writability)
					list.Content = append(list.Content, se)
				}
				e.Content = append(e.Content, ystr("settings"), list)
			}
			addAssignees(e, p.To)
			if len(e.Content) == 0 {
				e.Style = yaml.FlowStyle
			}
			m.Content = append(m.Content, ystr(p.Name), e)
		}
		root.Content = append(root.Content, ystr("settings_profiles"), m)
	}
	return root
}

type fileGrant struct {
	StateGrant
	columns []string
}

// mergeColumnGrants folds column grants that differ only in the column into
// one entry with a columns list.
func mergeColumnGrants(list []StateGrant) []fileGrant {
	var out []fileGrant
	index := map[string]int{}
	for _, p := range list {
		if p.Column == "" {
			out = append(out, fileGrant{StateGrant: p})
			continue
		}
		key := strings.Join(p.Access, "\x00") + "\x01" + p.Database + "\x01" + p.Table + "\x01" + strconv.FormatBool(p.Revoke) + strconv.FormatBool(p.GrantOption)
		if i, ok := index[key]; ok {
			out[i].columns = append(out[i].columns, p.Column)
			continue
		}
		index[key] = len(out)
		g := p
		g.Column = ""
		out = append(out, fileGrant{StateGrant: g, columns: []string{p.Column}})
	}
	return out
}

func sortedLimitKeys(m map[string]uint64) []string {
	order := map[string]int{}
	for i, r := range QuotaResources {
		order[r] = i
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		oi, iok := order[keys[i]]
		oj, jok := order[keys[j]]
		if iok != jok {
			return iok
		}
		if oi != oj {
			return oi < oj
		}
		return keys[i] < keys[j]
	})
	return keys
}

func addStr(m *yaml.Node, key, v string) {
	if v != "" {
		m.Content = append(m.Content, ystr(key), ystr(v))
	}
}

// addAssignees writes a TO clause: a list of names, or all with except.
// TO NONE is left out.
func addAssignees(m *yaml.Node, a Assignees) {
	switch {
	case a.All:
		m.Content = append(m.Content, ystr("to"), ystr("all"))
		if len(a.Except) > 0 {
			m.Content = append(m.Content, ystr("except"), ystrs(true, a.Except))
		}
	case len(a.Names) > 0:
		m.Content = append(m.Content, ystr("to"), ystrs(true, a.Names))
	}
}

// granteeKey is the key of a grantee in grants and role_grants: the bare
// name when the roles or users section says what it is, else role:name or
// user:name.
func granteeKey(t Target, roles, users map[string]bool) string {
	qualified := strings.HasPrefix(t.Name, "role:") || strings.HasPrefix(t.Name, "user:")
	switch {
	case qualified:
	case t.Type == "role" && roles[t.Name] && !users[t.Name]:
		return t.Name
	case t.Type == "user" && users[t.Name] && !roles[t.Name]:
		return t.Name
	}
	return t.Type + ":" + t.Name
}

// yaml11Bool matches the plain scalars YAML 1.1 readers take for booleans;
// they are quoted so every reader sees a string.
var yaml11Bool = regexp.MustCompile(`^(?i:y|n|yes|no|on|off|true|false)$`)

func ystr(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	if yaml11Bool.MatchString(v) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// ykey is a schema key, written plain even where YAML 1.1 would read a
// boolean ("on").
func ykey(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func yint(v uint64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(v, 10)}
}

func ybool(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
}

func ymap(flow bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if flow {
		n.Style = yaml.FlowStyle
	}
	return n
}

func yseq(flow bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if flow {
		n.Style = yaml.FlowStyle
	}
	return n
}

func ystrs(flow bool, list []string) *yaml.Node {
	n := yseq(flow)
	for _, s := range list {
		n.Content = append(n.Content, ystr(s))
	}
	return n
}

// writeJSONNode writes a node built by fileNode as compact JSON, keeping
// key order.
func writeJSONNode(w *bytes.Buffer, n *yaml.Node) error {
	switch n.Kind {
	case yaml.MappingNode:
		w.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := writeJSONNode(w, n.Content[i]); err != nil {
				return err
			}
			w.WriteByte(':')
			if err := writeJSONNode(w, n.Content[i+1]); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	case yaml.SequenceNode:
		w.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := writeJSONNode(w, c); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!int", "!!bool":
			w.WriteString(n.Value)
		default:
			b, err := json.Marshal(n.Value)
			if err != nil {
				return err
			}
			w.Write(b)
		}
	default:
		return fmt.Errorf("unexpected node kind %d", n.Kind)
	}
	return nil
}

// ── On clauses ──────────────────────────────────────────────────────

// formatOn writes an object as *.*, db.* or db.table, backtick quoting
// names that contain a dot, a backtick or a star.
func formatOn(db, table string) string {
	if db == "" {
		return "*.*"
	}
	t := "*"
	if table != "" {
		t = quoteOnName(table)
	}
	return quoteOnName(db) + "." + t
}

func quoteOnName(s string) string {
	if s == "" || strings.ContainsAny(s, ".`*") || strings.TrimSpace(s) != s {
		return "`" + strings.ReplaceAll(s, "`", "``") + "`"
	}
	return s
}

// parseOn reads *.*, db.* or db.table. Names may be backtick quoted, with
// a doubled backtick for a backtick.
func parseOn(s string) (db, table string, err error) {
	db, quotedDB, rest, err := readOnName(strings.TrimSpace(s))
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(rest, ".") {
		return "", "", fmt.Errorf("%q must be *.*, db.* or db.table", s)
	}
	table, quotedTable, rest, err := readOnName(rest[1:])
	if err != nil {
		return "", "", err
	}
	if rest != "" {
		return "", "", fmt.Errorf("%q must be *.*, db.* or db.table", s)
	}
	dbStar, tableStar := db == "*" && !quotedDB, table == "*" && !quotedTable
	switch {
	case dbStar && tableStar:
		return "", "", nil
	case dbStar:
		return "", "", fmt.Errorf("%q: a table needs a database", s)
	case tableStar:
		table = ""
	}
	if err := ValidateName("database", db); err != nil {
		return "", "", fmt.Errorf("%q: %w", s, err)
	}
	if !tableStar {
		if err := ValidateName("table", table); err != nil {
			return "", "", fmt.Errorf("%q: %w", s, err)
		}
	}
	return db, table, nil
}

func readOnName(s string) (name string, quoted bool, rest string, err error) {
	if strings.HasPrefix(s, "`") {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] != '`' {
				b.WriteByte(s[i])
				continue
			}
			if i+1 < len(s) && s[i+1] == '`' {
				b.WriteByte('`')
				i++
				continue
			}
			return b.String(), true, s[i+1:], nil
		}
		return "", false, "", fmt.Errorf("%q has an unclosed backtick", s)
	}
	i := strings.IndexByte(s, '.')
	if i < 0 {
		i = len(s)
	}
	if strings.ContainsRune(s[:i], '`') {
		return "", false, "", fmt.Errorf("%q has a stray backtick", s)
	}
	return s[:i], false, s[i:], nil
}

// ── Reading ─────────────────────────────────────────────────────────

// ParseFile reads an access file, YAML or JSON. Unknown keys, a missing or
// newer chui_access version and malformed entries are errors naming the
// path. The state comes back canonical.
func ParseFile(b []byte) (*AccessFile, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, &FileError{Msg: "the file is empty"}
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, &FileError{Msg: "not valid YAML or JSON: " + strings.TrimPrefix(err.Error(), "yaml: ")}
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, &FileError{Msg: "the file holds more than one YAML document"}
	}
	if len(doc.Content) == 0 {
		return nil, &FileError{Msg: "the file is empty"}
	}
	p := &fileParser{roles: map[string]bool{}, users: map[string]bool{}}
	f, err := p.file(doc.Content[0])
	if err != nil {
		return nil, err
	}
	return f, nil
}

type fileParser struct {
	roles, users map[string]bool
}

type entry struct {
	key  string
	node *yaml.Node
	line int
}

func ferr(path string, n *yaml.Node, format string, args ...any) error {
	e := &FileError{Path: path, Msg: fmt.Sprintf(format, args...)}
	if n != nil {
		e.Line = n.Line
	}
	return e
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

// child is path joined with a map key, bracketed when the key would read
// as path syntax.
func child(path, key string) string {
	if key == "" || strings.ContainsAny(key, ".[]\" ") {
		key = "[" + strconv.Quote(key) + "]"
		return path + key
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

func index(path string, i int) string { return path + "[" + strconv.Itoa(i) + "]" }

// mapping returns the entries of a mapping node in order; null is an empty
// mapping. Duplicate keys are errors.
func mapping(n *yaml.Node, path string) ([]entry, error) {
	n = deref(n)
	if isNull(n) {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, ferr(path, n, "must be a mapping")
	}
	var out []entry
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := deref(n.Content[i])
		if k.Kind != yaml.ScalarNode || isNull(k) {
			return nil, ferr(path, k, "keys must be names")
		}
		if seen[k.Value] {
			return nil, ferr(child(path, k.Value), k, "listed twice")
		}
		seen[k.Value] = true
		out = append(out, entry{key: k.Value, node: deref(n.Content[i+1]), line: k.Line})
	}
	return out, nil
}

// fields reads a mapping whose keys must be among allowed.
func fields(n *yaml.Node, path string, allowed ...string) (map[string]*yaml.Node, error) {
	list, err := mapping(n, path)
	if err != nil {
		return nil, err
	}
	out := map[string]*yaml.Node{}
	for _, e := range list {
		if !contains(allowed, e.key) {
			return nil, &FileError{Path: child(path, e.key), Line: e.line, Msg: "unknown key (expected one of " + strings.Join(allowed, ", ") + ")"}
		}
		out[e.key] = e.node
	}
	return out, nil
}

func sequence(n *yaml.Node, path string) ([]*yaml.Node, error) {
	n = deref(n)
	if isNull(n) {
		return nil, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, ferr(path, n, "must be a list")
	}
	out := make([]*yaml.Node, len(n.Content))
	for i, c := range n.Content {
		out[i] = deref(c)
	}
	return out, nil
}

func scalar(n *yaml.Node, path string) (string, error) {
	n = deref(n)
	if n == nil || n.Kind != yaml.ScalarNode || isNull(n) {
		return "", ferr(path, n, "must be a text value")
	}
	return n.Value, nil
}

func optScalar(n *yaml.Node, path string) (*string, error) {
	if isNull(deref(n)) {
		return nil, nil
	}
	s, err := scalar(n, path)
	return &s, err
}

func flag(n *yaml.Node, path string) (bool, error) {
	n = deref(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!bool" {
		return false, ferr(path, n, "must be true or false")
	}
	var b bool
	if err := n.Decode(&b); err != nil {
		return false, ferr(path, n, "must be true or false")
	}
	return b, nil
}

func uinteger(n *yaml.Node, path string) (uint64, error) {
	n = deref(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!int" {
		return 0, ferr(path, n, "must be a whole number")
	}
	v, err := strconv.ParseUint(n.Value, 10, 64)
	if err != nil {
		return 0, ferr(path, n, "must be a whole number")
	}
	return v, nil
}

// names reads a list of names; a single name is accepted as a list of one.
func names(n *yaml.Node, path, what string) ([]string, error) {
	n = deref(n)
	var items []*yaml.Node
	if n != nil && n.Kind == yaml.ScalarNode && !isNull(n) {
		items = []*yaml.Node{n}
	} else {
		var err error
		if items, err = sequence(n, path); err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(items))
	for i, c := range items {
		p := index(path, i)
		if len(items) == 1 && c == n {
			p = path
		}
		s, err := scalar(c, p)
		if err != nil {
			return nil, err
		}
		if err := ValidateName(what, s); err != nil {
			return nil, ferr(p, c, "%s", err.Error())
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *fileParser) file(root *yaml.Node) (*AccessFile, error) {
	root = deref(root)
	if root.Kind != yaml.MappingNode {
		return nil, ferr("", root, "the file must be a mapping with chui_access at the top")
	}
	f, err := fields(root, "", "chui_access", "source", "roles", "users", "grants", "role_grants", "row_policies", "quotas", "settings_profiles")
	if err != nil {
		return nil, err
	}
	v, ok := f["chui_access"]
	if !ok {
		return nil, &FileError{Path: "chui_access", Msg: "missing; this is not a CH-UI access file"}
	}
	ver, err := uinteger(v, "chui_access")
	if err != nil {
		return nil, err
	}
	switch {
	case ver > FileVersion:
		return nil, ferr("chui_access", v, "version %d is newer than this server reads (%d)", ver, FileVersion)
	case ver != FileVersion:
		return nil, ferr("chui_access", v, "version %d is not supported (expected %d)", ver, FileVersion)
	}
	out := &AccessFile{State: &State{Version: StateVersion}, Logins: map[string]UserLogin{}}
	if n, ok := f["source"]; ok {
		if out.Source, err = p.source(n); err != nil {
			return nil, err
		}
	}
	// Roles and users first: grants and role_grants resolve bare grantee
	// names against them.
	if err := p.rolesSection(f["roles"], out.State); err != nil {
		return nil, err
	}
	if err := p.usersSection(f["users"], out); err != nil {
		return nil, err
	}
	if err := p.grantsSection(f["grants"], out.State); err != nil {
		return nil, err
	}
	if err := p.roleGrantsSection(f["role_grants"], out.State); err != nil {
		return nil, err
	}
	if err := p.policiesSection(f["row_policies"], out.State); err != nil {
		return nil, err
	}
	if err := p.quotasSection(f["quotas"], out.State); err != nil {
		return nil, err
	}
	if err := p.profilesSection(f["settings_profiles"], out.State); err != nil {
		return nil, err
	}
	out.State = out.State.Canonical()
	return out, nil
}

func (p *fileParser) source(n *yaml.Node) (FileSource, error) {
	var src FileSource
	f, err := fields(n, "source", "connection", "host", "exported_at", "incomplete")
	if err != nil {
		return src, err
	}
	for key, dst := range map[string]*string{"connection": &src.Connection, "host": &src.Host, "exported_at": &src.ExportedAt} {
		if v, ok := f[key]; ok && !isNull(v) {
			if *dst, err = scalar(v, "source."+key); err != nil {
				return src, err
			}
		}
	}
	if v, ok := f["incomplete"]; ok {
		items, err := sequence(v, "source.incomplete")
		if err != nil {
			return src, err
		}
		for i, c := range items {
			s, err := scalar(c, index("source.incomplete", i))
			if err != nil {
				return src, err
			}
			src.Incomplete = append(src.Incomplete, s)
		}
	}
	return src, nil
}

func (p *fileParser) rolesSection(n *yaml.Node, st *State) error {
	list, err := mapping(n, "roles")
	if err != nil {
		return err
	}
	for _, e := range list {
		path := child("roles", e.key)
		if err := ValidateName("role", e.key); err != nil {
			return &FileError{Path: path, Line: e.line, Msg: err.Error()}
		}
		f, err := fields(e.node, path, "profile", "profiles")
		if err != nil {
			return err
		}
		r := StateRole{Name: e.key}
		if v, ok := f["profile"]; ok {
			if _, both := f["profiles"]; both {
				return ferr(path, e.node, "use profile or profiles, not both")
			}
			s, err := scalar(v, path+".profile")
			if err != nil {
				return err
			}
			if err := ValidateName("settings profile", s); err != nil {
				return ferr(path+".profile", v, "%s", err.Error())
			}
			r.Profiles = []string{s}
		}
		if v, ok := f["profiles"]; ok {
			if r.Profiles, err = names(v, path+".profiles", "settings profile"); err != nil {
				return err
			}
		}
		p.roles[e.key] = true
		st.Roles = append(st.Roles, r)
	}
	return nil
}

func (p *fileParser) usersSection(n *yaml.Node, out *AccessFile) error {
	st := out.State
	list, err := mapping(n, "users")
	if err != nil {
		return err
	}
	for _, e := range list {
		path := child("users", e.key)
		if err := ValidateName("user", e.key); err != nil {
			return &FileError{Path: path, Line: e.line, Msg: err.Error()}
		}
		f, err := fields(e.node, path, "default_roles", "except", "auth", "hosts")
		if err != nil {
			return err
		}
		var login UserLogin
		if v, ok := f["auth"]; ok && !isNull(v) {
			if login.Auth, err = scalar(v, path+".auth"); err != nil {
				return err
			}
			if !authTypRe.MatchString(login.Auth) {
				return ferr(path+".auth", v, "%q is not an authentication type", login.Auth)
			}
		}
		if v, ok := f["hosts"]; ok && !isNull(v) {
			items, err := sequence(v, path+".hosts")
			if err != nil {
				return err
			}
			for i, c := range items {
				hs, err := scalar(c, index(path+".hosts", i))
				if err != nil {
					return err
				}
				h, err := ParseHost(hs)
				if err != nil {
					return ferr(index(path+".hosts", i), c, "%s", err.Error())
				}
				login.Hosts = append(login.Hosts, h)
			}
			if err := ValidateHosts(login.Hosts); err != nil {
				return ferr(path+".hosts", v, "%s", err.Error())
			}
		}
		if login.Auth != "" || len(login.Hosts) > 0 {
			out.Logins[e.key] = login
		}
		u := StateUser{Name: e.key}
		v, ok := f["default_roles"]
		if !ok {
			return &FileError{Path: path + ".default_roles", Line: e.line, Msg: "missing; write all, none or a list of roles"}
		}
		if u.DefaultAll, u.DefaultRoles, u.DefaultExcept, err = allNoneOrList(v, f["except"], path, "default_roles", "role"); err != nil {
			return err
		}
		p.users[e.key] = true
		st.Users = append(st.Users, u)
	}
	return nil
}

// allNoneOrList reads `key: all` (with an optional except list), `key:
// none` or `key: [names]`.
func allNoneOrList(v, except *yaml.Node, path, key, what string) (all bool, list, exc []string, err error) {
	v = deref(v)
	kp := path + "." + key
	if v != nil && v.Kind == yaml.ScalarNode && !isNull(v) {
		switch v.Value {
		case "all":
			all = true
		case "none":
		default:
			return false, nil, nil, ferr(kp, v, "must be all, none or a list")
		}
	} else if list, err = names(v, kp, what); err != nil {
		return false, nil, nil, err
	}
	if except != nil {
		if !all {
			return false, nil, nil, ferr(path+".except", except, "except needs %s: all", key)
		}
		if exc, err = names(except, path+".except", what); err != nil {
			return false, nil, nil, err
		}
	}
	return all, list, exc, nil
}

// grantee resolves a key of grants or role_grants.
func (p *fileParser) grantee(key, path string, line int) (Target, error) {
	for _, typ := range []string{"role", "user"} {
		if name, ok := strings.CutPrefix(key, typ+":"); ok {
			if err := ValidateName(typ, name); err != nil {
				return Target{}, &FileError{Path: path, Line: line, Msg: err.Error()}
			}
			return Target{Type: typ, Name: name}, nil
		}
	}
	role, user := p.roles[key], p.users[key]
	switch {
	case role && user:
		return Target{}, &FileError{Path: path, Line: line, Msg: fmt.Sprintf("%q is both a role and a user here; write role:%s or user:%s", key, key, key)}
	case role:
		return Target{Type: "role", Name: key}, nil
	case user:
		return Target{Type: "user", Name: key}, nil
	}
	return Target{}, &FileError{Path: path, Line: line, Msg: fmt.Sprintf("%q is listed under neither roles nor users; add it to one, or write role:%s or user:%s", key, key, key)}
}

func (p *fileParser) grantsSection(n *yaml.Node, st *State) error {
	list, err := mapping(n, "grants")
	if err != nil {
		return err
	}
	for _, e := range list {
		path := child("grants", e.key)
		t, err := p.grantee(e.key, path, e.line)
		if err != nil {
			return err
		}
		items, err := sequence(e.node, path)
		if err != nil {
			return err
		}
		gg := GranteeGrants{Type: t.Type, Name: t.Name}
		for i, item := range items {
			ip := index(path, i)
			f, err := fields(item, ip, "access", "on", "columns", "revoke", "grant_option")
			if err != nil {
				return err
			}
			var g StateGrant
			v, ok := f["access"]
			if !ok {
				return ferr(ip, item, "access is required")
			}
			if g.Access, err = privilegeNames(v, ip+".access"); err != nil {
				return err
			}
			on, ok := f["on"]
			if !ok {
				return ferr(ip, item, `on is required ("*.*", "db.*" or "db.table")`)
			}
			s, err := scalar(on, ip+".on")
			if err != nil {
				return err
			}
			if g.Database, g.Table, err = parseOn(s); err != nil {
				return ferr(ip+".on", on, "%s", err.Error())
			}
			for key, dst := range map[string]*bool{"revoke": &g.Revoke, "grant_option": &g.GrantOption} {
				if v, ok := f[key]; ok {
					if *dst, err = flag(v, ip+"."+key); err != nil {
						return err
					}
				}
			}
			var cols []string
			if v, ok := f["columns"]; ok {
				if cols, err = names(v, ip+".columns", "column"); err != nil {
					return err
				}
				if len(cols) > 0 && g.Table == "" {
					return ferr(ip+".columns", v, "columns need a table in on")
				}
			}
			if len(cols) == 0 {
				gg.Privileges = append(gg.Privileges, g)
			}
			for _, c := range cols {
				cg := g
				cg.Access = cloneStrings(g.Access)
				cg.Column = c
				gg.Privileges = append(gg.Privileges, cg)
			}
		}
		st.Grants = append(st.Grants, gg)
	}
	return nil
}

func privilegeNames(n *yaml.Node, path string) ([]string, error) {
	n = deref(n)
	var items []*yaml.Node
	if n != nil && n.Kind == yaml.ScalarNode && !isNull(n) {
		items = []*yaml.Node{n}
	} else {
		var err error
		if items, err = sequence(n, path); err != nil {
			return nil, err
		}
	}
	if len(items) == 0 {
		return nil, ferr(path, n, "at least one privilege is required")
	}
	out := make([]string, 0, len(items))
	for i, c := range items {
		s, err := scalar(c, index(path, i))
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if err := ValidatePrivilege(s, nil); err != nil {
			return nil, ferr(index(path, i), c, "%s", err.Error())
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *fileParser) roleGrantsSection(n *yaml.Node, st *State) error {
	list, err := mapping(n, "role_grants")
	if err != nil {
		return err
	}
	for _, e := range list {
		path := child("role_grants", e.key)
		t, err := p.grantee(e.key, path, e.line)
		if err != nil {
			return err
		}
		v := deref(e.node)
		var items []*yaml.Node
		if v != nil && v.Kind == yaml.ScalarNode && !isNull(v) {
			items = []*yaml.Node{v}
		} else if items, err = sequence(v, path); err != nil {
			return err
		}
		for i, item := range items {
			ip := index(path, i)
			g := StateRoleGrant{Type: t.Type, Name: t.Name}
			if item.Kind == yaml.MappingNode {
				f, err := fields(item, ip, "role", "admin_option")
				if err != nil {
					return err
				}
				r, ok := f["role"]
				if !ok {
					return ferr(ip, item, "role is required")
				}
				if g.Role, err = scalar(r, ip+".role"); err != nil {
					return err
				}
				if a, ok := f["admin_option"]; ok {
					if g.AdminOption, err = flag(a, ip+".admin_option"); err != nil {
						return err
					}
				}
			} else if g.Role, err = scalar(item, ip); err != nil {
				return ferr(ip, item, "must be a role name or {role: name, admin_option: true}")
			}
			if err := ValidateName("role", g.Role); err != nil {
				return ferr(ip, item, "%s", err.Error())
			}
			st.RoleGrants = append(st.RoleGrants, g)
		}
	}
	return nil
}

// assignees reads to (all, none or a list) and except from an entry.
func assignees(f map[string]*yaml.Node, path string) (Assignees, error) {
	v, ok := f["to"]
	if !ok {
		if e, has := f["except"]; has {
			return Assignees{}, ferr(path+".except", e, "except needs to: all")
		}
		return Assignees{}, nil
	}
	all, list, except, err := allNoneOrList(v, f["except"], path, "to", "assignee")
	return Assignees{All: all, Names: list, Except: except}, err
}

func (p *fileParser) policiesSection(n *yaml.Node, st *State) error {
	items, err := sequence(n, "row_policies")
	if err != nil {
		return err
	}
	for i, item := range items {
		ip := index("row_policies", i)
		f, err := fields(item, ip, "name", "on", "using", "restrictive", "to", "except")
		if err != nil {
			return err
		}
		var rp RowPolicySpec
		for _, key := range []string{"name", "on", "using"} {
			if _, ok := f[key]; !ok {
				return ferr(ip, item, "%s is required", key)
			}
		}
		if rp.Name, err = scalar(f["name"], ip+".name"); err != nil {
			return err
		}
		if err := ValidateName("row policy", rp.Name); err != nil {
			return ferr(ip+".name", f["name"], "%s", err.Error())
		}
		on, err := scalar(f["on"], ip+".on")
		if err != nil {
			return err
		}
		if rp.Database, rp.Table, err = parseOn(on); err != nil {
			return ferr(ip+".on", f["on"], "%s", err.Error())
		}
		if rp.Table == "" {
			return ferr(ip+".on", f["on"], "a row policy is on one table: db.table")
		}
		if rp.Using, err = scalar(f["using"], ip+".using"); err != nil {
			return err
		}
		if v, ok := f["restrictive"]; ok {
			if rp.Restrictive, err = flag(v, ip+".restrictive"); err != nil {
				return err
			}
		}
		if rp.To, err = assignees(f, ip); err != nil {
			return err
		}
		st.RowPolicies = append(st.RowPolicies, rp)
	}
	return nil
}

func (p *fileParser) quotasSection(n *yaml.Node, st *State) error {
	list, err := mapping(n, "quotas")
	if err != nil {
		return err
	}
	for _, e := range list {
		path := child("quotas", e.key)
		if err := ValidateName("quota", e.key); err != nil {
			return &FileError{Path: path, Line: e.line, Msg: err.Error()}
		}
		f, err := fields(e.node, path, "keyed_by", "intervals", "to", "except")
		if err != nil {
			return err
		}
		q := QuotaSpec{Name: e.key}
		if v, ok := f["keyed_by"]; ok {
			if q.KeyedBy, err = scalar(v, path+".keyed_by"); err != nil {
				return err
			}
			if !contains(QuotaKeys, q.KeyedBy) {
				return ferr(path+".keyed_by", v, "%q is not supported (expected one of %s)", q.KeyedBy, strings.Join(QuotaKeys[1:], ", "))
			}
		}
		ivs, err := sequence(f["intervals"], path+".intervals")
		if err != nil {
			return err
		}
		for i, item := range ivs {
			ip := index(path+".intervals", i)
			iv, err := quotaInterval(item, ip)
			if err != nil {
				return err
			}
			q.Intervals = append(q.Intervals, iv)
		}
		if q.To, err = assignees(f, path); err != nil {
			return err
		}
		st.Quotas = append(st.Quotas, q)
	}
	return nil
}

func quotaInterval(item *yaml.Node, ip string) (QuotaInterval, error) {
	var iv QuotaInterval
	f, err := fields(item, ip, "every", "randomized", "limits")
	if err != nil {
		return iv, err
	}
	every, ok := f["every"]
	if !ok {
		return iv, ferr(ip, item, `every is required, like "1 hour"`)
	}
	s, err := scalar(every, ip+".every")
	if err != nil {
		return iv, err
	}
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return iv, ferr(ip+".every", every, `%q must be a number and a unit, like "1 hour"`, s)
	}
	if iv.Duration, err = strconv.ParseUint(parts[0], 10, 64); err != nil || iv.Duration == 0 {
		return iv, ferr(ip+".every", every, "%q must start with a whole number above 0", s)
	}
	iv.Unit = strings.TrimSuffix(strings.ToLower(parts[1]), "s")
	if !contains(QuotaUnits, iv.Unit) {
		return iv, ferr(ip+".every", every, "unit %q is not supported (expected one of %s)", parts[1], strings.Join(QuotaUnits, ", "))
	}
	if v, ok := f["randomized"]; ok {
		if iv.Randomized, err = flag(v, ip+".randomized"); err != nil {
			return iv, err
		}
	}
	limits, err := mapping(f["limits"], ip+".limits")
	if err != nil {
		return iv, err
	}
	for _, l := range limits {
		lp := child(ip+".limits", l.key)
		if !contains(QuotaResources, l.key) {
			return iv, &FileError{Path: lp, Line: l.line, Msg: "unknown quota limit (expected one of " + strings.Join(QuotaResources, ", ") + ")"}
		}
		v, err := uinteger(l.node, lp)
		if err != nil {
			return iv, err
		}
		if iv.Limits == nil {
			iv.Limits = map[string]uint64{}
		}
		iv.Limits[l.key] = v
	}
	return iv, nil
}

func (p *fileParser) profilesSection(n *yaml.Node, st *State) error {
	list, err := mapping(n, "settings_profiles")
	if err != nil {
		return err
	}
	for _, e := range list {
		path := child("settings_profiles", e.key)
		if err := ValidateName("settings profile", e.key); err != nil {
			return &FileError{Path: path, Line: e.line, Msg: err.Error()}
		}
		f, err := fields(e.node, path, "inherit", "settings", "to", "except")
		if err != nil {
			return err
		}
		sp := ProfileSpec{Name: e.key}
		if v, ok := f["inherit"]; ok {
			if sp.Inherit, err = names(v, path+".inherit", "settings profile"); err != nil {
				return err
			}
		}
		items, err := sequence(f["settings"], path+".settings")
		if err != nil {
			return err
		}
		for i, item := range items {
			ip := index(path+".settings", i)
			sf, err := fields(item, ip, "name", "value", "min", "max", "writability")
			if err != nil {
				return err
			}
			var s ProfileSetting
			nv, ok := sf["name"]
			if !ok {
				return ferr(ip, item, "name is required")
			}
			if s.Name, err = scalar(nv, ip+".name"); err != nil {
				return err
			}
			if !settingRe.MatchString(s.Name) {
				return ferr(ip+".name", nv, "%q is not a setting name", s.Name)
			}
			for key, dst := range map[string]**string{"value": &s.Value, "min": &s.Min, "max": &s.Max} {
				if v, ok := sf[key]; ok {
					if *dst, err = optScalar(v, ip+"."+key); err != nil {
						return err
					}
				}
			}
			if v, ok := sf["writability"]; ok {
				if s.Writability, err = scalar(v, ip+".writability"); err != nil {
					return err
				}
				s.Writability = strings.ToUpper(s.Writability)
				if !contains(Writabilities, s.Writability) {
					return ferr(ip+".writability", v, "must be one of %s", strings.Join(Writabilities[1:], ", "))
				}
			}
			sp.Settings = append(sp.Settings, s)
		}
		if sp.To, err = assignees(f, path); err != nil {
			return err
		}
		st.SettingsProfiles = append(st.SettingsProfiles, sp)
	}
	return nil
}
