package pipelines

import (
	"context"
	"encoding/json"
	"strings"
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
