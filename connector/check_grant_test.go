package connector

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CHECK GRANT refuses a FORMAT clause, so it must reach ClickHouse as is,
// with default_format shaping the result; other reads still get FORMAT JSON.
func TestCheckGrantGetsNoFormatClause(t *testing.T) {
	var bodies, formats []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		formats = append(formats, r.URL.Query().Get("default_format"))
		_, _ = io.WriteString(w, `{"meta":[{"name":"result","type":"UInt8"}],"data":[{"result":1}],"rows":1}`)
	}))
	defer srv.Close()
	c := NewCHClient(srv.URL, false)

	for _, q := range []string{"CHECK GRANT INSERT ON `db`.`t`", "-- probe\n  check grant SELECT ON db.*"} {
		if _, err := c.Execute(context.Background(), q, "u", "p", nil); err != nil {
			t.Fatalf("%q: %v", q, err)
		}
	}
	if _, err := c.ExecuteRaw(context.Background(), "CHECK GRANT INSERT ON db.t", "u", "p", "JSONCompact", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), "SELECT 1", "u", "p", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if strings.Contains(strings.ToUpper(bodies[i]), "FORMAT") {
			t.Fatalf("CHECK GRANT got a FORMAT clause: %q", bodies[i])
		}
	}
	if formats[2] != "JSONCompact" {
		t.Fatalf("ExecuteRaw default_format = %q, want JSONCompact", formats[2])
	}
	if !strings.HasSuffix(bodies[3], "\nFORMAT JSON") {
		t.Fatalf("SELECT lost its FORMAT JSON: %q", bodies[3])
	}
}
