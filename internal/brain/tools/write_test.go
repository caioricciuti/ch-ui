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

func TestCreateModelValidates(t *testing.T) {
	db, err := dbtest.Open(t, "brain.db")
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

	for _, mat := range []string{"ephemeral", "snapshot"} {
		if _, err := call(base("m_"+mat, mat)); err == nil {
			t.Errorf("%s must be rejected", mat)
		}
	}
	bad := base("inc_bad", "incremental")
	bad["incremental_strategy"] = "delete_insert"
	if _, err := call(bad); err == nil {
		t.Error("delete_insert without unique_key must be rejected")
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

	raw := json.RawMessage(`{"name":"inc","target_database":"default","materialization":"incremental","sql_body":"SELECT 1 AS id",
		"incremental_strategy":"delete_insert","unique_key":"id","watermark_column":"id",
		"tests":[{"kind":"unique","column":"id"},{"kind":"accepted_values","column":"id","values":["1"],"severity":"warn"}]}`)
	res, err := createModel.Handler(tctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	id := res.(map[string]any)["model_id"].(string)
	if m, _ := db.GetModelByID(id); m == nil || m.IncrementalStrategy != "delete_insert" || m.UniqueKey != "id" {
		t.Fatalf("incremental options not stored: %+v", m)
	}
	if tests, _ := db.GetModelTests(id); len(tests) != 2 || tests[1].Severity != "warn" {
		t.Fatalf("tests not stored: %+v", tests)
	}
	bad2 := json.RawMessage(`{"name":"inc2","target_database":"default","materialization":"view","sql_body":"SELECT 1","tests":[{"kind":"relationship","column":"id"}]}`)
	if _, err := createModel.Handler(tctx, bad2); err == nil {
		t.Error("a relationship test without to/field must be rejected")
	}
	if m, _ := db.GetModelByName(conn, "inc2"); m != nil {
		t.Error("a model with an invalid test must not be created")
	}
}

func TestUpdateModelRenameRewritesPipelines(t *testing.T) {
	db, err := dbtest.Open(t, "brain.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateModel(conn, "orders", "", "default", "view", "SELECT 1 AS id", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := db.CreateModelPipeline(database.ModelPipeline{ConnectionID: conn, Name: "nightly", Selector: "+orders+"})
	if err != nil {
		t.Fatal(err)
	}
	tctx := Context{ConnectionID: conn, DB: db, WorkspaceURL: "http://localhost"}
	if _, err := updateModel.Handler(tctx, json.RawMessage(`{"id":"`+id+`","name":"sales","unique_key":"id"}`)); err != nil {
		t.Fatal(err)
	}
	if m, _ := db.GetModelByID(id); m == nil || m.Name != "sales" || m.UniqueKey != "id" {
		t.Fatalf("model not updated: %+v", m)
	}
	if p, _ := db.GetModelPipelineByID(pid); p == nil || p.Selector != "+sales+" {
		t.Fatalf("the rename must rewrite the pipeline selector, got %+v", p)
	}
}
