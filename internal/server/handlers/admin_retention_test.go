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

func retentionRequest(t *testing.T, h *AdminHandler, method, body string) (*httptest.ResponseRecorder, retentionSettingsResponse) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/admin/retention", strings.NewReader(body))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "admin", ClickhouseUser: "admin"}))
	rec := httptest.NewRecorder()
	if method == http.MethodGet {
		h.GetRetentionSettings(rec, req)
	} else {
		h.UpdateRetentionSettings(rec, req)
	}
	var resp retentionSettingsResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rec, resp
}

func TestRetentionSettingsExternalWindows(t *testing.T) {
	db, err := dbtest.Open(t, "retention.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := &AdminHandler{DB: db}

	// GET exposes the new windows with their defaults.
	rec, resp := retentionRequest(t, h, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	for _, key := range []string{`"gov_query_log":30`, `"gov_policy_violations":30`, `"rbac_snapshots":180`, `"alert_evaluations":30`, `"report_deliveries":90`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Fatalf("response lacks %s: %s", key, rec.Body.String())
		}
	}
	if resp.Config != database.DefaultRetentionConfig() || resp.Defaults != database.DefaultRetentionConfig() {
		t.Fatalf("unexpected config %+v defaults %+v", resp.Config, resp.Defaults)
	}

	// A partial PUT changes only the keys it names.
	rec, resp = retentionRequest(t, h, http.MethodPut, `{"rbac_snapshots":0,"alert_evaluations":90}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	want := database.DefaultRetentionConfig()
	want.RBACSnapshots, want.AlertEvaluations = 0, 90
	if resp.Config != want {
		t.Fatalf("config after put %+v, want %+v", resp.Config, want)
	}
	if stored, err := db.GetRetentionConfig(); err != nil || stored != want {
		t.Fatalf("stored %+v err=%v", stored, err)
	}

	// Out of range values are rejected and nothing is stored.
	rec, _ = retentionRequest(t, h, http.MethodPut, `{"gov_query_log":3651}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("put 3651: %d, want 400", rec.Code)
	}
	if stored, _ := db.GetRetentionConfig(); stored != want {
		t.Fatalf("rejected put changed config: %+v", stored)
	}
}
