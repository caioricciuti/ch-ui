package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestPipelineResetPosition(t *testing.T) {
	db, err := dbtest.Open(t, "pipelines.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreatePipeline("p", "", conn, "analyst")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SavePipelineCheckpoint(id, "watermark", `{"kind":"int","value":"5"}`); err != nil {
		t.Fatal(err)
	}
	router := (&PipelinesHandler{DB: db}).Routes()
	call := func(role, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(""))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: role, ConnectionID: conn}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := call("viewer", http.MethodGet, "/"+id+"/checkpoints")
	var list struct {
		Checkpoints []database.PipelineCheckpoint `json:"checkpoints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Checkpoints) != 1 {
		t.Fatalf("checkpoints: %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/" + id + "/reset-position", "/" + id + "/test"} {
		if got := call("viewer", http.MethodPost, path).Code; got != http.StatusForbidden {
			t.Fatalf("viewer %s: status %d, want 403", path, got)
		}
	}

	db.UpdatePipelineStatus(id, "running", "")
	if got := call("analyst", http.MethodPost, "/"+id+"/reset-position").Code; got != http.StatusConflict {
		t.Fatalf("reset while running: status %d, want 409", got)
	}
	db.UpdatePipelineStatus(id, "stopped", "")
	if rec := call("analyst", http.MethodPost, "/"+id+"/reset-position"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cleared":1`) {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	if m, _ := db.PipelineCheckpointMap(id); len(m) != 0 {
		t.Fatalf("checkpoints left: %v", m)
	}
}
