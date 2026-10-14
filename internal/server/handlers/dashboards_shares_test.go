package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// A share link runs panels with its creator's ClickHouse credentials and
// needs no login: viewers cannot list shares, and only the creator and
// admins get the token.
func TestListSharesGatesTokens(t *testing.T) {
	f := newDashFixture(t)
	creator := "alice"
	share, err := f.db.CreateDashboardShare(f.dashID, "viewer", "public", creator, "conn-1", "alice", "x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := func(user, role string) (int, []database.DashboardShare) {
		req := httptest.NewRequest(http.MethodGet, "/"+f.dashID+"/shares", nil)
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: "conn-1", ClickhouseUser: user, UserRole: role}))
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		var body struct {
			Shares []database.DashboardShare `json:"shares"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.Shares
	}

	if code, _ := list("carol", "viewer"); code != http.StatusForbidden {
		t.Fatalf("viewer list: %d", code)
	}
	for _, c := range []struct {
		user, role string
		seesToken  bool
	}{
		{"bob", "analyst", false},
		{creator, "analyst", true},
		{"root", "admin", true},
	} {
		code, shares := list(c.user, c.role)
		if code != http.StatusOK || len(shares) != 1 || shares[0].ID != share.ID {
			t.Fatalf("%s list: %d %+v", c.user, code, shares)
		}
		if got := shares[0].Token == share.Token; got != c.seesToken {
			t.Fatalf("%s/%s sees token: %v, want %v", c.user, c.role, got, c.seesToken)
		}
		if !c.seesToken && shares[0].Token != "" {
			t.Fatalf("%s got a token", c.user)
		}
	}

	// Invites mail the token, so another writer cannot send them either.
	req := httptest.NewRequest(http.MethodPost, "/"+f.dashID+"/shares/"+share.ID+"/invite", nil)
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: "conn-1", ClickhouseUser: "bob", UserRole: "analyst"}))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other writer invite: %d %s", rec.Code, rec.Body.String())
	}
}
