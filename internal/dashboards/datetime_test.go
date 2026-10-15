package dashboards

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDateTimeZone(t *testing.T) {
	cases := map[string]struct {
		tz string
		ok bool
	}{
		"DateTime":                                {"", true},
		"DateTime('Europe/Madrid')":               {"Europe/Madrid", true},
		"DateTime64(3)":                           {"", true},
		"DateTime64(6, 'America/New_York')":       {"America/New_York", true},
		"Nullable(DateTime('UTC'))":               {"UTC", true},
		"LowCardinality(Nullable(DateTime64(9)))": {"", true},
		"Date":                    {"", false},
		"Date32":                  {"", false},
		"String":                  {"", false},
		"Array(DateTime)":         {"", false},
		"Tuple(DateTime, String)": {"", false},
		"Nullable(DateTime64(3, 'Asia/Kolkata'))   ": {"Asia/Kolkata", true},
	}
	for in, want := range cases {
		tz, ok := DateTimeZone(in)
		if tz != want.tz || ok != want.ok {
			t.Errorf("DateTimeZone(%q) = %q, %v; want %q, %v", in, tz, ok, want.tz, want.ok)
		}
	}
}

func TestSetInstants(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("tzdata unavailable:", err)
	}
	opts := []Option{
		{Value: "2026-01-01 10:00:00"},
		{Value: "2026-07-01 10:00:00.123"},
		{Value: "2026-01-01T09:00:00Z"},
		{Value: "not a time"},
	}
	SetInstants(opts, madrid)
	want := []string{"2026-01-01T09:00:00Z", "2026-07-01T08:00:00.123Z", "2026-01-01T09:00:00Z", ""}
	for i, w := range want {
		if opts[i].Instant != w {
			t.Errorf("option %d: instant %q, want %q", i, opts[i].Instant, w)
		}
	}
}

// dateTimeVars is a DateTime and a DateTime64(3) query variable whose
// options are text in Europe/Madrid, the way ClickHouse returns them.
func dateTimeVars(t *testing.T) ([]Variable, OptionsRunner, *int) {
	t.Helper()
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("tzdata unavailable:", err)
	}
	vars := []Variable{
		{Name: "ts", Type: TypeQuery, Query: "SELECT DISTINCT ts FROM t", Multi: true},
		{Name: "ts64", Type: TypeQuery, Query: "SELECT DISTINCT ts64 FROM t"},
	}
	calls := 0
	run := func(v Variable, _ map[string][]string, _ map[string]string) ([]Option, string, error) {
		calls++
		var opts []Option
		switch v.Name {
		case "ts":
			opts = []Option{{Value: "2026-01-01 10:00:00", Label: "2026-01-01 10:00:00"}, {Value: "2026-01-01 11:00:00", Label: "2026-01-01 11:00:00"}}
		case "ts64":
			opts = []Option{{Value: "2026-01-01 10:00:00.120", Label: "2026-01-01 10:00:00.120"}, {Value: "2026-01-01 10:00:00.123", Label: "2026-01-01 10:00:00.123"}}
		}
		SetInstants(opts, madrid)
		return opts, "", nil
	}
	return vars, run, &calls
}

func TestResolveMapsClickedInstantsToOptions(t *testing.T) {
	vars, run, _ := dateTimeVars(t)
	for _, strict := range []bool{false, true} {
		values, _, err := Resolve(vars, map[string][]string{
			// What a panel returns (iso), what a browser writes (toISOString),
			// an offset, and the stored text form, which stays as it is.
			"ts":   {"2026-01-01T09:00:00Z", "2026-01-01T10:00:00.000Z", "2026-01-01T11:00:00+02:00", "2026-01-01 10:00:00"},
			"ts64": {"2026-01-01T09:00:00.123Z"},
		}, strict, run)
		if err != nil {
			t.Fatalf("strict=%v: %v", strict, err)
		}
		if want := []string{"2026-01-01 10:00:00", "2026-01-01 11:00:00"}; !reflect.DeepEqual(values["ts"], want) {
			t.Errorf("strict=%v: ts = %q, want %q", strict, values["ts"], want)
		}
		if want := []string{"2026-01-01 10:00:00.123"}; !reflect.DeepEqual(values["ts64"], want) {
			t.Errorf("strict=%v: ts64 = %q, want %q", strict, values["ts64"], want)
		}
	}
}

func TestResolveStrictRefusesInstantsThatAreNotOptions(t *testing.T) {
	vars, run, _ := dateTimeVars(t)
	for _, req := range []map[string][]string{
		{"ts": {"2026-01-01T12:00:00Z"}},
		// One millisecond off is another instant.
		{"ts64": {"2026-01-01T09:00:00.124Z"}},
		// Text read in another zone than the column's is not an option.
		{"ts": {"2026-01-01 09:00:00"}},
	} {
		if _, _, err := Resolve(vars, req, true, run); err == nil || !strings.Contains(err.Error(), "not an option") {
			t.Errorf("%v: want not an option, got %v", req, err)
		}
	}
	// Signed in, a value that is no option is used as given.
	values, _, err := Resolve(vars, map[string][]string{"ts": {"2026-01-01T12:00:00Z"}}, false, run)
	if err != nil || !reflect.DeepEqual(values["ts"], []string{"2026-01-01T12:00:00Z"}) {
		t.Fatalf("non-strict: %q, %v", values["ts"], err)
	}
}

func TestResolveLoadsOptionsOnlyForInstants(t *testing.T) {
	vars, run, calls := dateTimeVars(t)
	if _, _, err := Resolve(vars, map[string][]string{"ts": {"2026-01-01 10:00:00"}, "ts64": {"x"}}, false, run); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("plain values must not run options queries, ran %d", *calls)
	}
	// Signed in, an options query that fails leaves the value as given.
	failing := func(Variable, map[string][]string, map[string]string) ([]Option, string, error) {
		return nil, "", errors.New("boom")
	}
	values, _, err := Resolve(vars, map[string][]string{"ts": {"2026-01-01T09:00:00Z"}, "ts64": {"x"}}, false, failing)
	if err != nil || values["ts"][0] != "2026-01-01T09:00:00Z" {
		t.Fatalf("non-strict with failing options: %q, %v", values["ts"], err)
	}
	if _, _, err := Resolve(vars, map[string][]string{"ts": {"2026-01-01T09:00:00Z"}}, true, failing); err == nil {
		t.Fatal("strict with failing options must fail")
	}
}
