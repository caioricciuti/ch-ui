package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

type publicFixture struct {
	db       *database.DB
	handler  http.Handler
	token    string
	panelID  string
	otherID  string
	textID   string
	expiredT string
}

func newPublicFixture(t *testing.T) publicFixture {
	t.Helper()
	db, err := dbtest.Open(t, "public.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "Production", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := db.CreateDashboard("Shared", "", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateDashboard("Other", "", "")
	if err != nil {
		t.Fatal(err)
	}
	panelID, err := db.CreatePanel(shared, "Rows", "", "table", "SELECT 1", "", "{}", 0, 0, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	textID, err := db.CreatePanel(shared, "Notes", "", "text", "", "", `{"content":"hi"}`, 0, 4, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := db.CreatePanel(other, "Secret", "", "table", "SELECT * FROM secrets", "", "{}", 0, 0, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	share, err := db.CreateDashboardShare(shared, "viewer", "public", "", connID, "default", "x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	badExpiry := "not-a-date"
	expired, err := db.CreateDashboardShare(shared, "viewer", "public", "", connID, "default", "x", nil, &badExpiry)
	if err != nil {
		t.Fatal(err)
	}

	h := &DashboardsHandler{DB: db, Config: &config.Config{AppSecretKey: "test-secret"}}
	return publicFixture{db: db, handler: h.PublicRoutes(), token: share.Token, panelID: panelID, otherID: otherID, textID: textID, expiredT: expired.Token}
}

func (f publicFixture) post(t *testing.T, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+token+"/query", strings.NewReader(body)))
	return rec
}

func TestPublicQueryNeverRunsCallerSQL(t *testing.T) {
	f := newPublicFixture(t)
	tests := []struct {
		name  string
		token string
		body  string
		want  int
	}{
		{"raw sql only", f.token, `{"query":"DROP TABLE users"}`, http.StatusBadRequest},
		{"panel from another dashboard", f.token, `{"panel_id":"` + f.otherID + `"}`, http.StatusNotFound},
		{"unknown panel", f.token, `{"panel_id":"nope"}`, http.StatusNotFound},
		{"text panel", f.token, `{"panel_id":"` + f.textID + `"}`, http.StatusBadRequest},
		{"unparseable expiry", f.expiredT, `{"panel_id":"` + f.panelID + `"}`, http.StatusGone},
		{"unknown token", "missing", `{"panel_id":"` + f.panelID + `"}`, http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if rec := f.post(t, test.token, test.body); rec.Code != test.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, test.want, rec.Body.String())
			}
		})
	}
}

func TestPublicDashboardHidesPanelSQL(t *testing.T) {
	f := newPublicFixture(t)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+f.token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Panels []database.Panel `json:"panels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Panels) != 2 {
		t.Fatalf("got %d panels, want 2", len(resp.Panels))
	}
	for _, p := range resp.Panels {
		if p.Query != "" {
			t.Fatalf("panel %s leaked its SQL: %q", p.Name, p.Query)
		}
	}
}

func TestPublicDashboardStripsPrivateLinks(t *testing.T) {
	f := newPublicFixture(t)
	panel, err := f.db.GetPanelByID(f.panelID)
	if err != nil || panel == nil {
		t.Fatal(err)
	}
	cfg := `{"chartType":"bar","links":[
		{"kind":"setVariable","variable":"host"},
		{"kind":"url","template":"https://x.example/${__value}"},
		{"kind":"sql","template":"SELECT * FROM private_table"},
		{"kind":"dashboard","dashboardId":"` + f.otherID + `"}]}`
	if _, err := f.db.CreatePanel(panel.DashboardID, "Linked", "", "bar", "SELECT 1", "", cfg, 4, 0, 4, 4); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+f.token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"private_table", f.otherID, `\"sql\"`, `\"dashboard\"`} {
		if strings.Contains(body, leak) {
			t.Fatalf("public dashboard leaks %s: %s", leak, body)
		}
	}
	if !strings.Contains(body, "setVariable") || !strings.Contains(body, "https://x.example/") {
		t.Fatalf("public dashboard dropped the safe links: %s", body)
	}
}

func TestPublicQuerySettingsAreReadOnly(t *testing.T) {
	if publicQuerySettings["readonly"] != "1" {
		t.Fatalf("public queries must run with readonly=1, got %q", publicQuerySettings["readonly"])
	}
	for _, key := range []string{"max_execution_time", "max_result_rows", "max_result_bytes"} {
		if publicQuerySettings[key] == "" {
			t.Fatalf("public queries must set %s", key)
		}
	}
}
