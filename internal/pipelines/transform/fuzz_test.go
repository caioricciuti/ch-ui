package transform

import "testing"

// The parsers read untrusted log lines: they must never panic. Run with
// go test -fuzz=FuzzLogfmt (or FuzzSyslog, FuzzCombined) for longer.

func FuzzLogfmt(f *testing.F) {
	for _, s := range []string{`a=1 b="x y" c`, `msg="\"" k=`, `="`, `a="\`, ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		m, err := parseLogfmt(s)
		if err == nil && len(m) == 0 {
			t.Fatalf("no error and no fields for %q", s)
		}
	})
}

func FuzzSyslog(f *testing.F) {
	for _, s := range []string{
		`<165>1 2026-10-11T22:14:15.003Z host app - ID47 [a b="]\""] msg`,
		`<34>Oct 11 22:14:15 mymachine su[230]: failed`,
		`<13>text`, `<1>1 - - - - - -`, `<999>`, `<`, `<1>Oct 11 22:14:15 h t[`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = parseSyslog(s)
	})
}

func FuzzCombined(f *testing.F) {
	f.Add(`1.2.3.4 - - [10/Oct/2000:13:55:36 -0700] "GET / HTTP/1.0" 200 2326 "r" "ua \"x\""`)
	f.Add(`1.2.3.4 - - [x] "\" 200 -`)
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = parseCombined(s)
	})
}

func FuzzRedact(f *testing.F) {
	n, err := newRedact(map[string]any{
		"kinds":  []any{KindEmail, KindIPv4, KindIPv6, KindCreditCard, KindJWT, KindBearerToken, KindAWSKey, KindCustom},
		"custom": []any{map[string]any{"name": "x", "pattern": `(a)?b*`}},
	}, Options{})
	if err != nil {
		f.Fatal(err)
	}
	r := n.(*redactNode)
	for _, s := range []string{"bearer abc", "a@b.co 1.2.3.4 ::1 4111111111111111", "eyJa.eyJb.", "AKIAIOSFODNN7EXAMPLE", "std::x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = r.redact(s)
	})
}
