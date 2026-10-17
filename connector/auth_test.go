package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A user named sso:ana@example.com must reach ClickHouse intact. HTTP Basic
// auth would split it at the first colon, so the client sends ClickHouse's
// own headers.
func TestAuthUsesClickHouseHeaders(t *testing.T) {
	var gotUser, gotKey, gotAuthz string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotKey, gotAuthz = r.Header.Get("X-ClickHouse-User"), r.Header.Get("X-ClickHouse-Key"), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"meta":[{"name":"version","type":"String"}],"data":[{"version":"25.8"}],"rows":1}`))
	}))
	t.Cleanup(srv.Close)

	c := NewCHClient(srv.URL, false)
	if _, err := c.TestConnection(context.Background(), "sso:ana@example.com", "p:w:d"); err != nil {
		t.Fatal(err)
	}
	if gotUser != "sso:ana@example.com" || gotKey != "p:w:d" {
		t.Errorf("headers: user %q key %q", gotUser, gotKey)
	}
	if gotAuthz != "" {
		t.Errorf("no Authorization header expected, got %q", gotAuthz)
	}

	gotUser, gotKey = "", ""
	if _, err := c.Execute(context.Background(), "SELECT 1", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if gotUser != "" || gotKey != "" {
		t.Errorf("an empty user must send no auth headers, got %q %q", gotUser, gotKey)
	}
}
