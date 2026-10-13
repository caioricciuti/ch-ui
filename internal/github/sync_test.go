// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// fakeGitHub serves one commit; only requests made at that SHA succeed.
type fakeGitHub struct {
	mu        sync.Mutex
	sha       string
	files     map[string]string
	truncated bool
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/repos/o/r/branches/main":
		json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"sha": f.sha}})
	case r.URL.Path == "/repos/o/r/git/trees/"+f.sha:
		var tree []TreeEntry
		for p := range f.files {
			tree = append(tree, TreeEntry{Path: p, Type: "blob"})
		}
		json.NewEncoder(w).Encode(map[string]any{"tree": tree, "truncated": f.truncated})
	case strings.HasPrefix(r.URL.Path, "/repos/o/r/contents/") && r.URL.Query().Get("ref") == f.sha:
		content, ok := f.files[strings.TrimPrefix(r.URL.Path, "/repos/o/r/contents/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
	default:
		http.NotFound(w, r)
	}
}

func TestSyncRecordsTheCommitOnlyWhenClean(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	pat, err := crypto.Encrypt("token", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveGitHubIntegration(conn, map[string]string{"repo": "o/r", "encrypted_pat": pat}); err != nil {
		t.Fatal(err)
	}
	// A GitHub model whose file will fail to parse must survive the sync.
	keepID, err := db.CreateModel(conn, "keep", "", "default", "view", "SELECT 0", "MergeTree", "tuple()", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetModelSource(keepID, "github"); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGitHub{sha: "sha1", files: map[string]string{
		"models/good.sql":     "SELECT 1 AS id",
		"models/bad-name.sql": "SELECT 2",
		"models/keep.sql":     "---\nmaterialization: ephemeral\n---\nSELECT 3",
	}}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	s := &Syncer{db: db, secret: "secret", apiBase: srv.URL}
	lastSHA := func() string {
		_, _, _, _, _, sha := db.GetGitHubRawSettings(conn)
		return sha
	}

	res, err := s.SyncConnection(conn, "test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || len(res.Errors) != 2 {
		t.Fatalf("want 1 created and 2 errors, got %+v", res)
	}
	joined := strings.Join(res.Errors, "; ")
	if !strings.Contains(joined, "bad-name") || !strings.Contains(joined, "materialization") {
		t.Fatalf("errors should name the bad file and the bad materialization: %s", joined)
	}
	if got := lastSHA(); got != "" {
		t.Fatalf("a sync with errors must not record the commit, recorded %q", got)
	}
	if m, _ := db.GetModelByID(keepID); m == nil {
		t.Fatal("a model whose file failed to parse was deleted")
	}

	gh.mu.Lock()
	delete(gh.files, "models/bad-name.sql")
	gh.files["models/keep.sql"] = "---\nmaterialization: table\n---\nSELECT 3"
	gh.mu.Unlock()
	res, err = s.SyncConnection(conn, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 || res.Updated != 1 {
		t.Fatalf("clean retry of the same commit: %+v", res)
	}
	if got := lastSHA(); got != "sha1" {
		t.Fatalf("clean sync must record the commit, got %q", got)
	}
	if m, _ := db.GetModelByID(keepID); m == nil || m.Materialization != "table" || m.OrderBy != "tuple()" {
		t.Fatalf("keep not updated: %+v", m)
	}

	gh.mu.Lock()
	gh.sha, gh.truncated = "sha2", true
	gh.mu.Unlock()
	if _, err := s.SyncConnection(conn, "test"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("a truncated tree must fail the sync, got %v", err)
	}
	if got := lastSHA(); got != "sha1" {
		t.Fatalf("failed sync moved the recorded commit to %q", got)
	}
}

func TestParseModelFileValidation(t *testing.T) {
	if _, err := ParseModelFile("models/my-model.sql", "SELECT 1"); err == nil {
		t.Error("a file name that is not an identifier must be rejected")
	}
	if _, err := ParseModelFile("models/m.sql", "---\nmaterialization: ephemeral\n---\nSELECT 1"); err == nil {
		t.Error("unknown materialization must be rejected")
	}
	if _, err := ParseModelFile("models/m.sql", "---\nmaterialization: incremental\nincremental_strategy: delete_insert\n---\nSELECT 1"); err == nil {
		t.Error("delete_insert without unique_key must be rejected")
	}
	if _, err := ParseModelFile("models/m.sql", "---\ntests:\n  - kind: accepted_values\n    column: status\n---\nSELECT 1"); err == nil {
		t.Error("a test without its values must be rejected")
	}
	p, err := ParseModelFile("models/m.sql", "---\nmaterialization: table\norder_by: ''\n---\nSELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "m" || p.Frontmatter.TableEngine != "MergeTree" || p.Frontmatter.OrderBy != "tuple()" {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}

func TestParseModelFileOptionsAndTests(t *testing.T) {
	p, err := ParseModelFile("models/events.sql", `---
materialization: incremental
incremental_strategy: delete_insert
unique_key: id
watermark_column: updated_at
table_engine: ReplacingMergeTree
engine_args: updated_at
partition_by: toYYYYMM(updated_at)
order_by: id
tests:
  - kind: not_null
    column: id
  - kind: accepted_values
    column: status
    values: [paid, refunded]
    severity: warn
  - kind: relationship
    column: user_id
    to: users
    field: id
  - kind: expression
    expression: amount >= 0
---
SELECT * FROM raw.events`)
	if err != nil {
		t.Fatal(err)
	}
	m := p.Model()
	if m.IncrementalStrategy != "delete_insert" || m.UniqueKey != "id" || m.EngineArgs != "updated_at" || m.PartitionBy != "toYYYYMM(updated_at)" {
		t.Fatalf("options: %+v", m)
	}
	if len(p.Tests) != 4 || p.Tests[1].Severity != "warn" || p.Tests[1].Args != `{"values":["paid","refunded"]}` || p.Tests[2].Args != `{"to":"users","field":"id"}` || p.Tests[3].Args != `{"expression":"amount >= 0"}` {
		t.Fatalf("tests: %+v", p.Tests)
	}
}

func TestSyncCreatesModelAndTestsTogether(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	pat, err := crypto.Encrypt("token", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveGitHubIntegration(conn, map[string]string{"repo": "o/r", "encrypted_pat": pat}); err != nil {
		t.Fatal(err)
	}
	// Writing the tests fails, after the model row would have been written.
	if _, err := db.Conn().Exec(`CREATE TRIGGER fail_tests BEFORE INSERT ON model_tests BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{sha: "sha1", files: map[string]string{
		"models/tested.sql": "---\nmaterialization: table\npartition_by: id\ntests:\n  - kind: not_null\n    column: id\n---\nSELECT 1 AS id",
	}}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	s := &Syncer{db: db, secret: "secret", apiBase: srv.URL}

	res, err := s.SyncConnection(conn, "test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 0 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "injected") {
		t.Fatalf("want the create to fail on its tests, got %+v", res)
	}
	if m, _ := db.GetModelByName(conn, "tested"); m != nil {
		t.Fatalf("a failed create left a half-created model: %+v", m)
	}

	if _, err := db.Conn().Exec(`DROP TRIGGER fail_tests`); err != nil {
		t.Fatal(err)
	}
	res, err = s.SyncConnection(conn, "test")
	if err != nil || res.Created != 1 || len(res.Errors) != 0 {
		t.Fatalf("retry: %+v %v", res, err)
	}
	m, _ := db.GetModelByName(conn, "tested")
	if m == nil || m.Source != "github" || m.PartitionBy != "id" {
		t.Fatalf("model not created with its source and options: %+v", m)
	}
	if ts, _ := db.GetModelTests(m.ID); len(ts) != 1 || ts[0].Kind != "not_null" {
		t.Fatalf("tests not created: %+v", ts)
	}
}
