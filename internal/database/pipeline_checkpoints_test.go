package database

import "testing"

func TestPipelineCheckpointsAndDLQRows(t *testing.T) {
	db := openTestDB(t)
	// The migration is idempotent.
	if err := db.migratePipelinesV2(); err != nil {
		t.Fatal(err)
	}
	mustHaveColumn(t, db, "pipelines", "dlq_rows")

	conn, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreatePipeline("p", "", conn, "u")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SavePipelineCheckpoint(id, "watermark", "1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePipelineCheckpoint(id, "watermark", "2"); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePipelineCheckpoint(id, "kafka:t/0", "10"); err != nil {
		t.Fatal(err)
	}
	m, err := db.PipelineCheckpointMap(id)
	if err != nil || len(m) != 2 || m["watermark"] != "2" {
		t.Fatalf("checkpoints = %v, %v", m, err)
	}

	if err := db.AddPipelineDLQRows(id, 3); err != nil {
		t.Fatal(err)
	}
	db.AddPipelineDLQRows(id, 2)
	if n, _ := db.GetPipelineDLQRows(id); n != 5 {
		t.Fatalf("dlq_rows = %d", n)
	}

	// Deleting the pipeline removes its checkpoints (ON DELETE CASCADE).
	if err := db.DeletePipeline(id); err != nil {
		t.Fatal(err)
	}
	var left int
	db.conn.QueryRow("SELECT COUNT(*) FROM pipeline_checkpoints WHERE pipeline_id = ?", id).Scan(&left)
	if left != 0 {
		t.Fatalf("%d checkpoints left after delete", left)
	}
}
