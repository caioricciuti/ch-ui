package dashboards

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/queryproc"
)

func TestValidateVariables(t *testing.T) {
	bad := map[string]string{
		"bad name":         `[{"name":"1x","type":"text"}]`,
		"reserved name":    `[{"name":"__interval","type":"text"}]`,
		"duplicate":        `[{"name":"a","type":"text"},{"name":"a","type":"text"}]`,
		"unknown type":     `[{"name":"a","type":"list"}]`,
		"long custom list": `[{"name":"a","type":"custom","options":"` + longList(MaxOptions+1) + `"}]`,
		"query missing":    `[{"name":"a","type":"query"}]`,
		"options missing":  `[{"name":"a","type":"custom"}]`,
		"bad interval":     `[{"name":"a","type":"interval","options":"5m,soon"}]`,
		"constant default": `[{"name":"a","type":"constant"}]`,
		"not an array":     `{"name":"a"}`,
	}
	for name, raw := range bad {
		if _, err := ParseVariables([]byte(raw)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	vars, err := ParseVariables([]byte(`[
		{"name":"db","type":"QUERY","query":" SELECT name FROM system.databases ","options":"x","default":"system"},
		{"name":"env","type":"custom","options":"prod, dev,,prod","multi":true,"default":["prod","dev"]},
		{"name":"one","type":"custom","options":"a,b","default":["a","b"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if vars[0].Type != TypeQuery || vars[0].Options != "" || vars[0].Query != "SELECT name FROM system.databases" {
		t.Fatalf("query variable not normalized: %+v", vars[0])
	}
	if !reflect.DeepEqual([]string(vars[0].Default), []string{"system"}) {
		t.Fatalf("string default not decoded: %v", vars[0].Default)
	}
	if got := vars[1].StaticOptions(); len(got) != 2 || got[0].Value != "prod" || got[1].Value != "dev" {
		t.Fatalf("options: %+v", got)
	}
	if len(vars[2].Default) != 1 {
		t.Fatalf("single-value variable kept %d defaults", len(vars[2].Default))
	}
}

func longList(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprint("v", i)
	}
	return strings.Join(parts, ",")
}

func TestResolve(t *testing.T) {
	vars := []Variable{
		{Name: "db", Type: TypeQuery, Query: "SELECT name FROM system.databases", IncludeAll: true, Multi: true},
		{Name: "tbl", Type: TypeQuery, Query: "SELECT name FROM system.tables WHERE database IN (${db})"},
		{Name: "env", Type: TypeCustom, Options: "prod,dev", Default: StringList{"dev"}},
		{Name: "q", Type: TypeText, Default: StringList{"hello"}},
		{Name: "c", Type: TypeConstant, Default: StringList{"fixed"}},
		{Name: "bucket", Type: TypeInterval, Options: "1m,5m"},
	}
	var seenParents []map[string][]string
	run := func(v Variable, parents map[string][]string, _ map[string]string) ([]Option, string, error) {
		cp := map[string][]string{}
		for k, val := range parents {
			cp[k] = val
		}
		seenParents = append(seenParents, cp)
		if v.Name == "db" {
			return []Option{{Value: "system"}, {Value: "default"}}, "", nil
		}
		return []Option{{Value: parents["db"][0] + ".t1"}, {Value: parents["db"][0] + ".t2"}}, "", nil
	}

	values, types, err := Resolve(vars, map[string][]string{"db": {AllValue}, "q": {"typed"}, "c": {"override"}}, false, run)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"db": {"system", "default"}, "tbl": {"system.t1"}, "env": {"dev"},
		"q": {"typed"}, "c": {"fixed"}, "bucket": {"1m"},
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("got %v\nwant %v", values, want)
	}
	if types["bucket"] != TypeInterval {
		t.Fatalf("interval type missing: %v", types)
	}
	// The second variable saw the first one's resolved values (cascading).
	if len(seenParents) != 2 || !reflect.DeepEqual(seenParents[1]["db"], []string{"system", "default"}) {
		t.Fatalf("cascading parents: %v", seenParents)
	}

	// Strict (share links): only options, text is its default.
	if _, _, err := Resolve(vars, map[string][]string{"env": {"staging"}}, true, run); err == nil || !strings.Contains(err.Error(), "not an option") {
		t.Fatalf("strict must refuse a value outside the options, got %v", err)
	}
	if _, _, err := Resolve(vars, map[string][]string{"db": {"x' OR 1=1 --"}}, true, run); err == nil {
		t.Fatal("strict must refuse an injected value")
	}
	values, _, err = Resolve(vars, map[string][]string{"q": {"anything"}, "tbl": {"default.t2"}, "db": {"default"}}, true, run)
	if err != nil {
		t.Fatal(err)
	}
	if values["q"][0] != "hello" || values["tbl"][0] != "default.t2" {
		t.Fatalf("strict values: %v", values)
	}
	if _, _, err := Resolve([]Variable{{Name: "env", Type: TypeCustom, Options: "a"}}, map[string][]string{"env": {AllValue}}, false, run); err == nil {
		t.Fatal("All must be refused without include_all")
	}
}

// "All" of a query variable whose options were cut at MaxOptions becomes the
// runner's subquery, never the first MaxOptions values, also on share links;
// a child variable sees it with its type.
func TestResolveAllPastTheCap(t *testing.T) {
	vars := []Variable{
		{Name: "host", Type: TypeQuery, Query: "SELECT host FROM logs", IncludeAll: true, Multi: true},
		{Name: "path", Type: TypeQuery, Query: "SELECT path FROM logs WHERE host IN ($host)", IncludeAll: true, Multi: true},
	}
	full := make([]Option, MaxOptions)
	for i := range full {
		full[i] = Option{Value: fmt.Sprint("h", i)}
	}
	var childTypes map[string]string
	run := func(v Variable, parents map[string][]string, parentTypes map[string]string) ([]Option, string, error) {
		if v.Name == "host" {
			return full, AllSubquery("SELECT host FROM logs;", "host"), nil
		}
		childTypes = parentTypes
		return []Option{{Value: "/a"}, {Value: "/b"}}, "", nil
	}
	for _, strict := range []bool{false, true} {
		values, types, err := Resolve(vars, map[string][]string{"host": {AllValue}, "path": {AllValue}}, strict, run)
		if err != nil {
			t.Fatal(err)
		}
		want := "SELECT `host` FROM (\nSELECT host FROM logs\n)"
		if !reflect.DeepEqual(values["host"], []string{want}) || types["host"] != queryproc.VarTypeSubquery {
			t.Fatalf("strict=%v: host %q type %q", strict, values["host"], types["host"])
		}
		if childTypes["host"] != queryproc.VarTypeSubquery {
			t.Fatalf("child options query did not see the subquery type: %v", childTypes)
		}
		// A list under the cap still expands to its values.
		if !reflect.DeepEqual(values["path"], []string{"/a", "/b"}) || types["path"] != "" {
			t.Fatalf("path %v type %q", values["path"], types["path"])
		}
	}
	if got := AllSubquery("SELECT 1 AS `a``b` -- note", "a`b"); got != "SELECT `a\\`b` FROM (\nSELECT 1 AS `a``b` -- note\n)" {
		t.Fatalf("AllSubquery: %q", got)
	}
}

func TestTemplatesAreValid(t *testing.T) {
	list, err := Templates()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 templates, got %d", len(list))
	}
	for _, tpl := range list {
		raw, got, err := TemplateContent(tpl.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.PanelCount == 0 || got.Name == "" {
			t.Fatalf("template %s is empty: %+v", tpl.ID, got)
		}
		var body struct {
			Dashboard struct {
				Name string `json:"name"`
			} `json:"dashboard"`
			Panels []struct {
				Query string `json:"query"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(raw, &body); err != nil || body.Dashboard.Name == "" {
			t.Fatalf("template %s: %v", tpl.ID, err)
		}
	}
	if _, _, err := TemplateContent("../etc/passwd"); err == nil {
		t.Fatal("unknown template must fail")
	}
}

func TestCanonicalKeyIsStable(t *testing.T) {
	a := CanonicalKey(map[string][]string{"b": {"1", "2"}, "a": {"x"}})
	b := CanonicalKey(map[string][]string{"a": {"x"}, "b": {"1", "2"}})
	if a != b || a == CanonicalKey(map[string][]string{"a": {"x"}, "b": {"12"}}) {
		t.Fatal("canonical key must be stable and unambiguous")
	}
}

func TestNeededFollowsParents(t *testing.T) {
	vars := []Variable{
		{Name: "db", Type: TypeCustom, Options: "a"},
		{Name: "other", Type: TypeCustom, Options: "a"},
		{Name: "tbl", Type: TypeQuery, Query: "SELECT name FROM system.tables WHERE database = $db"},
	}
	got := Needed(vars, "SELECT * FROM system.columns WHERE table = ${tbl}")
	if len(got) != 2 || got[0].Name != "db" || got[1].Name != "tbl" {
		t.Fatalf("got %+v", got)
	}
	if len(Needed(vars, "SELECT 1")) != 0 {
		t.Fatal("a query without variables needs none")
	}
}

func TestPublicListDropsConstantsAndQueries(t *testing.T) {
	vars := []Variable{
		{Name: "tenant", Type: TypeConstant, Default: StringList{"acme"}},
		{Name: "db", Type: TypeQuery, Query: "SELECT name FROM system.databases"},
		{Name: "env", Type: TypeCustom, Options: "prod,dev"},
	}
	got := PublicList(vars)
	if len(got) != 2 || got[0].Name != "db" || got[1].Name != "env" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Query != "" {
		t.Fatalf("query leaked: %q", got[0].Query)
	}
	if vars[1].Query == "" {
		t.Fatal("the input must not change")
	}
}

func TestTemplateTimeSeriesFillEmptyBuckets(t *testing.T) {
	list, err := Templates()
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range list {
		raw, _, err := TemplateContent(tpl.ID)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Panels []struct {
				Name      string `json:"name"`
				PanelType string `json:"panel_type"`
				Query     string `json:"query"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		for _, p := range body.Panels {
			bucketed := strings.Contains(p.Query, "toStartOfInterval(") && strings.Contains(p.Query, "GROUP BY ts")
			if p.PanelType == "timeseries" && bucketed && !strings.Contains(p.Query, "WITH FILL") {
				t.Errorf("%s / %s: time series without WITH FILL", tpl.ID, p.Name)
			}
			if p.PanelType != "timeseries" && strings.Contains(p.Query, "WITH FILL") {
				t.Errorf("%s / %s: WITH FILL on a %s panel", tpl.ID, p.Name, p.PanelType)
			}
		}
	}
}
