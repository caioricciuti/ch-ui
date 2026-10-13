package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/dashboards"
	"github.com/caioricciuti/ch-ui/internal/database"
)

type detailResponse struct {
	Dashboard database.Dashboard    `json:"dashboard"`
	Panels    []database.Panel      `json:"panels"`
	Variables []dashboards.Variable `json:"variables"`
}

func decodeDetail(t *testing.T, rec *httptest.ResponseRecorder) detailResponse {
	t.Helper()
	var d detailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return d
}

func TestSetVariablesValidatesAndNeedsWriter(t *testing.T) {
	f := newDashFixture(t)
	path := "/" + f.dashID + "/variables"

	if rec := f.do(t, http.MethodPut, path, `{"variables":[{"name":"bad-name","type":"text"}]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: status %d", rec.Code)
	}
	viewer := withSession(httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"variables":[]}`)), "viewer")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, viewer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer: status %d, want 403", rec.Code)
	}

	body := `{"variables":[{"name":"env","type":"custom","options":"prod,dev","default":"prod"}]}`
	if rec := f.do(t, http.MethodPut, path, body); rec.Code != http.StatusOK {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeDetail(t, f.do(t, http.MethodGet, "/"+f.dashID+"/", ""))
	if len(got.Variables) != 1 || got.Variables[0].Name != "env" || got.Variables[0].Default[0] != "prod" {
		t.Fatalf("variables not returned: %+v", got.Variables)
	}

	// Options of a custom variable need no ClickHouse.
	rec = f.do(t, http.MethodPost, "/"+f.dashID+"/variables/env/options", `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"dev"`) {
		t.Fatalf("options: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, http.MethodPost, "/"+f.dashID+"/variables/nope/options", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown variable: status %d", rec.Code)
	}
}

func TestVersionsRecordAndRestore(t *testing.T) {
	f := newDashFixture(t)
	base := "/" + f.dashID

	// The first change records the state before it, then the change.
	if rec := f.do(t, http.MethodPut, base+"/panels/"+f.panelID, `{"query":"SELECT 2"}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	if rec := f.do(t, http.MethodPut, base+"/variables", `{"variables":[{"name":"q","type":"text","default":"x"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("variables: %d", rec.Code)
	}
	rec := f.do(t, http.MethodGet, base+"/versions", "")
	var list struct {
		Versions []database.DashboardVersion `json:"versions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Versions) != 3 {
		t.Fatalf("want 3 versions (baseline, query, variables), got %+v", list.Versions)
	}

	// Restore version 1: the original query, no variables.
	rec = f.do(t, http.MethodPost, base+"/versions/1/restore", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeDetail(t, rec)
	if len(got.Panels) != 1 || got.Panels[0].Query != "SELECT 1" || len(got.Variables) != 0 {
		t.Fatalf("restore did not bring back version 1: %+v", got)
	}
	rec = f.do(t, http.MethodGet, base+"/versions", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Versions) != 4 || list.Versions[0].Note != "Restored version 1" {
		t.Fatalf("restore must add a version: %+v", list.Versions)
	}
	if rec := f.do(t, http.MethodPost, base+"/versions/99/restore", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing version: %d", rec.Code)
	}
	if rec := f.do(t, http.MethodGet, base+"/versions/2", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SELECT 2") {
		t.Fatalf("get version: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportKeepsOrReplacesVariables(t *testing.T) {
	f := newDashFixture(t)
	base := "/" + f.dashID
	f.do(t, http.MethodPut, base+"/variables", `{"variables":[{"name":"a","type":"text"}]}`)

	got := decodeDetail(t, f.do(t, http.MethodPost, base+"/import", `{"dashboard":{"name":"X"},"panels":[]}`))
	if len(got.Variables) != 1 {
		t.Fatalf("an import without variables must keep them: %+v", got.Variables)
	}
	got = decodeDetail(t, f.do(t, http.MethodPost, base+"/import", `{"dashboard":{"name":"X"},"panels":[],"variables":[{"name":"b","type":"custom","options":"1,2"}]}`))
	if len(got.Variables) != 1 || got.Variables[0].Name != "b" {
		t.Fatalf("import must replace variables: %+v", got.Variables)
	}
	if rec := f.do(t, http.MethodPost, base+"/import", `{"dashboard":{"name":"X"},"panels":[],"variables":[{"name":"b","type":"nope"}]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad variables: %d", rec.Code)
	}
}

func TestDuplicateAndTemplates(t *testing.T) {
	f := newDashFixture(t)
	f.do(t, http.MethodPut, "/"+f.dashID+"/variables", `{"variables":[{"name":"a","type":"text"}]}`)

	rec := f.do(t, http.MethodPost, "/"+f.dashID+"/duplicate", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body.String())
	}
	dup := decodeDetail(t, rec)
	if dup.Dashboard.ID == f.dashID || dup.Dashboard.Name != "Ops (copy)" || len(dup.Panels) != 1 || len(dup.Variables) != 1 {
		t.Fatalf("duplicate: %+v", dup)
	}
	if dup.Panels[0].ID == f.panelID {
		t.Fatal("duplicate must copy panels, not share them")
	}

	rec = f.do(t, http.MethodGet, "/templates", "")
	var tl struct {
		Templates []dashboards.Template `json:"templates"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tl)
	if len(tl.Templates) < 2 {
		t.Fatalf("templates: %s", rec.Body.String())
	}
	for _, tpl := range tl.Templates {
		rec := f.do(t, http.MethodPost, "/from-template", `{"template_id":"`+tpl.ID+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("from template %s: %d %s", tpl.ID, rec.Code, rec.Body.String())
		}
		d := decodeDetail(t, rec)
		if len(d.Panels) != tpl.PanelCount || len(d.Variables) != len(tpl.Variables) || d.Dashboard.Name != tpl.Name {
			t.Fatalf("template %s created %+v", tpl.ID, d.Dashboard)
		}
	}
	if rec := f.do(t, http.MethodPost, "/from-template", `{"template_id":"nope"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown template: %d", rec.Code)
	}
}

func TestPublicVariablesAreRestricted(t *testing.T) {
	f := newPublicFixture(t)
	share, _ := f.db.GetDashboardShareByToken(f.token)
	vars := `[{"name":"env","type":"custom","options":"prod,dev"},{"name":"q","type":"text","default":"x"},` +
		`{"name":"secret","type":"query","query":"SELECT name FROM secrets"}]`
	if err := f.db.SetDashboardVariables(share.DashboardID, vars); err != nil {
		t.Fatal(err)
	}
	// Only the variables a panel uses are checked.
	if err := f.db.UpdatePanel(f.panelID, "Rows", "", "table", "SELECT 1 WHERE $env != ''", "", "{}", 0, 0, 4, 4); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+f.token, nil))
	if strings.Contains(rec.Body.String(), "secrets") || !strings.Contains(rec.Body.String(), `"name":"secret"`) {
		t.Fatalf("public dashboard must list variables without their queries: %s", rec.Body.String())
	}

	// A value outside the options is refused before anything runs.
	rec = f.post(t, f.token, `{"panel_id":"`+f.panelID+`","variables":{"env":["') OR 1=1 --"]}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not an option") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+f.token+"/variables/env/options", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"prod"`) {
		t.Fatalf("public options: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicDashboardOmitsConstants(t *testing.T) {
	f := newPublicFixture(t)
	share, _ := f.db.GetDashboardShareByToken(f.token)
	vars := `[{"name":"tenant","type":"constant","default":"acme-secret"},{"name":"env","type":"custom","options":"prod,dev"}]`
	if err := f.db.SetDashboardVariables(share.DashboardID, vars); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+f.token, nil))
	var resp struct {
		Variables []dashboards.Variable `json:"variables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if rec.Code != http.StatusOK || len(resp.Variables) != 1 || resp.Variables[0].Name != "env" {
		t.Fatalf("want only env: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "acme-secret") || strings.Contains(rec.Body.String(), "tenant") {
		t.Fatalf("constant leaked: %s", rec.Body.String())
	}

	// Its options are not served either.
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+f.token+"/variables/tenant/options", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("constant options: status %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestVariableOptionsPreviewNeedsWriter(t *testing.T) {
	f := newDashFixture(t)
	if rec := f.do(t, http.MethodPut, "/"+f.dashID+"/variables", `{"variables":[{"name":"env","type":"custom","options":"prod,dev"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	post := func(role, name, body string) *httptest.ResponseRecorder {
		req := withSession(httptest.NewRequest(http.MethodPost, "/"+f.dashID+"/variables/"+name+"/options", strings.NewReader(body)), role)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		return rec
	}
	preview := `{"definition":{"type":"custom","options":"a,b"}}`

	// A viewer still loads the options of a saved variable.
	if rec := post("viewer", "env", `{}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"dev"`) {
		t.Fatalf("viewer saved options: %d %s", rec.Code, rec.Body.String())
	}
	// But cannot preview a definition, saved name or not.
	for _, name := range []string{"env", "draft"} {
		if rec := post("viewer", name, preview); rec.Code != http.StatusForbidden {
			t.Fatalf("viewer preview of %s: status %d, want 403: %s", name, rec.Code, rec.Body.String())
		}
	}
	for _, role := range []string{"analyst", "admin"} {
		rec := post(role, "draft", preview)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"b"`) {
			t.Fatalf("%s preview: %d %s", role, rec.Code, rec.Body.String())
		}
	}
}
