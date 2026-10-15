package lineage

import (
	"fmt"
	"testing"
)

// benchInput is 2,000 objects: 1,500 tables across 20 databases and 500
// parsed definitions (250 MVs with TO, 150 MVs with inner tables, 100 views),
// each reading two or three tables through joins, a CTE and a subquery.
func benchInput() Input {
	var in Input
	table := func(i int) (string, string) { return fmt.Sprintf("db%02d", i%20), fmt.Sprintf("t%04d", i) }
	for i := 0; i < 1500; i++ {
		db, name := table(i)
		r := mergeTree(db, name)
		if i < 400 {
			mdb, _ := table(i)
			r.DependenciesDatabase = []string{mdb}
			r.DependenciesTable = []string{fmt.Sprintf("mv%04d", i)}
		}
		in.Tables = append(in.Tables, r)
	}
	for i := 0; i < 500; i++ {
		db, _ := table(i)
		srcDB, src := table(i)
		joinDB, join := table((i + 7) % 1500)
		subDB, sub := table((i + 13) % 1500)
		body := fmt.Sprintf("WITH recent AS (SELECT id, ts FROM %s.%s WHERE ts > now() - INTERVAL 1 DAY) "+
			"SELECT r.id, count() AS c, dictGet('%s.dict', 'name', r.id) AS n, 'FROM nowhere' AS s "+
			"FROM recent AS r INNER JOIN %s.%s AS j ON r.id = j.id "+
			"WHERE r.id IN (SELECT id FROM %s.%s WHERE flag = 1) -- trailing FROM comment\n"+
			"GROUP BY r.id ORDER BY c DESC", srcDB, src, db, joinDB, join, subDB, sub)
		switch {
		case i < 250:
			name := fmt.Sprintf("mv%04d", i)
			tdb, target := table((i + 1) % 1500)
			r := mv(db, name, fmt.Sprintf("CREATE MATERIALIZED VIEW %s.%s TO %s.%s (`id` UInt64, `c` UInt64) AS %s", db, name, tdb, target, body), body)
			in.Tables = append(in.Tables, r)
		case i < 400:
			name := fmt.Sprintf("mv%04d", i)
			r := mv(db, name, fmt.Sprintf("CREATE MATERIALIZED VIEW %s.%s (`id` UInt64) ENGINE = MergeTree ORDER BY id AS %s", db, name, body), body)
			r.UUID = fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
			in.Tables = append(in.Tables, r, TableRow{Database: db, Name: ".inner_id." + r.UUID, Engine: "MergeTree", TotalRows: count(1)})
		default:
			in.Tables = append(in.Tables, TableRow{Database: db, Name: fmt.Sprintf("v%04d", i), Engine: "View", DDL: "CREATE VIEW x AS " + body, AsSelect: body})
		}
	}
	return in
}

func BenchmarkBuild2000(b *testing.B) {
	in := benchInput()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		g := Build(in)
		if len(g.Nodes) < 2000 {
			b.Fatalf("nodes %d", len(g.Nodes))
		}
	}
}

// BenchmarkBuild2000Filtered selects one of the 20 databases: every
// definition is still parsed to find readers elsewhere.
func BenchmarkBuild2000Filtered(b *testing.B) {
	in := benchInput()
	in.Databases = []string{"db00"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if g := Build(in); len(g.Nodes) == 0 {
			b.Fatal("empty graph")
		}
	}
}
