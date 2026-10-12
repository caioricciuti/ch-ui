// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestCreateModelValidates(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	tctx := Context{ConnectionID: conn, DB: db, WorkspaceURL: "http://localhost"}
	call := func(args map[string]string) (map[string]any, error) {
		raw, _ := json.Marshal(args)
		out, err := createModel.Handler(tctx, raw)
		if err != nil {
			return nil, err
		}
		return out.(map[string]any), nil
	}
	base := func(name, mat string) map[string]string {
		return map[string]string{"name": name, "target_database": "default", "materialization": mat, "sql_body": "SELECT 1 AS id"}
	}

	for _, mat := range []string{"incremental", "materialized_view"} {
		if _, err := call(base("m_"+mat, mat)); err == nil {
			t.Errorf("%s must be rejected", mat)
		}
	}
	if _, err := call(base("bad-name", "view")); err == nil {
		t.Error("an invalid name must be rejected")
	}

	out, err := call(base("tbl", "table"))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := db.GetModelByID(out["model_id"].(string))
	if m == nil || m.TableEngine != "MergeTree" || m.OrderBy != "tuple()" {
		t.Fatalf("table without order_by must default to tuple(), got %+v", m)
	}

	args := base("mem", "table")
	args["table_engine"] = "Memory"
	out, err = call(args)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := db.GetModelByID(out["model_id"].(string)); m == nil || m.OrderBy != "" {
		t.Fatalf("Memory takes no ORDER BY, got %+v", m)
	}
}
