package governance

import (
	"fmt"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func TestPruneRetentionFollowsRetentionConfig(t *testing.T) {
	db, err := dbtest.Open(t, "gov-retention.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Conn().Exec(`INSERT INTO connections (id, name, tunnel_token, status) VALUES ('conn-1', 'Local', 'token-1', 'connected')`); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	store := NewStore(db)
	syncer := NewSyncer(store, db, nil, "", nil)
	conns := []database.Connection{{ID: "conn-1"}}

	now := time.Now().UTC()
	var entries []QueryLogEntry
	for _, days := range []int{400, 45, 1} {
		entries = append(entries, QueryLogEntry{
			ConnectionID: "conn-1", QueryID: fmt.Sprintf("q-%d", days), User: "default", QueryText: "SELECT 1",
			QueryKind: "Select", EventTime: now.AddDate(0, 0, -days).Format("2006-01-02 15:04:05"), TablesUsed: "[]",
		})
	}
	if _, err := store.InsertQueryLogBatch(entries); err != nil {
		t.Fatalf("insert query log: %v", err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM gov_query_log WHERE connection_id = 'conn-1'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 0 keeps the query log forever.
	cfg := database.DefaultRetentionConfig()
	cfg.GovQueryLog = 0
	if err := db.SetRetentionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	syncer.pruneRetention(conns)
	if n := count(); n != 3 {
		t.Fatalf("retention 0 must keep every row, %d left", n)
	}

	// A 60 day window drops only the 400 day old row.
	cfg.GovQueryLog = 60
	if err := db.SetRetentionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	syncer.pruneRetention(conns)
	if n := count(); n != 2 {
		t.Fatalf("60 day window: %d left, want 2", n)
	}

	// The default (30 days) drops the 45 day old row too.
	if err := db.SetRetentionConfig(database.DefaultRetentionConfig()); err != nil {
		t.Fatal(err)
	}
	syncer.pruneRetention(conns)
	if n := count(); n != 1 {
		t.Fatalf("default window: %d left, want 1", n)
	}
}
