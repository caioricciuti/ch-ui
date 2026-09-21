// SPDX-License-Identifier: BUSL-1.1
package incidenttimeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestLocalTimelineConnectionIsolationAndPressure(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range database.IncidentTimelineSchema {
		if _, err := db.Conn().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	a, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range []string{a, b} {
		_, err = db.Conn().Exec(`INSERT INTO gov_incidents (id,connection_id,source_type,title,severity,status,first_seen_at,last_seen_at) VALUES (?,?,'manual',?,'warn','open','2026-09-21T10:00:00Z','2026-09-21T10:00:00Z')`, connection, connection, connection)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Conn().Exec(`INSERT INTO gov_incident_comments (id,incident_id,comment_text,created_by,created_at) VALUES (?,?,'note','person','2026-09-21 10:01:00')`, connection, connection)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.CreateIncidentAnnotation(context.Background(), database.IncidentAnnotation{ConnectionID: connection, OccurredAt: "2026-09-21T10:02:00.12Z", Title: connection, CreatedBy: "human"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Conn().Exec(`INSERT INTO ch_health_samples (connection_id,node,captured_at,replication_max_delay) VALUES (?,?,'2026-09-21T10:03:00Z',30.5)`, connection, connection)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Conn().Exec(`INSERT INTO ch_health_samples (connection_id,node,captured_at,replication_max_delay) VALUES (?,'normal','2026-09-21T10:04:00Z',1), (?,'outside','2026-09-21T09:59:59Z',60)`, a, a)
	if err != nil {
		t.Fatal(err)
	}
	w, err := ParseWindow("2026-09-21T10:00:00Z", "2026-09-21T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	store := Store{DB: db}
	for _, source := range []string{"incidents", "comments", "deployments", "health"} {
		rows, err := store.Local(context.Background(), a, source, w)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s: %+v %v", source, rows, err)
		}
		if source == "health" && rows[0].Values["replication_delay_seconds"] != 30.5 {
			t.Fatalf("fractional delay lost: %+v", rows)
		}
		if source == "incidents" && rows[0].IncidentID != a {
			t.Fatal("incident leaked across connection")
		}
		if source == "comments" && rows[0].IncidentID != a {
			t.Fatal("comment leaked across connection")
		}
		if source == "deployments" && rows[0].Title != a {
			t.Fatal("annotation leaked across connection")
		}
	}
	if exists, err := store.IncidentExists(context.Background(), a, b); err != nil || exists {
		t.Fatalf("cross connection lookup: %v %v", exists, err)
	}
}
