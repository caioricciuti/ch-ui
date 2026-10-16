package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var logsOpts = Options{Signal: SignalLogs}

// obj decodes a JSON object the way the runner does (json.Number).
func obj(t testing.TB, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("bad test JSON %s: %v", s, err)
	}
	return m
}

func cfgOf(t testing.TB, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad test config %s: %v", s, err)
	}
	return m
}

// runOne compiles one node and runs it on rec (in place).
func runOne(t *testing.T, typ, cfg string, opts Options, rec map[string]any) (bool, error) {
	t.Helper()
	p, err := compileNode(NodeSpec{ID: "n", Type: typ, Config: cfgOf(t, cfg)}, opts)
	if err != nil {
		t.Fatalf("compile %s %s: %v", typ, cfg, err)
	}
	keep, _, perr := p.process(rec)
	return keep, perr
}

func jsonEq(t *testing.T, got map[string]any, want string) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(obj(t, want))
	if string(g) != string(w) {
		t.Fatalf("record\n got %s\nwant %s", g, w)
	}
}

func TestParsePath(t *testing.T) {
	cases := []struct {
		in     string
		opts   Options
		segs   []string
		mapCol bool
		err    bool
	}{
		{in: "name", segs: []string{"name"}},
		{in: "a.b.c", segs: []string{"a", "b", "c"}},
		{in: "a['b.c'].d", segs: []string{"a", "b.c", "d"}},
		{in: `a["x"]["y"]`, segs: []string{"a", "x", "y"}},
		{in: "LogAttributes.http.method", opts: logsOpts, segs: []string{"LogAttributes", "http.method"}, mapCol: true},
		{in: "LogAttributes['k']", opts: logsOpts, segs: []string{"LogAttributes", "k"}, mapCol: true},
		{in: "LogAttributes", opts: logsOpts, segs: []string{"LogAttributes"}, mapCol: true},
		// Without the OpenTelemetry layout a dot is nesting.
		{in: "LogAttributes.http.method", segs: []string{"LogAttributes", "http", "method"}},
		{in: "", err: true},
		{in: "a..b", err: true},
		{in: "a.", err: true},
		{in: "a[b]", err: true},
		{in: "a['b'", err: true},
		{in: "a['']", err: true},
		{in: "a['b']c", err: true},
		{in: "LogAttributes['a']['b']", opts: logsOpts, err: true},
		{in: strings.Repeat("a", maxPathLen+1), err: true},
	}
	for _, c := range cases {
		p, err := ParsePath(c.in, c.opts)
		if c.err {
			if err == nil {
				t.Errorf("ParsePath(%q) = %v, want an error", c.in, p.segs)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePath(%q): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(p.segs, c.segs) || p.mapCol != c.mapCol {
			t.Errorf("ParsePath(%q) = %q mapCol=%v, want %q mapCol=%v", c.in, p.segs, p.mapCol, c.segs, c.mapCol)
		}
	}
}

func TestStringify(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"x", "x"}, {json.Number("12345678901234567890"), "12345678901234567890"}, {float64(1.5), "1.5"},
		{float64(3), "3"}, {true, "true"}, {nil, ""}, {map[string]any{"b": 1, "a": "x"}, `{"a":"x","b":1}`}, {[]any{"a", json.Number("1")}, `["a",1]`},
	}
	for _, c := range cases {
		if got := Stringify(c.in); got != c.want {
			t.Errorf("Stringify(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseNode(t *testing.T) {
	otelRow := `{"Body":"level=warn msg=\"disk almost full\" pct=91","SeverityText":"","LogAttributes":{"existing":"1"}}`
	cases := []struct {
		name    string
		cfg     string
		opts    Options
		in      string
		want    string
		keep    bool
		wantErr bool
	}{
		{name: "json root", cfg: `{"field":"msg","format":"json"}`, in: `{"msg":"{\"a\":1,\"b\":{\"c\":true}}"}`,
			want: `{"msg":"{\"a\":1,\"b\":{\"c\":true}}","a":1,"b":{"c":true}}`, keep: true},
		{name: "json big integer keeps digits", cfg: `{"field":"msg","format":"json"}`, in: `{"msg":"{\"id\":12345678901234567890}"}`,
			want: `{"msg":"{\"id\":12345678901234567890}","id":12345678901234567890}`, keep: true},
		{name: "json prefix remove_source", cfg: `{"field":"msg","format":"json","prefix":"p_","remove_source":true}`, in: `{"msg":"{\"a\":\"x\"}"}`,
			want: `{"p_a":"x"}`, keep: true},
		{name: "json nested target", cfg: `{"field":"msg","format":"json","target":"parsed.inner"}`, in: `{"msg":"{\"a\":1}"}`,
			want: `{"msg":"{\"a\":1}","parsed":{"inner":{"a":1}}}`, keep: true},
		{name: "json object already decoded", cfg: `{"field":"payload","format":"json"}`, in: `{"payload":{"a":1}}`,
			want: `{"payload":{"a":1},"a":1}`, keep: true},
		{name: "otel logs default target is LogAttributes and stringifies", cfg: `{"format":"json"}`, opts: logsOpts,
			in:   `{"Body":"{\"n\":3,\"ok\":true,\"o\":{\"x\":1}}","LogAttributes":{}}`,
			want: `{"Body":"{\"n\":3,\"ok\":true,\"o\":{\"x\":1}}","LogAttributes":{"n":"3","ok":"true","o":"{\"x\":1}"}}`, keep: true},
		{name: "otel logs explicit empty target is root", cfg: `{"field":"Body","format":"json","target":""}`, opts: logsOpts,
			in: `{"Body":"{\"n\":3}"}`, want: `{"Body":"{\"n\":3}","n":3}`, keep: true},
		{name: "otel logs logfmt", cfg: `{"field":"Body","format":"logfmt","target":"LogAttributes"}`, opts: logsOpts, in: otelRow,
			want: `{"Body":"level=warn msg=\"disk almost full\" pct=91","SeverityText":"","LogAttributes":{"existing":"1","level":"warn","msg":"disk almost full","pct":"91"}}`, keep: true},
		{name: "json invalid keep", cfg: `{"field":"msg","format":"json"}`, in: `{"msg":"not json"}`, want: `{"msg":"not json"}`, keep: true, wantErr: true},
		{name: "json array is not an object", cfg: `{"field":"msg","format":"json","on_error":"drop"}`, in: `{"msg":"[1,2]"}`, want: `{"msg":"[1,2]"}`, keep: false, wantErr: true},
		{name: "missing field drop", cfg: `{"field":"msg","format":"json","on_error":"drop"}`, in: `{"other":1}`, want: `{"other":1}`, keep: false, wantErr: true},
		{name: "regex named groups", cfg: `{"field":"line","format":"regex","pattern":"^(?P<ip>\\S+) (?P<code>\\d+)( (?P<opt>x))?"}`, in: `{"line":"10.0.0.1 503"}`,
			want: `{"line":"10.0.0.1 503","ip":"10.0.0.1","code":"503"}`, keep: true},
		{name: "regex no match", cfg: `{"field":"line","format":"regex","pattern":"^(?P<n>\\d+)$"}`, in: `{"line":"abc"}`, want: `{"line":"abc"}`, keep: true, wantErr: true},
		{name: "nginx combined", cfg: `{"field":"line","format":"nginx","remove_source":true}`,
			in:   `{"line":"203.0.113.9 - alice [10/Oct/2026:13:55:36 +0000] \"GET /api/x?y=1 HTTP/1.1\" 200 2326 \"https://ref.example/\" \"curl/8.4 \\\"q\\\"\""}`,
			want: `{"client_ip":"203.0.113.9","remote_user":"alice","time_local":"10/Oct/2026:13:55:36 +0000","request":"GET /api/x?y=1 HTTP/1.1","method":"GET","path":"/api/x?y=1","protocol":"HTTP/1.1","status":"200","body_bytes_sent":"2326","http_referer":"https://ref.example/","http_user_agent":"curl/8.4 \"q\""}`, keep: true},
		{name: "apache common log", cfg: `{"field":"line","format":"apache","remove_source":true}`,
			in:   `{"line":"127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] \"GET /apache_pb.gif HTTP/1.0\" 404 -"}`,
			want: `{"client_ip":"127.0.0.1","time_local":"10/Oct/2000:13:55:36 -0700","request":"GET /apache_pb.gif HTTP/1.0","method":"GET","path":"/apache_pb.gif","protocol":"HTTP/1.0","status":"404"}`, keep: true},
		{name: "syslog 5424", cfg: `{"field":"m","format":"syslog","remove_source":true}`,
			in:   `{"m":"<165>1 2026-10-11T22:14:15.003Z mymachine.example.com evntslog - ID47 [exampleSDID@32473 iut=\"3\" eventSource=\"App\\]lication\"] An application event"}`,
			want: `{"priority":"165","facility":"20","severity":"notice","version":"1","timestamp":"2026-10-11T22:14:15.003Z","hostname":"mymachine.example.com","app_name":"evntslog","msg_id":"ID47","structured_data":"[exampleSDID@32473 iut=\"3\" eventSource=\"App\\]lication\"]","message":"An application event"}`, keep: true},
		{name: "syslog 5424 no sd no msg", cfg: `{"field":"m","format":"syslog","remove_source":true}`,
			in: `{"m":"<14>1 - host app 123 - -"}`, want: `{"priority":"14","facility":"1","severity":"info","version":"1","hostname":"host","app_name":"app","proc_id":"123"}`, keep: true},
		{name: "syslog 3164", cfg: `{"field":"m","format":"syslog","remove_source":true}`,
			in: `{"m":"<34>Oct 11 22:14:15 mymachine su[230]: 'su root' failed on /dev/pts/8"}`, want: `{"priority":"34","facility":"4","severity":"crit","timestamp":"Oct 11 22:14:15","hostname":"mymachine","app_name":"su","proc_id":"230","message":"'su root' failed on /dev/pts/8"}`, keep: true},
		{name: "syslog 3164 no header", cfg: `{"field":"m","format":"syslog","remove_source":true}`,
			in: `{"m":"<13>just text"}`, want: `{"priority":"13","facility":"1","severity":"notice","message":"just text"}`, keep: true},
		{name: "syslog not syslog", cfg: `{"field":"m","format":"syslog"}`, in: `{"m":"hello"}`, want: `{"m":"hello"}`, keep: true, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := obj(t, c.in)
			keep, err := runOne(t, TypeParse, c.cfg, c.opts, rec)
			if keep != c.keep || (err != nil) != c.wantErr {
				t.Fatalf("keep=%v err=%v, want keep=%v err=%v", keep, err, c.keep, c.wantErr)
			}
			jsonEq(t, rec, c.want)
		})
	}
}

func TestLogfmt(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]any
		err  bool
	}{
		{in: `a=1 b="x y" c`, want: map[string]any{"a": "1", "b": "x y", "c": "true"}},
		{in: `msg="say \"hi\"\n" empty= k=v`, want: map[string]any{"msg": "say \"hi\"\n", "empty": "", "k": "v"}},
		{in: `just words`, err: true},
		{in: `=v`, err: true},
		{in: `a="unterminated`, err: true},
		{in: `a"b=1`, err: true},
		{in: ``, err: true},
	}
	for _, c := range cases {
		got, err := parseLogfmt(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseLogfmt(%q) = %v, want an error", c.in, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseLogfmt(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
}

func TestMapNode(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		opts    Options
		in      string
		want    string
		wantErr bool
	}{
		{name: "order copy rename set drop",
			cfg:  `{"copy":[{"from":"a","to":"a_copy"}],"rename":[{"from":"a","to":"b.c"}],"set":[{"field":"env","value":"prod"},{"field":"n","value":3}],"drop":["tmp","missing"]}`,
			in:   `{"a":{"x":1},"tmp":"t"}`,
			want: `{"a_copy":{"x":1},"b":{"c":{"x":1}},"env":"prod","n":3}`},
		{name: "rename missing is skipped", cfg: `{"rename":[{"from":"nope","to":"x"}]}`, in: `{"a":1}`, want: `{"a":1}`},
		{name: "set into map column stringifies", cfg: `{"set":[{"field":"ResourceAttributes['deployment.env']","value":42}]}`, opts: logsOpts,
			in: `{"ResourceAttributes":{"service.name":"api"}}`, want: `{"ResourceAttributes":{"service.name":"api","deployment.env":"42"}}`},
		{name: "rename map key to top level", cfg: `{"rename":[{"from":"LogAttributes.user.id","to":"UserId"}]}`, opts: logsOpts,
			in: `{"LogAttributes":{"user.id":"7","k":"v"}}`, want: `{"LogAttributes":{"k":"v"},"UserId":"7"}`},
		{name: "severity otel", cfg: `{"severity_from":"LogAttributes.level"}`, opts: logsOpts,
			in: `{"SeverityText":"","SeverityNumber":0,"LogAttributes":{"level":"Warning"}}`, want: `{"SeverityText":"WARN","SeverityNumber":13,"LogAttributes":{"level":"Warning"}}`},
		{name: "severity otel each level", cfg: `{"severity_from":"lvl"}`, opts: logsOpts,
			in: `{"lvl":"CRITICAL"}`, want: `{"lvl":"CRITICAL","SeverityText":"FATAL","SeverityNumber":21}`},
		{name: "severity unknown untouched", cfg: `{"severity_from":"lvl"}`, opts: logsOpts,
			in: `{"lvl":"chatty","SeverityText":"x"}`, want: `{"lvl":"chatty","SeverityText":"x"}`},
		{name: "severity plain sets severity", cfg: `{"severity_from":"level"}`, in: `{"level":"err"}`, want: `{"level":"err","severity":"ERROR"}`},
		{name: "timestamp rfc3339 otel", cfg: `{"timestamp_from":"ts"}`, opts: logsOpts,
			in: `{"ts":"2026-10-16T10:00:00.123+02:00"}`, want: `{"ts":"2026-10-16T10:00:00.123+02:00","Timestamp":"1792137600.123000000"}`},
		{name: "timestamp unix decimal string", cfg: `{"timestamp_from":"ts","timestamp_format":"unix"}`, opts: logsOpts,
			in: `{"ts":"1700000000.5"}`, want: `{"ts":"1700000000.5","Timestamp":"1700000000.500000000"}`},
		{name: "timestamp unix_ms number", cfg: `{"timestamp_from":"ts","timestamp_format":"unix_ms"}`, opts: logsOpts,
			in: `{"ts":1700000000123}`, want: `{"ts":1700000000123,"Timestamp":"1700000000.123000000"}`},
		{name: "timestamp unix_ns", cfg: `{"timestamp_from":"ts","timestamp_format":"unix_ns"}`, opts: logsOpts,
			in: `{"ts":"1700000000000000007"}`, want: `{"ts":"1700000000000000007","Timestamp":"1700000000.000000007"}`},
		{name: "timestamp layout plain", cfg: `{"timestamp_from":"t","timestamp_format":"layout:02/Jan/2006:15:04:05 -0700"}`,
			in: `{"t":"10/Oct/2026:13:55:36 +0200"}`, want: `{"t":"10/Oct/2026:13:55:36 +0200","timestamp":"2026-10-10T11:55:36Z"}`},
		{name: "timestamp bad counts an error", cfg: `{"timestamp_from":"ts","timestamp_format":"unix"}`,
			in: `{"ts":"yesterday"}`, want: `{"ts":"yesterday"}`, wantErr: true},
		{name: "set through a non-object reports an error", cfg: `{"set":[{"field":"a.b","value":1}]}`,
			in: `{"a":"scalar"}`, want: `{"a":"scalar"}`, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := obj(t, c.in)
			keep, err := runOne(t, TypeMap, c.cfg, c.opts, rec)
			if !keep || (err != nil) != c.wantErr {
				t.Fatalf("keep=%v err=%v, wantErr=%v", keep, err, c.wantErr)
			}
			jsonEq(t, rec, c.want)
		})
	}
}

func TestSeverityLevels(t *testing.T) {
	cases := map[string]int{"debug": 5, "INFO": 9, "warn": 13, "warning": 13, "error": 17, "err": 17, "fatal": 21, "critical": 21, "trace": 1, "notice": 9}
	for word, num := range cases {
		if _, got, ok := severityOf(word); !ok || got != num {
			t.Errorf("severityOf(%q) = %d %v, want %d", word, got, ok, num)
		}
	}
}

func TestFilterOps(t *testing.T) {
	rec := `{"SeverityText":"ERROR","n":12,"s":"10","msg":"disk full on /var","LogAttributes":{"code":"503"},"nul":null}`
	cases := []struct {
		rule  string
		match bool
	}{
		{`{"field":"SeverityText","op":"eq","value":"ERROR"}`, true},
		{`{"field":"SeverityText","op":"eq","value":"error"}`, false},
		{`{"field":"n","op":"eq","value":"12.0"}`, true}, // numeric when both parse
		{`{"field":"n","op":"eq","value":12}`, true},
		{`{"field":"SeverityText","op":"ne","value":"INFO"}`, true},
		{`{"field":"absent","op":"ne","value":"x"}`, true},
		{`{"field":"msg","op":"contains","value":"full"}`, true},
		{`{"field":"msg","op":"not_contains","value":"full"}`, false},
		{`{"field":"absent","op":"not_contains","value":"x"}`, true},
		{`{"field":"msg","op":"matches","value":"^disk \\w+ on /"}`, true},
		{`{"field":"absent","op":"matches","value":".*"}`, false},
		{`{"field":"n","op":"gt","value":9}`, true},
		{`{"field":"s","op":"gt","value":"9"}`, true}, // numbers, not "10" < "9"
		{`{"field":"n","op":"gte","value":12}`, true},
		{`{"field":"n","op":"lt","value":12}`, false},
		{`{"field":"n","op":"lte","value":"12"}`, true},
		{`{"field":"SeverityText","op":"gt","value":"DEBUG"}`, true}, // text compare
		{`{"field":"absent","op":"gt","value":0}`, false},
		{`{"field":"LogAttributes.code","op":"in","value":["500","503"]}`, true},
		{`{"field":"LogAttributes.code","op":"in","value":"[500, 502]"}`, false},
		{`{"field":"n","op":"in","value":"[12]"}`, true},
		{`{"field":"msg","op":"exists"}`, true},
		{`{"field":"nul","op":"exists"}`, false},
		{`{"field":"nul","op":"missing"}`, true},
		{`{"field":"absent","op":"missing"}`, true},
	}
	for _, c := range cases {
		cfg := `{"mode":"keep","rules":[` + c.rule + `]}`
		keep, err := runOne(t, TypeFilter, cfg, logsOpts, obj(t, rec))
		if err != nil || keep != c.match {
			t.Errorf("rule %s: keep=%v err=%v, want %v", c.rule, keep, err, c.match)
		}
		drop := `{"mode":"drop","rules":[` + c.rule + `]}`
		keep, _ = runOne(t, TypeFilter, drop, logsOpts, obj(t, rec))
		if keep == c.match {
			t.Errorf("drop rule %s: keep=%v, want %v", c.rule, keep, !c.match)
		}
	}
}

func TestFilterMatchAllAny(t *testing.T) {
	rec := `{"a":"1","b":"2"}`
	rules := `[{"field":"a","op":"eq","value":"1"},{"field":"b","op":"eq","value":"x"}]`
	if keep, _ := runOne(t, TypeFilter, `{"mode":"keep","match":"all","rules":`+rules+`}`, Options{}, obj(t, rec)); keep {
		t.Error("all: one rule fails, record kept")
	}
	if keep, _ := runOne(t, TypeFilter, `{"mode":"keep","match":"any","rules":`+rules+`}`, Options{}, obj(t, rec)); !keep {
		t.Error("any: one rule matches, record dropped")
	}
	// No rules is a no-op in both modes.
	for _, mode := range []string{"keep", "drop"} {
		if keep, _ := runOne(t, TypeFilter, `{"mode":"`+mode+`","rules":[]}`, Options{}, obj(t, rec)); !keep {
			t.Errorf("%s without rules dropped the record", mode)
		}
	}
}

func TestSampleDeterministicAndDistributed(t *testing.T) {
	p, err := compileNode(NodeSpec{Type: TypeSample, Config: cfgOf(t, `{"keep_percent":10}`)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	const n = 20000
	kept := 0
	for i := 0; i < n; i++ {
		rec := map[string]any{"i": json.Number(fmt.Sprint(i)), "msg": "hello"}
		k1, _, _ := p.process(rec)
		k2, _, _ := p.process(map[string]any{"msg": "hello", "i": json.Number(fmt.Sprint(i))})
		if k1 != k2 {
			t.Fatalf("record %d sampled differently on a second run", i)
		}
		if k1 {
			kept++
		}
	}
	if pct := float64(kept) * 100 / n; pct < 9 || pct > 11 {
		t.Fatalf("kept %.2f%%, want 10%% +- 1", pct)
	}

	// Keyed on TraceId: every span of a trace has the same fate.
	p, _ = compileNode(NodeSpec{Type: TypeSample, Config: cfgOf(t, `{"keep_percent":"50","key":"TraceId"}`)}, Options{})
	for tr := 0; tr < 200; tr++ {
		id := fmt.Sprintf("trace-%d", tr)
		first, _, _ := p.process(map[string]any{"TraceId": id, "span": "0"})
		for s := 1; s < 5; s++ {
			if k, _, _ := p.process(map[string]any{"TraceId": id, "span": fmt.Sprint(s)}); k != first {
				t.Fatalf("trace %s split by sampling", id)
			}
		}
	}

	// The bucket is FNV-1a 64 of the key mod 10000.
	if got := sampleBucket(map[string]any{"k": "abc"}, &Path{segs: []string{"k"}}); got != 0xe71fa2190541574b%10000 {
		t.Fatalf("bucket(abc) = %d", got)
	}

	// always_keep wins over a 0% sample.
	p, _ = compileNode(NodeSpec{Type: TypeSample, Config: cfgOf(t, `{"keep_percent":0,"always_keep":{"match":"any","rules":[{"field":"SeverityText","op":"in","value":["ERROR","FATAL"]}]}}`)}, Options{})
	if k, _, _ := p.process(map[string]any{"SeverityText": "ERROR"}); !k {
		t.Fatal("always_keep record dropped")
	}
	if k, _, _ := p.process(map[string]any{"SeverityText": "INFO"}); k {
		t.Fatal("0% sample kept a record")
	}
	p, _ = compileNode(NodeSpec{Type: TypeSample, Config: cfgOf(t, `{"keep_percent":100}`)}, Options{})
	for i := 0; i < 1000; i++ {
		if k, _, _ := p.process(map[string]any{"i": i}); !k {
			t.Fatal("100% sample dropped a record")
		}
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		in   string
		want string
	}{
		{name: "email everywhere incl maps and arrays", cfg: `{"kinds":["email"]}`,
			in:   `{"Body":"user bob@example.com logged in","LogAttributes":{"to":"a.b+c@mail.example.org"},"list":["x@y.io",1]}`,
			want: `{"Body":"user [REDACTED:email] logged in","LogAttributes":{"to":"[REDACTED:email]"},"list":["[REDACTED:email]",1]}`},
		{name: "ipv4 and ipv6", cfg: `{"kinds":["ipv4","ipv6"]}`,
			in:   `{"m":"from 192.168.1.20 and 2001:db8::8a2e:370:7334 at 10:30:00 std::vector 999.1.1.1"}`,
			want: `{"m":"from [REDACTED:ipv4] and [REDACTED:ipv6] at 10:30:00 std::vector 999.1.1.1"}`},
		{name: "credit card with Luhn", cfg: `{"kinds":["credit_card"]}`,
			in:   `{"m":"card 4111 1111 1111 1111 ok, 4111-1111-1111-1112 bad, order 1234567890123 bad, 5500005555555559"}`,
			want: `{"m":"card [REDACTED:credit_card] ok, 4111-1111-1111-1112 bad, order 1234567890123 bad, [REDACTED:credit_card]"}`},
		{name: "jwt bearer aws", cfg: `{"kinds":["jwt","bearer_token","aws_key"]}`,
			in:   `{"h":"Authorization: Bearer abc.def-123","t":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig_nature","k":"key AKIAIOSFODNN7EXAMPLE end"}`,
			want: `{"h":"Authorization: Bearer [REDACTED:bearer_token]","t":"[REDACTED:jwt]","k":"key [REDACTED:aws_key] end"}`},
		{name: "custom and replacement", cfg: `{"kinds":["custom"],"custom":[{"name":"order id","pattern":"ORD-\\d+"}],"replacement":"<{kind}>"}`,
			in: `{"m":"ORD-12 and ORD-999"}`, want: `{"m":"<order id> and <order id>"}`},
		{name: "only listed fields", cfg: `{"kinds":["email"],"fields":["a","LogAttributes.mail"]}`,
			in:   `{"a":"x@y.io","b":"x@y.io","LogAttributes":{"mail":"q@w.io"}}`,
			want: `{"a":"[REDACTED:email]","b":"x@y.io","LogAttributes":{"mail":"[REDACTED:email]"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := obj(t, c.in)
			keep, err := runOne(t, TypeRedact, c.cfg, logsOpts, rec)
			if !keep || err != nil {
				t.Fatalf("keep=%v err=%v", keep, err)
			}
			jsonEq(t, rec, c.want)
		})
	}
}

func TestLuhn(t *testing.T) {
	for s, want := range map[string]bool{"4111111111111111": true, "4111111111111112": false, "378282246310005": true, "6011 1111 1111 1117": true, "123": false} {
		if got := luhnValid(s, 0, len(s)); got != want {
			t.Errorf("luhnValid(%q) = %v", s, got)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		typ, cfg, want string
	}{
		{TypeParse, `{"format":"json"}`, "field path is empty"},
		{TypeParse, `{"field":"x"}`, "format is required"},
		{TypeParse, `{"field":"x","format":"xml"}`, "unknown format"},
		{TypeParse, `{"field":"x","format":"regex","pattern":"(\\d+"}`, "pattern"},
		{TypeParse, `{"field":"x","format":"regex","pattern":"\\d+"}`, "named group"},
		{TypeParse, `{"field":"x","format":"regex","pattern":"(?P<a>` + strings.Repeat("a", maxPatternLen) + `)"}`, "longer than"},
		{TypeParse, `{"field":"x","format":"json","on_error":"explode"}`, "on_error"},
		{TypeParse, `{"field":"a..b","format":"json"}`, "empty segment"},
		{TypeMap, `{"rename":[{"from":"","to":"b"}]}`, "rename from"},
		{TypeMap, `{"timestamp_from":"ts","timestamp_format":"weird"}`, "timestamp_format"},
		{TypeFilter, `{"mode":"maybe"}`, "mode"},
		{TypeFilter, `{"rules":[{"field":"a","op":"like","value":"x"}]}`, "unknown op"},
		{TypeFilter, `{"rules":[{"field":"a","op":"matches","value":"(["}]}`, "pattern"},
		{TypeFilter, `{"rules":[{"field":"a","op":"in","value":"x"}]}`, "JSON array"},
		{TypeFilter, `{"rules":[` + strings.TrimSuffix(strings.Repeat(`{"field":"a","op":"exists"},`, maxRules+1), ",") + `]}`, "at most"},
		{TypeSample, `{}`, "keep_percent"},
		{TypeSample, `{"keep_percent":101}`, "keep_percent"},
		{TypeSample, `{"keep_percent":"ten"}`, "number"},
		{TypeRedact, `{"kinds":[]}`, "at least one kind"},
		{TypeRedact, `{"kinds":["phone"]}`, "unknown kind"},
		{TypeRedact, `{"kinds":["custom"]}`, "custom needs"},
		{TypeRedact, `{"kinds":["custom"],"custom":[{"name":"x","pattern":"(a"}]}`, "custom pattern 1"},
		{"transform_route", `{}`, "unknown transform type"},
	}
	for _, c := range cases {
		_, err := Compile([]NodeSpec{{ID: "id1", Type: c.typ, Label: "My node", Config: cfgOf(t, c.cfg)}}, Options{})
		var ne *NodeError
		if err == nil || !errors.As(err, &ne) || ne.NodeID != "id1" || !strings.HasPrefix(err.Error(), "My node: ") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: err = %v, want a node error containing %q", c.typ, c.cfg, err, c.want)
		}
	}
	many := make([]NodeSpec, MaxNodes+1)
	for i := range many {
		many[i] = NodeSpec{Type: TypeMap}
	}
	if _, err := Compile(many, Options{}); err == nil {
		t.Error("more than MaxNodes nodes accepted")
	}
}

func TestRequirePro(t *testing.T) {
	specs := []NodeSpec{{ID: "p", Type: TypeParse}, {ID: "s", Type: TypeSample, Label: "Keep 10%"}}
	err := RequirePro(specs, false)
	if err == nil || !errors.Is(err, ErrNeedsPro) || err.Error() != "Keep 10%: Sampling needs CH-UI Pro" {
		t.Fatalf("err = %v", err)
	}
	if err := RequirePro(specs, true); err != nil {
		t.Fatal(err)
	}
	if err := RequirePro(specs[:1], false); err != nil {
		t.Fatalf("free node refused: %v", err)
	}
	if err := RequirePro([]NodeSpec{{ID: "r", Type: TypeRedact}}, false); err == nil || !strings.Contains(err.Error(), "Redaction needs CH-UI Pro") {
		t.Fatalf("redact err = %v", err)
	}
}

func testChain(t testing.TB) *Chain {
	t.Helper()
	c, err := Compile([]NodeSpec{
		{ID: "parse", Type: TypeParse, Config: cfgOf(t, `{"field":"Body","format":"logfmt","target":"LogAttributes"}`)},
		{ID: "map", Type: TypeMap, Config: cfgOf(t, `{"severity_from":"LogAttributes.level"}`)},
		{ID: "filter", Type: TypeFilter, Config: cfgOf(t, `{"mode":"drop","rules":[{"field":"SeverityText","op":"eq","value":"DEBUG"}]}`)},
	}, logsOpts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestChainRunCounters(t *testing.T) {
	c := testChain(t)
	recs := []map[string]any{
		obj(t, `{"Body":"level=debug msg=a","LogAttributes":{}}`),
		obj(t, `{"Body":"level=info msg=b","LogAttributes":{}}`),
		obj(t, `{"Body":"not logfmt","LogAttributes":{}}`),
		obj(t, `{"Body":"level=error msg=c","LogAttributes":{}}`),
	}
	kept, idx := c.Run(recs)
	if len(kept) != 3 || !reflect.DeepEqual(idx, []int{1, 2, 3}) {
		t.Fatalf("kept %d idx %v", len(kept), idx)
	}
	if kept[0]["SeverityText"] != "INFO" || kept[2]["SeverityText"] != "ERROR" {
		t.Fatalf("kept rows %v", kept)
	}
	want := []Counters{
		{NodeID: "parse", In: 4, Out: 4, Errors: 1},
		{NodeID: "map", In: 4, Out: 4},
		{NodeID: "filter", In: 4, Out: 3, Dropped: 1},
	}
	if got := c.Counters(); !reflect.DeepEqual(got, want) {
		t.Fatalf("counters %+v, want %+v", got, want)
	}
}

func TestPreview(t *testing.T) {
	c := testChain(t)
	in := []map[string]any{
		obj(t, `{"Body":"level=debug msg=a","LogAttributes":{}}`),
		obj(t, `{"Body":"plain","LogAttributes":{}}`),
	}
	before, _ := json.Marshal(in)
	steps := c.Preview(in)
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatal("preview changed its input")
	}
	if len(steps) != 3 {
		t.Fatalf("%d steps", len(steps))
	}
	if len(steps[0].Output) != 2 || len(steps[0].Errors) != 1 || steps[0].Errors[0].Index != 1 || fmt.Sprint(steps[0].Kept) != "[0 1]" {
		t.Fatalf("parse step %+v", steps[0])
	}
	if steps[2].Dropped != 1 || len(steps[2].Output) != 1 || steps[2].Drops[0].Index != 0 || steps[2].Drops[0].Reason != "filter: matched the drop rules" || len(steps[2].Kept) != 1 || steps[2].Kept[0] != 1 {
		t.Fatalf("filter step %+v", steps[2])
	}
	for _, c := range c.Counters() {
		if c.In != 0 {
			t.Fatal("preview moved the counters")
		}
	}
}

// Redaction errors and drop reasons never carry the original values.
// Rule values arrive as strings from the editor, "in" as JSON array text.
func TestFilterStringValues(t *testing.T) {
	rec := `{"SeverityText":"ERROR","n":12}`
	for rule, want := range map[string]bool{
		`{"field":"SeverityText","op":"in","value":"[\"ERROR\",\"FATAL\"]"}`: true,
		`{"field":"SeverityText","op":"in","value":["WARN"]}`:                false,
		`{"field":"n","op":"gte","value":"12"}`:                              true,
		`{"field":"n","op":"eq","value":"12"}`:                               true,
	} {
		if keep, err := runOne(t, TypeFilter, `{"mode":"keep","rules":[`+rule+`]}`, Options{}, obj(t, rec)); err != nil || keep != want {
			t.Errorf("%s: keep=%v err=%v, want %v", rule, keep, err, want)
		}
	}
}

func TestMessagesDoNotLeakValues(t *testing.T) {
	c, err := Compile([]NodeSpec{{ID: "p", Type: TypeParse, Config: cfgOf(t, `{"field":"secret","format":"json","on_error":"drop"}`)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	steps := c.Preview([]map[string]any{{"secret": "hunter2-password"}})
	b, _ := json.Marshal(steps)
	if strings.Contains(string(b), "hunter2") {
		t.Fatalf("value leaked into the preview messages: %s", b)
	}
}
