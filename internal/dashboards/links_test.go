package dashboards

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidatePanelLinks(t *testing.T) {
	eleven := "[" + strings.TrimSuffix(strings.Repeat(`{"kind":"setVariable","variable":"a"},`, 11), ",") + "]"
	bad := map[string]string{
		"not an array":       `{"links":{"kind":"url"}}`,
		"too many":           `{"links":` + eleven + `}`,
		"unknown kind":       `{"links":[{"kind":"script","template":"x"}]}`,
		"no variable":        `{"links":[{"kind":"setVariable"}]}`,
		"bad variable":       `{"links":[{"kind":"setVariable","variable":"__from"}]}`,
		"no dashboard":       `{"links":[{"kind":"dashboard"}]}`,
		"dashboard path":     `{"links":[{"kind":"dashboard","dashboardId":"../admin?x=1"}]}`,
		"bad param name":     `{"links":[{"kind":"dashboard","dashboardId":"d1","params":{"a b":"x"}}]}`,
		"raw in param":       `{"links":[{"kind":"dashboard","dashboardId":"d1","params":{"a":"${__value:raw}"}}]}`,
		"empty sql":          `{"links":[{"kind":"sql","template":"  "}]}`,
		"javascript url":     `{"links":[{"kind":"url","template":"javascript:alert(1)"}]}`,
		"data url":           `{"links":[{"kind":"url","template":"data:text/html,x"}]}`,
		"protocol relative":  `{"links":[{"kind":"url","template":"//evil.example"}]}`,
		"leading space":      `{"links":[{"kind":"url","template":" https://x.example"}]}`,
		"token as scheme":    `{"links":[{"kind":"url","template":"${__value}"}]}`,
		"raw in url":         `{"links":[{"kind":"url","template":"https://x.example/${__value:raw}"}]}`,
		"raw in url, caps":   `{"links":[{"kind":"url","template":"https://x.example/${__VALUE:RAW}"}]}`,
		"long label":         `{"links":[{"kind":"url","label":"` + strings.Repeat("x", 101) + `","template":"https://x.example"}]}`,
		"long url":           `{"links":[{"kind":"url","template":"https://` + strings.Repeat("x", 4000) + `"}]}`,
		"raw in setVariable": `{"links":[{"kind":"setVariable","variable":"a","template":"${__value:raw}"}]}`,
	}
	for name, cfg := range bad {
		if err := ValidatePanelLinks(cfg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	good := map[string]string{
		"no links":   `{"chartType":"bar"}`,
		"null links": `{"links":null}`,
		"empty":      `{"links":[]}`,
		"all kinds": `{"links":[
			{"kind":"setVariable","variable":"host"},
			{"kind":"dashboard","dashboardId":"d1","carryVariables":true,"params":{"host":"${__value}"}},
			{"kind":"sql","template":"SELECT * FROM t WHERE x = ${__value:raw}"},
			{"kind":"url","label":"Docs","template":"HTTPS://example.com/?q=${__value}"}]}`,
		"not an object": `[1,2]`,
	}
	for name, cfg := range good {
		if err := ValidatePanelLinks(cfg); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPublicPanelConfigKeepsOnlySafeLinks(t *testing.T) {
	cfg := `{"chartType":"bar","links":[
		{"kind":"setVariable","label":"Filter","variable":"host","template":"leftover"},
		{"kind":"dashboard","dashboardId":"secret-id","params":{"a":"b"}},
		{"kind":"sql","template":"SELECT secret"},
		{"kind":"url","template":"https://x.example/${__value}","dashboardId":"smuggled"}]}`
	out := PublicPanelConfig(cfg)
	for _, leak := range []string{"secret", "smuggled", "leftover"} {
		if strings.Contains(out, leak) {
			t.Fatalf("public config leaks %q: %s", leak, out)
		}
	}
	var got struct {
		ChartType string      `json:"chartType"`
		Links     []PanelLink `json:"links"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChartType != "bar" || len(got.Links) != 2 || got.Links[0].Variable != "host" || got.Links[1].Kind != LinkURL {
		t.Fatalf("unexpected public config: %s", out)
	}

	if out := PublicPanelConfig(`{"links":[{"kind":"sql","template":"x"}]}`); strings.Contains(out, "links") {
		t.Fatalf("links key kept with nothing in it: %s", out)
	}
	if out := PublicPanelConfig(`{"chartType":"pie"}`); out != `{"chartType":"pie"}` {
		t.Fatalf("config without links changed: %s", out)
	}
}
