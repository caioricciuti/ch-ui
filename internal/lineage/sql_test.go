package lineage

import (
	"reflect"
	"strings"
	"testing"
)

func refStrings(refs []ref) []string {
	out := []string{}
	for _, r := range refs {
		if r.kind == refExternal {
			out = append(out, "ext:"+r.external)
		} else {
			out = append(out, qualified(r.db, r.name))
		}
	}
	return out
}

func TestSelectRefs(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"simple", "SELECT * FROM db.t", []string{"db.t"}},
		{"unqualified", "SELECT a FROM events WHERE x = 1", []string{"events"}},
		{"alias", "SELECT e.a FROM db.events AS e", []string{"db.events"}},
		{"bare alias", "SELECT e.a FROM db.events e WHERE 1", []string{"db.events"}},
		{"join", "SELECT * FROM a.x AS l INNER JOIN a.y AS r ON l.id = r.id LEFT JOIN z USING id", []string{"a.x", "a.y", "z"}},
		{"global any join", "SELECT * FROM a GLOBAL ANY LEFT JOIN b USING k", []string{"a", "b"}},
		{"comma join", "SELECT * FROM a.x, a.y AS yy, z WHERE 1", []string{"a.x", "a.y", "z"}},
		{"array join is not a table", "SELECT * FROM t ARRAY JOIN arr AS a LEFT ARRAY JOIN b", []string{"t"}},
		{"final", "SELECT * FROM db.t FINAL WHERE 1", []string{"db.t"}},
		{"final comma", "SELECT * FROM db.t FINAL, db.u", []string{"db.t", "db.u"}},
		{"backticks", "SELECT * FROM `my db`.`my-table`", []string{"my db.my-table"}},
		{"double quotes", `SELECT * FROM "Db"."Tbl" JOIN "x" ON 1`, []string{"Db.Tbl", "x"}},
		{"backtick with escaped quote", "SELECT * FROM `a``b`.c", []string{"a`b.c"}},
		{"keyword in string", "SELECT 'FROM fake JOIN fake2' AS s FROM real", []string{"real"}},
		{"escaped quote in string", `SELECT 'it\'s FROM x', 'a''FROM y' FROM real`, []string{"real"}},
		{"line comment", "SELECT 1 -- FROM fake\nFROM real", []string{"real"}},
		{"hash comment", "SELECT 1 # FROM fake\nFROM real", []string{"real"}},
		{"block comment", "SELECT /* FROM fake /* nested FROM x */ */ 1 FROM real", []string{"real"}},
		{"subquery", "SELECT * FROM (SELECT * FROM db.inner_t WHERE x IN (SELECT id FROM db.ids)) AS s", []string{"db.inner_t", "db.ids"}},
		{"subquery then comma", "SELECT * FROM (SELECT 1 FROM a) s, b", []string{"a", "b"}},
		{"nested subqueries", "SELECT * FROM (SELECT * FROM (SELECT * FROM (SELECT * FROM deep.t)))", []string{"deep.t"}},
		{"in subquery", "SELECT * FROM t WHERE id IN (SELECT id FROM db.allowed) AND k GLOBAL IN (SELECT k FROM db.g)", []string{"t", "db.allowed", "db.g"}},
		{"exists", "SELECT * FROM t WHERE EXISTS (SELECT 1 FROM u)", []string{"t", "u"}},
		{"scalar subquery in select", "SELECT (SELECT max(x) FROM db.m) AS mx FROM t", []string{"db.m", "t"}},
		{"cte not a table", "WITH recent AS (SELECT * FROM db.events WHERE ts > now() - 60) SELECT * FROM recent JOIN db.users USING uid", []string{"db.events", "db.users"}},
		{"two ctes", "WITH a AS (SELECT * FROM x), b AS (SELECT * FROM a JOIN y USING k) SELECT * FROM b", []string{"x", "y"}},
		{"with expression", "WITH 10 AS n, (SELECT 1 FROM z) AS s SELECT n FROM t", []string{"z", "t"}},
		{"cte name qualified is a table", "WITH x AS (SELECT 1) SELECT * FROM db.x", []string{"db.x"}},
		{"union", "SELECT a FROM x UNION ALL SELECT a FROM y UNION DISTINCT SELECT a FROM db.z", []string{"x", "y", "db.z"}},
		{"except intersect", "SELECT a FROM x EXCEPT SELECT a FROM y INTERSECT SELECT a FROM z", []string{"x", "y", "z"}},
		{"duplicates once", "SELECT * FROM t JOIN t USING id", []string{"t"}},
		{"extract from", "SELECT extract(DAY FROM ts), trim(BOTH ' ' FROM s), substring(s FROM 2) FROM real", []string{"real"}},
		{"is distinct from", "SELECT * FROM t WHERE a IS NOT DISTINCT FROM b", []string{"t"}},
		{"numbers ignored", "SELECT number FROM numbers(10)", []string{}},
		{"generators ignored", "SELECT * FROM zeros(3), generateRandom('a UInt8'), values('x', 1)", []string{}},
		{"system tables skipped", "SELECT * FROM system.parts JOIN db.t USING table", []string{"db.t"}},
		{"information schema skipped", "SELECT * FROM INFORMATION_SCHEMA.TABLES", []string{}},
		{"s3", "SELECT * FROM s3('https://key:secret@bucket.s3.amazonaws.com/data/*.parquet?X-Amz-Signature=abc', 'AKIA', 'SECRET', 'Parquet')", []string{"ext:https://bucket.s3.amazonaws.com/data/*.parquet"}},
		{"url", "SELECT * FROM url('http://example.com/feed.csv', CSV)", []string{"ext:http://example.com/feed.csv"}},
		{"mysql function", "SELECT * FROM mysql('db1:3306', 'shop', 'orders', 'user', 'pw')", []string{"ext:mysql://db1:3306/shop.orders"}},
		{"postgresql named collection", "SELECT * FROM postgresql(pg_conn, table = 'x')", []string{"ext:postgresql:pg_conn"}},
		{"file", "SELECT * FROM file('data/*.csv', CSV)", []string{"ext:file://data/*.csv"}},
		{"remote", "SELECT * FROM remote('ch2:9000', db.t, 'user', 'pw')", []string{"ext:remote://ch2:9000/db.t"}},
		{"remote three args", "SELECT * FROM remoteSecure('ch2:9440', 'db', 't')", []string{"ext:remote://ch2:9440/db.t"}},
		{"cluster", "SELECT * FROM cluster('main', db.t) JOIN clusterAllReplicas('main', 'db', 'u') USING k", []string{"db.t", "db.u"}},
		{"cluster string", "SELECT * FROM cluster('main', 'db.t')", []string{"db.t"}},
		{"view function", "SELECT * FROM view(SELECT * FROM db.inside)", []string{"db.inside"}},
		{"merge function ignored", "SELECT * FROM merge('db', '^t')", []string{}},
		{"dictGet", "SELECT dictGet('db.users', 'name', uid), dictGetOrDefault('countries', 'n', c, ''), dictHas('db.blocked', ip) FROM t", []string{"db.users", "countries", "db.blocked", "t"}},
		{"dictionary function", "SELECT * FROM dictionary('db.d')", []string{"db.d"}},
		{"param identifier", "SELECT * FROM {tbl:Identifier} JOIN db.t USING k", []string{"db.t"}},
		{"heredoc", "SELECT $$ FROM fake $$ AS s FROM real", []string{"real"}},
		{"lowercase keywords", "select * from a join b using k", []string{"a", "b"}},
		{"settings after from", "SELECT * FROM t SETTINGS max_threads = 1", []string{"t"}},
		{"sample", "SELECT * FROM db.t SAMPLE 0.1", []string{"db.t"}},
		{"prewhere", "SELECT * FROM db.t PREWHERE a = 1", []string{"db.t"}},
		{"unbalanced", "SELECT * FROM (SELECT * FROM x", []string{"x"}},
		{"garbage", "FROM , JOIN ( ) . `", []string{}},
		{"empty", "", []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := refStrings(selectRefs(c.sql))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("selectRefs(%q)\n got %q\nwant %q", c.sql, got, c.want)
			}
		})
	}
}

func TestParseDefinition(t *testing.T) {
	cases := []struct {
		name        string
		ddl         string
		to          string
		depends     []string
		refreshable bool
		body        []string
	}{
		{name: "mv to", ddl: "CREATE MATERIALIZED VIEW db.mv TO db.target (`id` UInt64) AS SELECT id FROM db.src", to: "db.target", body: []string{"db.src"}},
		{name: "mv to unqualified", ddl: "CREATE MATERIALIZED VIEW db.mv TO target AS SELECT id FROM src", to: "target", body: []string{"src"}},
		{name: "mv to quoted", ddl: "CREATE MATERIALIZED VIEW db.mv TO `my db`.`t-1` AS SELECT 1 FROM s", to: "my db.t-1", body: []string{"s"}},
		{name: "mv inner engine with TTL TO DISK", ddl: "CREATE MATERIALIZED VIEW db.mv (`ts` DateTime) ENGINE = MergeTree ORDER BY ts TTL ts + toIntervalDay(1) TO DISK 'cold' AS SELECT ts FROM db.src", body: []string{"db.src"}},
		{name: "refreshable", ddl: "CREATE MATERIALIZED VIEW db.rmv REFRESH EVERY 1 HOUR DEPENDS ON db.a, b TO db.t (`id` UInt64) DEFINER = default SQL SECURITY DEFINER AS SELECT id FROM db.a", to: "db.t", depends: []string{"db.a", "b"}, refreshable: true, body: []string{"db.a"}},
		{name: "refreshable append", ddl: "CREATE MATERIALIZED VIEW db.rmv REFRESH AFTER 5 MINUTE APPEND TO db.t AS SELECT 1 FROM x", to: "db.t", refreshable: true, body: []string{"x"}},
		{name: "view", ddl: "CREATE VIEW db.v (`a` UInt8) AS SELECT a FROM db.t AS x JOIN db.u USING a", body: []string{"db.t", "db.u"}},
		{name: "view with cte body", ddl: "CREATE VIEW db.v AS WITH c AS (SELECT 1 FROM db.t) SELECT * FROM c", body: []string{"db.t"}},
		{name: "default expression with AS in parens", ddl: "CREATE MATERIALIZED VIEW db.mv TO db.t (`x` String DEFAULT CAST(1 AS String)) AS SELECT x FROM db.s", to: "db.t", body: []string{"db.s"}},
		{name: "comment string mentions TO", ddl: "CREATE VIEW db.v AS SELECT 'TO db.fake' AS s FROM db.real COMMENT 'TO x'", body: []string{"db.real"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := parseDefinition(c.ddl, true)
			to := ""
			if d.to != nil {
				to = qualified(d.to.db, d.to.name)
			}
			if to != c.to {
				t.Fatalf("to = %q, want %q", to, c.to)
			}
			deps := refStrings(d.dependsOn)
			if c.depends == nil {
				c.depends = []string{}
			}
			if !reflect.DeepEqual(deps, c.depends) {
				t.Fatalf("depends = %q, want %q", deps, c.depends)
			}
			if d.refreshable != c.refreshable {
				t.Fatalf("refreshable = %v", d.refreshable)
			}
			if body := refStrings(refsFromTokens(d.body)); !reflect.DeepEqual(body, c.body) {
				t.Fatalf("body refs = %q, want %q", body, c.body)
			}
		})
	}
}

func TestParseDictionarySource(t *testing.T) {
	cases := []struct {
		ddl  string
		want []string
	}{
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(TABLE 'src' DB 'other')) LIFETIME(MIN 0 MAX 0) LAYOUT(FLAT())", []string{"other.src"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(TABLE 'src')) LAYOUT(FLAT())", []string{"db.src"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(QUERY 'SELECT id FROM a JOIN x.b USING id' DB 'q')) LAYOUT(FLAT())", []string{"q.a", "x.b"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(HOST 'ch9' PORT 9000 USER 'u' PASSWORD '[HIDDEN]' TABLE 't' DB 'x')) LAYOUT(FLAT())", []string{"external:clickhouse://ch9/x.t"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(MYSQL(PORT 3306 USER 'u' PASSWORD 'p' REPLICA(HOST 'm1' PRIORITY 1) HOST 'm0' DB 'shop' TABLE 'items')) LAYOUT(HASHED())", []string{"external:mysql://m0/shop.items"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(HTTP(URL 'https://u:p@api.example.com/x.csv?token=s' FORMAT 'CSV')) LAYOUT(FLAT())", []string{"external:https://api.example.com/x.csv"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(POSTGRESQL(NAME pg_conn)) LAYOUT(FLAT())", []string{"external:postgresql:pg_conn"}},
		{"CREATE DICTIONARY db.d (`id` UInt64) PRIMARY KEY id SOURCE(NULL()) LAYOUT(FLAT())", nil},
	}
	for _, c := range cases {
		src, ok := parseDictionarySource(c.ddl)
		if !ok {
			t.Fatalf("no source in %q", c.ddl)
		}
		if got := dictionarySourceKeys(src, "db"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n got %q\nwant %q", c.ddl, got, c.want)
		}
	}
}

func TestExternalEngine(t *testing.T) {
	cases := []struct {
		engine, full, want string
	}{
		{"Kafka", "Kafka('k1:9092,k2:9092', 'events', 'grp', 'JSONEachRow')", "kafka://k1:9092,k2:9092/events"},
		{"Kafka", "Kafka SETTINGS kafka_broker_list = 'k1:9092', kafka_topic_list = 'clicks', kafka_group_name = 'g', kafka_format = 'JSONEachRow'", "kafka://k1:9092/clicks"},
		{"RabbitMQ", "RabbitMQ SETTINGS rabbitmq_host_port = 'rmq:5672', rabbitmq_exchange_name = 'ex', rabbitmq_format = 'JSONEachRow'", "rabbitmq://rmq:5672/ex"},
		{"NATS", "NATS SETTINGS nats_url = 'nats://n1:4222', nats_subjects = 'a.b', nats_format = 'JSONEachRow'", "nats://n1:4222/a.b"},
		{"S3Queue", "S3Queue('https://bucket.s3.amazonaws.com/in/*.json', 'JSONEachRow') SETTINGS mode = 'unordered'", "https://bucket.s3.amazonaws.com/in/*.json"},
		{"S3", "S3('https://minio:9000/b/data.parquet', 'KEY', '[HIDDEN]', 'Parquet')", "https://minio:9000/b/data.parquet"},
		{"S3", "S3(s3_conn, filename = 'x.csv')", "s3:s3_conn"},
		{"URL", "URL('https://user:pw@host/path?sig=1', 'CSV')", "https://host/path"},
		{"MySQL", "MySQL('db1:3306', 'shop', 'orders', 'u', '[HIDDEN]')", "mysql://db1:3306/shop.orders"},
		{"PostgreSQL", "PostgreSQL('pg:5432', 'app', 'users', 'u', '[HIDDEN]', 'public')", "postgresql://pg:5432/app.users"},
		{"MongoDB", "MongoDB('mongo:27017', 'app', 'events', 'u', '[HIDDEN]')", "mongodb://mongo:27017/app.events"},
		{"File", "File(CSV)", ""},
		{"File", "File(CSV, '/data/in.csv')", "file:///data/in.csv"},
		{"MergeTree", "MergeTree ORDER BY id SETTINGS index_granularity = 8192", ""},
		{"Memory", "", ""},
		{"IcebergS3", "IcebergS3('https://b.s3.amazonaws.com/warehouse/t/', 'K', 'S')", "https://b.s3.amazonaws.com/warehouse/t/"},
	}
	for _, c := range cases {
		if got := externalEngine(c.engine, parseEngineFull(c.full)); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.engine, c.full, got, c.want)
		}
	}
}

func TestSanitizeLocationDropsCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:p@host:9000/a/b?X-Amz-Signature=s#f": "https://host:9000/a/b",
		"host:3306":                              "host:3306",
		"user@host":                              "host",
		"https://host/p@th":                      "https://host/p@th",
		"jdbc:mysql://h:3306/?user=r&password=r": "jdbc:mysql://h:3306/",
	} {
		if got := sanitizeLocation(in); got != want {
			t.Errorf("sanitizeLocation(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(sanitizeLocation(in), "password") {
			t.Errorf("credential kept in %q", in)
		}
	}
}
