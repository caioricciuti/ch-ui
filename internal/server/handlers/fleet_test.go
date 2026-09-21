package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestFleetStatusesDoNotCallMissingDataHealthy(t *testing.T) {
	now := time.Now().UTC()
	base := fleetEntry{Online: true, MonitoringEnabled: true, CapturedAt: now.Format(time.RFC3339), StaleAfterSeconds: 180, Nodes: []clusterhealth.Sample{{Node: "a"}}}
	tests := []struct {
		name, want string
		change     func(*fleetEntry)
	}{
		{"healthy", "healthy", func(e *fleetEntry) {}},
		{"offline", "offline", func(e *fleetEntry) { e.Online = false }},
		{"disabled", "disabled", func(e *fleetEntry) { e.MonitoringEnabled = false }},
		{"missing", "missing", func(e *fleetEntry) { e.Nodes = nil }},
		{"stale", "stale", func(e *fleetEntry) { e.CapturedAt = now.Add(-4 * time.Minute).Format(time.RFC3339) }},
		{"future", "stale", func(e *fleetEntry) { e.CapturedAt = now.Add(time.Hour).Format(time.RFC3339) }},
		{"invalid", "stale", func(e *fleetEntry) { e.CapturedAt = "bad" }},
		{"critical", "critical", func(e *fleetEntry) { e.ReadonlyReplicas = 1 }},
		{"warning", "warning", func(e *fleetEntry) { e.OpenIncidents = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := base
			test.change(&e)
			if got := fleetStatus(e, now); got != test.want {
				t.Fatalf("%s != %s", got, test.want)
			}
		})
	}
}

func TestFleetAdminOnlyAndUsesLatestCompletePoll(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connection, err := db.CreateConnection(database.CreateConnectionParams{Name: "Production", TunnelToken: "do-not-leak-token"})
	if err != nil {
		t.Fatal(err)
	}
	store := clusterhealth.NewStore(db)
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)
	if err := store.InsertSamples(connection, "prod", []clusterhealth.Sample{{Node: "removed", CapturedAt: old, ReplicasReadonly: 9}, {Node: "current", CapturedAt: recent, ReplicationMaxDelay: 15}}); err != nil {
		t.Fatal(err)
	}
	router := (&FleetHandler{DB: db}).Routes()
	for _, role := range []string{"", "viewer", "analyst", "admin"} {
		req := httptest.NewRequest("GET", "/", nil)
		if role != "" {
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role}))
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if role != "admin" {
			if rr.Code != 403 {
				t.Fatalf("role %q: %d", role, rr.Code)
			}
			continue
		}
		if rr.Code != 200 {
			t.Fatalf("admin: %d %s", rr.Code, rr.Body.String())
		}
		var result struct {
			Connections []fleetEntry `json:"connections"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Connections) != 1 {
			t.Fatal("missing connection")
		}
		e := result.Connections[0]
		if len(e.Nodes) != 1 || e.Nodes[0].Node != "current" || e.ReadonlyReplicas != 0 || e.Status != "offline" {
			t.Fatalf("bad fleet: %+v", e)
		}
		var raw map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &raw)
		entry := raw["connections"].([]any)[0].(map[string]any)
		if _, ok := entry["tunnel_token"]; ok {
			t.Fatal("leaked connection secret")
		}
	}
}
