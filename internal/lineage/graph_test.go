package lineage

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func edgeStrings(g Graph) []string {
	out := make([]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		out = append(out, e.From+" -"+e.Kind+"/"+e.Confidence+"-> "+e.To)
	}
	sort.Strings(out)
	return out
}

func nodeKinds(g Graph) map[string]string {
	out := map[string]string{}
	for _, n := range g.Nodes {
		out[n.ID] = n.Kind
	}
	return out
}

func nodeByID(t *testing.T, g Graph, id string) Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %s", id)
	return Node{}
}

func count(v uint64) Count { return Count{V: v, Valid: true} }

func mergeTree(db, name string, deps ...string) TableRow {
	r := TableRow{Database: db, Name: name, Engine: "MergeTree", EngineFull: "MergeTree ORDER BY id", TotalRows: count(1), TotalBytes: count(10)}
	for _, d := range deps {
		ddb, dt := splitQualified(d)
		r.DependenciesDatabase = append(r.DependenciesDatabase, ddb)
		r.DependenciesTable = append(r.DependenciesTable, dt)
	}
	return r
}

func mv(db, name, ddl, asSelect string) TableRow {
	return TableRow{Database: db, Name: name, Engine: "MaterializedView", DDL: ddl, AsSelect: asSelect}
}

func scenario() Input {
	innerMV := mv("app", "mv_inner", "CREATE MATERIALIZED VIEW app.mv_inner (`id` UInt64) ENGINE = MergeTree ORDER BY id AS SELECT id FROM app.events JOIN app.users USING id", "SELECT id FROM app.events INNER JOIN app.users USING (id)")
	innerMV.UUID = "aaaaaaaa-0000-0000-0000-000000000001"
	legacy := mv("legacy", "m", "CREATE MATERIALIZED VIEW legacy.m (`id` UInt64) ENGINE = MergeTree ORDER BY id AS SELECT id FROM legacy.src", "SELECT id FROM legacy.src")
	legacy.UUID = zeroUUID
	return Input{
		Tables: []TableRow{
			mergeTree("app", "events", "app.mv_inner", "app.mv_to", "other.hidden_mv", "app.mv_cycle"),
			mergeTree("app", "daily"),
			mergeTree("app", "users"),
			mv("app", "mv_to", "CREATE MATERIALIZED VIEW app.mv_to TO app.daily (`id` UInt64) AS SELECT id FROM app.events", "SELECT id FROM app.events"),
			innerMV,
			{Database: "app", Name: ".inner_id.aaaaaaaa-0000-0000-0000-000000000001", Engine: "MergeTree", TotalRows: count(5), TotalBytes: count(50)},
			{Database: "app", Name: "v_report", Engine: "View", DDL: "CREATE VIEW app.v_report AS SELECT * FROM app.daily", AsSelect: "SELECT * FROM app.daily JOIN app.gone USING id WHERE dictGet('app.dict', 'x', id) > 0"},
			{Database: "app", Name: "v_ext", Engine: "View", AsSelect: "SELECT * FROM s3('https://b/x.parquet')"},
			{Database: "app", Name: "dict", Engine: "Dictionary", DDL: "CREATE DICTIONARY app.dict (`id` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(TABLE 'users')) LIFETIME(0) LAYOUT(FLAT())"},
			{Database: "app", Name: "buf", Engine: "Buffer", EngineFull: "Buffer('app', 'daily', 1, 10, 100, 10000, 1000000, 10000000, 100000000)"},
			{Database: "app", Name: "dist", Engine: "Distributed", EngineFull: "Distributed('c', 'app', 'events_local', rand())"},
			{Database: "app", Name: "mrg", Engine: "Merge", EngineFull: "Merge('app', '^ev')"},
			{Database: "app", Name: "kafka_in", Engine: "Kafka", EngineFull: "Kafka SETTINGS kafka_broker_list = 'k:9092', kafka_topic_list = 'ev'", DependenciesDatabase: []string{"app"}, DependenciesTable: []string{"mv_kafka"}},
			mv("app", "mv_kafka", "CREATE MATERIALIZED VIEW app.mv_kafka TO app.events AS SELECT * FROM app.kafka_in", "SELECT * FROM app.kafka_in"),
			mv("app", "mv_cycle", "CREATE MATERIALIZED VIEW app.mv_cycle TO app.events AS SELECT * FROM app.events", "SELECT * FROM app.events"),
			mv("app", "rmv", "CREATE MATERIALIZED VIEW app.rmv REFRESH EVERY 1 HOUR DEPENDS ON app.mv_to TO app.daily (`id` UInt64) AS SELECT id FROM app.users", "SELECT id FROM app.users"),
			mv("app", "mv_dangling", "CREATE MATERIALIZED VIEW app.mv_dangling TO app.nowhere AS SELECT id FROM app.events", "SELECT id FROM app.events"),
			mergeTree("legacy", "src", "legacy.m"),
			legacy,
			{Database: "legacy", Name: ".inner.m", Engine: "MergeTree", TotalRows: count(7), TotalBytes: count(70)},
		},
		Refreshes: []RefreshRow{{Database: "app", View: "rmv", Status: "Scheduled", LastSuccess: count(1700000000), Next: count(0)}},
	}
}

func TestBuildGraph(t *testing.T) {
	g := Build(scenario())
	want := []string{
		"app.buf -routes/system-> app.daily",
		"app.daily -reads/parsed-> app.v_report",
		"app.dict -reads/parsed-> app.v_report",
		"app.dist -routes/system-> app.events_local",
		"app.events -reads/parsed-> app.mv_dangling",
		"app.events -routes/system-> app.mrg",
		"app.events -triggers/system-> app.mv_cycle",
		"app.events -triggers/system-> app.mv_inner",
		"app.events -triggers/system-> app.mv_to",
		"app.events -triggers/system-> other.hidden_mv",
		"app.gone -reads/parsed-> app.v_report",
		"app.kafka_in -triggers/system-> app.mv_kafka",
		"app.mv_cycle -writes/parsed-> app.events",
		"app.mv_dangling -writes/parsed-> app.nowhere",
		"app.mv_kafka -writes/parsed-> app.events",
		"app.mv_to -reads/parsed-> app.rmv",
		"app.mv_to -writes/parsed-> app.daily",
		"app.rmv -writes/parsed-> app.daily",
		"app.users -reads/parsed-> app.mv_inner",
		"app.users -reads/parsed-> app.rmv",
		"app.users -sources/parsed-> app.dict",
		"external:https://b/x.parquet -reads/parsed-> app.v_ext",
		"legacy.src -triggers/system-> legacy.m",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
	wantKinds := map[string]string{
		"app.events": KindTable, "app.daily": KindTable, "app.users": KindTable,
		"app.mv_to": KindMV, "app.mv_inner": KindMV, "app.mv_kafka": KindMV, "app.mv_cycle": KindMV, "app.mv_dangling": KindMV,
		"app.rmv": KindRefreshableMV, "app.v_report": KindView, "app.v_ext": KindView, "app.dict": KindDictionary,
		"app.buf": KindBuffer, "app.dist": KindDistributed, "app.mrg": KindMerge, "app.kafka_in": KindExternal,
		"app.events_local": KindMissing, "app.gone": KindMissing, "app.nowhere": KindMissing,
		"other.hidden_mv":              KindHidden,
		"external:https://b/x.parquet": KindExternal,
		"legacy.src":                   KindTable, "legacy.m": KindMV,
	}
	if got := nodeKinds(g); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("kinds\n got %v\nwant %v", got, wantKinds)
	}
	if g.Meta.NodeCount != len(wantKinds) || g.Meta.Warnings == nil {
		t.Fatalf("meta: %+v", g.Meta)
	}
	for _, n := range g.Nodes {
		if strings.Contains(n.Name, ".inner") {
			t.Fatalf("inner table is a node: %s", n.ID)
		}
	}
	if n := nodeByID(t, g, "app.mv_inner"); n.Rows != count(5) || n.Bytes != count(50) {
		t.Fatalf("inner rows not folded: %+v", n)
	}
	if n := nodeByID(t, g, "legacy.m"); n.Rows != count(7) {
		t.Fatalf("ordinary inner rows not folded: %+v", n)
	}
	if n := nodeByID(t, g, "app.rmv"); n.Refresh == nil || n.Refresh.Status != "Scheduled" || n.Refresh.LastSuccess != "2023-11-14T22:13:20Z" || n.Refresh.Next != "" {
		t.Fatalf("refresh: %+v", n.Refresh)
	}
	if n := nodeByID(t, g, "app.kafka_in"); n.External != "kafka://k:9092/ev" {
		t.Fatalf("kafka descriptor: %q", n.External)
	}
	if n := nodeByID(t, g, "external:https://b/x.parquet"); n.Database != "" || n.External != "https://b/x.parquet" {
		t.Fatalf("table function node: %+v", n)
	}
	if n := nodeByID(t, g, "other.hidden_mv"); n.Database != "other" || n.Name != "hidden_mv" || n.Engine != "" || n.Rows.Valid {
		t.Fatalf("hidden node: %+v", n)
	}
	if n := nodeByID(t, g, "app.mv_to"); n.Refresh != nil {
		t.Fatalf("plain MV has refresh: %+v", n.Refresh)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	a, _ := json.Marshal(Build(scenario()))
	for range 5 {
		b, _ := json.Marshal(Build(scenario()))
		if string(a) != string(b) {
			t.Fatal("output order changes between builds")
		}
	}
}

func TestBuildJSONShape(t *testing.T) {
	data, err := json.Marshal(Build(scenario()))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
		Meta  map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, n := range raw.Nodes {
		for _, k := range []string{"id", "database", "name", "kind", "engine", "rows", "bytes", "refresh"} {
			if _, ok := n[k]; !ok {
				t.Fatalf("node %v lacks %s", n["id"], k)
			}
		}
		if _, ok := n["external"]; ok != (n["kind"] == KindExternal) {
			t.Fatalf("external field on %v", n["id"])
		}
		if _, ok := n["ddl"]; ok {
			t.Fatal("ddl in graph response")
		}
	}
	for _, k := range []string{"host", "multi_node", "generated_at", "cached", "node_count", "warnings"} {
		if _, ok := raw.Meta[k]; !ok {
			t.Fatalf("meta lacks %s", k)
		}
	}
	empty, _ := json.Marshal(Build(Input{}))
	if !strings.Contains(string(empty), `"nodes":[]`) || !strings.Contains(string(empty), `"edges":[]`) || !strings.Contains(string(empty), `"warnings":[]`) {
		t.Fatalf("empty graph: %s", empty)
	}
}

func TestBuildDatabaseFilterKeepsNeighbours(t *testing.T) {
	in := scenario()
	// Rows outside the selected database come without definitions.
	for i := range in.Tables {
		if in.Tables[i].Database == "app" {
			in.Tables[i].DDL, in.Tables[i].AsSelect = "", ""
		}
	}
	in.Tables = append(in.Tables,
		TableRow{Database: "bi", Name: "v", Engine: "View", AsSelect: "SELECT * FROM app.daily JOIN bi.local USING id"},
		mergeTree("bi", "local"),
		TableRow{Database: "bi", Name: "lonely", Engine: "Memory"},
	)
	in.Databases = []string{"bi"}
	g := Build(in)
	want := []string{
		"app.daily -reads/parsed-> bi.v",
		"bi.local -reads/parsed-> bi.v",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %q", got)
	}
	if got := nodeKinds(g); !reflect.DeepEqual(got, map[string]string{"app.daily": KindTable, "bi.v": KindView, "bi.local": KindTable, "bi.lonely": KindTable}) {
		t.Fatalf("nodes %v", got)
	}

	// System edges from rows outside the selection still reach into it.
	in.Databases = []string{"legacy"}
	in.Tables = append(in.Tables, mergeTree("ingest", "raw", "legacy.m"))
	g = Build(in)
	want = []string{"ingest.raw -triggers/system-> legacy.m", "legacy.src -triggers/system-> legacy.m"}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %q", got)
	}
}

func TestBuildMergeRegexpDatabase(t *testing.T) {
	g := Build(Input{Tables: []TableRow{
		mergeTree("logs_2024", "hits"),
		mergeTree("logs_2025", "hits"),
		mergeTree("logs_2025", "other"),
		mergeTree("misc", "hits"),
		{Database: "all", Name: "hits", Engine: "Merge", EngineFull: "Merge(REGEXP('^logs_'), '^hits$')"},
		{Database: "all", Name: "bad", Engine: "Merge", EngineFull: "Merge('x', '(')"},
		{Database: "all", Name: "own", Engine: "Merge", EngineFull: "Merge(currentDatabase(), '^hi')"},
	}})
	want := []string{
		"all.hits -routes/system-> all.own",
		"logs_2024.hits -routes/system-> all.hits",
		"logs_2025.hits -routes/system-> all.hits",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %q", got)
	}
}

func TestBuildDefinitionWithoutAsSelect(t *testing.T) {
	// Servers without system.tables.as_select: the body comes from the DDL,
	// and unqualified names resolve to the object's own database.
	g := Build(Input{Tables: []TableRow{
		mergeTree("db", "src"),
		mergeTree("db", "dst"),
		mv("db", "m", "CREATE MATERIALIZED VIEW db.m TO dst AS SELECT * FROM src WHERE 'FROM fake' != ''", ""),
		{Database: "db", Name: "v", Engine: "View", DDL: "CREATE VIEW db.v AS SELECT * FROM src -- FROM fake2"},
	}})
	want := []string{
		"db.m -writes/parsed-> db.dst",
		"db.src -reads/parsed-> db.m",
		"db.src -reads/parsed-> db.v",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %q", got)
	}
}

func TestBuildDictionaryFromSystemSource(t *testing.T) {
	g := Build(Input{
		Tables: []TableRow{
			mergeTree("db", "src"),
			{Database: "db", Name: "d", Engine: "Dictionary"},
			{Database: "db", Name: "m", Engine: "Dictionary"},
		},
		Dictionaries: []DictionaryRow{
			{Database: "db", Name: "d", Source: "ClickHouse: db.src"},
			{Database: "db", Name: "m", Source: "MySQL: shop.items"},
		},
	})
	want := []string{"db.src -sources/system-> db.d", "external:mysql -sources/system-> db.m"}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %q", got)
	}
}

func TestCountJSON(t *testing.T) {
	var rows []struct {
		A Count `json:"a"`
	}
	if err := json.Unmarshal([]byte(`[{"a":12},{"a":"34"},{"a":null},{}]`), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].A != count(12) || rows[1].A != count(34) || rows[2].A.Valid || rows[3].A.Valid {
		t.Fatalf("decoded %+v", rows)
	}
	out, _ := json.Marshal(rows)
	if string(out) != `[{"a":12},{"a":34},{"a":null},{"a":null}]` {
		t.Fatalf("encoded %s", out)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]TableRow{
		KindRefreshableMV: {Engine: "MaterializedView", DDL: "CREATE MATERIALIZED VIEW a.b REFRESH EVERY 1 DAY TO a.c AS SELECT 1"},
		KindMV:            {Engine: "MaterializedView", DDL: "CREATE MATERIALIZED VIEW a.b TO a.c AS SELECT 1"},
		KindExternal:      {Engine: "S3", EngineFull: "S3('https://x/y', 'CSV')"},
		KindTable:         {Engine: "ReplicatedMergeTree", EngineFull: "ReplicatedMergeTree('/p', '{replica}') ORDER BY id"},
	}
	for want, row := range cases {
		if got := Classify(row, false); got != want {
			t.Errorf("%s: got %s", want, got)
		}
	}
	if got := Classify(TableRow{Engine: "MaterializedView"}, true); got != KindRefreshableMV {
		t.Errorf("view_refreshes listed MV: %s", got)
	}
}

func TestBuildOrphanInnerTableIsNeverANode(t *testing.T) {
	g := Build(Input{Tables: []TableRow{
		{Database: "db", Name: ".inner_id.0000", Engine: "MergeTree"},
		{Database: "db", Name: "v", Engine: "View", AsSelect: "SELECT * FROM db.`.inner_id.0000` JOIN db.`.inner.gone` USING id"},
	}})
	if len(g.Edges) != 0 || len(g.Nodes) != 1 || g.Nodes[0].ID != "db.v" {
		t.Fatalf("nodes %v edges %v", g.Nodes, g.Edges)
	}
}

func TestBuildDatabaseFilterShowsReadersElsewhere(t *testing.T) {
	// demo0.v0 reads demo1.agg0 only through its own JOIN; selecting demo1
	// must still show it, and the other side of v0 (demo0.t) is two edges
	// away from demo1, so it stays out.
	g := Build(Input{
		Tables: []TableRow{
			mergeTree("demo0", "t"),
			{Database: "demo0", Name: "v0", Engine: "View", AsSelect: "SELECT * FROM demo0.t JOIN demo1.agg0 USING id"},
			mv("demo0", "mv0", "CREATE MATERIALIZED VIEW demo0.mv0 TO demo1.sink AS SELECT * FROM demo0.t", "SELECT * FROM demo0.t"),
			{Database: "demo2", Name: "d", Engine: "Dictionary", DDL: "CREATE DICTIONARY demo2.d (`id` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(TABLE 'agg0' DB 'demo1')) LAYOUT(FLAT())"},
			{Database: "demo2", Name: "unrelated", Engine: "View", AsSelect: "SELECT * FROM demo0.t"},
			mergeTree("demo1", "agg0"),
			mergeTree("demo1", "sink"),
		},
		Databases: []string{"demo1"},
	})
	want := []string{
		"demo0.mv0 -writes/parsed-> demo1.sink",
		"demo1.agg0 -reads/parsed-> demo0.v0",
		"demo1.agg0 -sources/parsed-> demo2.d",
	}
	if got := edgeStrings(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %q", got)
	}
	wantKinds := map[string]string{"demo1.agg0": KindTable, "demo1.sink": KindTable, "demo0.v0": KindView, "demo0.mv0": KindMV, "demo2.d": KindDictionary}
	if got := nodeKinds(g); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("nodes %v", got)
	}
}
