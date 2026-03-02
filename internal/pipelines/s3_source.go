package pipelines

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Source polls an S3-compatible bucket for new files and parses them.
type S3Source struct{}

func (s *S3Source) Type() string { return "source_s3" }

// Validate checks S3 configuration.
func (s *S3Source) Validate(cfg ConnectorConfig) error {
	endpoint := stringField(cfg.Fields, "endpoint", "")
	bucket := stringField(cfg.Fields, "bucket", "")
	accessKey := stringField(cfg.Fields, "access_key", "")
	secretKey := stringField(cfg.Fields, "secret_key", "")

	if endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if accessKey == "" {
		return fmt.Errorf("access_key is required")
	}
	if secretKey == "" {
		return fmt.Errorf("secret_key is required")
	}
	return nil
}

// Start begins polling S3 for new files and sends batches to the output channel.
func (s *S3Source) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	endpoint := stringField(cfg.Fields, "endpoint", "")
	bucket := stringField(cfg.Fields, "bucket", "")
	prefix := stringField(cfg.Fields, "prefix", "")
	accessKey := stringField(cfg.Fields, "access_key", "")
	secretKey := stringField(cfg.Fields, "secret_key", "")
	region := stringField(cfg.Fields, "region", "us-east-1")
	format := stringField(cfg.Fields, "format", "json")
	pollIntervalSec := intField(cfg.Fields, "poll_interval", 300)
	useSSL := boolField(cfg.Fields, "use_ssl", true)
	batchSize := intField(cfg.Fields, "batch_size", 1000)

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return fmt.Errorf("create S3 client: %w", err)
	}

	slog.Info("S3 source started", "endpoint", endpoint, "bucket", bucket, "prefix", prefix, "format", format)

	// Track processed files to avoid reprocessing
	var processed sync.Map

	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()

	poll := func() error {
		objectCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		})

		for obj := range objectCh {
			if obj.Err != nil {
				slog.Warn("S3 list error", "error", obj.Err)
				continue
			}

			// Skip already processed
			if _, seen := processed.LoadOrStore(obj.Key, true); seen {
				continue
			}

			if err := s.processFile(ctx, client, bucket, obj.Key, format, batchSize, out); err != nil {
				slog.Error("S3 file processing error", "key", obj.Key, "error", err)
				processed.Delete(obj.Key) // Allow retry
			}
		}

		return nil
	}

	// First poll
	if err := poll(); err != nil {
		slog.Error("S3 source poll error", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := poll(); err != nil {
				slog.Error("S3 source poll error", "error", err)
			}
		}
	}
}

// processFile reads and parses a single S3 object.
func (s *S3Source) processFile(ctx context.Context, client *minio.Client, bucket, key, format string, batchSize int, out chan<- Batch) error {
	obj, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	defer obj.Close()

	slog.Info("Processing S3 file", "key", key, "format", format)

	switch strings.ToLower(format) {
	case "json", "ndjson", "jsonl":
		return s.parseNDJSON(ctx, obj, batchSize, out)
	case "csv":
		return s.parseCSV(ctx, obj, batchSize, out)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// parseNDJSON reads newline-delimited JSON.
func (s *S3Source) parseNDJSON(ctx context.Context, r io.Reader, batchSize int, out chan<- Batch) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	var buf []Record
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			slog.Warn("S3 JSON parse error, skipping line", "error", err)
			continue
		}

		buf = append(buf, Record{
			Data:    data,
			RawJSON: []byte(line),
		})

		if len(buf) >= batchSize {
			select {
			case out <- Batch{Records: buf, SourceTS: time.Now()}:
			case <-ctx.Done():
				return nil
			}
			buf = nil
		}
	}

	if len(buf) > 0 {
		select {
		case out <- Batch{Records: buf, SourceTS: time.Now()}:
		case <-ctx.Done():
		}
	}

	return scanner.Err()
}

// parseCSV reads CSV files (first row = headers).
func (s *S3Source) parseCSV(ctx context.Context, r io.Reader, batchSize int, out chan<- Batch) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	// Read header
	if !scanner.Scan() {
		return fmt.Errorf("empty CSV file")
	}
	headers := strings.Split(scanner.Text(), ",")
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}

	var buf []Record
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		values := strings.Split(line, ",")
		data := make(map[string]interface{})
		for i, h := range headers {
			if i < len(values) {
				data[h] = strings.TrimSpace(values[i])
			} else {
				data[h] = ""
			}
		}

		raw, _ := json.Marshal(data)
		buf = append(buf, Record{
			Data:    data,
			RawJSON: raw,
		})

		if len(buf) >= batchSize {
			select {
			case out <- Batch{Records: buf, SourceTS: time.Now()}:
			case <-ctx.Done():
				return nil
			}
			buf = nil
		}
	}

	if len(buf) > 0 {
		select {
		case out <- Batch{Records: buf, SourceTS: time.Now()}:
		case <-ctx.Done():
		}
	}

	return scanner.Err()
}
