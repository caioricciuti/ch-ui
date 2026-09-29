package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestUpdateGovernanceSettingsRequiresProToEnable(t *testing.T) {
	cases := []struct {
		name        string
		access      config.ProAccess
		body        string
		wantStatus  int
		wantEnabled bool
	}{
		{"enable without Pro", config.ProNone, `{"sync_enabled":true}`, http.StatusPaymentRequired, false},
		{"enable in grace", config.ProGrace, `{"sync_enabled":true}`, http.StatusOK, true},
		{"enable when active", config.ProActive, `{"sync_enabled":true}`, http.StatusOK, true},
		{"disable without Pro", config.ProNone, `{"sync_enabled":false}`, http.StatusOK, false},
		{"dismiss banner without Pro", config.ProNone, `{"banner_dismissed":true}`, http.StatusOK, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := database.Open(filepath.Join(t.TempDir(), "gov.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			cfg := &config.Config{}
			access := tc.access
			cfg.SetProAccessForTest(func() config.ProAccess { return access })
			syncer := governance.NewSyncer(governance.NewStore(db), db, nil, "test-secret", func() bool { return false })
			defer syncer.Stop()
			h := &AdminHandler{DB: db, Config: cfg, GovSyncer: syncer}

			req := httptest.NewRequest(http.MethodPut, "/api/admin/governance/settings", strings.NewReader(tc.body))
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "admin", ClickhouseUser: "admin"}))
			rec := httptest.NewRecorder()
			h.UpdateGovernanceSettings(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := db.GovernanceSyncEnabled(); got != tc.wantEnabled {
				t.Fatalf("sync enabled %v, want %v", got, tc.wantEnabled)
			}
			if got := syncer.IsRunning(); got != tc.wantEnabled {
				t.Fatalf("syncer running %v, want %v", got, tc.wantEnabled)
			}
		})
	}
}
