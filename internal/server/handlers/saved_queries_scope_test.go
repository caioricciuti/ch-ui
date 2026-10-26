package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// The saved query library is shared across connections, but a query
// created or changed through a session is bound to the session's
// connection, never to another one.
func TestSavedQueryConnectionIsTheSessions(t *testing.T) {
	f := newSQFixture(t)
	other, err := f.db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	connOf := func(id string) string {
		t.Helper()
		q, err := f.db.GetSavedQueryByID(id)
		if err != nil || q == nil {
			t.Fatalf("saved query %s: %v", id, err)
		}
		return deref(q.ConnectionID)
	}

	rec := f.as(t, "alice", "analyst", http.MethodPost, "/", `{"name":"x","query":"SELECT 1","connection_id":"`+other+`"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Switch to the other connection") {
		t.Fatalf("create on another connection: %d %s, want 400", rec.Code, rec.Body.String())
	}
	rec = f.as(t, "alice", "analyst", http.MethodPost, "/", `{"name":"mine","query":"SELECT 1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created database.SavedQuery
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if got := connOf(created.ID); got != f.connID {
		t.Fatalf("new query on %q, want the session connection %q", got, f.connID)
	}

	rec = f.as(t, "alice", "analyst", http.MethodPut, "/"+created.ID, `{"connection_id":"`+other+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("move to another connection: %d %s, want 400", rec.Code, rec.Body.String())
	}
	// An empty connection_id leaves the query where it is.
	rec = f.as(t, "alice", "analyst", http.MethodPut, "/"+created.ID, `{"name":"renamed","connection_id":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if got := connOf(created.ID); got != f.connID {
		t.Fatalf("query moved to %q, want %q", got, f.connID)
	}

	// A query bound to another connection stays editable from here (the
	// library is shared) but cannot be sent to a third one, and a copy is
	// bound to the caller's connection.
	theirs, err := f.db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "theirs", Query: "SELECT 2", ConnectionID: other, CreatedBy: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	rec = f.as(t, "alice", "analyst", http.MethodPut, "/"+theirs, `{"name":"edited"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit a query of another connection: %d %s", rec.Code, rec.Body.String())
	}
	if got := connOf(theirs); got != other {
		t.Fatalf("edit moved the query to %q, want %q", got, other)
	}
	rec = f.as(t, "alice", "analyst", http.MethodPost, "/"+theirs+"/duplicate", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body.String())
	}
	var copied database.SavedQuery
	if err := json.Unmarshal(rec.Body.Bytes(), &copied); err != nil {
		t.Fatal(err)
	}
	if got := connOf(copied.ID); got != f.connID {
		t.Fatalf("copy on %q, want the session connection %q", got, f.connID)
	}

	// The list stays global.
	rec = f.as(t, "alice", "analyst", http.MethodGet, "/", "")
	if ids := listIDs(t, rec); len(ids) != 3 {
		t.Fatalf("list: %d queries, want all 3", len(ids))
	}
}
