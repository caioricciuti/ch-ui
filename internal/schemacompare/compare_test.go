package schemacompare

import (
	"strings"
	"testing"
)

func TestCompareDriftAndReviewOnly(t *testing.T) {
	source := []Table{{Name: "events`archive", Engine: "MergeTree", SortingKey: "(day, id)", PartitionKey: "day", CreateSQL: "CREATE TABLE source.events (day Date, id UInt64) ENGINE = MergeTree ORDER BY (day, id) TTL day + INTERVAL 30 DAY SETTINGS index_granularity = 8192", Columns: []Column{{Name: "id", Type: "UInt64", Position: 1}, {Name: "extra", Type: "String", Position: 2, DefaultKind: "DEFAULT", DefaultExpression: "'hello'"}}}, {Name: "missing", Engine: "MergeTree", Columns: []Column{{Name: "id", Type: "UInt32"}}}}
	target := []Table{{Name: "events`archive", Engine: "MergeTree", SortingKey: "id", PartitionKey: "day", CreateSQL: "CREATE TABLE target.events (day Date, id UInt32) ENGINE = MergeTree ORDER BY id", Columns: []Column{{Name: "id", Type: "UInt32", Position: 1}, {Name: "old", Type: "String", Position: 2}}}, {Name: "retained", Engine: "MergeTree"}}
	result := Compare(source, target, "prod`db")
	fields := map[string]bool{}
	for _, d := range result.Differences {
		fields[d.Field] = true
	}
	for _, field := range []string{"table", "sorting_key", "ttl", "column.id", "column.extra", "column.old"} {
		if !fields[field] {
			t.Errorf("missing difference: %s", field)
		}
	}
	if strings.Contains(result.ReviewSQL, "DROP ") {
		t.Fatal("generated destructive drop")
	}
	if !strings.Contains(result.ReviewSQL, "`prod\\`db`.`events\\`archive`") {
		t.Fatal("target identifiers not quoted")
	}
	assertCommented(t, result.ReviewSQL)
}

func TestIdentifierAndMetadataInjectionCannotEscapeReview(t *testing.T) {
	if got := QuoteIdentifier("a\\`b"); got != "`a\\\\\\`b`" {
		t.Fatalf("quote = %q", got)
	}
	result := Compare([]Table{{Name: "x\nDROP TABLE y", Engine: "MergeTree", Columns: []Column{{Name: "v", Type: "String\r\nDROP TABLE secret;"}}}}, nil, "d")
	assertCommented(t, result.ReviewSQL)
	for _, d := range result.Differences {
		assertCommented(t, d.ReviewSQL)
	}
}

func TestTTLParsingSkipsQuotedAndColumnExpressions(t *testing.T) {
	sql := "CREATE TABLE `TTL` (value String DEFAULT 'TTL SETTINGS', date Date TTL date + INTERVAL 1 DAY) ENGINE = MergeTree ORDER BY tuple() TTL date + INTERVAL 30 DAY DELETE WHERE value = 'SETTINGS' SETTINGS index_granularity = 8192"
	want := "date + INTERVAL 30 DAY DELETE WHERE value = 'SETTINGS'"
	if got := topLevelClause(sql, "TTL", "SETTINGS", "COMMENT", "AS"); got != want {
		t.Fatalf("TTL=%q want %q", got, want)
	}
	if got := topLevelClause("CREATE TABLE t (x String DEFAULT 'TTL x') ENGINE=MergeTree ORDER BY x", "TTL"); got != "" {
		t.Fatalf("false table TTL: %s", got)
	}
}

func TestViewAndMaterializedDestinationChanges(t *testing.T) {
	for _, pair := range [][2]string{{"CREATE VIEW one.v AS SELECT x FROM source", "CREATE VIEW two.v AS SELECT y FROM source"}, {"CREATE MATERIALIZED VIEW one.v TO one.events AS SELECT x FROM source", "CREATE MATERIALIZED VIEW two.v TO two.changed AS SELECT x FROM source"}} {
		result := Compare([]Table{{Name: "v", Engine: "MaterializedView", CreateSQL: pair[0]}}, []Table{{Name: "v", Engine: "MaterializedView", CreateSQL: pair[1]}}, "two")
		if len(result.Differences) != 1 || result.Differences[0].Field != "view_definition" {
			t.Fatalf("view diff: %+v", result.Differences)
		}
	}
	result := Compare([]Table{{Name: "v", Engine: "View", CreateSQL: "CREATE VIEW a.v AS SELECT x FROM events"}}, []Table{{Name: "v", Engine: "View", CreateSQL: "CREATE VIEW b.v AS SELECT x FROM events"}}, "b")
	if len(result.Differences) != 0 || result.MatchingTables != 1 {
		t.Fatalf("own database name falsely differs: %+v", result)
	}
}

func TestCreateDoesNotExportEngineCredentials(t *testing.T) {
	result := Compare([]Table{{Name: "remote", Engine: "S3", CreateSQL: "CREATE TABLE remote (id UInt32) ENGINE = S3('https://private', 'secret-key', 'secret-password')", Columns: []Column{{Name: "id", Type: "UInt32"}}}}, nil, "target")
	if strings.Contains(result.ReviewSQL, "secret") || strings.Contains(result.ReviewSQL, "https://") {
		t.Fatal("exported source engine arguments")
	}
}

func TestNoChangesForIdenticalMetadata(t *testing.T) {
	tables := []Table{{Name: "t", Engine: "MergeTree", SortingKey: "id", Columns: []Column{{Name: "id", Type: "UInt64", Position: 1}}}}
	result := Compare(tables, tables, "target")
	if len(result.Differences) != 0 || result.MatchingTables != 1 {
		t.Fatalf("unexpected difference: %+v", result)
	}
}

func assertCommented(t *testing.T, text string) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if line != "" && !strings.HasPrefix(line, "-- ") {
			t.Fatalf("uncommented SQL line: %q", line)
		}
	}
}
