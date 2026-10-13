package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// Models v2 against a real ClickHouse: PARTITION BY and engine arguments,
// incremental append and delete_insert, materialized views, data tests and
// selectors. Everything lives in chui_models_feat_<random>, dropped at the end.
func TestModelsV2Live(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	admin, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if admin == "" {
		admin = "default"
	}
	query := func(sql string) (*connector.QueryResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return client.Execute(ctx, sql, admin, adminPassword, nil)
	}
	must := func(sql string) {
		t.Helper()
		if _, err := query(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	scalar := func(sql string) string {
		t.Helper()
		res, err := query(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if len(res.Data) == 0 {
			return ""
		}
		for _, v := range res.Data[0] {
			return fmt.Sprint(v)
		}
		return ""
	}

	fdb := fmt.Sprintf("chui_models_feat_%d", rand.New(rand.NewSource(time.Now().UnixNano())).Int63())
	must("CREATE DATABASE " + fdb + " ENGINE = Atomic")
	t.Cleanup(func() {
		if _, err := query("DROP DATABASE IF EXISTS " + fdb); err != nil {
			t.Error(err)
		}
	})

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	enc, err := crypto.Encrypt(adminPassword, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "model", Mode: "service_account", Username: admin, EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(db, agent.Gateway, "test-secret")

	create := func(m database.Model) string {
		t.Helper()
		m.TargetDatabase = fdb
		if err := NormalizeModel(&m); err != nil {
			t.Fatal(err)
		}
		id, err := db.CreateModel(conn, m.Name, "", fdb, m.Materialization, m.SQLBody, m.TableEngine, m.OrderBy, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetModelOptions(id, m.Options()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	update := func(id string, change func(*database.Model)) {
		t.Helper()
		m, err := db.GetModelByID(id)
		if err != nil || m == nil {
			t.Fatal(err)
		}
		change(m)
		if err := NormalizeModel(m); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdateModel(id, m.Name, m.Description, m.TargetDatabase, m.Materialization, m.SQLBody, m.TableEngine, m.OrderBy); err != nil {
			t.Fatal(err)
		}
		if err := db.SetModelOptions(id, m.Options()); err != nil {
			t.Fatal(err)
		}
	}
	lastError := func(id string) string {
		m, _ := db.GetModelByID(id)
		if m == nil || m.LastError == nil {
			return ""
		}
		return *m.LastError
	}
	run := func(id string, fullRefresh bool) error {
		t.Helper()
		_, err := r.RunModel(conn, id, ScopeOnly, fullRefresh, "test")
		return err
	}
	mustRun := func(id string, fullRefresh bool) {
		t.Helper()
		if err := run(id, fullRefresh); err != nil {
			t.Fatalf("run: %v (%s)", err, lastError(id))
		}
	}
	count := func(table string) string { return scalar("SELECT count() FROM " + fdb + "." + table) }
	comment := func(name string) string {
		return scalar(fmt.Sprintf("SELECT comment FROM system.tables WHERE database = '%s' AND name = '%s'", fdb, name))
	}

	t.Run("partition by and engine arguments", func(t *testing.T) {
		id := create(database.Model{
			Name: "versions", Materialization: MaterializationTable,
			SQLBody:     "SELECT number AS id, toDate('2026-01-01') + number * 40 AS day, 1 AS ver FROM numbers(6)",
			TableEngine: "ReplacingMergeTree", EngineArgs: "ver", PartitionBy: "toYYYYMM(day)", OrderBy: "id",
		})
		mustRun(id, false)
		full := scalar(fmt.Sprintf("SELECT engine_full FROM system.tables WHERE database = '%s' AND name = 'versions'", fdb))
		if !strings.Contains(full, "ReplacingMergeTree(ver)") || !strings.Contains(full, "PARTITION BY toYYYYMM(day)") {
			t.Fatalf("engine_full %q", full)
		}
		if got := scalar("SELECT count(DISTINCT partition) FROM system.parts WHERE database = '" + fdb + "' AND table = 'versions' AND active"); got != "6" {
			t.Fatalf("want 6 partitions, got %s", got)
		}
		// The rebuild goes through the swap and keeps the options.
		update(id, func(m *database.Model) { m.SQLBody = strings.Replace(m.SQLBody, "numbers(6)", "numbers(3)", 1) })
		mustRun(id, false)
		full = scalar(fmt.Sprintf("SELECT engine_full FROM system.tables WHERE database = '%s' AND name = 'versions'", fdb))
		if count("versions") != "3" || !strings.Contains(full, "PARTITION BY toYYYYMM(day)") {
			t.Fatalf("rebuild lost rows or options: %s rows, %q", count("versions"), full)
		}
		// A partition expression ClickHouse cannot parse fails before any DDL.
		update(id, func(m *database.Model) { m.PartitionBy = "toYYYYMM(day" + ")" + " +" })
		if err := run(id, false); err == nil || !strings.Contains(lastError(id), "PARTITION BY is not a valid expression") {
			t.Fatalf("want a parse error, got %v (%s)", err, lastError(id))
		}
		if count("versions") != "3" {
			t.Fatal("a refused run changed the table")
		}
	})

	t.Run("incremental append with a watermark", func(t *testing.T) {
		must("CREATE TABLE " + fdb + ".events (id UInt64, ts DateTime) ENGINE = MergeTree ORDER BY id")
		must("INSERT INTO " + fdb + ".events SELECT number, toDateTime('2026-01-01 00:00:00') + number FROM numbers(10)")
		id := create(database.Model{
			Name: "events_inc", Materialization: MaterializationIncremental, IncrementalStrategy: StrategyAppend,
			WatermarkColumn: "ts", SQLBody: "SELECT id, ts FROM " + fdb + ".events -- source", OrderBy: "id",
		})
		mustRun(id, false)
		if got := count("events_inc"); got != "10" {
			t.Fatalf("first build: %s rows", got)
		}
		if comment("events_inc") != OwnerComment(id) {
			t.Fatal("incremental table not marked")
		}
		must("INSERT INTO " + fdb + ".events SELECT number + 10, toDateTime('2026-01-01 00:00:00') + number + 10 FROM numbers(5)")
		mustRun(id, false)
		if got := count("events_inc"); got != "15" {
			t.Fatalf("append must add only the 5 new rows, got %s", got)
		}
		mustRun(id, false)
		if got := count("events_inc"); got != "15" {
			t.Fatalf("a run without new rows must add nothing, got %s", got)
		}
		results := lastResultSQL(t, db, conn)
		if !strings.Contains(results, "INSERT INTO `"+fdb+"`.`events_inc` SELECT * FROM (") || !strings.Contains(results, "WHERE `ts` > (SELECT max(`ts`)") {
			t.Fatalf("run log should show the incremental insert: %s", results)
		}
		// A full refresh rebuilds through the swap: duplicates added by hand
		// disappear.
		must("INSERT INTO " + fdb + ".events_inc SELECT * FROM " + fdb + ".events_inc")
		mustRun(id, true)
		if got := count("events_inc"); got != "15" {
			t.Fatalf("full refresh must rebuild from the SELECT, got %s rows", got)
		}
		// Columns that no longer match fail with a hint to fully refresh.
		update(id, func(m *database.Model) { m.SQLBody = "SELECT id, ts, 1 AS extra FROM " + fdb + ".events" })
		if err := run(id, false); err == nil || !strings.Contains(lastError(id), "full refresh") {
			t.Fatalf("want a full refresh hint, got %v (%s)", err, lastError(id))
		}
		mustRun(id, true)
		if got := scalar("SELECT count() FROM system.columns WHERE database = '" + fdb + "' AND table = 'events_inc'"); got != "3" {
			t.Fatalf("full refresh must pick up the new column, %s columns", got)
		}
	})

	t.Run("incremental with $this", func(t *testing.T) {
		id := create(database.Model{
			Name: "events_this", Materialization: MaterializationIncremental,
			SQLBody: "SELECT id, ts FROM " + fdb + ".events WHERE ts > (SELECT max(ts) FROM $this)",
		})
		if err := run(id, false); err == nil || !strings.Contains(lastError(id), "does not exist yet") {
			t.Fatalf("first run with $this must explain itself: %v (%s)", err, lastError(id))
		}
		must("CREATE TABLE " + fdb + ".events_this (id UInt64, ts DateTime) ENGINE = MergeTree ORDER BY id COMMENT '" + OwnerComment(id) + "'")
		must("INSERT INTO " + fdb + ".events_this SELECT id, ts FROM " + fdb + ".events WHERE id < 12")
		mustRun(id, false)
		if got := count("events_this"); got != "15" {
			t.Fatalf("$this filter: %s rows", got)
		}
	})

	t.Run("incremental delete_insert", func(t *testing.T) {
		must("CREATE TABLE " + fdb + ".orders_raw (id UInt64, status String, updated_at DateTime) ENGINE = MergeTree ORDER BY id")
		must("INSERT INTO " + fdb + ".orders_raw VALUES (1, 'new', '2026-01-01 00:00:00'), (2, 'new', '2026-01-01 00:00:00'), (3, 'new', '2026-01-01 00:00:00')")
		id := create(database.Model{
			Name: "orders_latest", Materialization: MaterializationIncremental, IncrementalStrategy: StrategyDeleteInsert,
			UniqueKey: "id", WatermarkColumn: "last_update", OrderBy: "id",
			SQLBody: "SELECT id, argMax(status, updated_at) AS last_status, max(updated_at) AS last_update FROM " + fdb + ".orders_raw GROUP BY id",
		})
		mustRun(id, false)
		must("INSERT INTO " + fdb + ".orders_raw VALUES (2, 'paid', '2026-01-02 00:00:00'), (4, 'new', '2026-01-02 00:00:00')")
		mustRun(id, false)
		if got := count("orders_latest"); got != "4" {
			t.Fatalf("want one row per id (4), got %s", got)
		}
		if got := scalar("SELECT last_status FROM " + fdb + ".orders_latest WHERE id = 2"); got != "paid" {
			t.Fatalf("id 2 must be replaced, status %s", got)
		}
		if got := scalar("SELECT count() FROM " + fdb + ".orders_latest WHERE id = 2"); got != "1" {
			t.Fatalf("id 2 must appear once, got %s", got)
		}
		if _, ok := tableExists(t, scalar, fdb, "orders_latest__chui_tmp"); ok {
			t.Fatal("staging table left behind")
		}
		if !strings.Contains(lastResultSQL(t, db, conn), "DELETE FROM `"+fdb+"`.`orders_latest` WHERE (`id`) IN") {
			t.Fatal("run log should show the lightweight delete")
		}
	})

	t.Run("materialized view", func(t *testing.T) {
		must("CREATE TABLE " + fdb + ".clicks (id UInt64, v UInt32) ENGINE = MergeTree ORDER BY id")
		must("INSERT INTO " + fdb + ".clicks VALUES (1, 1), (2, 2)")
		id := create(database.Model{
			Name: "clicks_x", Materialization: MaterializationMaterializedView, OrderBy: "id",
			SQLBody: "SELECT id, toUInt64(v * 2) AS vx FROM " + fdb + ".clicks",
		})
		mustRun(id, false)
		if got := count("clicks_x"); got != "2" {
			t.Fatalf("first build must backfill, %s rows", got)
		}
		if comment("clicks_x") != OwnerComment(id) || comment("clicks_x__mv") != OwnerComment(id) {
			t.Fatalf("both objects must be marked: %q %q", comment("clicks_x"), comment("clicks_x__mv"))
		}
		must("INSERT INTO " + fdb + ".clicks VALUES (3, 3), (4, 4), (5, 5)")
		if got := count("clicks_x"); got != "5" {
			t.Fatalf("the trigger must feed new rows, %s rows", got)
		}
		// Same columns, new logic: the trigger's query changes in place.
		update(id, func(m *database.Model) { m.SQLBody = strings.Replace(m.SQLBody, "v * 2", "v * 3", 1) })
		mustRun(id, false)
		must("INSERT INTO " + fdb + ".clicks VALUES (6, 10)")
		if got := scalar("SELECT vx FROM " + fdb + ".clicks_x WHERE id = 6"); got != "30" {
			t.Fatalf("the trigger must use the new query, vx=%s", got)
		}
		if got := count("clicks_x"); got != "6" {
			t.Fatalf("a normal run must not touch the table, %s rows", got)
		}
		// New columns need a full refresh.
		update(id, func(m *database.Model) {
			m.SQLBody = "SELECT id, toUInt64(v * 3) AS vx, v AS raw FROM " + fdb + ".clicks"
		})
		if err := run(id, false); err == nil || !strings.Contains(lastError(id), "full refresh") {
			t.Fatalf("want a full refresh hint, got %v (%s)", err, lastError(id))
		}
		mustRun(id, true)
		if got := scalar("SELECT sum(vx) FROM " + fdb + ".clicks_x"); got != fmt.Sprint(3*(1+2+3+4+5+10)) {
			t.Fatalf("full refresh must backfill with the new query, sum %s", got)
		}
		must("INSERT INTO " + fdb + ".clicks VALUES (7, 1)")
		if got := scalar("SELECT raw FROM " + fdb + ".clicks_x WHERE id = 7"); got != "1" {
			t.Fatalf("the recreated trigger must feed the new table, raw=%s", got)
		}
		// Switching away from materialized_view drops the trigger.
		update(id, func(m *database.Model) { m.Materialization = MaterializationTable })
		mustRun(id, false)
		if _, ok := tableExists(t, scalar, fdb, "clicks_x__mv"); ok {
			t.Fatal("the trigger must be dropped when the model is no longer a materialized view")
		}
	})

	t.Run("data tests", func(t *testing.T) {
		must("CREATE TABLE " + fdb + ".customers (id UInt64) ENGINE = MergeTree ORDER BY id")
		must("INSERT INTO " + fdb + ".customers VALUES (1), (2)")
		payments := create(database.Model{
			Name: "payments", Materialization: MaterializationTable, OrderBy: "tuple()",
			SQLBody: "SELECT * FROM values('id UInt64, customer_id UInt64, status String, amount Int64, note Nullable(String)', " +
				"(1, 1, 'paid', 10, 'a'), (2, 2, 'paid', 5, NULL), (3, 9, 'refunded', -3, 'c'), (3, 1, 'lost', 1, 'd'))",
		})
		downstream := create(database.Model{Name: "payments_daily", Materialization: MaterializationView, SQLBody: "SELECT count() AS n FROM $ref(payments)"})
		add := func(kind, column, args, severity string) {
			t.Helper()
			tt := database.ModelTest{ModelID: payments, Kind: kind, ColumnName: column, Args: args, Severity: severity}
			if err := NormalizeTest(&tt); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CreateModelTest(tt); err != nil {
				t.Fatal(err)
			}
		}
		add(TestNotNull, "status", "", "error")
		add(TestNotNull, "note", "", "warn")
		add(TestUnique, "id", "", "warn")
		add(TestAcceptedValues, "status", `{"values":["paid","refunded"]}`, "warn")
		add(TestRelationship, "customer_id", `{"to":"customers","field":"id"}`, "warn")
		add(TestExpression, "", `{"expression":"amount >= 0"}`, "warn")

		runID, err := r.RunModel(conn, payments, ScopeDownstream, false, "test")
		if err != nil {
			t.Fatalf("warn failures must not fail the run: %v (%s)", err, lastError(payments))
		}
		res := resultFor(t, db, runID, payments)
		if res.TestsPassed != 1 || res.TestsFailed != 5 || res.TestDetails == nil {
			t.Fatalf("want 1 passed, 5 failed: %+v", res)
		}
		var outcomes []TestOutcome
		if err := json.Unmarshal([]byte(*res.TestDetails), &outcomes); err != nil {
			t.Fatal(err)
		}
		got := map[string]int64{}
		for _, o := range outcomes {
			if o.Status == "error" {
				t.Fatalf("test query failed: %+v", o)
			}
			got[o.Label()] = o.Failures
		}
		want := map[string]int64{"not_null(status)": 0, "not_null(note)": 1, "unique(id)": 1, "accepted_values(status)": 1, "relationship(customer_id)": 1, "expression": 1}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s: %d failing rows, want %d (all: %v)", k, got[k], v, got)
			}
		}
		// The same failure with severity error fails the model and skips
		// its downstream model.
		add(TestExpression, "", `{"expression":"amount >= 0"}`, "error")
		runID, err = r.RunModel(conn, payments, ScopeDownstream, false, "test")
		var failed *RunFailedError
		if !errors.As(err, &failed) || failed.Skipped != 1 {
			t.Fatalf("want a failed run with the downstream model skipped, got %v", err)
		}
		if !strings.Contains(lastError(payments), "data tests failed: expression: 1 failing rows") {
			t.Fatalf("model error: %q", lastError(payments))
		}
		if res := resultFor(t, db, runID, downstream); res.Status != "skipped" {
			t.Fatalf("downstream: %+v", res)
		}
	})

	t.Run("selectors and named pipelines", func(t *testing.T) {
		a := create(database.Model{Name: "sel_a", Materialization: MaterializationView, SQLBody: "SELECT 1 AS x"})
		b := create(database.Model{Name: "sel_b", Materialization: MaterializationView, SQLBody: "SELECT x FROM $ref(sel_a)"})
		c := create(database.Model{Name: "sel_c", Materialization: MaterializationView, SQLBody: "SELECT x FROM $ref(sel_b)"})
		d := create(database.Model{Name: "sel_d", Materialization: MaterializationView, SQLBody: "SELECT 2 AS y"})
		ran := func(runID string) []string {
			results, err := db.GetModelRunResults(runID)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, res := range results {
				ids = append(ids, res.ModelID)
			}
			sort.Strings(ids)
			return ids
		}
		sorted := func(ids ...string) string { sort.Strings(ids); return strings.Join(ids, ",") }
		// In order: "+sel_b" builds sel_a first, which "sel_b+" reads.
		for _, tc := range []struct{ selector, want string }{
			{"+sel_b", sorted(a, b)},
			{"sel_b+", sorted(b, c)},
			{"+sel_b+", sorted(a, b, c)},
			{"sel_a, sel_d", sorted(a, d)},
		} {
			selector, want := tc.selector, tc.want
			runID, err := r.Run(conn, RunOptions{Selector: selector, TriggeredBy: "test"})
			if err != nil {
				t.Fatalf("%s: %v", selector, err)
			}
			if got := strings.Join(ran(runID), ","); got != want {
				t.Fatalf("%s ran %s, want %s", selector, got, want)
			}
		}
		pid, err := db.CreateModelPipeline(database.ModelPipeline{ConnectionID: conn, Name: "sel", Selector: "sel_b+"})
		if err != nil {
			t.Fatal(err)
		}
		runID, err := r.RunNamedPipeline(conn, pid, "test")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(ran(runID), ","); got != sorted(b, c) {
			t.Fatalf("pipeline ran %s", got)
		}
		if got := scalar("SELECT count() FROM system.tables WHERE database = '" + fdb + "' AND name LIKE 'sel_%'"); got != "4" {
			t.Fatalf("views built: %s", got)
		}
	})
}

func tableExists(t *testing.T, scalar func(string) string, db, name string) (string, bool) {
	t.Helper()
	n := scalar(fmt.Sprintf("SELECT count() FROM system.tables WHERE database = '%s' AND name = '%s'", db, name))
	return n, n != "0"
}

// lastResultSQL returns the statements of the newest run's first result.
func lastResultSQL(t *testing.T, db *database.DB, conn string) string {
	t.Helper()
	runs, err := db.GetModelRuns(conn, 1, 0)
	if err != nil || len(runs) == 0 {
		t.Fatalf("runs: %v", err)
	}
	results, err := db.GetModelRunResults(runs[0].ID)
	if err != nil || len(results) == 0 || results[0].ResolvedSQL == nil {
		t.Fatalf("results: %v", err)
	}
	return *results[0].ResolvedSQL
}

func resultFor(t *testing.T, db *database.DB, runID, modelID string) database.ModelRunResult {
	t.Helper()
	results, err := db.GetModelRunResults(runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ModelID == modelID {
			return r
		}
	}
	t.Fatalf("no result for %s in run %s", modelID, runID)
	return database.ModelRunResult{}
}
