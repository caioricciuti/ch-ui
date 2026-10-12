package pipelines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IBM/sarama"
)

func TestKafkaNonJSONMessageInsertsValidJSON(t *testing.T) {
	rec := kafkaRecord(&sarama.ConsumerMessage{Topic: "t", Partition: 2, Offset: 7, Value: []byte("not json {")})
	var back map[string]interface{}
	if err := json.Unmarshal(rec.RawJSON, &back); err != nil {
		t.Fatalf("RawJSON is not valid JSON: %q", rec.RawJSON)
	}
	if back["_raw"] != "not json {" || back["_offset"] != float64(7) {
		t.Fatalf("wrapper: %v", back)
	}
	ok := kafkaRecord(&sarama.ConsumerMessage{Value: []byte(`{"a":1}`)})
	if string(ok.RawJSON) != `{"a":1}` {
		t.Fatalf("object passthrough: %q", ok.RawJSON)
	}
}

func TestParseWebhookBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"object", `{"a":1}`, 1},
		{"pretty object", "{\n  \"a\": 1,\n  \"b\": \"x\"\n}\n", 1},
		{"array", `[{"a":1},{"a":2}]`, 2},
		{"pretty array", "[\n {\"a\": 1},\n {\"a\": 2}\n]", 2},
		{"ndjson", "{\"a\":1}\n{\"a\":2}\n{\"a\":3}", 3},
	}
	for _, c := range cases {
		recs, err := parseWebhookBody([]byte(c.body), "application/json")
		if err != nil || len(recs) != c.want {
			t.Errorf("%s: %d records, err %v", c.name, len(recs), err)
			continue
		}
		for _, r := range recs {
			if strings.Contains(string(r.RawJSON), "\n") {
				t.Errorf("%s: RawJSON spans lines: %q", c.name, r.RawJSON)
			}
		}
	}
	for _, bad := range []string{"", "42", `[1,2]`, "{\"a\":1}\nnope", "[{\"a\":1}"} {
		if _, err := parseWebhookBody([]byte(bad), ""); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// A request that does not fit is refused whole: nothing is queued.
func TestHandleWebhookAllOrNothing(t *testing.T) {
	const id = "pipe-capacity-test"
	recv := &webhookReceiver{incoming: make(chan Record, 3)}
	webhookRegistry.Store(id, recv)
	defer webhookRegistry.Delete(id)
	post := func(body string) int {
		rec := httptest.NewRecorder()
		HandleWebhook(rec, httptest.NewRequest(http.MethodPost, "/api/pipelines/webhook/"+id, strings.NewReader(body)))
		return rec.Code
	}
	if got := post(`[{"a":1},{"a":2}]`); got != http.StatusOK {
		t.Fatalf("first: %d", got)
	}
	if got := post(`[{"a":3},{"a":4}]`); got != http.StatusTooManyRequests {
		t.Fatalf("second: %d, want 429", got)
	}
	if len(recv.incoming) != 2 {
		t.Fatalf("queued %d records, want 2 (no partial accept)", len(recv.incoming))
	}
	if got := post(`[{"a":1},{"a":2},{"a":3},{"a":4}]`); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: %d, want 413", got)
	}
}

func TestParseS3Endpoint(t *testing.T) {
	cases := []struct {
		in      string
		ssl     bool
		host    string
		wantSSL bool
		bad     bool
	}{
		{"", false, "s3.amazonaws.com", true, false},
		{"https://minio.local:9000/", false, "minio.local:9000", true, false},
		{"http://minio.local:9000", true, "minio.local:9000", false, false},
		{"minio.local:9000", false, "minio.local:9000", false, false},
		{"https://host/bucket", true, "", false, true},
		{"ftp://host", true, "", false, true},
	}
	for _, c := range cases {
		host, ssl, err := parseS3Endpoint(c.in, c.ssl)
		if c.bad {
			if err == nil {
				t.Errorf("%q accepted", c.in)
			}
			continue
		}
		if err != nil || host != c.host || ssl != c.wantSSL {
			t.Errorf("%q: %q %v %v", c.in, host, ssl, err)
		}
	}
}

func collect(t *testing.T, parse func(context.Context, *s3File) error) ([]Batch, *s3File) {
	t.Helper()
	out := make(chan Batch, 100)
	f := &s3File{pipelineID: "p", key: "k", etag: "e", batchSize: 2, out: out, next: 1}
	if err := parse(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if err := f.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(out)
	var batches []Batch
	for b := range out {
		batches = append(batches, b)
	}
	return batches, f
}

func TestS3ParsesArraysNDJSONAndQuotedCSV(t *testing.T) {
	arr, _ := collect(t, func(ctx context.Context, f *s3File) error {
		return parseJSONRecords(ctx, strings.NewReader("\xEF\xBB\xBF [\n {\"a\":1},\n {\"a\":2},\n {\"a\":3}\n]"), f)
	})
	if len(arr) != 2 || len(arr[0].Records) != 2 || len(arr[1].Records) != 1 {
		t.Fatalf("JSON array batches: %+v", arr)
	}
	if arr[0].DedupToken == arr[1].DedupToken || arr[0].DedupToken == "" {
		t.Fatal("dedup tokens must be set and differ per batch")
	}
	again, _ := collect(t, func(ctx context.Context, f *s3File) error {
		return parseJSONRecords(ctx, strings.NewReader("[{\"a\":1},{\"a\":2},{\"a\":3}]"), f)
	})
	if again[0].DedupToken != arr[0].DedupToken {
		t.Fatal("re-reading the same file must give the same tokens")
	}

	nd, _ := collect(t, func(ctx context.Context, f *s3File) error {
		return parseJSONRecords(ctx, strings.NewReader("{\"a\":1}\n\n{\"a\":2}\n"), f)
	})
	if len(nd) != 1 || len(nd[0].Records) != 2 {
		t.Fatalf("NDJSON: %+v", nd)
	}

	csvB, _ := collect(t, func(ctx context.Context, f *s3File) error {
		return parseCSVRecords(ctx, strings.NewReader("name,note\n\"Smith, J\",\"said \"\"hi\"\"\"\nx,y\n"), f)
	})
	if len(csvB) != 1 || len(csvB[0].Records) != 2 {
		t.Fatalf("CSV: %+v", csvB)
	}
	if got := csvB[0].Records[0].Data["name"]; got != "Smith, J" {
		t.Fatalf("quoted CSV field = %q", got)
	}
	if got := csvB[0].Records[0].Data["note"]; got != `said "hi"` {
		t.Fatalf("escaped quotes = %q", got)
	}
}

func TestCheckSQLitePath(t *testing.T) {
	dataDir := t.TempDir()
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "ch-ui.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(other, "sneaky.db")
	if err := os.Symlink(filepath.Join(dataDir, "ch-ui.db"), link); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		filepath.Join(dataDir, "ch-ui.db"),
		"file:" + filepath.Join(dataDir, "ch-ui.db") + "?mode=ro",
		filepath.Join(other, "..", filepath.Base(dataDir), "ch-ui.db"),
		link,
		dataDir,
	} {
		if err := CheckSQLitePath(bad, dataDir); err == nil {
			t.Errorf("allowed %q", bad)
		}
	}
	for _, ok := range []string{filepath.Join(other, "app.db"), ":memory:"} {
		if err := CheckSQLitePath(ok, dataDir); err != nil {
			t.Errorf("refused %q: %v", ok, err)
		}
	}
}
