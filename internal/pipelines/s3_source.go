package pipelines

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// defaultS3Endpoint is used when the endpoint is left empty.
const defaultS3Endpoint = "s3.amazonaws.com"

// S3Source polls an S3-compatible bucket for new files and parses them.
type S3Source struct {
	// newClient builds the S3 client; nil means minio. Tests inject a fake.
	newClient func(cfg ConnectorConfig) (s3API, error)
}

// s3API is the part of the S3 client the source uses.
type s3API interface {
	ListObjects(ctx context.Context, bucket string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

// minioS3 adapts *minio.Client to s3API.
type minioS3 struct{ c *minio.Client }

func (m minioS3) ListObjects(ctx context.Context, bucket string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	return m.c.ListObjects(ctx, bucket, opts)
}

func (m minioS3) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return m.c.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
}

func (s *S3Source) client(cfg ConnectorConfig) (s3API, error) {
	if s.newClient != nil {
		return s.newClient(cfg)
	}
	endpoint, useSSL, err := parseS3Endpoint(stringField(cfg.Fields, "endpoint", ""), boolField(cfg.Fields, "use_ssl", true))
	if err != nil {
		return nil, err
	}
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(stringField(cfg.Fields, "access_key", ""), stringField(cfg.Fields, "secret_key", ""), ""),
		Secure: useSSL,
		Region: stringField(cfg.Fields, "region", "us-east-1"),
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	return minioS3{c}, nil
}

func (s *S3Source) Type() string { return "source_s3" }

// Validate checks S3 configuration.
func (s *S3Source) Validate(cfg ConnectorConfig) error {
	bucket := stringField(cfg.Fields, "bucket", "")
	accessKey := stringField(cfg.Fields, "access_key", "")
	secretKey := stringField(cfg.Fields, "secret_key", "")

	if _, _, err := parseS3Endpoint(stringField(cfg.Fields, "endpoint", ""), boolField(cfg.Fields, "use_ssl", true)); err != nil {
		return err
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
	switch strings.ToLower(stringField(cfg.Fields, "format", "json")) {
	case "json", "ndjson", "jsonl", "csv":
	default:
		return fmt.Errorf("format must be json, ndjson or csv")
	}
	return validateCommon(cfg.Fields)
}

// parseS3Endpoint turns the configured endpoint into the host[:port] minio
// expects. Empty means AWS S3. A scheme decides TLS (http:// off, https://
// on); without one, useSSL is used. A trailing path is not allowed.
func parseS3Endpoint(raw string, useSSL bool) (string, bool, error) {
	ep := strings.TrimSpace(raw)
	if ep == "" {
		return defaultS3Endpoint, true, nil
	}
	lower := strings.ToLower(ep)
	switch {
	case strings.HasPrefix(lower, "https://"):
		ep, useSSL = ep[len("https://"):], true
	case strings.HasPrefix(lower, "http://"):
		ep, useSSL = ep[len("http://"):], false
	case strings.Contains(ep, "://"):
		return "", false, fmt.Errorf("endpoint scheme must be http or https")
	}
	ep = strings.TrimRight(ep, "/")
	if ep == "" {
		return "", false, fmt.Errorf("endpoint has no host")
	}
	if strings.Contains(ep, "/") {
		return "", false, fmt.Errorf("endpoint must be a host (and port), without a path: %q", raw)
	}
	return ep, useSSL, nil
}

// Start polls S3 for new files and sends batches to the output channel. A
// file is marked processed only after every batch read from it is written.
func (s *S3Source) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	bucket := stringField(cfg.Fields, "bucket", "")
	prefix := stringField(cfg.Fields, "prefix", "")
	format := stringField(cfg.Fields, "format", "json")
	pollIntervalSec := max(intField(cfg.Fields, "poll_interval", 300), 1)
	batchSize := max(intField(cfg.Fields, "batch_size", 1000), 1)
	pipelineID := stringField(cfg.Fields, "pipeline_id", "")

	client, err := s.client(cfg)
	if err != nil {
		return err
	}

	slog.Info("S3 source started", "bucket", bucket, "prefix", prefix, "format", format)

	// Files fully written, resumed from the checkpoint saved by earlier runs.
	ckKey := s3CheckpointKey(bucket, prefix)
	processed := loadS3Processed(cfg.Checkpoints[ckKey])

	poll := func() error {
		var firstErr error
		for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if ctx.Err() != nil {
				return nil
			}
			if obj.Err != nil {
				return fmt.Errorf("list objects: %w", obj.Err)
			}
			if processed.done(obj.Key, obj.ETag) || strings.HasSuffix(obj.Key, "/") {
				continue
			}
			// The file's last batch carries the processed set including this
			// file; it is saved once every batch of the file is written.
			next := processed.with(obj.Key, obj.ETag)
			f := s3File{pipelineID: pipelineID, key: obj.Key, etag: obj.ETag, batchSize: batchSize, out: out, next: 1,
				checkpoint: &Checkpoint{Key: ckKey, Position: next.encode()}}
			if err := s.processFile(ctx, client, bucket, format, &f); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("file %s: %w", obj.Key, err)
				}
				continue
			}
			if f.waitAcked(ctx) {
				processed = next
			}
		}
		return firstErr
	}

	runPoll := func() {
		if err := poll(); err != nil && ctx.Err() == nil {
			cfg.Health.Fail("S3 source", err)
			return
		}
		if ctx.Err() == nil {
			cfg.Health.OK()
		}
	}

	runPoll()
	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runPoll()
		}
	}
}

// s3File batches the records of one object and tracks their acks.
type s3File struct {
	pipelineID string
	key, etag  string
	batchSize  int
	out        chan<- Batch

	buf       []Record
	firstLine int // record number of buf[0], 1-based
	next      int // record number of the next record
	acks      []chan struct{}
	// checkpoint goes on the file's last batch (see finish).
	checkpoint *Checkpoint
	// limit, when > 0, stops reading after that many records (sampling).
	limit   int
	sampled []Record
}

// errSampleFull stops parsing once a sample has enough records.
var errSampleFull = errors.New("sample full")

func (f *s3File) add(ctx context.Context, rec Record) error {
	rec.Position = fmt.Sprintf("%s#%d", f.key, f.next)
	if f.limit > 0 {
		f.sampled = append(f.sampled, rec)
		f.next++
		if len(f.sampled) >= f.limit {
			return errSampleFull
		}
		return nil
	}
	if len(f.buf) == 0 {
		f.firstLine = f.next
	}
	f.buf = append(f.buf, rec)
	f.next++
	if len(f.buf) >= f.batchSize {
		return f.flush(ctx)
	}
	return nil
}

func (f *s3File) flush(ctx context.Context) error {
	return f.send(ctx, false)
}

// finish sends the remaining records as the file's last batch with the
// checkpoint. A file without records still sends an empty batch so the
// checkpoint is saved and the file is not read again after a restart.
func (f *s3File) finish(ctx context.Context) error {
	return f.send(ctx, true)
}

func (f *s3File) send(ctx context.Context, last bool) error {
	if len(f.buf) == 0 && !last {
		return nil
	}
	acked := make(chan struct{})
	var once sync.Once
	batch := Batch{
		Records:    f.buf,
		SourceTS:   time.Now(),
		Ack:        func() { once.Do(func() { close(acked) }) },
		DedupToken: dedupToken("s3", f.pipelineID, f.key, f.etag, f.firstLine, f.firstLine+len(f.buf)-1),
	}
	if len(f.buf) == 0 {
		batch.DedupToken = ""
	}
	if last {
		batch.Checkpoint = f.checkpoint
	}
	select {
	case f.out <- batch:
		f.acks = append(f.acks, acked)
		f.buf = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitAcked waits until every batch of the file has been written.
func (f *s3File) waitAcked(ctx context.Context) bool {
	for _, a := range f.acks {
		select {
		case <-a:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// processFile reads and parses a single S3 object.
func (s *S3Source) processFile(ctx context.Context, client s3API, bucket, format string, f *s3File) error {
	obj, err := client.GetObject(ctx, bucket, f.key)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	defer obj.Close()

	slog.Info("Processing S3 file", "key", f.key, "format", format)

	switch strings.ToLower(format) {
	case "json", "ndjson", "jsonl":
		err = parseJSONRecords(ctx, obj, f)
	case "csv":
		err = parseCSVRecords(ctx, obj, f)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
	if errors.Is(err, errSampleFull) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.limit > 0 {
		return nil
	}
	return f.finish(ctx)
}

// parseJSONRecords reads a JSON array of objects or newline-delimited JSON,
// detected from the first non-space byte.
func parseJSONRecords(ctx context.Context, r io.Reader, f *s3File) error {
	br := bufio.NewReaderSize(r, 64*1024)
	if bom, err := br.Peek(3); err == nil && string(bom) == "\xEF\xBB\xBF" {
		br.Discard(3)
	}
	for {
		b, err := br.Peek(1)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n' {
			br.ReadByte()
			continue
		}
		if b[0] == '[' {
			return parseJSONArray(ctx, br, f)
		}
		return parseNDJSON(ctx, br, f)
	}
}

func parseJSONArray(ctx context.Context, r io.Reader, f *s3File) error {
	dec := json.NewDecoder(r)
	if _, err := dec.Token(); err != nil { // '['
		return fmt.Errorf("invalid JSON array: %w", err)
	}
	for dec.More() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("invalid JSON array element: %w", err)
		}
		var data map[string]interface{}
		if err := json.Unmarshal(raw, &data); err != nil || data == nil {
			slog.Warn("S3 JSON array element is not an object, skipping")
			f.next++
			continue
		}
		if err := f.add(ctx, Record{Data: data, RawJSON: raw}); err != nil {
			return err
		}
	}
	return nil
}

func parseNDJSON(ctx context.Context, r io.Reader, f *s3File) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(line), &data); err != nil || data == nil {
			slog.Warn("S3 JSON parse error, skipping line", "error", err)
			f.next++
			continue
		}
		if err := f.add(ctx, Record{Data: data, RawJSON: []byte(line)}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// parseCSVRecords reads CSV with a header row, using RFC 4180 quoting.
func parseCSVRecords(ctx context.Context, r io.Reader, f *s3File) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true

	headers, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("empty CSV file")
	}
	if err != nil {
		return fmt.Errorf("read CSV header: %w", err)
	}
	for i := range headers {
		headers[i] = strings.TrimSpace(strings.TrimPrefix(headers[i], "\uFEFF"))
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		values, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read CSV: %w", err)
		}
		if len(values) == 1 && strings.TrimSpace(values[0]) == "" {
			continue
		}
		data := make(map[string]interface{}, len(headers))
		for i, h := range headers {
			if i < len(values) {
				data[h] = strings.TrimSpace(values[i])
			} else {
				data[h] = ""
			}
		}
		raw, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encode CSV row: %w", err)
		}
		if err := f.add(ctx, Record{Data: data, RawJSON: raw}); err != nil {
			return err
		}
	}
}

// s3MaxTrackedFiles bounds the processed-file set saved per bucket and
// prefix. When it is exceeded the lexically smallest keys are dropped and
// the floor rises to the largest dropped key: keys at or below the floor
// count as processed.
const s3MaxTrackedFiles = 2000

// s3CheckpointKey names the checkpoint of one bucket and prefix, so changing
// either starts from scratch.
func s3CheckpointKey(bucket, prefix string) string {
	return "s3:" + bucket + "/" + prefix
}

// s3Processed is the set of written files: key to ETag, plus a floor.
type s3Processed struct {
	Floor string            `json:"floor,omitempty"`
	Files map[string]string `json:"files"`
}

func loadS3Processed(saved string) s3Processed {
	p := s3Processed{Files: map[string]string{}}
	if saved == "" {
		return p
	}
	if err := json.Unmarshal([]byte(saved), &p); err != nil {
		slog.Warn("Ignoring unreadable S3 checkpoint", "error", err)
		return s3Processed{Files: map[string]string{}}
	}
	if p.Files == nil {
		p.Files = map[string]string{}
	}
	return p
}

// done reports whether key was written with this ETag (a rewritten object
// with a new ETag is read again), or sorts at or below the floor.
func (p s3Processed) done(key, etag string) bool {
	if p.Floor != "" && key <= p.Floor {
		return true
	}
	saved, ok := p.Files[key]
	return ok && saved == etag
}

// with returns a copy that includes key, trimmed to s3MaxTrackedFiles.
func (p s3Processed) with(key, etag string) s3Processed {
	next := s3Processed{Floor: p.Floor, Files: make(map[string]string, len(p.Files)+1)}
	for k, v := range p.Files {
		next.Files[k] = v
	}
	next.Files[key] = etag
	if len(next.Files) > s3MaxTrackedFiles {
		keys := make([]string, 0, len(next.Files))
		for k := range next.Files {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys[:len(keys)-s3MaxTrackedFiles] {
			delete(next.Files, k)
			if k > next.Floor {
				next.Floor = k
			}
		}
	}
	return next
}

func (p s3Processed) encode() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// Sample reads up to limit records from the first file under the prefix
// that is not yet processed (or the first file at all when every file is).
func (s *S3Source) Sample(ctx context.Context, cfg ConnectorConfig, limit int) (SampleResult, error) {
	bucket := stringField(cfg.Fields, "bucket", "")
	prefix := stringField(cfg.Fields, "prefix", "")
	format := stringField(cfg.Fields, "format", "json")
	client, err := s.client(cfg)
	if err != nil {
		return SampleResult{}, err
	}
	processed := loadS3Processed(cfg.Checkpoints[s3CheckpointKey(bucket, prefix)])
	var first, firstNew string
	for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return SampleResult{}, fmt.Errorf("list objects: %w", obj.Err)
		}
		if strings.HasSuffix(obj.Key, "/") {
			continue
		}
		if first == "" {
			first = obj.Key
		}
		if !processed.done(obj.Key, obj.ETag) {
			firstNew = obj.Key
			break
		}
	}
	key, note := firstNew, ""
	switch {
	case first == "":
		return SampleResult{Records: nil, Note: fmt.Sprintf("No files under s3://%s/%s yet.", bucket, prefix)}, nil
	case firstNew == "":
		key, note = first, "Every file is already processed; showing the first one, "
	default:
		note = "Next file to process, "
	}
	f := s3File{key: key, limit: limit, next: 1}
	if err := s.processFile(ctx, client, bucket, format, &f); err != nil {
		return SampleResult{}, fmt.Errorf("file %s: %w", key, err)
	}
	out := make([]map[string]interface{}, 0, len(f.sampled))
	for _, r := range f.sampled {
		out = append(out, r.Data)
	}
	return SampleResult{Records: out, Note: fmt.Sprintf("%s%s (first %d records).", note, key, len(out))}, nil
}
