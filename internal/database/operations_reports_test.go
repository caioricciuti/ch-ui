package database

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func reportTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{"report-a", "report-b"} {
		if _, err = db.conn.Exec(`INSERT INTO connections(id,name,tunnel_token) VALUES(?,?,?)`, id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestReportScheduleIdempotencyAndConnectionScope(t *testing.T) {
	db := reportTestDB(t)
	first, err := db.SaveOperationsReport(OperationsReport{ConnectionID: "report-a", Body: "original", Payload: json.RawMessage(`{}`)}, "2026-09-21T09:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := db.SaveOperationsReport(OperationsReport{ConnectionID: "report-a", Body: "changed", Payload: json.RawMessage(`{}`)}, "2026-09-21T09:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != retry.ID || retry.Body != "original" {
		t.Fatal("retry must retain original report snapshot")
	}
	if other, err := db.GetOperationsReport("report-b", first.ID); err != nil || other != nil {
		t.Fatalf("cross-connection read: %v %v", other, err)
	}
	if err := db.QueueOperationsReport("report-b", first.ID, "channel", []string{"ops@example.com"}); err != nil {
		t.Fatal(err)
	}
	stored, _ := db.GetOperationsReport("report-a", first.ID)
	if stored.DeliveryStatus != "not_requested" {
		t.Fatal("cross-connection queue modified report")
	}
}

func TestReportDeliveryLeaseRetryAndDisable(t *testing.T) {
	db := reportTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SaveOperationsReportSettings(OperationsReportSettings{ConnectionID: "report-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	settings, _ := db.GetOperationsReportSettings("report-a")
	r, err := db.SaveOperationsReport(OperationsReport{ConnectionID: "report-a", CreatedAt: now.Format(time.RFC3339), Payload: json.RawMessage(`{}`), DeliveryStatus: "queued", ScheduledDelivery: true, ScheduleRevision: settings.Revision}, "weekly")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimOperationsReportDelivery(r.ID, now)
	if err != nil || !claimed {
		t.Fatalf("first claim: %v %v", claimed, err)
	}
	claimed, err = db.ClaimOperationsReportDelivery(r.ID, now)
	if err != nil || claimed {
		t.Fatal("delivery claimed twice")
	}
	due, err := db.DueOperationsReportDeliveries(now.Add(time.Minute))
	if err != nil || len(due) != 0 {
		t.Fatal("in-flight delivery must remain leased")
	}
	due, err = db.DueOperationsReportDeliveries(now.Add(6 * time.Minute))
	if err != nil || len(due) != 1 {
		t.Fatal("crashed sender lease must expire")
	}
	if err = db.CompleteOperationsReportDelivery(r.ID, "retry", "temporary", now); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveOperationsReportSettings(OperationsReportSettings{ConnectionID: "report-a", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	due, err = db.DueOperationsReportDeliveries(now)
	if err != nil || len(due) != 0 {
		t.Fatal("disabled schedule must cancel pending delivery")
	}
}

func TestReportScheduleEditWhileGeneratingOrSending(t *testing.T) {
	db := reportTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SaveOperationsReportSettings(OperationsReportSettings{ConnectionID: "report-a", Enabled: true, NextRunAt: "old"}); err != nil {
		t.Fatal(err)
	}
	original, _ := db.GetOperationsReportSettings("report-a")
	inFlight, err := db.SaveOperationsReport(OperationsReport{ConnectionID: "report-a", CreatedAt: now.Format(time.RFC3339), Payload: json.RawMessage(`{}`), DeliveryStatus: "queued", ScheduledDelivery: true, ScheduleRevision: original.Revision}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimOperationsReportDelivery(inFlight.ID, now); err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if err := db.SaveOperationsReportSettings(OperationsReportSettings{ConnectionID: "report-a", Enabled: false, NextRunAt: "changed"}); err != nil {
		t.Fatal(err)
	}
	// Generation finishing after a settings change must not queue an old email.
	late, err := db.SaveOperationsReport(OperationsReport{ConnectionID: "report-a", Payload: json.RawMessage(`{}`), DeliveryStatus: "queued", ScheduledDelivery: true, ScheduleRevision: original.Revision}, "second")
	if err != nil || late.DeliveryStatus != "canceled" {
		t.Fatalf("late report: %+v %v", late, err)
	}
	if err := db.CompleteOperationsReportDelivery(inFlight.ID, "retry", "temporary failure", now); err != nil {
		t.Fatal(err)
	}
	stored, _ := db.GetOperationsReport("report-a", inFlight.ID)
	if stored.DeliveryStatus != "canceled" {
		t.Fatal("disabled in-flight delivery must not retry")
	}
	if err := db.OperationsReportAttempt("report-a", original.Revision, "overwritten", "", now); err != nil {
		t.Fatal(err)
	}
	settings, _ := db.GetOperationsReportSettings("report-a")
	if settings.NextRunAt != "changed" || settings.Enabled {
		t.Fatal("old worker overwrote new settings")
	}
	// Explicit manual sending remains possible while schedules are disabled.
	if err := db.QueueOperationsReport("report-a", late.ID, "channel", []string{"ops@example.com"}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimOperationsReportDelivery(late.ID, now.Add(time.Second)); err != nil || !claimed {
		t.Fatalf("manual claim: %v %v", claimed, err)
	}
}

func TestReportFinalInterruptedDeliveryBecomesFailed(t *testing.T) {
	db := reportTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	r, err := db.SaveOperationsReport(OperationsReport{ConnectionID: "report-a", CreatedAt: now.Format(time.RFC3339), Payload: json.RawMessage(`{}`), DeliveryStatus: "queued"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		claimed, err := db.ClaimOperationsReportDelivery(r.ID, now.Add(time.Duration(i)*6*time.Minute))
		if err != nil || !claimed {
			t.Fatalf("claim %d failed", i)
		}
	}
	due, err := db.DueOperationsReportDeliveries(now.Add(time.Hour))
	if err != nil || len(due) != 0 {
		t.Fatal("final interrupted attempt should stop retrying")
	}
	stored, _ := db.GetOperationsReport("report-a", r.ID)
	if stored.DeliveryStatus != "failed" {
		t.Fatalf("status %s", stored.DeliveryStatus)
	}
}
