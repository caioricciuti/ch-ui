package models

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// Builds models against a real ClickHouse: safe swap, ownership refusal,
// legacy adoption, engines without ORDER BY and the rename fallback.
func TestMaterializeLive(t *testing.T) {
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
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return client.Execute(ctx, sql, admin, adminPassword, nil)
	}
	must := func(sql string) {
		t.Helper()
		if _, err := query(sql); err != nil {
			t.Fatal(err)
		}
	}
	type obj struct{ engine, comment string }
	objects := func(db string) map[string]obj {
		t.Helper()
		res, err := query("SELECT name, engine, comment FROM system.tables WHERE database = '" + db + "'")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]obj{}
		for _, row := range res.Data {
			out[fmt.Sprint(row["name"])] = obj{fmt.Sprint(row["engine"]), fmt.Sprint(row["comment"])}
		}
		return out
	}
	count := func(table string) string {
		t.Helper()
		res, err := query("SELECT count() AS c FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(res.Data[0]["c"])
	}

	suffix := time.Now().UnixNano()
	atomicDB := fmt.Sprintf("chui_models_live_%d", suffix)
	ordinaryDB := fmt.Sprintf("chui_models_live_ord_%d", suffix)
	must("CREATE DATABASE " + atomicDB + " ENGINE = Atomic")
	t.Cleanup(func() {
		for _, db := range []string{atomicDB, ordinaryDB} {
			if _, err := query("DROP DATABASE IF EXISTS " + db); err != nil {
				t.Error(err)
			}
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
	create := func(name, target, mat, sql, engine, orderBy string) string {
		t.Helper()
		id, err := db.CreateModel(conn, name, "", target, mat, sql, engine, orderBy, "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	run := func(id string) error {
		t.Helper()
		_, err := r.RunSingle(conn, id, "test")
		return err
	}
	lastError := func(id string) string {
		m, _ := db.GetModelByID(id)
		if m == nil || m.LastError == nil {
			return ""
		}
		return *m.LastError
	}

	t.Run("hand-made table is never replaced", func(t *testing.T) {
		must("CREATE TABLE " + atomicDB + ".orders (id UInt64) ENGINE = MergeTree ORDER BY id")
		must("INSERT INTO " + atomicDB + ".orders VALUES (1), (2), (3)")
		id := create("orders", atomicDB, "table", "SELECT 42 AS id", "MergeTree", "id")
		var failed *RunFailedError
		if err := run(id); !errors.As(err, &failed) {
			t.Fatalf("want a failed run, got %v", err)
		}
		if !strings.Contains(lastError(id), "was not created by this model") {
			t.Fatalf("unexpected error %q", lastError(id))
		}
		if got := count(atomicDB + ".orders"); got != "3" {
			t.Fatalf("hand-made table changed: %s rows", got)
		}
	})

	t.Run("memory engine table, rebuild and failed rebuild", func(t *testing.T) {
		id := create("mem_model", atomicDB, "table", "SELECT number AS id FROM numbers(5)", "Memory", "")
		if err := run(id); err != nil {
			t.Fatalf("first build: %v (%s)", err, lastError(id))
		}
		if o := objects(atomicDB)["mem_model"]; o.engine != "Memory" || o.comment != OwnerComment(id) {
			t.Fatalf("unexpected object %+v", o)
		}
		if err := db.UpdateModel(id, "mem_model", "", atomicDB, "table", "SELECT number AS id FROM numbers(7)", "Memory", ""); err != nil {
			t.Fatal(err)
		}
		if err := run(id); err != nil {
			t.Fatalf("rebuild: %v (%s)", err, lastError(id))
		}
		if got := count(atomicDB + ".mem_model"); got != "7" {
			t.Fatalf("rebuild not swapped in: %s rows", got)
		}
		if err := db.UpdateModel(id, "mem_model", "", atomicDB, "table", "SELECT no_such_column", "Memory", ""); err != nil {
			t.Fatal(err)
		}
		if err := run(id); err == nil {
			t.Fatal("broken SQL must fail")
		}
		if got := count(atomicDB + ".mem_model"); got != "7" {
			t.Fatalf("failed rebuild lost the previous table: %s rows", got)
		}
		if _, ok := objects(atomicDB)["mem_model__chui_tmp"]; ok {
			t.Fatal("tmp table left behind")
		}
	})

	t.Run("legacy unmarked table is adopted only after a successful run", func(t *testing.T) {
		must("CREATE TABLE " + atomicDB + ".legacy ENGINE = MergeTree ORDER BY tuple() AS SELECT 1 AS id")
		id := create("legacy", atomicDB, "table", "SELECT 2 AS id", "MergeTree", "")
		if err := run(id); err == nil {
			t.Fatal("unmarked table without run history must be refused")
		}
		runID, err := db.CreateModelRun(conn, 1, "older-ch-ui")
		if err != nil {
			t.Fatal(err)
		}
		db.CreateModelRunResult(runID, id, "legacy")
		db.UpdateModelRunResult(runID, id, "success", "", 1, "")
		if err := run(id); err != nil {
			t.Fatalf("legacy adoption: %v (%s)", err, lastError(id))
		}
		if o := objects(atomicDB)["legacy"]; o.comment != OwnerComment(id) {
			t.Fatalf("adopted table not marked: %+v", o)
		}
	})

	t.Run("view, then switched to table", func(t *testing.T) {
		id := create("flip", atomicDB, "view", "SELECT 1 AS id -- trailing comment", "", "")
		if err := run(id); err != nil {
			t.Fatalf("view: %v (%s)", err, lastError(id))
		}
		if o := objects(atomicDB)["flip"]; o.engine != "View" || o.comment != OwnerComment(id) {
			t.Fatalf("unexpected view %+v", o)
		}
		if err := run(id); err != nil {
			t.Fatalf("view replace: %v (%s)", err, lastError(id))
		}
		if err := db.UpdateModel(id, "flip", "", atomicDB, "table", "SELECT 1 AS id", "MergeTree", ""); err != nil {
			t.Fatal(err)
		}
		if err := run(id); err != nil {
			t.Fatalf("view to table: %v (%s)", err, lastError(id))
		}
		objs := objects(atomicDB)
		if o := objs["flip"]; o.engine != "MergeTree" || o.comment != OwnerComment(id) {
			t.Fatalf("unexpected table %+v", o)
		}
		if _, ok := objs["flip__chui_tmp"]; ok {
			t.Fatal("tmp left behind")
		}
	})

	t.Run("rename fallback outside Atomic databases", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := client.Execute(ctx, "CREATE DATABASE "+ordinaryDB+" ENGINE = Ordinary", admin, adminPassword, map[string]string{"allow_deprecated_database_ordinary": "1"})
		cancel()
		if err != nil {
			t.Skipf("server refuses Ordinary databases: %v", err)
		}
		id := create("ord_model", ordinaryDB, "table", "SELECT 1 AS id", "MergeTree", "")
		if err := run(id); err != nil {
			t.Fatalf("first build: %v (%s)", err, lastError(id))
		}
		if err := db.UpdateModel(id, "ord_model", "", ordinaryDB, "table", "SELECT number AS id FROM numbers(4)", "MergeTree", ""); err != nil {
			t.Fatal(err)
		}
		if err := run(id); err != nil {
			t.Fatalf("rebuild: %v (%s)", err, lastError(id))
		}
		if got := count(ordinaryDB + ".ord_model"); got != "4" {
			t.Fatalf("rebuild not swapped in: %s rows", got)
		}
		objs := objects(ordinaryDB)
		if len(objs) != 1 {
			t.Fatalf("leftovers after rename swap: %v", objs)
		}
	})
}
