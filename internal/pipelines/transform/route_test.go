package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func compileRoute(t *testing.T, cfg string, opts Options) *Route {
	t.Helper()
	r, err := CompileRoute(NodeSpec{ID: "rt", Type: TypeRoute, Label: "Route", Config: cfgOf(t, cfg)}, opts)
	if err != nil {
		t.Fatalf("compile %s: %v", cfg, err)
	}
	return r
}

func routeRecords(t *testing.T) []map[string]any {
	return []map[string]any{
		obj(t, `{"level":"error","code":502}`),
		obj(t, `{"level":"warn","code":404}`),
		obj(t, `{"level":"info","code":200}`),
		obj(t, `{"level":"error","code":200}`),
	}
}

const routeCfg = `{"mode":%q,"unmatched":%q,"routes":[
	{"id":"errors","name":"Errors","rules":[{"field":"level","op":"eq","value":"error"}]},
	{"id":"bad","match":"any","rules":[{"field":"code","op":"gte","value":400},{"field":"level","op":"in","value":["warn"]}]}
]}`

func TestRouteModes(t *testing.T) {
	cases := []struct {
		mode, unmatched string
		handles         string
		outs            string
		out, dropped    int64
	}{
		// Record 0 matches both routes, 1 only "bad", 2 none, 3 only "errors".
		{"first", "drop", "[errors bad]", "[[0 3] [1]]", 3, 1},
		{"all", "drop", "[errors bad]", "[[0 3] [0 1]]", 4, 1},
		{"first", "output", "[errors bad unmatched]", "[[0 3] [1] [2]]", 4, 0},
		{"", "", "[errors bad]", "[[0 3] [1]]", 3, 1}, // defaults: first, drop
	}
	for _, c := range cases {
		t.Run(c.mode+"/"+c.unmatched, func(t *testing.T) {
			r := compileRoute(t, fmt.Sprintf(routeCfg, c.mode, c.unmatched), Options{})
			if got := fmt.Sprint(r.Handles()); got != c.handles {
				t.Fatalf("handles %s, want %s", got, c.handles)
			}
			recs := routeRecords(t)
			before, _ := json.Marshal(recs)
			if got := fmt.Sprint(r.Split(recs)); got != c.outs {
				t.Fatalf("split %s, want %s", got, c.outs)
			}
			if after, _ := json.Marshal(recs); string(after) != string(before) {
				t.Fatal("Split changed the records")
			}
			// Deterministic: the same records route the same way again.
			if got := fmt.Sprint(r.Split(routeRecords(t))); got != c.outs {
				t.Fatalf("second split %s, want %s", got, c.outs)
			}
			cnt := r.Counters()
			if cnt.NodeID != "rt" || cnt.In != 8 || cnt.Out != 2*c.out || cnt.Dropped != 2*c.dropped || cnt.UnmatchedDropped != 2*c.dropped || len(cnt.Outputs) != len(r.Handles()) {
				t.Fatalf("counters %+v %+v", cnt, cnt.RouteCounters)
			}
			if cnt.Outputs[0].Handle != "errors" || cnt.Outputs[0].Out != 4 {
				t.Fatalf("errors output %+v", cnt.Outputs[0])
			}
		})
	}
}

func TestRouteNoRulesMatchesEverything(t *testing.T) {
	// A route without rules matches every record, also with match "any".
	r := compileRoute(t, `{"routes":[{"id":"x","rules":[{"field":"level","op":"eq","value":"nope"}]},{"id":"rest","match":"any"}]}`, Options{})
	if got := fmt.Sprint(r.Split(routeRecords(t))); got != "[[] [0 1 2 3]]" {
		t.Fatalf("split %s", got)
	}
	// No routes at all: everything is unmatched.
	r = compileRoute(t, `{"unmatched":"output"}`, Options{})
	if got := fmt.Sprint(r.Split(routeRecords(t))); got != "[[0 1 2 3]]" {
		t.Fatalf("split %s", got)
	}
}

func TestRouteDropUnmatched(t *testing.T) {
	r := compileRoute(t, fmt.Sprintf(routeCfg, "first", "output"), Options{})
	r.DropUnmatched()
	if got := fmt.Sprint(r.Handles()); got != "[errors bad]" {
		t.Fatalf("handles %s", got)
	}
	r.Split(routeRecords(t))
	if c := r.Counters(); c.UnmatchedDropped != 1 || len(c.Outputs) != 2 {
		t.Fatalf("counters %+v %+v", c, c.RouteCounters)
	}
}

// The route reuses the filter's rules, including field paths of the
// OpenTelemetry layout.
func TestRouteRulesAndPaths(t *testing.T) {
	r := compileRoute(t, `{"routes":[
		{"id":"sev","rules":[{"field":"SeverityText","op":"matches","value":"^(ERROR|FATAL)$"}]},
		{"id":"attr","rules":[{"field":"LogAttributes.team","op":"exists"}]}
	]}`, logsOpts)
	recs := []map[string]any{
		obj(t, `{"SeverityText":"FATAL","LogAttributes":{}}`),
		obj(t, `{"SeverityText":"INFO","LogAttributes":{"team":"pay"}}`),
		obj(t, `{"SeverityText":"INFO","LogAttributes":{}}`),
	}
	if got := fmt.Sprint(r.Split(recs)); got != "[[0] [1]]" {
		t.Fatalf("split %s", got)
	}
}

func TestRouteConfigErrors(t *testing.T) {
	many := make([]string, MaxRoutes+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"id":"r%d"}`, i)
	}
	cases := map[string]string{
		`{"mode":"some"}`:                                                            "Route: mode must be first or all",
		`{"unmatched":"keep"}`:                                                       "Route: unmatched must be drop or output",
		`{"routes":[{"id":"Errors"}]}`:                                               "Route: route 1: id must be 1 to 32 characters",
		`{"routes":[{"id":""}]}`:                                                     "Route: route 1: id must be",
		`{"routes":[{"id":"` + strings.Repeat("a", 33) + `"}]}`:                      "id must be",
		`{"routes":[{"id":"unmatched"}]}`:                                            `id "unmatched" is reserved`,
		`{"routes":[{"id":"a"},{"id":"a","name":"Again"}]}`:                          `Route: route 2 ("Again"): id "a" is used by another route`,
		`{"routes":[{"id":"a","name":"` + strings.Repeat("n", 101) + `"}]}`:          "name is longer than 100",
		`{"routes":[` + strings.Join(many, ",") + `]}`:                               "at most 16 routes",
		`{"routes":[{"id":"a","match":"most"}]}`:                                     "Route: route 1: match must be all or any",
		`{"routes":[{"id":"a","rules":[{"field":"x","op":"like"}]}]}`:                `Route: route 1: rule 1: unknown op "like"`,
		`{"routes":[{"id":"a","rules":[{"field":"x","op":"matches","value":"("}]}]}`: "rule 1: pattern",
	}
	for cfg, want := range cases {
		err := Validate(NodeSpec{ID: "rt", Type: TypeRoute, Label: "Route", Config: cfgOf(t, cfg)}, Options{})
		var ne *NodeError
		if err == nil || !strings.Contains(err.Error(), want) || !errors.As(err, &ne) || ne.NodeID != "rt" {
			t.Errorf("%s: err %v, want %q", cfg, err, want)
		}
	}
	if err := Validate(NodeSpec{ID: "rt", Type: TypeRoute, Config: cfgOf(t, `{}`)}, Options{}); err != nil {
		t.Fatalf("an empty route config is refused: %v", err)
	}
	handles, err := RouteHandles(NodeSpec{ID: "rt", Type: TypeRoute, Config: cfgOf(t, fmt.Sprintf(routeCfg, "all", "output"))})
	if err != nil || fmt.Sprint(handles) != "[errors bad unmatched]" {
		t.Fatalf("RouteHandles %v %v", handles, err)
	}
}

func TestRouteNeedsPro(t *testing.T) {
	specs := []NodeSpec{{ID: "f", Type: TypeFilter}, {ID: "rt", Type: TypeRoute, Label: "Route"}}
	if !NeedsPro(specs) || IsTransform(TypeRoute) || !IsRoute(TypeRoute) {
		t.Fatal("route is not a Pro route node")
	}
	err := RequirePro(specs, false)
	var ne *NodeError
	if !errors.As(err, &ne) || !errors.Is(err, ErrNeedsPro) || ne.Msg != "Routing needs CH-UI Pro" || ne.NodeID != "rt" {
		t.Fatalf("RequirePro: %v", err)
	}
}

func TestRoutePreview(t *testing.T) {
	filter := NodeSpec{ID: "f", Type: TypeFilter, Config: cfgOf(t, `{"mode":"drop","rules":[{"field":"code","op":"eq","value":404}]}`)}
	route := NodeSpec{ID: "rt", Type: TypeRoute, Config: cfgOf(t, fmt.Sprintf(routeCfg, "all", "output"))}
	mapNode := NodeSpec{ID: "m", Type: TypeMap, Config: cfgOf(t, `{"set":[{"field":"routed","value":"yes"}]}`)}
	recs := routeRecords(t)
	before, _ := json.Marshal(recs)

	// Up to the route: the filter drops record 1; of the rest (0, 2, 3)
	// 0 goes to both routes, 2 is unmatched and 3 goes to errors.
	p, err := CompilePreview([]NodeSpec{filter, route}, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	steps := p.Preview(recs)
	if len(steps) != 2 {
		t.Fatalf("steps %d", len(steps))
	}
	st := steps[1]
	if st.NodeID != "rt" || st.RouteResult == nil || fmt.Sprint(st.Kept) != "[0 1 2]" || st.Dropped != 0 ||
		fmt.Sprint(st.Unmatched) != "[1]" || len(st.Routes) != 3 ||
		fmt.Sprintf("%s%v %s%v %s%v", st.Routes[0].Handle, st.Routes[0].Kept, st.Routes[1].Handle, st.Routes[1].Kept, st.Routes[2].Handle, st.Routes[2].Kept) != "errors[0 2] bad[0] unmatched[1]" {
		t.Fatalf("route step %+v %+v", st, st.RouteResult)
	}
	if steps[0].RouteResult != nil {
		t.Fatal("a filter step has route results")
	}

	// A branch: only that output's records go on to the branch nodes.
	p, err = CompilePreview([]NodeSpec{filter, route, mapNode}, "bad", Options{})
	if err != nil {
		t.Fatal(err)
	}
	steps = p.Preview(recs)
	if len(steps) != 3 || fmt.Sprint(steps[1].Kept) != "[0]" || steps[1].Dropped != 2 || len(steps[2].Output) != 1 || steps[2].Output[0]["routed"] != "yes" || steps[2].Output[0]["level"] != "error" {
		t.Fatalf("branch preview %+v", steps)
	}
	if steps[1].Drops[0].Index != 1 || steps[1].Drops[0].Reason != "route: matched no route" || steps[1].Drops[1].Reason != "route: goes to another output" {
		t.Fatalf("drop reasons %+v", steps[1].Drops)
	}
	p, _ = CompilePreview([]NodeSpec{route}, "unmatched", Options{})
	if steps = p.Preview(recs); fmt.Sprint(steps[0].Kept) != "[2]" {
		t.Fatalf("unmatched branch %+v", steps[0])
	}
	if after, _ := json.Marshal(recs); string(after) != string(before) {
		t.Fatal("preview changed the input")
	}
	// The route step's JSON has the route fields; other steps do not.
	b, _ := json.Marshal(steps)
	if !strings.Contains(string(b), `"routes":[{"handle":"errors","kept":[0,3]}`) || !strings.Contains(string(b), `"unmatched":[2]`) {
		t.Fatalf("route step JSON %s", b)
	}
	b, _ = json.Marshal(Step{})
	if strings.Contains(string(b), "routes") {
		t.Fatalf("plain step JSON %s", b)
	}

	// Unmatched records are dropped from the step when unmatched is drop.
	drop := NodeSpec{ID: "rt", Type: TypeRoute, Config: cfgOf(t, fmt.Sprintf(routeCfg, "first", "drop"))}
	p, _ = CompilePreview([]NodeSpec{drop}, "", Options{})
	if st := p.Preview(recs)[0]; fmt.Sprint(st.Kept) != "[0 1 3]" || st.Dropped != 1 || st.Drops[0].Index != 2 || len(st.Routes) != 2 || fmt.Sprint(st.Unmatched) != "[2]" {
		t.Fatalf("drop preview %+v %+v", st, st.RouteResult)
	}

	for _, c := range []struct {
		specs  []NodeSpec
		branch string
		err    string
	}{
		{[]NodeSpec{route, mapNode}, "", "Set branch to preview nodes after a Route node"},
		{[]NodeSpec{route}, "nope", `branch "nope" is not an output of the Route node`},
		{[]NodeSpec{drop}, "unmatched", `branch "unmatched" is not an output`},
		{[]NodeSpec{mapNode}, "bad", "branch needs a Route node in nodes"},
		{[]NodeSpec{route, route}, "bad", "at most one Route node"},
	} {
		if _, err := CompilePreview(c.specs, c.branch, Options{}); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("err %v, want %q", err, c.err)
		}
	}
}
