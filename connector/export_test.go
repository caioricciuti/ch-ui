package connector

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExportTrailingException(t *testing.T) {
	tests := []struct {
		name string
		tail string
		want string
		ok   bool
	}{
		{name: "clean csv", tail: "a,b\n1,2\n", ok: false},
		{name: "classic framing", tail: "1,0\n2,0\n__exception__\nCode: 395. DB::Exception: boom. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO)\n", want: "Code: 395. DB::Exception: boom. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO)", ok: true},
		{name: "tagged framing", tail: "1,0\r\n__exception__\r\nxyzTAG\r\nCode: 396. DB::Exception: too many rows\r\n44 xyzTAG\r\n__exception__\r\n", want: "Code: 396. DB::Exception: too many rows", ok: true},
		{name: "marker inside a value", tail: "x__exception__\nCode: 1\n", ok: false},
		{name: "marker without code", tail: "__exception__\nnot an error\n", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := exportTrailingException([]byte(tc.tail))
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func fakeExportServer(t *testing.T, status int, body []byte) (*httptest.Server, *string) {
	t.Helper()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(r.Body)
		gotQuery = b.String() + "|" + r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotQuery
}

func TestExecuteExportForwardsAllBytes(t *testing.T) {
	// Larger than one chunk plus the holdback, so both paths run.
	body := bytes.Repeat([]byte("0123456789abcdef\n"), (exportChunkSize+exportHoldback)/8)
	srv, got := fakeExportServer(t, http.StatusOK, body)
	c := NewCHClient(srv.URL, false)

	var out bytes.Buffer
	chunks := 0
	n, err := c.ExecuteExport(context.Background(), "q-1", "SELECT 1 -- note", "u", "p", "CSVWithNames", map[string]string{"readonly": "1"}, func(b []byte) error {
		chunks++
		out.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(body)) || !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("forwarded %d bytes, want %d (equal=%v)", n, len(body), bytes.Equal(out.Bytes(), body))
	}
	if chunks < 2 {
		t.Fatalf("want several chunks, got %d", chunks)
	}
	if !strings.Contains(*got, "SELECT 1\nFORMAT CSVWithNames|") || !strings.Contains(*got, "readonly=1") || !strings.Contains(*got, "query_id=q-1") {
		t.Fatalf("unexpected request: %q", *got)
	}
}

func TestExecuteExportReportsMidStreamException(t *testing.T) {
	body := append(bytes.Repeat([]byte("1,0\n"), 1000), []byte("__exception__\nCode: 395. DB::Exception: boom\n")...)
	srv, _ := fakeExportServer(t, http.StatusOK, body)
	c := NewCHClient(srv.URL, false)
	var out bytes.Buffer
	_, err := c.ExecuteExport(context.Background(), "q-2", "SELECT 1", "", "", "CSVWithNames", nil, func(b []byte) error {
		out.Write(b)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "Code: 395") {
		t.Fatalf("want the ClickHouse exception, got %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("__exception__")) {
		t.Fatal("the exception block must never be forwarded as data")
	}
}

func TestExecuteExportRejectsBadInput(t *testing.T) {
	srv, _ := fakeExportServer(t, http.StatusInternalServerError, []byte("Code: 60. DB::Exception: Unknown table"))
	c := NewCHClient(srv.URL, false)
	noop := func([]byte) error { return nil }
	if _, err := c.ExecuteExport(context.Background(), "", "SELECT 1", "", "", "CSV; DROP", nil, noop); err == nil {
		t.Fatal("an invalid format must be refused")
	}
	if _, err := c.ExecuteExport(context.Background(), "", "SELECT 1 FORMAT JSON", "", "", "CSVWithNames", nil, noop); err == nil {
		t.Fatal("a statement with its own FORMAT must be refused")
	}
	if _, err := c.ExecuteExport(context.Background(), "", "SELECT * FROM nope", "", "", "CSVWithNames", nil, noop); err == nil || !strings.Contains(err.Error(), "Unknown table") {
		t.Fatalf("want the ClickHouse error, got %v", err)
	}
}
