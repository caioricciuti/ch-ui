package lineage

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Node kinds.
const (
	KindTable         = "table"
	KindMV            = "mv"
	KindRefreshableMV = "refreshable_mv"
	KindView          = "view"
	KindDictionary    = "dictionary"
	KindDistributed   = "distributed"
	KindBuffer        = "buffer"
	KindMerge         = "merge"
	KindExternal      = "external"
	KindHidden        = "hidden"
	KindMissing       = "missing"
)

// Edge kinds and confidences.
const (
	EdgeTriggers = "triggers"
	EdgeWrites   = "writes"
	EdgeReads    = "reads"
	EdgeRoutes   = "routes"
	EdgeSources  = "sources"

	ConfidenceSystem = "system"
	ConfidenceParsed = "parsed"
)

// ExternalPrefix starts the id of an external source found in a table
// function or dictionary source; such nodes have no database.
const ExternalPrefix = "external:"

// Count is a ClickHouse UInt64 that may be NULL. It reads numbers and quoted
// numbers (output_format_json_quote_64bit_integers) and writes a number or null.
type Count struct {
	V     uint64
	Valid bool
}

func (c *Count) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" {
		*c = Count{}
		return nil
	}
	v, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		// Floats from an unusual setting: keep what fits.
		f, ferr := strconv.ParseFloat(string(b), 64)
		if ferr != nil || f < 0 {
			return err
		}
		v = uint64(f)
	}
	*c = Count{V: v, Valid: true}
	return nil
}

func (c Count) MarshalJSON() ([]byte, error) {
	if !c.Valid {
		return []byte("null"), nil
	}
	return strconv.AppendUint(nil, c.V, 10), nil
}

// TableRow is one row of system.tables as the lineage query selects it.
type TableRow struct {
	Database             string   `json:"database"`
	Name                 string   `json:"name"`
	UUID                 string   `json:"uuid"`
	Engine               string   `json:"engine"`
	EngineFull           string   `json:"engine_full"`
	DDL                  string   `json:"ddl"`
	AsSelect             string   `json:"as_select"`
	DependenciesDatabase []string `json:"dependencies_database"`
	DependenciesTable    []string `json:"dependencies_table"`
	TotalRows            Count    `json:"total_rows"`
	TotalBytes           Count    `json:"total_bytes"`
}

// DictionaryRow is one row of system.dictionaries.
type DictionaryRow struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Status   string `json:"status"`
}

// RefreshRow is one row of system.view_refreshes; times are Unix seconds.
type RefreshRow struct {
	Database    string `json:"database"`
	View        string `json:"view"`
	Status      string `json:"status"`
	LastSuccess Count  `json:"last_success"`
	Next        Count  `json:"next"`
}

// Refresh is a refreshable MV's schedule state; times are RFC 3339 or "".
type Refresh struct {
	Status      string `json:"status"`
	LastSuccess string `json:"last_success"`
	Next        string `json:"next"`
}

type Node struct {
	ID       string   `json:"id"`
	Database string   `json:"database"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Engine   string   `json:"engine"`
	Rows     Count    `json:"rows"`
	Bytes    Count    `json:"bytes"`
	Refresh  *Refresh `json:"refresh"`
	External string   `json:"external,omitempty"`
}

type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
}

type Meta struct {
	Host        string   `json:"host"`
	MultiNode   bool     `json:"multi_node"`
	GeneratedAt string   `json:"generated_at"`
	Cached      bool     `json:"cached"`
	NodeCount   int      `json:"node_count"`
	Warnings    []string `json:"warnings"`
}

type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	Meta  Meta   `json:"meta"`
}

// Input is everything Build reads. Databases limits the graph to objects in
// those databases plus their direct neighbours; empty means every row.
// Definitions (DDL, as_select) of views, MVs and dictionaries are needed in
// every database: a reader elsewhere is a neighbour of a selected table.
type Input struct {
	Tables       []TableRow
	Dictionaries []DictionaryRow
	Refreshes    []RefreshRow
	Databases    []string
}

// object is a row of system.tables indexed for the build.
type object struct {
	row  *TableRow
	key  string
	kind string
	// inner is the MV's inner table folded into it, or nil.
	inner *TableRow
	def   definition
}

type builder struct {
	objects  map[string]*object // db.name to object, inner tables excluded
	folded   map[string]string  // inner table key to its MV key
	filter   map[string]bool    // nil: every database
	nodes    map[string]*Node
	edges    []Edge
	edgeAt   map[[2]string]int
	refresh  map[string]*Refresh
	dictSrc  map[string]string
	byDBName map[string][]*object // database to its objects, for Merge
}

// Build turns system tables rows into the lineage graph. It is linear in the
// number of rows plus the size of the parsed definitions, apart from sorting
// the output and Merge tables, which match their pattern against the
// objects of the databases they name.
func Build(in Input) Graph {
	b := &builder{
		objects: make(map[string]*object, len(in.Tables)),
		folded:  map[string]string{},
		nodes:   make(map[string]*Node, len(in.Tables)),
		edgeAt:  map[[2]string]int{},
		refresh: make(map[string]*Refresh, len(in.Refreshes)),
		dictSrc: make(map[string]string, len(in.Dictionaries)),
	}
	if len(in.Databases) > 0 {
		b.filter = make(map[string]bool, len(in.Databases))
		for _, db := range in.Databases {
			b.filter[db] = true
		}
	}
	for _, r := range in.Refreshes {
		b.refresh[r.Database+"."+r.View] = &Refresh{Status: r.Status, LastSuccess: unixTime(r.LastSuccess), Next: unixTime(r.Next)}
	}
	for _, d := range in.Dictionaries {
		b.dictSrc[d.Database+"."+d.Name] = d.Source
	}
	b.index(in.Tables)

	// Edges from system metadata, over every row: a neighbour outside the
	// selected databases still shows when it touches one inside.
	for _, o := range b.sortedObjects() {
		b.systemEdges(o)
	}
	// Edges parsed from definitions, over every row with one; edge keeps
	// only those touching a selected database.
	for _, o := range b.sortedObjects() {
		if b.selected(o.row.Database) {
			b.node(o.key)
		}
		b.parsedEdges(o)
	}

	g := Graph{Nodes: make([]Node, 0, len(b.nodes)), Edges: b.edges}
	for _, n := range b.nodes {
		g.Nodes = append(g.Nodes, *n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	if g.Edges == nil {
		g.Edges = []Edge{}
	}
	g.Meta.NodeCount = len(g.Nodes)
	g.Meta.Warnings = []string{}
	return g
}

func unixTime(c Count) string {
	if !c.Valid || c.V == 0 {
		return ""
	}
	return time.Unix(int64(c.V), 0).UTC().Format(time.RFC3339)
}

func (b *builder) selected(db string) bool { return b.filter == nil || b.filter[db] }

func isInnerName(name string) bool {
	return strings.HasPrefix(name, ".inner_id.") || strings.HasPrefix(name, ".inner.")
}

func (b *builder) index(rows []TableRow) {
	inner := map[string]*TableRow{}
	for i := range rows {
		r := &rows[i]
		key := r.Database + "." + r.Name
		if isInnerName(r.Name) {
			inner[key] = r
			continue
		}
		b.objects[key] = &object{row: r, key: key}
	}
	for _, o := range b.objects {
		o.kind = b.classify(o)
		if o.kind != KindMV && o.kind != KindRefreshableMV {
			continue
		}
		if o.def.to != nil {
			continue
		}
		// Atomic databases name the inner table after the MV's UUID,
		// Ordinary ones after its name.
		keys := []string{o.row.Database + "..inner." + o.row.Name}
		if o.row.UUID != "" && o.row.UUID != zeroUUID {
			keys = append([]string{o.row.Database + "..inner_id." + o.row.UUID}, keys...)
		}
		for _, k := range keys {
			if r, ok := inner[k]; ok {
				o.inner = r
				b.folded[k] = o.key
				break
			}
		}
	}
}

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// Classify returns the node kind for a system.tables row. refreshable says
// whether system.view_refreshes lists it.
func Classify(r TableRow, refreshable bool) string {
	o := &object{row: &r}
	b := &builder{}
	if refreshable {
		b.refresh = map[string]*Refresh{r.Database + "." + r.Name: {}}
	}
	return b.classify(o)
}

func (b *builder) classify(o *object) string {
	switch o.row.Engine {
	case "View":
		return KindView
	case "MaterializedView":
		if o.row.DDL != "" {
			o.def = parseDefinition(o.row.DDL, o.row.AsSelect == "")
		}
		if o.def.refreshable || b.refresh[o.row.Database+"."+o.row.Name] != nil {
			return KindRefreshableMV
		}
		return KindMV
	case "Dictionary":
		return KindDictionary
	case "Distributed":
		return KindDistributed
	case "Buffer":
		return KindBuffer
	case "Merge":
		return KindMerge
	}
	if o.row.EngineFull != "" || isExternalEngineName(o.row.Engine) {
		if externalEngine(o.row.Engine, parseEngineFull(o.row.EngineFull)) != "" {
			return KindExternal
		}
	}
	return KindTable
}

func isExternalEngineName(engine string) bool {
	e := strings.ToLower(engine)
	return externalEngines[e] || e == "kafka" || e == "rabbitmq" || e == "nats" ||
		strings.HasPrefix(e, "iceberg") || strings.HasPrefix(e, "deltalake") || strings.HasPrefix(e, "hudi")
}

func (b *builder) sortedObjects() []*object {
	if b.byDBName == nil {
		b.byDBName = map[string][]*object{}
		for _, o := range b.objects {
			b.byDBName[o.row.Database] = append(b.byDBName[o.row.Database], o)
		}
		for _, list := range b.byDBName {
			sort.Slice(list, func(i, j int) bool { return list[i].row.Name < list[j].row.Name })
		}
	}
	dbs := make([]string, 0, len(b.byDBName))
	for db := range b.byDBName {
		dbs = append(dbs, db)
	}
	sort.Strings(dbs)
	out := make([]*object, 0, len(b.objects))
	for _, db := range dbs {
		out = append(out, b.byDBName[db]...)
	}
	return out
}

// resolve maps an inner table to the MV it belongs to.
func (b *builder) resolve(key string) string {
	if mv, ok := b.folded[key]; ok {
		return mv
	}
	return key
}

// node returns the node for key, creating it from its row, or as absent
// (hidden or missing) when no visible row has that name.
func (b *builder) nodeAs(key, absent string) *Node {
	key = b.resolve(key)
	if n, ok := b.nodes[key]; ok {
		return n
	}
	n := &Node{ID: key}
	if o, ok := b.objects[key]; ok {
		n.Database, n.Name, n.Kind, n.Engine = o.row.Database, o.row.Name, o.kind, o.row.Engine
		n.Rows, n.Bytes = o.row.TotalRows, o.row.TotalBytes
		if o.inner != nil {
			n.Rows, n.Bytes = o.inner.TotalRows, o.inner.TotalBytes
		}
		if o.kind == KindRefreshableMV {
			n.Refresh = b.refresh[key]
			if n.Refresh == nil {
				n.Refresh = &Refresh{}
			}
		}
		if o.kind == KindExternal {
			n.External = externalEngine(o.row.Engine, parseEngineFull(o.row.EngineFull))
		}
	} else if strings.HasPrefix(key, ExternalPrefix) {
		n.Name, n.Kind, n.External = strings.TrimPrefix(key, ExternalPrefix), KindExternal, strings.TrimPrefix(key, ExternalPrefix)
	} else {
		n.Database, n.Name = splitQualified(key)
		n.Kind = absent
	}
	b.nodes[key] = n
	return n
}

func (b *builder) node(key string) *Node { return b.nodeAs(key, KindMissing) }

// edge adds from -kind-> to once per (from, to) pair; a system edge replaces
// a parsed one for the same pair. Only edges touching a selected database
// are kept, and only kept edges create nodes.
func (b *builder) edge(from, to, kind, confidence, absentFrom, absentTo string) {
	from, to = b.resolve(from), b.resolve(to)
	if from == to || isInnerKey(from) || isInnerKey(to) {
		// An inner table without a visible MV is never a node of its own.
		return
	}
	if !b.selected(dbOf(from)) && !b.selected(dbOf(to)) {
		return
	}
	pair := [2]string{from, to}
	if i, ok := b.edgeAt[pair]; ok {
		if b.edges[i].Confidence == ConfidenceParsed && confidence == ConfidenceSystem {
			b.edges[i].Kind, b.edges[i].Confidence = kind, confidence
		}
		return
	}
	b.nodeAs(from, absentFrom)
	b.nodeAs(to, absentTo)
	b.edgeAt[pair] = len(b.edges)
	b.edges = append(b.edges, Edge{From: from, To: to, Kind: kind, Confidence: confidence})
}

func isInnerKey(key string) bool {
	_, name := splitQualified(key)
	return isInnerName(name)
}

func dbOf(key string) string {
	if strings.HasPrefix(key, ExternalPrefix) {
		return "" // never selected on its own: kept through its other end
	}
	db, _ := splitQualified(key)
	return db
}

// systemEdges adds the edges system metadata states: MV triggers from
// dependencies_*, and Distributed, Buffer and Merge routing from engine_full.
func (b *builder) systemEdges(o *object) {
	for i, db := range o.row.DependenciesDatabase {
		if i < len(o.row.DependenciesTable) {
			b.edge(o.key, db+"."+o.row.DependenciesTable[i], EdgeTriggers, ConfidenceSystem, KindMissing, KindHidden)
		}
	}
	if o.row.EngineFull == "" {
		return
	}
	own := o.row.Database
	switch o.kind {
	case KindDistributed:
		spec := parseEngineFull(o.row.EngineFull)
		if db, t := spec.arg(1, own), spec.arg(2, own); t != "" {
			b.edge(o.key, orDefault(db, own)+"."+t, EdgeRoutes, ConfidenceSystem, KindMissing, KindMissing)
		}
	case KindBuffer:
		spec := parseEngineFull(o.row.EngineFull)
		if db, t := spec.arg(0, own), spec.arg(1, own); t != "" {
			b.edge(o.key, orDefault(db, own)+"."+t, EdgeRoutes, ConfidenceSystem, KindMissing, KindMissing)
		}
	case KindMerge:
		b.mergeEdges(o, parseEngineFull(o.row.EngineFull))
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// mergeEdges routes each table a Merge table reads into it. Merge is read
// only, so data flows from its member tables to it.
func (b *builder) mergeEdges(o *object, spec engineSpec) {
	tablePattern := spec.arg(1, "")
	tableRe, err := regexp.Compile(tablePattern)
	if err != nil || tablePattern == "" {
		return
	}
	var dbs []string
	if fn, pattern := spec.argCall(0); fn == "regexp" {
		dbRe, err := regexp.Compile(pattern)
		if err != nil {
			return
		}
		for db := range b.byDBName {
			if dbRe.MatchString(db) {
				dbs = append(dbs, db)
			}
		}
		sort.Strings(dbs)
	} else if db := spec.arg(0, o.row.Database); db != "" {
		dbs = []string{db}
	}
	for _, db := range dbs {
		for _, m := range b.byDBName[db] {
			if m != o && tableRe.MatchString(m.row.Name) {
				b.edge(m.key, o.key, EdgeRoutes, ConfidenceSystem, KindMissing, KindMissing)
			}
		}
	}
}

// parsedEdges adds the edges read from an object's definition.
func (b *builder) parsedEdges(o *object) {
	own := o.row.Database
	switch o.kind {
	case KindView, KindMV, KindRefreshableMV:
		if o.kind != KindView && o.row.DDL != "" {
			if o.def.to != nil {
				b.edge(o.key, refKey(*o.def.to, own), EdgeWrites, ConfidenceParsed, KindMissing, KindMissing)
			}
			for _, r := range o.def.dependsOn {
				b.edge(refKey(r, own), o.key, EdgeReads, ConfidenceParsed, KindMissing, KindMissing)
			}
		}
		var refs []ref
		switch {
		case o.row.AsSelect != "":
			refs = selectRefs(o.row.AsSelect)
		case o.def.body != nil:
			refs = refsFromTokens(o.def.body)
		case o.row.DDL != "":
			refs = refsFromTokens(parseDefinition(o.row.DDL, true).body)
		}
		for _, r := range refs {
			b.edge(refKey(r, own), o.key, EdgeReads, ConfidenceParsed, KindMissing, KindMissing)
		}
	case KindDictionary:
		b.dictionaryEdges(o)
	}
}

func refKey(r ref, own string) string {
	if r.kind == refExternal {
		return ExternalPrefix + r.external
	}
	return orDefault(r.db, own) + "." + r.name
}

func (b *builder) dictionaryEdges(o *object) {
	own := o.row.Database
	if o.row.DDL != "" {
		if src, ok := parseDictionarySource(o.row.DDL); ok {
			for _, from := range dictionarySourceKeys(src, own) {
				b.edge(from, o.key, EdgeSources, ConfidenceParsed, KindMissing, KindMissing)
			}
			return
		}
	}
	// No definition: system.dictionaries describes a ClickHouse source as
	// "ClickHouse: db.table".
	source := b.dictSrc[o.key]
	if rest, ok := strings.CutPrefix(source, "ClickHouse: "); ok {
		if db, t := splitQualified(strings.TrimSpace(rest)); t != "" {
			b.edge(orDefault(db, own)+"."+t, o.key, EdgeSources, ConfidenceSystem, KindMissing, KindMissing)
		}
	} else if kind, _, ok := strings.Cut(source, ":"); ok && kind != "" {
		b.edge(ExternalPrefix+strings.ToLower(strings.TrimSpace(kind)), o.key, EdgeSources, ConfidenceSystem, KindMissing, KindMissing)
	}
}

// dictionarySourceKeys returns the node keys a dictionary source reads.
func dictionarySourceKeys(src dictionarySource, own string) []string {
	k := src.keys
	if name := k["NAME"]; name != "" && k["TABLE"] == "" && k["QUERY"] == "" {
		return []string{ExternalPrefix + src.kind + ":" + name}
	}
	switch src.kind {
	case "clickhouse":
		host := strings.ToLower(k["HOST"])
		local := host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
		db := orDefault(k["DB"], own)
		if !local {
			return []string{ExternalPrefix + joinDescriptor("clickhouse", sanitizeLocation(k["HOST"]), qualified(k["DB"], k["TABLE"]))}
		}
		if k["QUERY"] != "" {
			var out []string
			for _, r := range selectRefs(k["QUERY"]) {
				out = append(out, refKey(r, db))
			}
			return out
		}
		if k["TABLE"] != "" {
			return []string{db + "." + k["TABLE"]}
		}
		return nil
	case "mysql", "postgresql", "mongodb", "odbc", "jdbc", "cassandra", "ytsaurus":
		obj := k["TABLE"]
		if obj == "" {
			obj = k["COLLECTION"]
		}
		return []string{ExternalPrefix + joinDescriptor(src.kind, sanitizeLocation(orDefault(k["HOST"], k["CONNECTION_STRING"])), qualified(k["DB"], obj))}
	case "http":
		return []string{ExternalPrefix + orDefault(sanitizeLocation(k["URL"]), "http")}
	case "file":
		return []string{ExternalPrefix + joinDescriptor("file", k["PATH"])}
	case "executable", "executable_pool":
		cmd, _, _ := strings.Cut(k["COMMAND"], " ")
		return []string{ExternalPrefix + joinDescriptor("executable", cmd)}
	case "redis":
		return []string{ExternalPrefix + joinDescriptor("redis", k["HOST"])}
	case "null":
		return nil
	}
	return []string{ExternalPrefix + src.kind}
}
