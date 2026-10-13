package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ON CLUSTER DDL answers with one status row per host. Those rows must reach
// the caller; any other write body is still ignored.
func TestExecuteKeepsDistributedDDLRows(t *testing.T) {
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewCHClient(srv.URL, false)

	body = `{"meta":[{"name":"host","type":"String"}],"data":[{"host":"a","status":"0"},{"host":"b","status":"493"}],"rows":2}`
	res, err := c.Execute(context.Background(), "CREATE ROLE r ON CLUSTER c", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Data) != 2 || res.Data[1]["host"] != "b" {
		t.Fatalf("data = %+v", res.Data)
	}

	body = "Ok.\n"
	res, err = c.Execute(context.Background(), "INSERT INTO t VALUES (1)", "", "", nil)
	if err != nil || len(res.Data) != 0 {
		t.Fatalf("plain write: %+v %v", res, err)
	}
}
