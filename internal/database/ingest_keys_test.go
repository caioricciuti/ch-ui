package database

import (
	"testing"
	"time"
)

func TestIngestKeysLifecycle(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := db.CreatePipeline("p", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	k, err := db.CreateIngestKey(pid, "collector", "hash-1", "abcdefgh", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateIngestKey(pid, "dup", "hash-1", "abcdefgh", "alice"); err == nil {
		t.Fatal("a duplicate hash was accepted")
	}

	got, err := db.GetIngestKeyByHash("hash-1")
	if err != nil || got == nil || got.ID != k.ID || got.PipelineID != pid || got.RevokedAt != nil {
		t.Fatalf("lookup by hash: %+v, %v", got, err)
	}
	if missing, err := db.GetIngestKeyByHash("nope"); err != nil || missing != nil {
		t.Fatalf("unknown hash: %+v, %v", missing, err)
	}

	if err := db.TouchIngestKey(k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.RevokeIngestKey("other-pipeline", k.ID); err != nil || ok {
		t.Fatalf("revoke through another pipeline: %v, %v", ok, err)
	}
	if ok, err := db.RevokeIngestKey(pid, k.ID); err != nil || !ok {
		t.Fatalf("revoke: %v, %v", ok, err)
	}
	if ok, _ := db.RevokeIngestKey(pid, k.ID); ok {
		t.Fatal("second revoke reported success")
	}
	got, _ = db.GetIngestKeyByHash("hash-1")
	if got == nil || got.RevokedAt == nil || got.LastUsedAt == nil {
		t.Fatalf("revoked key: %+v", got)
	}

	list, err := db.ListIngestKeys(pid)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, %v", list, err)
	}

	// Keys go with their pipeline.
	if err := db.DeletePipeline(pid); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetIngestKeyByHash("hash-1"); got != nil {
		t.Fatal("key survived its pipeline")
	}
}
