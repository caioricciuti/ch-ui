// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"encoding/json"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func TestBrainSavedQueryToolsRespectVisibility(t *testing.T) {
	db, err := dbtest.Open(t, "brain.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	shared, _ := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Shared", Query: "SELECT 1", CreatedBy: "alice"})
	private, _ := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Private", Query: "SELECT 2", CreatedBy: "alice", Visibility: database.SavedQueryPrivate})

	bob := Context{DB: db, Username: "bob", Role: "analyst", WorkspaceURL: "http://localhost"}
	out, err := listSavedQueries.Handler(bob, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	var listed struct {
		SavedQueries []struct {
			ID string `json:"id"`
		} `json:"saved_queries"`
	}
	_ = json.Unmarshal(raw, &listed)
	if len(listed.SavedQueries) != 1 || listed.SavedQueries[0].ID != shared {
		t.Fatalf("bob should list only the shared query: %s", raw)
	}

	args, _ := json.Marshal(map[string]string{"id": private, "sql": "SELECT 9"})
	if _, err := updateSavedQuery.Handler(bob, args); err == nil {
		t.Fatal("updating someone else's private query must fail")
	}
	args, _ = json.Marshal(map[string]string{"id": shared, "sql": "SELECT 9"})
	if _, err := updateSavedQuery.Handler(bob, args); err == nil {
		t.Fatal("an analyst must not update someone else's query")
	}
	idArg, _ := json.Marshal(map[string]string{"id": private})
	if _, err := deleteSavedQuery.Handler(bob, idArg); err == nil {
		t.Fatal("deleting someone else's private query must fail")
	}

	alice := Context{DB: db, Username: "alice", Role: "analyst", WorkspaceURL: "http://localhost"}
	args, _ = json.Marshal(map[string]string{"id": private, "sql": "SELECT 9"})
	if _, err := updateSavedQuery.Handler(alice, args); err != nil {
		t.Fatalf("owner update: %v", err)
	}
	versions, _ := db.ListSavedQueryVersions(private)
	if len(versions) != 2 {
		t.Fatalf("baseline plus update expected, got %d versions", len(versions))
	}
}
