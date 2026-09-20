package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/writer"
)

func TestParquetUploadRoundTrip(t *testing.T) {
	type row struct {
		Name  string `parquet:"name=name, type=BYTE_ARRAY, convertedtype=UTF8"`
		Count int64  `parquet:"name=count, type=INT64"`
	}
	path := filepath.Join(t.TempDir(), "sample.parquet")
	f, err := local.NewLocalFileWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := writer.NewParquetWriter(f, new(row), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []row{{"first", 42}, {"second", 7}} {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteStop(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseParquetDataset(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("rows: %#v", got.Rows)
	}
	// The reader preserves values independently of its generated field casing.
	for i, want := range []string{"first", "second"} {
		found := false
		for _, value := range got.Rows[i] {
			if value == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("row %d lost value: %#v", i, got.Rows[i])
		}
	}
	if _, err := parseParquetDataset([]byte("not parquet")); err == nil {
		t.Fatal("invalid parquet accepted")
	}
}
