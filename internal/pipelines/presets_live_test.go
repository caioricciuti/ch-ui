package pipelines

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Against a live ClickHouse (CHUI_TEST_CLICKHOUSE_URL): ingest events
// pipelines write into a wide events table and into a custom table
// inferred from the first batch, which then gains a column for a new key.
func TestLivePresets(t *testing.T) {
	shortBackoff(t)
	env := newLiveEnv(t)
	// A scratch database named for this test, next to newLiveEnv's.
	dbName := fmt.Sprintf("chui_preset_%d", time.Now().UnixNano())
	env.exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { env.exec("DROP DATABASE IF EXISTS " + dbName) })
	jsonType := jsonTypeSupported(env.exec("SELECT version()"))

	wideID := graphPipeline(t, env.db, env.conn, "source_ingest", map[string]interface{}{"signal": "events"}, map[string]interface{}{
		"database": dbName, "table": "events", "create_table": true, "preset": "wide_events", "keep_raw": true, "ttl_days": float64(3650), // the sample events are from 2025
	})
	customID := graphPipeline(t, env.db, env.conn, "source_ingest", map[string]interface{}{"signal": "events"}, map[string]interface{}{
		"database": dbName, "table": "inferred", "create_table": true, "add_new_columns": true,
	})
	r := NewRunner(env.db, env.agent.Gateway, env.cfg)
	hub := testHub(t, t.TempDir(), fastSpool())
	r.SetHub(hub)
	for _, id := range []string{wideID, customID} {
		if err := r.StartPipeline(id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(r.Stop)
	waitFor(t, "endpoints", func() bool { return hub.Endpoint(wideID) != nil && hub.Endpoint(customID) != nil })
	waitRows := func(table, want string) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			got := env.exec("SELECT count() FROM system.tables WHERE database = '" + dbName + "' AND name = '" + table + "'")
			if got == "1" {
				if got = env.exec("SELECT count() FROM " + dbName + "." + table); got == want {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: rows %s, want %s", table, got, want)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Wide events: a Filebeat-like event, a logfmt-parsed line, a pino line.
	wide := strings.Join([]string{
		`{"@timestamp":"2025-10-16T10:30:00.123Z","message":"GET /health 200","log":{"level":"WARNING"},"service":{"name":"api"},"host":{"name":"web-1"},"trace":{"id":"t-1"}}`,
		`{"ts":"1760610601","level":"error","msg":"boom","app":"billing","user":"7"}`,
		`{"time":1760610602000,"level":30,"msg":"ok","service":"auth","pid":42}`,
	}, "\n") + "\n"
	if err := hub.Append(wideID, []byte(wide)); err != nil {
		t.Fatal(err)
	}
	waitRows("events", "3")
	attrType := "Map(LowCardinality(String), String)"
	if jsonType {
		attrType = "JSON"
	}
	cols := env.exec("SELECT arrayStringConcat(groupArray(name || ' ' || type), '|') FROM (SELECT name, type FROM system.columns WHERE database = '" + dbName + "' AND table = 'events' ORDER BY position)")
	if want := "timestamp DateTime64(3)|service LowCardinality(String)|level LowCardinality(String)|message String|trace_id String|attributes " + attrType + "|raw String"; cols != want {
		t.Fatalf("wide columns:\n%s\nwant\n%s", cols, want)
	}
	if ttl := env.exec("SELECT engine_full LIKE '%TTL toDateTime(timestamp) + toIntervalDay(3650)%' FROM system.tables WHERE database = '" + dbName + "' AND name = 'events'"); ttl != "1" {
		t.Fatalf("no TTL on the wide table")
	}
	host := "attributes.host.name"
	if !jsonType {
		host = "attributes['host.name']"
	}
	got := env.exec("SELECT arrayStringConcat(groupArray(r), '|') FROM (SELECT concat(toString(timestamp), ',', service, ',', level, ',', message, ',', trace_id, ',', toString(" + host + ")) AS r FROM " + dbName + ".events ORDER BY timestamp)")
	want := "2025-10-16 10:30:00.123,api,warn,GET /health 200,t-1,web-1|2025-10-16 10:30:01.000,billing,error,boom,,|2025-10-16 10:30:02.000,auth,info,ok,,"
	if env.exec("SELECT timezone()") != "UTC" {
		got = env.exec("SELECT arrayStringConcat(groupArray(r), '|') FROM (SELECT concat(toString(timestamp, 'UTC'), ',', service, ',', level, ',', message, ',', trace_id, ',', toString(" + host + ")) AS r FROM " + dbName + ".events ORDER BY timestamp)")
	}
	if strings.ReplaceAll(got, ",NULL", ",") != want {
		t.Fatalf("wide rows:\n%s\nwant\n%s", got, want)
	}
	if raw := env.exec("SELECT countIf(raw LIKE '{%}') FROM " + dbName + ".events"); raw != "3" {
		t.Fatalf("raw kept for %s rows", raw)
	}

	// The layout for servers before 25.3 is valid too, and a sink writing
	// to it flattens attributes into the map.
	env.exec(wideEventsDDL(dbName, "events_map", "MergeTree", false, false, 0))
	sink := NewClickHouseSink(env.agent.Gateway, env.db, "test-secret")
	mapCfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": env.conn, "database": dbName, "table": "events_map", "preset": "wide_events"}}
	if _, err := sink.WriteBatch(context.Background(), mapCfg, Batch{Records: []Record{{RawJSON: []byte(strings.SplitN(wide, "\n", 2)[0])}}}); err != nil {
		t.Fatal(err)
	}
	if got := env.exec("SELECT service, level, attributes['host.name'] FROM " + dbName + ".events_map"); got != "api,warn,web-1" {
		t.Fatalf("map layout row: %s", got)
	}

	// Custom: inferred from the first batch.
	var first []string
	for i := 0; i < 30; i++ {
		first = append(first, fmt.Sprintf(`{"id":%d,"env":"%s","amount":%d.5,"ok":%t,"at":"2025-10-16T10:%02d:00Z","tags":["a","b"],"meta":{"k":%d},"note":%s}`,
			i, []string{"prod", "dev"}[i%2], i, i%2 == 0, i, i, map[bool]string{true: `"n"`, false: "null"}[i%3 == 0]))
	}
	if err := hub.Append(customID, []byte(strings.Join(first, "\n")+"\n")); err != nil {
		t.Fatal(err)
	}
	waitRows("inferred", "30")
	types := func() string {
		return env.exec("SELECT arrayStringConcat(groupArray(name || ' ' || type), '|') FROM (SELECT name, type FROM system.columns WHERE database = '" + dbName + "' AND table = 'inferred' ORDER BY position)")
	}
	metaType := "String" // before 25.3: an object with a number value is JSON text
	if jsonType {
		metaType = "JSON"
	}
	if want := "amount Float64|at DateTime64(3)|env LowCardinality(String)|id Int64|meta " + metaType + "|note LowCardinality(Nullable(String))|ok Bool|tags Array(String)"; types() != want {
		t.Fatalf("inferred columns:\n%s\nwant\n%s", types(), want)
	}
	if got := env.exec("SELECT toString(max(at), 'UTC'), sum(id), countIf(note IS NULL) FROM " + dbName + ".inferred"); got != "2025-10-16 10:29:00.000,435,20" {
		t.Fatalf("custom rows: %s", got)
	}

	// A new key is added as a column once; existing columns stay as they are.
	if err := hub.Append(customID, []byte(`{"id":100,"env":"prod","region":"eu-west-1","latency_ms":12}`+"\n"+`{"id":101,"region":"us-east-1","latency_ms":15}`+"\n")); err != nil {
		t.Fatal(err)
	}
	waitRows("inferred", "32")
	if err := hub.Append(customID, []byte(`{"id":102,"region":"eu-west-1","latency_ms":9}`+"\n")); err != nil {
		t.Fatal(err)
	}
	waitRows("inferred", "33")
	if want := "amount Float64|at DateTime64(3)|env LowCardinality(String)|id Int64|meta " + metaType + "|note LowCardinality(Nullable(String))|ok Bool|tags Array(String)|latency_ms Int64|region String"; types() != want {
		t.Fatalf("evolved columns:\n%s\nwant\n%s", types(), want)
	}
	if got := env.exec("SELECT arrayStringConcat(groupArray(region), ',') FROM (SELECT region FROM " + dbName + ".inferred WHERE id >= 100 ORDER BY id)"); got != "eu-west-1,us-east-1,eu-west-1" {
		t.Fatalf("evolved rows: %s", got)
	}
	runs, _ := env.db.GetPipelineRuns(customID, 1, 0)
	logs, _ := env.db.GetPipelineRunLogs(runs[0].ID, 50)
	added := 0
	for _, l := range logs {
		if strings.HasPrefix(l.Message, "Added column") {
			added++
		}
	}
	if added != 2 {
		t.Fatalf("run log has %d column additions: %+v", added, logs)
	}
}
