package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/queryproc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func withSession(req *http.Request, role string) *http.Request {
	return req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
		ConnectionID:      "conn-1",
		ClickhouseUser:    "alice",
		EncryptedPassword: "unused",
		UserRole:          role,
	}))
}

func TestPanelQueryBlockedByGuardrailPolicy(t *testing.T) {
	q, cleanup := newBlockedQueryHandler(t)
	defer cleanup()
	h := &DashboardsHandler{DB: q.DB, Guardrails: q.Guardrails}

	for name, body := range map[string]string{
		"plain":       `{"query":"SELECT * FROM db.tbl"}`,
		"after macro": `{"query":"SELECT * FROM $__table","table":"db.tbl"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := withSession(httptest.NewRequest(http.MethodPost, "/api/dashboards/query", strings.NewReader(body)), "analyst")
			rec := httptest.NewRecorder()
			h.ExecutePanelQuery(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"policy_blocked"`)) {
				t.Fatalf("expected policy_blocked, got %s", rec.Body.String())
			}
		})
	}
}

func TestPanelQuerySettingsCapRowsAndUseISODates(t *testing.T) {
	s := panelQuerySettings()
	if s["max_result_rows"] != "100000" || s["result_overflow_mode"] != "break" {
		t.Fatalf("panel queries must cap rows with break mode, got %v", s)
	}
	if s["date_time_output_format"] != "iso" || publicQuerySettings["date_time_output_format"] != "iso" {
		t.Fatalf("panel queries must ask for ISO dates")
	}
}

type dashFixture struct {
	db      *database.DB
	router  http.Handler
	dashID  string
	panelID string
}

func newDashFixture(t *testing.T) dashFixture {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "dash.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dashID, err := db.CreateDashboard("Ops", "before", "alice")
	if err != nil {
		t.Fatal(err)
	}
	panelID, err := db.CreatePanel(dashID, "Rows", "", "table", "SELECT 1", "", "{}", 0, 0, 6, 4)
	if err != nil {
		t.Fatal(err)
	}
	h := &DashboardsHandler{DB: db}
	return dashFixture{db: db, router: h.Routes(), dashID: dashID, panelID: panelID}
}

func (f dashFixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := withSession(httptest.NewRequest(method, path, strings.NewReader(body)), "admin")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestCreateAndUpdatePanelValidation(t *testing.T) {
	f := newDashFixture(t)
	create := "/" + f.dashID + "/panels"
	update := "/" + f.dashID + "/panels/" + f.panelID

	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{"unknown type", http.MethodPost, create, `{"name":"a","query":"SELECT 1","panel_type":"heatmap"}`, 400},
		{"too wide", http.MethodPost, create, `{"name":"a","query":"SELECT 1","layout_x":8,"layout_w":6}`, 400},
		{"negative y", http.MethodPost, create, `{"name":"a","query":"SELECT 1","layout_y":-1}`, 400},
		{"zero height", http.MethodPost, create, `{"name":"a","query":"SELECT 1","layout_h":0}`, 400},
		{"bad config", http.MethodPost, create, `{"name":"a","query":"SELECT 1","config":"{nope"}`, 400},
		{"valid", http.MethodPost, create, `{"name":"a","query":"SELECT 1","panel_type":"pie","layout_x":6,"layout_y":4,"layout_w":6,"layout_h":4}`, 201},
		{"update type", http.MethodPut, update, `{"panel_type":"donut"}`, 400},
		{"update layout", http.MethodPut, update, `{"layout_x":11,"layout_w":2}`, 400},
		{"update empty name", http.MethodPut, update, `{"name":"  "}`, 400},
		{"update valid", http.MethodPut, update, `{"layout_x":6,"layout_w":6,"panel_type":"gauge"}`, 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rec := f.do(t, tc.method, tc.path, tc.body); rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCreatePanelWithoutLayoutYAppendsBelow(t *testing.T) {
	f := newDashFixture(t)
	// The fixture panel sits at y=0, h=4. Add a taller one beside it.
	if _, err := f.db.CreatePanel(f.dashID, "Tall", "", "table", "SELECT 1", "", "{}", 6, 0, 6, 7); err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, http.MethodPost, "/"+f.dashID+"/panels", `{"name":"From editor","query":"SELECT 1","layout_w":12}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Panel struct {
			LayoutY int `json:"layout_y"`
		} `json:"panel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Panel.LayoutY != 7 {
		t.Fatalf("layout_y = %d, want 7 (below the tallest panel)", out.Panel.LayoutY)
	}

	// An explicit layout_y is kept as given.
	rec = f.do(t, http.MethodPost, "/"+f.dashID+"/panels", `{"name":"Top","query":"SELECT 1","layout_y":0}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Panel.LayoutY != 0 {
		t.Fatalf("explicit layout_y = %d, want 0", out.Panel.LayoutY)
	}
}

func TestNextPanelRowOnEmptyDashboard(t *testing.T) {
	if got := nextPanelRow(nil); got != 0 {
		t.Fatalf("nextPanelRow(nil) = %d, want 0", got)
	}
}

func TestImportDashboardIsAllOrNothing(t *testing.T) {
	f := newDashFixture(t)
	path := "/" + f.dashID + "/import"

	// The second panel is invalid: nothing may change.
	bad := `{"dashboard":{"name":"New"},"panels":[
		{"name":"ok","panel_type":"stat","query":"SELECT 1"},
		{"name":"broken","panel_type":"nope","query":"SELECT 1"}]}`
	rec := f.do(t, http.MethodPost, path, bad)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "panel 2 (broken)") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	panels, _ := f.db.GetPanelsByDashboard(f.dashID)
	if len(panels) != 1 || panels[0].ID != f.panelID {
		t.Fatalf("a failed import changed the panels: %+v", panels)
	}
	if d, _ := f.db.GetDashboardByID(f.dashID); d.Name != "Ops" {
		t.Fatalf("a failed import renamed the dashboard to %q", d.Name)
	}

	good := `{"dashboard":{"name":"New","description":""},"panels":[
		{"name":"A","panel_type":"timeseries","query":"SELECT 1","config":{"chartType":"timeseries"},"layout_x":0,"layout_y":0,"layout_w":12,"layout_h":4},
		{"name":"B","panel_type":"text","config":"{\"content\":\"hi\"}","connection_id":"gone"}]}`
	rec = f.do(t, http.MethodPost, path, good)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Dashboard database.Dashboard `json:"dashboard"`
		Panels    []database.Panel   `json:"panels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Dashboard.Name != "New" || resp.Dashboard.Description != nil {
		t.Fatalf("dashboard not updated: %+v", resp.Dashboard)
	}
	if len(resp.Panels) != 2 {
		t.Fatalf("got %d panels, want 2", len(resp.Panels))
	}
	for _, p := range resp.Panels {
		if p.ID == f.panelID {
			t.Fatalf("old panel survived the import")
		}
		if p.Name == "B" && (p.Config != `{"content":"hi"}` || p.ConnectionID != nil) {
			t.Fatalf("text panel imported wrong: %+v", p)
		}
	}
}

func TestSnapPublicTimeRangeWidensToStep(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 7, 0, time.UTC)
	tr, key := snapPublicTimeRange(&queryproc.TimeRange{Type: "relative", From: "1h", To: "now"}, now)
	if tr.Type != "absolute" || tr.From != "2026-01-01T11:00:00Z" || tr.To != "2026-01-01T12:00:15Z" {
		t.Fatalf("got %+v", tr)
	}
	// A viewer a few seconds later shares the key.
	_, key2 := snapPublicTimeRange(&queryproc.TimeRange{Type: "relative", From: "1h", To: "now"}, now.Add(5*time.Second))
	if key != key2 {
		t.Fatalf("keys differ: %s vs %s", key, key2)
	}
	if tr, key := snapPublicTimeRange(nil, now); tr != nil || key != "none" {
		t.Fatalf("nil range: %+v %s", tr, key)
	}
}

func TestPublicQueryCacheSharesAndExpires(t *testing.T) {
	c := newPublicQueryCache(20*time.Second, 2, 1<<20, 1<<10)
	clock := time.Unix(0, 0)
	c.now = func() time.Time { return clock }

	var runs atomic.Int32
	release := make(chan struct{})
	fn := func() (int, []byte) {
		runs.Add(1)
		<-release
		return http.StatusOK, []byte(`{"success":true}`)
	}

	// Concurrent misses for one key run one query.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.do("k", fn) }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if runs.Load() != 1 {
		t.Fatalf("ran %d queries, want 1", runs.Load())
	}
	if _, _, ok := c.get("k"); !ok {
		t.Fatal("result was not cached")
	}

	clock = clock.Add(21 * time.Second)
	if _, _, ok := c.get("k"); ok {
		t.Fatal("expired entry was served")
	}

	// Errors are not cached; the LRU keeps at most two entries.
	c.do("err", func() (int, []byte) { return http.StatusBadGateway, []byte(`{}`) })
	if _, _, ok := c.get("err"); ok {
		t.Fatal("an error was cached")
	}
	ok := func() (int, []byte) { return http.StatusOK, []byte(`{}`) }
	c.do("a", ok)
	c.do("b", ok)
	c.do("c", ok)
	if _, _, hit := c.get("a"); hit {
		t.Fatal("least recently used entry was kept")
	}
	if c.order.Len() != 2 {
		t.Fatalf("cache holds %d entries, want 2", c.order.Len())
	}
}

func TestKeyedRateLimiterIsPerKey(t *testing.T) {
	l := newKeyedRateLimiter(2, time.Minute)
	if !l.allow("a") || !l.allow("a") || l.allow("a") {
		t.Fatal("limit not enforced for key a")
	}
	if !l.allow("b") {
		t.Fatal("key b shares key a's budget")
	}
}
