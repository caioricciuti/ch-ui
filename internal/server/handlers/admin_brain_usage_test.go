package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

func brainUsageRouter(t *testing.T) (*database.DB, http.Handler) {
	t.Helper()
	db, _ := testutil.WorkerDB(t)
	h := &AdminHandler{DB: db}
	r := chi.NewRouter()
	h.registerBrainUsageRoutes(r)
	return db, r
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAdminBrainUsageSummaryAndCost(t *testing.T) {
	db, h := brainUsageRouter(t)
	for _, rec := range []database.BrainUsageRecord{
		{Username: "ana", Provider: "Claude", Model: "claude-sonnet-5-5", Purpose: "chat", InputTokens: 1_000_000, OutputTokens: 100_000},
		{Username: "ana", Provider: "Claude", Model: "claude-sonnet-5-5", Purpose: "ask_ai", InputTokens: 1000},
		{Username: "bo", Provider: "Local", Model: "llama", Purpose: "chat", InputTokens: 5, OutputTokens: 5},
	} {
		if err := db.RecordBrainUsage(rec); err != nil {
			t.Fatal(err)
		}
	}
	code, out := doJSON(t, h, http.MethodGet, "/brain/usage?days=7", "")
	if code != http.StatusOK {
		t.Fatalf("status %d %v", code, out)
	}
	rows := out["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows %v", rows)
	}
	top := rows[0].(map[string]any)
	if top["username"] != "ana" || top["requests"] != float64(2) || top["ask_ai"] != float64(1) || top["cost_usd"] == nil {
		t.Errorf("top row %v", top)
	}
	if rows[1].(map[string]any)["cost_usd"] != nil {
		t.Error("an unpriced model has no cost")
	}
	totals := out["totals"].(map[string]any)
	if totals["users"] != float64(2) || totals["unpriced_rows"] != float64(1) {
		t.Errorf("totals %v", totals)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/brain/usage?days=0", ""); code != http.StatusBadRequest {
		t.Errorf("days=0 should be rejected, got %d", code)
	}
}

func TestAdminBrainSettingsRoundTrip(t *testing.T) {
	_, h := brainUsageRouter(t)
	code, out := doJSON(t, h, http.MethodGet, "/brain/settings", "")
	prices := out["settings"].(map[string]any)["prices"].(map[string]any)
	if code != http.StatusOK || prices["claude-sonnet-5-5"] == nil {
		t.Fatalf("defaults %d %v", code, out)
	}
	code, out = doJSON(t, h, http.MethodPut, "/brain/settings", `{"dailyTokenBudget":50000,"maxOutputTokens":4096,"prices":{"my-model":{"input":1,"output":2}}}`)
	if code != http.StatusOK {
		t.Fatalf("save %d %v", code, out)
	}
	s := out["settings"].(map[string]any)
	if s["dailyTokenBudget"] != float64(50000) || s["maxOutputTokens"] != float64(4096) {
		t.Errorf("saved %v", s)
	}
	if p := s["prices"].(map[string]any); p["claude-sonnet-5-5"] != nil || p["my-model"] == nil {
		t.Errorf("a saved table replaces the defaults: %v", p)
	}
	for _, bad := range []string{`{"dailyTokenBudget":-1}`, `{"maxOutputTokens":999999}`, `{"agentModelId":"nope"}`, `{"prices":{"m":{"input":-1}}}`} {
		if code, _ := doJSON(t, h, http.MethodPut, "/brain/settings", bad); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", bad, code)
		}
	}
}
