package database

import (
	"testing"
	"time"
)

// A transaction that reads and then writes must wait for a concurrent writer
// instead of failing with SQLITE_BUSY. With deferred transactions the other
// connection's commit lands between the read and the write, and SQLite
// refuses to upgrade the stale read snapshot without honouring busy_timeout.
func TestReadThenWriteTransactionWaitsForConcurrentWriter(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(`CREATE TABLE txlock_probe (id INTEGER PRIMARY KEY, n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`INSERT INTO txlock_probe (id, n) VALUES (1, 0), (2, 0)`); err != nil {
		t.Fatal(err)
	}

	tx, err := db.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT n FROM txlock_probe WHERE id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}

	// Another connection writes while the transaction is open.
	done := make(chan error, 1)
	go func() {
		_, err := db.conn.Exec(`UPDATE txlock_probe SET n = n + 1 WHERE id = 2`)
		done <- err
	}()
	// Give that write the chance to commit first, as it would with deferred
	// transactions; with immediate ones it waits for this transaction.
	select {
	case err := <-done:
		done <- err
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := tx.Exec(`UPDATE txlock_probe SET n = ? WHERE id = 1`, n+1); err != nil {
		t.Fatalf("write after read in a transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("concurrent writer: %v", err)
	}
}
