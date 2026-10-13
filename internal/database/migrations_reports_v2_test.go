// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"testing"
	"time"
)

// TestMigrateReportsV2Idempotent runs the report migration again on a
// migrated database, keeps the stored rows, and checks the cascades.
func TestMigrateReportsV2Idempotent(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	queryID, err := db.CreateSavedQuery(CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: connID})
	if err != nil {
		t.Fatal(err)
	}
	channelID, err := db.CreateAlertChannel("mail", "smtp", "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateReportSubscription(ReportSubscription{
		Name: "daily", SourceType: "saved_query", SavedQueryID: &queryID, ConnectionID: connID, Cron: "0 8 * * *", Timezone: "UTC",
		Enabled: true, Format: "csv", RowLimit: 1000, InlineRows: 50, MaxBytes: 1 << 20, TimeoutSeconds: 60,
		Targets: []ReportTarget{{ChannelID: channelID, Recipients: []string{"a@example.com"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deliveryID, err := db.CreateReportDelivery(id, "schedule", "reporter", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishReportDelivery(deliveryID, ReportDeliveryResult{Status: "sending", Snapshot: []byte(`{}`), Targets: []string{channelID}}, time.Now()); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := db.migrateReportsV2(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := db.runMigrations(); err != nil {
		t.Fatalf("full rerun: %v", err)
	}

	s, err := db.GetReportSubscription(id)
	if err != nil || s == nil || s.Name != "daily" || len(s.Targets) != 1 || s.Targets[0].Recipients[0] != "a@example.com" {
		t.Fatalf("stored subscription lost: %+v %v", s, err)
	}
	d, err := db.GetReportDelivery(deliveryID)
	if err != nil || d == nil || len(d.Targets) != 1 || string(d.Snapshot) != "{}" {
		t.Fatalf("stored delivery lost: %+v %v", d, err)
	}

	// Deleting the saved query removes the report and its history.
	mustExec(t, db, `DELETE FROM saved_queries WHERE id = ?`, queryID)
	for _, table := range []string{"report_subscriptions", "report_targets", "report_deliveries", "report_delivery_targets"} {
		if n := countRows(t, db, table); n != 0 {
			t.Errorf("%s keeps %d rows after the saved query is deleted", table, n)
		}
	}
}

func TestPruneReportDeliveries(t *testing.T) {
	db := openTestDB(t)
	connID, _ := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	queryID, _ := db.CreateSavedQuery(CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: connID})
	id, err := db.CreateReportSubscription(ReportSubscription{Name: "r", SourceType: "saved_query", SavedQueryID: &queryID, ConnectionID: connID, Cron: "* * * * *", Timezone: "UTC", Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old, _ := db.CreateReportDelivery(id, "schedule", "", now.AddDate(0, 0, -100))
	week, _ := db.CreateReportDelivery(id, "schedule", "", now.AddDate(0, 0, -8))
	fresh, _ := db.CreateReportDelivery(id, "schedule", "", now)
	for _, d := range []struct {
		id string
		at time.Time
	}{{old, now.AddDate(0, 0, -100)}, {week, now.AddDate(0, 0, -8)}, {fresh, now}} {
		if err := db.FinishReportDelivery(d.id, ReportDeliveryResult{Status: "delivered", Snapshot: []byte(`{"x":1}`), Finished: true}, d.at); err != nil {
			t.Fatal(err)
		}
	}
	cleared, deleted, err := db.PruneReportDeliveries(now.Add(-7*24*time.Hour), now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || cleared != 2 {
		t.Fatalf("cleared %d deleted %d, want 2 and 1", cleared, deleted)
	}
	if d, _ := db.GetReportDelivery(week); d == nil || d.HasSnapshot {
		t.Fatal("week-old snapshot should be cleared, row kept")
	}
	if d, _ := db.GetReportDelivery(fresh); d == nil || !d.HasSnapshot {
		t.Fatal("fresh snapshot must be kept")
	}
}
