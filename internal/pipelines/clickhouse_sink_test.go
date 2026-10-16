package pipelines

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestQuoteIdent(t *testing.T) {
	cases := map[string]string{
		"events":    "`events`",
		"a`b":       "`a\\`b`",
		`a\b`:       "`a\\\\b`",
		"x` ; DROP": "`x\\` ; DROP`",
	}
	for in, want := range cases {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestValidateExprAndEngine(t *testing.T) {
	for _, ok := range []string{"", "tuple()", "(user_id, ts)", "toYYYYMM(ts)", "intHash32(id) % 16", "`my col`", "concat(a, ';')"} {
		if err := validateExpr("ORDER BY", ok); err != nil {
			t.Errorf("rejected %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"id; DROP TABLE x", "id -- c", "id /* c */", "(id", "id)", "'open", "id AS SELECT * FROM system.users", "id SETTINGS x=1", "id\nTTL ts"} {
		if err := validateExpr("ORDER BY", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, ok := range []string{"", "MergeTree", "ReplacingMergeTree", "MergeTree()"} {
		if _, err := tableEngine(ok); err != nil {
			t.Errorf("engine %q rejected", ok)
		}
	}
	for _, bad := range []string{"Log", "MergeTree ORDER BY x; DROP", "ReplacingMergeTree(ver)", "URL('http://x')"} {
		if _, err := tableEngine(bad); err == nil {
			t.Errorf("engine %q accepted", bad)
		}
	}
}

func pipelineAccount(t *testing.T, db *database.DB, conn string) {
	t.Helper()
	enc, err := crypto.Encrypt("", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "pipeline", Mode: "service_account", Username: "default", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
}

// A failed auto-create is retried on the next batch, and the dedup token is
// sent as a query setting.
func TestSinkRetriesTableCreationAndSendsDedupToken(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var creates atomic.Int32
	var tokens []string
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.HasPrefix(msg.SQL, "CREATE TABLE") && creates.Add(1) == 1 {
			return &tunnel.AgentMessage{Type: "query_error", Error: "temporary failure"}
		}
		if strings.HasPrefix(msg.SQL, "INSERT") {
			tokens = append(tokens, msg.Settings["insert_deduplication_token"])
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	pipelineAccount(t, db, conn)
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d`b", "table": "t", "create_table": true}}
	batch := Batch{Records: []Record{{Data: map[string]interface{}{"v": float64(1)}, RawJSON: []byte("{\n\"v\": 1}")}}, DedupToken: "tok-1"}
	if _, err := sink.WriteBatch(context.Background(), cfg, batch); err == nil {
		t.Fatal("first write should fail on CREATE")
	}
	if _, err := sink.WriteBatch(context.Background(), cfg, batch); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if creates.Load() != 2 {
		t.Fatalf("CREATE attempts = %d, want 2", creates.Load())
	}
	if len(tokens) != 1 || tokens[0] != "tok-1" {
		t.Fatalf("dedup tokens sent: %v", tokens)
	}
	var insert string
	for _, m := range agent.Messages() {
		if strings.HasPrefix(m.SQL, "INSERT") {
			insert = m.SQL
		}
	}
	if !strings.HasPrefix(insert, "INSERT INTO `d\\`b`.`t` FORMAT JSONEachRow\n{\"v\":1}\n") {
		t.Fatalf("insert not escaped/compacted: %q", insert)
	}
}

// Plain MergeTree tables the sink creates keep a dedup window, so a
// replayed batch is dropped by its token; an insert without a token turns
// deduplication off so identical batches are kept. Replicated engines and
// existing tables are left as they are.
func TestSinkCreatedTablesDeduplicateByToken(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var mu sync.Mutex
	inserts := map[string]map[string]string{}
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.HasPrefix(msg.SQL, "INSERT") {
			mu.Lock()
			inserts[strings.SplitN(msg.SQL, " ", 4)[2]] = msg.Settings
			mu.Unlock()
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	pipelineAccount(t, db, conn)
	write := func(table, engine string, create bool, token string) {
		t.Helper()
		sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
		cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": table, "create_table": create, "create_table_engine": engine}}
		if _, err := sink.WriteBatch(context.Background(), cfg, Batch{Records: []Record{{Data: map[string]interface{}{"v": float64(1)}, RawJSON: []byte(`{"v":1}`)}}, DedupToken: token}); err != nil {
			t.Fatal(err)
		}
	}
	write("plain", "", true, "tok")
	write("notoken", "MergeTree", true, "")
	write("repl", "ReplicatedMergeTree", true, "")
	write("existing", "", false, "")

	creates := map[string]string{}
	for _, m := range agent.Messages() {
		if strings.HasPrefix(m.SQL, "CREATE TABLE") {
			creates[strings.SplitN(m.SQL, " ", 7)[5]] = m.SQL
		}
	}
	if c := creates["`d`.`plain`"]; !strings.HasSuffix(c, "ORDER BY tuple()\nSETTINGS non_replicated_deduplication_window = 1000") {
		t.Fatalf("plain MergeTree create:\n%s", c)
	}
	if c := creates["`d`.`repl`"]; c == "" || strings.Contains(c, "SETTINGS") {
		t.Fatalf("replicated create:\n%s", c)
	}
	mu.Lock()
	defer mu.Unlock()
	if s := inserts["`d`.`plain`"]; s["insert_deduplication_token"] != "tok" || s["insert_deduplicate"] != "" {
		t.Fatalf("insert with a token: %v", s)
	}
	if s := inserts["`d`.`notoken`"]; s["insert_deduplicate"] != "0" {
		t.Fatalf("insert without a token: %v", s)
	}
	for _, table := range []string{"`d`.`repl`", "`d`.`existing`"} {
		if s := inserts[table]; s["insert_deduplicate"] != "" {
			t.Fatalf("%s: %v", table, s)
		}
	}
}
