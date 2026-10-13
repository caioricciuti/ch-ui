// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts_test

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/sqlalerts"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TestSQLAlertReachesChannel runs a SQL alert into firing and checks that a
// sql.* rule turns its event into a dispatch job on the rule's channel.
func TestSQLAlertReachesChannel(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"errors":"42"}]`)}
	})
	channelID, err := db.CreateAlertChannel("ops", alerts.ChannelTypeWebhook, "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("sql", "sql.*", alerts.SeverityWarn, true, 300, 3, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	a := database.SQLAlert{ConnectionID: conn, Name: "errors", SQL: "SELECT count() AS errors FROM logs", Comparator: "gt", Threshold: 10, Severity: "error", Enabled: true}
	if err := sqlalerts.ValidateAlert(&a); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSQLAlert(&a)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := db.GetSQLAlert(id)
	out, err := sqlalerts.NewRunner(db, agent.Gateway, nil, "test-secret", nil).Evaluate(saved, sqlalerts.Credentials{User: "u"})
	if err != nil || out.State != alerts.StateFiring {
		t.Fatalf("evaluate: %+v %v", out, err)
	}
	alerts.MaterializeForTest(db)
	jobs, err := db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ChannelID != channelID || jobs[0].EventType != alerts.EventTypeSQLAlert {
		t.Fatalf("want one sql.alert job on the channel, got %+v", jobs)
	}
}

// TestRefireInsideCooldownIsDelivered checks that an alert that fires,
// resolves and fires again inside the rule cooldown still delivers the
// second firing: each incident has its own fingerprint.
func TestRefireInsideCooldownIsDelivered(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var value atomic.Int64
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(fmt.Sprintf(`[{"v":"%d"}]`, value.Load()))}
	})
	channelID, err := db.CreateAlertChannel("ops", alerts.ChannelTypeWebhook, "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("sql", "sql.*", alerts.SeverityWarn, true, 3600, 3, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	a := database.SQLAlert{ConnectionID: conn, Name: "v", SQL: "SELECT 1 AS v", Comparator: "gt", Threshold: 10, Severity: "error",
		ForEvaluations: 1, ResolveEvaluations: 1, Enabled: true}
	if err := sqlalerts.ValidateAlert(&a); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSQLAlert(&a)
	if err != nil {
		t.Fatal(err)
	}
	runner := sqlalerts.NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	for _, v := range []int64{50, 1, 50} {
		value.Store(v)
		saved, _ := db.GetSQLAlert(id)
		if _, err := runner.Evaluate(saved, sqlalerts.Credentials{User: "u"}); err != nil {
			t.Fatal(err)
		}
		alerts.MaterializeForTest(db)
	}
	jobs, err := db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 {
		t.Fatalf("want firing, resolved and the second firing delivered, got %d jobs", len(jobs))
	}
}
