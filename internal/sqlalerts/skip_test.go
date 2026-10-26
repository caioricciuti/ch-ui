// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package sqlalerts

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// Without a dedicated account an alert shows why it does not run, without
// counting an error or a run; once an account is set it runs and the
// reason clears.
func TestMissingAccountIsShownOnTheAlert(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	agent := valueAgent(t, db, &v, nil)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "a", SQL: "SELECT 1 AS v", Comparator: "gt", Threshold: 10, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", func() bool { return true })
	for i := 0; i < 2; i++ {
		r.tick()
	}
	a, err := db.GetSQLAlert(id)
	if err != nil || a.LastError == nil || !strings.Contains(*a.LastError, "dedicated background account") ||
		a.ErrorCount != 0 || a.LastRunAt != nil || a.State != alerts.StateOK || !a.Enabled {
		t.Fatalf("missing account: %+v %v", a, err)
	}
	if n := len(agent.Messages()); n != 0 {
		t.Fatalf("missing account: %d queries sent", n)
	}

	setAccount(t, db, conn, "service_account")
	r.tick()
	if a, err = db.GetSQLAlert(id); err != nil || a.LastError != nil || a.LastRunAt == nil || a.ErrorCount != 0 {
		t.Fatalf("with an account the alert runs: %+v %v", a, err)
	}
}
