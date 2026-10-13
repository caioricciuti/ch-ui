package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

type sqFixture struct {
	db     *database.DB
	router http.Handler
	connID string
}

func newSQFixture(t *testing.T) sqFixture {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "sq.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "cht_33333333333333333333333333333333", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	// Config nil keeps the Pro-gated run route out of Routes; it is mounted
	// directly so visibility can be checked without a license.
	h := &SavedQueriesHandler{DB: db, Config: &config.Config{AppSecretKey: "test"}}
	r := chi.NewRouter()
	(&SavedQueriesHandler{DB: db}).Routes(r)
	r.Post("/{id}/run", h.Run)
	return sqFixture{db: db, router: r, connID: connID}
}

func (f sqFixture) as(t *testing.T, actor, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
		ConnectionID: f.connID, ClickhouseUser: actor, EncryptedPassword: "unused", UserRole: role,
	}))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func (f sqFixture) create(t *testing.T, owner, visibility string) string {
	t.Helper()
	id, err := f.db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Q " + visibility, Query: "SELECT 1", CreatedBy: owner, Visibility: visibility})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func listIDs(t *testing.T, rec *httptest.ResponseRecorder) map[string]database.SavedQuery {
	t.Helper()
	var body struct {
		SavedQueries []database.SavedQuery `json:"saved_queries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list %s: %v", rec.Body.String(), err)
	}
	out := map[string]database.SavedQuery{}
	for _, q := range body.SavedQueries {
		out[q.ID] = q
	}
	return out
}

// TestSavedQueryVisibilityMatrix covers 3 roles x 3 visibilities x owner or
// not, for list, get, run, update and delete.
func TestSavedQueryVisibilityMatrix(t *testing.T) {
	for _, role := range []string{"admin", "analyst", "viewer"} {
		for _, vis := range []string{database.SavedQueryPrivate, database.SavedQueryShared, database.SavedQueryAdmins} {
			for _, isOwner := range []bool{true, false} {
				name := fmt.Sprintf("%s/%s/owner=%v", role, vis, isOwner)
				t.Run(name, func(t *testing.T) {
					f := newSQFixture(t)
					actor := "bob"
					if isOwner {
						actor = "alice"
					}
					id := f.create(t, "alice", vis)

					// What the rule says.
					seesInList := isOwner || vis == database.SavedQueryShared || (vis == database.SavedQueryAdmins && role == "admin")
					seesByID := seesInList || role == "admin"
					canEdit := role == "admin" || (role == "analyst" && isOwner)

					list := listIDs(t, f.as(t, actor, role, http.MethodGet, "/", ""))
					if _, ok := list[id]; ok != seesInList {
						t.Fatalf("list: visible=%v, want %v", ok, seesInList)
					}
					if ok := list[id]; seesInList && ok.CanEdit != canEdit {
						t.Fatalf("list can_edit=%v, want %v", ok.CanEdit, canEdit)
					}

					get := f.as(t, actor, role, http.MethodGet, "/"+id, "")
					if (get.Code == http.StatusOK) != seesByID || (!seesByID && get.Code != http.StatusNotFound) {
						t.Fatalf("get: status %d, want visible=%v (404 when hidden)", get.Code, seesByID)
					}

					// Run fails later on the fake credentials (500); hidden is 404.
					run := f.as(t, actor, role, http.MethodPost, "/"+id+"/run", "")
					if seesByID == (run.Code == http.StatusNotFound) {
						t.Fatalf("run: status %d, want visible=%v", run.Code, seesByID)
					}

					wantWrite := http.StatusOK
					switch {
					case role == "viewer":
						wantWrite = http.StatusForbidden // RequireWriter, same for every id
					case !seesByID:
						wantWrite = http.StatusNotFound
					case !canEdit:
						wantWrite = http.StatusForbidden
					}
					if rec := f.as(t, actor, role, http.MethodPut, "/"+id, `{"name":"Renamed"}`); rec.Code != wantWrite {
						t.Fatalf("update: status %d, want %d: %s", rec.Code, wantWrite, rec.Body.String())
					}
					if rec := f.as(t, actor, role, http.MethodDelete, "/"+id, ""); rec.Code != wantWrite {
						t.Fatalf("delete: status %d, want %d", rec.Code, wantWrite)
					}
				})
			}
		}
	}
}

func TestSavedQueryAdminAllOwnersFilter(t *testing.T) {
	f := newSQFixture(t)
	id := f.create(t, "alice", database.SavedQueryPrivate)
	if _, ok := listIDs(t, f.as(t, "root", "admin", http.MethodGet, "/", ""))[id]; ok {
		t.Fatal("admin default list must not show others' private queries")
	}
	if _, ok := listIDs(t, f.as(t, "root", "admin", http.MethodGet, "/?owners=all", ""))[id]; !ok {
		t.Fatal("admin owners=all must show others' private queries")
	}
	if _, ok := listIDs(t, f.as(t, "bob", "analyst", http.MethodGet, "/?owners=all", ""))[id]; ok {
		t.Fatal("owners=all is admin only")
	}
}

func TestSavedQueryLegacyRowEditableByAnyWriter(t *testing.T) {
	f := newSQFixture(t)
	id := f.create(t, "", "")
	if rec := f.as(t, "bob", "analyst", http.MethodPut, "/"+id, `{"query":"SELECT 2"}`); rec.Code != http.StatusOK {
		t.Fatalf("analyst edit legacy: %d %s", rec.Code, rec.Body.String())
	}
	// The verified mark is owner or admin; a legacy row has no owner.
	if rec := f.as(t, "bob", "analyst", http.MethodPut, "/"+id, `{"verified":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst verify legacy: %d", rec.Code)
	}
	if rec := f.as(t, "root", "admin", http.MethodPut, "/"+id, `{"verified":true}`); rec.Code != http.StatusOK {
		t.Fatalf("admin verify legacy: %d", rec.Code)
	}
}

func TestSavedQueryCreateUpdateFieldsAndVersions(t *testing.T) {
	f := newSQFixture(t)
	folder := f.as(t, "alice", "analyst", http.MethodPost, "/folders", `{"name":"Ops"}`)
	if folder.Code != http.StatusCreated {
		t.Fatalf("folder: %d %s", folder.Code, folder.Body.String())
	}
	if rec := f.as(t, "bob", "analyst", http.MethodPost, "/folders", `{"name":"Ops"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate folder: %d, want 409", rec.Code)
	}
	var fb struct {
		Folder database.SavedQueryFolder `json:"folder"`
	}
	_ = json.Unmarshal(folder.Body.Bytes(), &fb)

	if rec := f.as(t, "alice", "analyst", http.MethodPost, "/", `{"name":"x","query":"SELECT 1","visibility":"admins"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst admins visibility: %d", rec.Code)
	}
	if rec := f.as(t, "alice", "analyst", http.MethodPost, "/", `{"name":"x","query":"SELECT 1","folder_id":"nope"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown folder: %d", rec.Code)
	}

	rec := f.as(t, "alice", "analyst", http.MethodPost, "/", `{"name":"Daily","query":"SELECT 1","folder_id":"`+fb.Folder.ID+`","tags":["ops"," ops ","kpi"],"visibility":"private"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var q database.SavedQuery
	_ = json.Unmarshal(rec.Body.Bytes(), &q)
	if q.FolderID == nil || *q.FolderID != fb.Folder.ID || len(q.Tags) != 2 || q.Visibility != "private" || q.Owner != "alice" || !q.CanEdit {
		t.Fatalf("created: %+v", q)
	}
	base := "/" + q.ID

	// Content change: version 2. Identical save: nothing. Move: no version.
	f.as(t, "alice", "analyst", http.MethodPut, base, `{"query":"SELECT 2"}`)
	f.as(t, "alice", "analyst", http.MethodPut, base, `{"query":"SELECT 2"}`)
	f.as(t, "alice", "analyst", http.MethodPut, base, `{"folder_id":"","visibility":"shared","tags":[]}`)
	var versions struct {
		Versions []database.SavedQueryVersion `json:"versions"`
	}
	_ = json.Unmarshal(f.as(t, "alice", "analyst", http.MethodGet, base+"/versions", "").Body.Bytes(), &versions)
	if len(versions.Versions) != 2 || versions.Versions[0].Version != 2 || versions.Versions[1].Note != "Created" {
		t.Fatalf("versions: %+v", versions.Versions)
	}
	got, _ := f.db.GetSavedQueryByID(q.ID)
	if got.FolderID != nil || got.Visibility != "shared" || len(got.Tags) != 0 {
		t.Fatalf("move/visibility/tags not saved: %+v", got)
	}

	one := f.as(t, "carol", "viewer", http.MethodGet, base+"/versions/1", "")
	if one.Code != http.StatusOK || !strings.Contains(one.Body.String(), `"query":"SELECT 1"`) {
		t.Fatalf("version 1: %d %s", one.Code, one.Body.String())
	}
	if rec := f.as(t, "bob", "analyst", http.MethodPost, base+"/versions/1/restore", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner restore: %d", rec.Code)
	}
	if rec := f.as(t, "alice", "analyst", http.MethodPost, base+"/versions/1/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	got, _ = f.db.GetSavedQueryByID(q.ID)
	v3, _ := f.db.GetSavedQueryVersion(q.ID, 3)
	if got.Query != "SELECT 1" || v3 == nil || v3.Note != "Restored version 1" {
		t.Fatalf("after restore: %q %+v", got.Query, v3)
	}
	if rec := f.as(t, "alice", "analyst", http.MethodPost, base+"/versions/99/restore", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing version: %d", rec.Code)
	}

	logs, _ := f.db.GetAuditLogs(50)
	seen := map[string]bool{}
	for _, l := range logs {
		seen[l.Action] = true
	}
	for _, a := range []string{"saved_query.moved", "saved_query.visibility_changed", "saved_query.version_restored"} {
		if !seen[a] {
			t.Errorf("audit %s missing", a)
		}
	}
}

func TestSavedQueryDuplicateIsPrivateCopyOfCaller(t *testing.T) {
	f := newSQFixture(t)
	id := f.create(t, "alice", database.SavedQueryShared)
	rec := f.as(t, "bob", "analyst", http.MethodPost, "/"+id+"/duplicate", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate: %d", rec.Code)
	}
	var q database.SavedQuery
	_ = json.Unmarshal(rec.Body.Bytes(), &q)
	if q.Owner != "bob" || q.Visibility != database.SavedQueryPrivate {
		t.Fatalf("copy: owner %q visibility %q", q.Owner, q.Visibility)
	}
	if _, ok := listIDs(t, f.as(t, "alice", "analyst", http.MethodGet, "/", ""))[q.ID]; ok {
		t.Fatal("the private copy must be hidden from others")
	}
}

func TestSavedQueryFolderDeleteMovesQueriesToRoot(t *testing.T) {
	f := newSQFixture(t)
	parent, _ := f.db.CreateSavedQueryFolder("Top", "", "alice")
	child, _ := f.db.CreateSavedQueryFolder("Child", parent, "alice")
	id, _ := f.db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Q", Query: "SELECT 1", FolderID: child})

	inFolder := listIDs(t, f.as(t, "alice", "viewer", http.MethodGet, "/?folder="+parent, ""))
	if _, ok := inFolder[id]; !ok {
		t.Fatal("folder filter includes subfolders")
	}
	if rec := f.as(t, "alice", "viewer", http.MethodDelete, "/folders/"+parent, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer delete folder: %d", rec.Code)
	}
	rec := f.as(t, "alice", "analyst", http.MethodDelete, "/folders/"+parent, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"moved":1`) {
		t.Fatalf("delete folder: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := listIDs(t, f.as(t, "alice", "viewer", http.MethodGet, "/?folder=root", ""))[id]; !ok {
		t.Fatal("query should be at the root")
	}
}
