// Package spool is a durable on-disk queue for ingest data. Receivers Append
// records and only answer the client once Append returns, because by then the
// bytes are fsynced. A writer Claims sealed segments, inserts them into
// ClickHouse with the segment's DedupToken as insert_deduplication_token, and
// Acks them, which deletes the file. Delivery is at-least-once: a crash
// between insert and Ack replays the same segment with the same token, so the
// replay is deduplicated by ClickHouse.
//
// On-disk layout:
//
//	<dir>/.spool-id                   32 hex chars, instance id, part of every dedup token
//	<dir>/<stream>/.next-id           decimal segment id high-water mark (reserved in blocks)
//	<dir>/<stream>/<20-digit id>.seg  segment
//	<dir>/<stream>/<20-digit id>.bad  quarantined bytes cut from a corrupt segment
//
// A segment file is a 16-byte header (8-byte magic "CHUISPL1", then the
// creation time as little-endian int64 unix nanoseconds) followed by frames.
// A frame is little-endian uint32 payload length, little-endian uint32
// CRC32C (Castagnoli) over the 4 length bytes plus the payload, then the
// payload. Covering the length with the CRC means a zero-filled tail never
// parses as valid empty records.
//
// Only the highest segment of a stream is ever written to (the active one).
// Every lower segment was fsynced in full before the next one was created, so
// on recovery an invalid frame at the end of the highest segment is a torn
// write and is truncated with a warning. Any other invalid frame (a CRC
// mismatch followed by more data, an impossible length, a bad header, or any
// damage in a sealed segment) is corruption: the segment is cut at the last
// valid frame and the rest is moved to a .bad file next to it, with a
// warning. Valid data after a corrupt frame cannot be located reliably once a
// length field is damaged, so it is never skipped over silently; it stays in
// the .bad file for inspection. .bad files are not counted against MaxBytes
// and are removed only by DropStream.
//
// The same check runs again when a segment is claimed, so damage that
// appears after Open is handled the same way.
//
// One process per directory. The spool does not lock the directory.
package spool

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caioricciuti/ch-ui/internal/safe"
)

var (
	// ErrFull is returned by Append when the record would take the spool over
	// MaxBytes. Nothing is written.
	ErrFull = errors.New("spool: full")
	// ErrClosed is returned after Close.
	ErrClosed = errors.New("spool: closed")
	// ErrTooLarge is returned by Append for a record over MaxRecordBytes.
	ErrTooLarge = errors.New("spool: record too large")
	// ErrInvalidStream is returned for a stream name that is not a safe path segment.
	ErrInvalidStream = errors.New("spool: invalid stream name")
	// ErrStreamDropped is returned to callers blocked on a stream that DropStream removed.
	ErrStreamDropped = errors.New("spool: stream dropped")
	// ErrNotClaimed is returned by Ack or Release for a segment that is not
	// currently claimed (already acked or released).
	ErrNotClaimed = errors.New("spool: segment not claimed")
)

// SyncMode controls fsync behaviour.
type SyncMode int

const (
	// SyncAlways fsyncs before Append returns. Concurrent Appends to the same
	// stream share one fsync (group commit). This is the default.
	SyncAlways SyncMode = iota
	// SyncNone never fsyncs and leaves flushing to the OS. Data acknowledged
	// by Append can be lost on power failure. For tests and throwaway setups.
	SyncNone
)

const (
	DefaultMaxBytes        int64 = 2 << 30
	DefaultSegmentMaxBytes int64 = 8 << 20
	DefaultSegmentMaxAge         = time.Second
	DefaultMaxRecordBytes        = 64 << 20

	// maxRecordCeiling bounds MaxRecordBytes and is the limit used when
	// parsing, so lowering MaxRecordBytes never makes old data look corrupt.
	maxRecordCeiling = 1 << 30

	headerSize      = 16
	frameHeaderSize = 8
	segMagic        = "CHUISPL1"

	instanceFile = ".spool-id"
	hwmFile      = ".next-id"
	// hwmBlock is how many segment ids are reserved per .next-id write.
	hwmBlock = 1024
	segExt   = ".seg"
	badExt   = ".bad"
	maxName  = 128
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Options configures a Spool. Zero values take the defaults.
type Options struct {
	MaxBytes        int64         // total cap across streams, default 2 GiB
	SegmentMaxBytes int64         // seal when a segment reaches this size, default 8 MiB
	SegmentMaxAge   time.Duration // seal this long after a segment's first record, default 1s
	MaxRecordBytes  int           // largest accepted record, default 64 MiB
	SyncMode        SyncMode
}

// Stats describes the unacked data of one stream or of the whole spool.
type Stats struct {
	Bytes     int64         // segment file bytes, headers included
	Segments  int           // sealed plus active
	Records   int64         // records in those segments
	OldestAge time.Duration // age of the oldest unacked segment, 0 when empty
}

// Segment is a claimed sealed segment. Pass it back to Ack or Release.
type Segment struct {
	Stream  string
	ID      uint64
	Bytes   int64
	records [][]byte
	token   string
	s       *stream
	m       *segMeta
	settled bool // guarded by s.mu
}

// Records returns the segment's records in append order.
func (g *Segment) Records() [][]byte { return g.records }

// DedupToken is stable across restarts for the same segment and unique per
// spool directory: it combines the instance id, the stream and the segment id.
func (g *Segment) DedupToken() string { return g.token }

type segMeta struct {
	id      uint64
	bytes   int64
	records int64
	created time.Time
	claimed bool
}

type stream struct {
	name string
	dir  string

	mu       sync.Mutex
	cond     *sync.Cond
	dirReady bool
	active   *segMeta
	f        *os.File // active segment file
	timer    *time.Timer
	segs     []*segMeta // sealed, ascending id
	nextID   uint64
	hwm      uint64 // ids below this are reserved in .next-id
	writeSeq uint64
	syncSeq  uint64
	syncing  bool
	failed   error
	notify   chan struct{} // closed and replaced when a segment becomes claimable
	dropped  bool
	closed   bool
}

// Spool is safe for concurrent use.
type Spool struct {
	dir        string
	opts       Options
	instanceID string
	used       atomic.Int64

	mu      sync.Mutex // guards streams
	streams map[string]*stream

	closeMu sync.RWMutex // Append holds RLock for its whole duration
	closed  bool
	done    chan struct{}
}

// Open opens or creates a spool in dir and recovers any data left in it.
func Open(dir string, opts Options) (*Spool, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.SegmentMaxBytes <= 0 {
		opts.SegmentMaxBytes = DefaultSegmentMaxBytes
	}
	if opts.SegmentMaxAge <= 0 {
		opts.SegmentMaxAge = DefaultSegmentMaxAge
	}
	if opts.MaxRecordBytes <= 0 {
		opts.MaxRecordBytes = DefaultMaxRecordBytes
	}
	if opts.MaxRecordBytes > maxRecordCeiling {
		opts.MaxRecordBytes = maxRecordCeiling
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("spool: create dir: %w", err)
	}
	sp := &Spool{
		dir:     dir,
		opts:    opts,
		streams: map[string]*stream{},
		done:    make(chan struct{}),
	}
	id, err := sp.loadInstanceID()
	if err != nil {
		return nil, err
	}
	sp.instanceID = id

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("spool: read dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || validStream(e.Name()) != nil {
			continue
		}
		s, err := sp.recoverStream(e.Name())
		if err != nil {
			return nil, err
		}
		sp.streams[s.name] = s
	}
	return sp, nil
}

func (sp *Spool) loadInstanceID() (string, error) {
	path := filepath.Join(sp.dir, instanceFile)
	b, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(b))
		if _, derr := hex.DecodeString(id); derr != nil || len(id) != 32 {
			return "", fmt.Errorf("spool: %s is not a valid instance id; refusing to replace it because dedup tokens depend on it", path)
		}
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("spool: read instance id: %w", err)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("spool: generate instance id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	if err := writeFileAtomic(sp.dir, instanceFile, []byte(id+"\n")); err != nil {
		return "", fmt.Errorf("spool: write instance id: %w", err)
	}
	return id, nil
}

func (sp *Spool) newStream(name string) *stream {
	s := &stream{
		name:   name,
		dir:    filepath.Join(sp.dir, name),
		nextID: 1,
		notify: make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// recoverStream loads one stream directory: validates every segment, cuts
// torn tails and quarantines corruption, and positions nextID after every id
// seen on disk or reserved in .next-id.
func (sp *Spool) recoverStream(name string) (*stream, error) {
	s := sp.newStream(name)
	s.dirReady = true
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("spool: read stream dir: %w", err)
	}
	var ids []uint64
	var maxSeen uint64
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".tmp") {
			_ = os.Remove(filepath.Join(s.dir, n))
			continue
		}
		id, ok := parseID(n)
		if !ok {
			continue
		}
		maxSeen = max(maxSeen, id)
		if strings.HasSuffix(n, segExt) && len(n) == 20+len(segExt) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	if b, err := os.ReadFile(filepath.Join(s.dir, hwmFile)); err == nil {
		if v, perr := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); perr == nil {
			s.hwm = v
		}
	}
	s.nextID = max(maxSeen+1, s.hwm, 1)

	for i, id := range ids {
		last := i == len(ids)-1
		path := segPath(s.dir, id)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("spool: read segment: %w", err)
		}
		m, err := sp.checkSegment(s, id, data, last)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		s.segs = append(s.segs, m)
		sp.used.Add(m.bytes)
	}
	return s, nil
}

// checkSegment validates a segment read into data and repairs the file when
// needed. tornOK marks the segment that may have been active at a crash. It
// returns nil when nothing valid remains (the file is then deleted).
func (sp *Spool) checkSegment(s *stream, id uint64, data []byte, tornOK bool) (*segMeta, error) {
	path := segPath(s.dir, id)
	recs, end, prob := parseSegment(data)
	if prob == problemTorn && tornOK {
		slog.Warn("Spool: truncated torn tail of last segment",
			"stream", s.name, "segment", id, "kept_records", len(recs), "dropped_bytes", len(data)-end)
		if err := truncateSync(path, int64(end)); err != nil {
			return nil, fmt.Errorf("spool: truncate torn tail: %w", err)
		}
	} else if prob != problemNone {
		bad, err := sp.quarantine(s, id, data[end:])
		if err != nil {
			return nil, err
		}
		slog.Warn("Spool: corrupt segment, moved the rest to a quarantine file",
			"stream", s.name, "segment", id, "kept_records", len(recs),
			"quarantined_bytes", len(data)-end, "file", bad)
		if err := truncateSync(path, int64(end)); err != nil {
			return nil, fmt.Errorf("spool: truncate corrupt segment: %w", err)
		}
	}
	if len(recs) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("spool: remove empty segment: %w", err)
		}
		sp.syncDir(s.dir)
		return nil, nil
	}
	return &segMeta{
		id:      id,
		bytes:   int64(end),
		records: int64(len(recs)),
		created: time.Unix(0, int64(binary.LittleEndian.Uint64(data[8:16]))),
	}, nil
}

// quarantine writes tail to a new .bad file next to the segment.
func (sp *Spool) quarantine(s *stream, id uint64, tail []byte) (string, error) {
	base := fmt.Sprintf("%020d", id)
	for n := 0; ; n++ {
		name := base + badExt
		if n > 0 {
			name = fmt.Sprintf("%s.%d%s", base, n, badExt)
		}
		path := filepath.Join(s.dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("spool: create quarantine file: %w", err)
		}
		_, err = f.Write(tail)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", fmt.Errorf("spool: write quarantine file: %w", err)
		}
		sp.syncDir(s.dir)
		return path, nil
	}
}

type problem int

const (
	problemNone    problem = iota
	problemTorn            // incomplete or bad frame at the very end of the data
	problemCorrupt         // bad header, impossible length, or bad CRC followed by more data
)

// parseSegment returns the valid records, the offset just past the last
// valid frame, and what stopped the parse. Records alias data.
func parseSegment(data []byte) ([][]byte, int, problem) {
	if len(data) < headerSize {
		return nil, 0, problemTorn
	}
	if string(data[:8]) != segMagic {
		return nil, 0, problemCorrupt
	}
	var recs [][]byte
	off := headerSize
	for off < len(data) {
		if len(data)-off < frameHeaderSize {
			return recs, off, problemTorn
		}
		n := binary.LittleEndian.Uint32(data[off:])
		sum := binary.LittleEndian.Uint32(data[off+4:])
		if n > maxRecordCeiling {
			return recs, off, problemCorrupt
		}
		end := off + frameHeaderSize + int(n)
		if end > len(data) {
			return recs, off, problemTorn
		}
		if frameCRC(data[off:off+4], data[off+frameHeaderSize:end]) != sum {
			if end == len(data) {
				return recs, off, problemTorn
			}
			return recs, off, problemCorrupt
		}
		recs = append(recs, data[off+frameHeaderSize:end])
		off = end
	}
	return recs, off, problemNone
}

func frameCRC(lenBytes, payload []byte) uint32 {
	return crc32.Update(crc32.Checksum(lenBytes, castagnoli), castagnoli, payload)
}

// Append writes rec to stream and returns once it is durable (SyncAlways).
func (sp *Spool) Append(name string, rec []byte) error {
	if err := validStream(name); err != nil {
		return err
	}
	if len(rec) > sp.opts.MaxRecordBytes {
		return ErrTooLarge
	}
	sp.closeMu.RLock()
	defer sp.closeMu.RUnlock()
	if sp.closed {
		return ErrClosed
	}
	frameLen := int64(frameHeaderSize + len(rec))
	if sp.used.Add(frameLen) > sp.opts.MaxBytes {
		sp.used.Add(-frameLen)
		return ErrFull
	}
	frame := make([]byte, frameLen)
	binary.LittleEndian.PutUint32(frame, uint32(len(rec)))
	copy(frame[frameHeaderSize:], rec)
	binary.LittleEndian.PutUint32(frame[4:], frameCRC(frame[:4], rec))

	for {
		s := sp.stream(name)
		s.mu.Lock()
		if s.dropped {
			s.mu.Unlock()
			continue
		}
		err := sp.appendLocked(s, frame)
		s.mu.Unlock()
		return err
	}
}

// appendLocked owns the reservation of len(frame) bytes: it either moves it
// into the active segment's size or refunds it.
func (sp *Spool) appendLocked(s *stream, frame []byte) error {
	frameLen := int64(len(frame))
	if s.failed != nil {
		sp.used.Add(-frameLen)
		return s.failed
	}
	if s.active != nil && s.active.records > 0 && s.active.bytes+frameLen > sp.opts.SegmentMaxBytes {
		sp.sealLocked(s, s.active.id)
		if s.failed != nil {
			sp.used.Add(-frameLen)
			return s.failed
		}
	}
	if s.dropped {
		// sealLocked can wait on the lock, and DropStream may run meanwhile.
		sp.used.Add(-frameLen)
		return ErrStreamDropped
	}
	if s.active == nil {
		if err := sp.openActiveLocked(s); err != nil {
			sp.used.Add(-frameLen)
			return err
		}
	}
	a := s.active
	if _, err := s.f.WriteAt(frame, a.bytes); err != nil {
		sp.used.Add(-frameLen)
		// Cut any partial frame so the next write lands on a clean boundary.
		if terr := s.f.Truncate(a.bytes); terr != nil {
			s.failed = fmt.Errorf("spool: stream %s unusable after failed write: %w", s.name, terr)
		}
		return fmt.Errorf("spool: write: %w", err)
	}
	a.bytes += frameLen
	a.records++
	s.writeSeq++
	seq := s.writeSeq
	if a.bytes >= sp.opts.SegmentMaxBytes {
		sp.sealLocked(s, a.id)
	}
	if sp.opts.SyncMode == SyncNone {
		return nil
	}
	// Group commit: one appender at a time runs fsync with the lock released;
	// everyone whose write landed before that fsync started is covered by it.
	for s.syncSeq < seq {
		if s.failed != nil {
			return s.failed
		}
		if s.dropped {
			return ErrStreamDropped
		}
		if s.syncing {
			s.cond.Wait()
			continue
		}
		if s.f == nil {
			return fmt.Errorf("spool: stream %s has no active segment to sync", s.name)
		}
		s.syncing = true
		target, f := s.writeSeq, s.f
		s.mu.Unlock()
		err := f.Sync()
		s.mu.Lock()
		s.syncing = false
		if err != nil {
			s.failed = fmt.Errorf("spool: fsync failed, stream %s stops accepting data until restart: %w", s.name, err)
			slog.Error("Spool fsync failed", "stream", s.name, "error", err)
		} else if target > s.syncSeq {
			s.syncSeq = target
		}
		s.cond.Broadcast()
	}
	return nil
}

func (sp *Spool) openActiveLocked(s *stream) error {
	if !s.dirReady {
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			return fmt.Errorf("spool: create stream dir: %w", err)
		}
		sp.syncDir(sp.dir)
		s.dirReady = true
	}
	if s.nextID >= s.hwm {
		hwm := s.nextID + hwmBlock
		if err := writeFileAtomic(s.dir, hwmFile, []byte(strconv.FormatUint(hwm, 10)+"\n")); err != nil {
			return fmt.Errorf("spool: reserve segment ids: %w", err)
		}
		s.hwm = hwm
	}
	id := s.nextID
	s.nextID++
	f, err := os.OpenFile(segPath(s.dir, id), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("spool: create segment: %w", err)
	}
	now := time.Now()
	var hdr [headerSize]byte
	copy(hdr[:], segMagic)
	binary.LittleEndian.PutUint64(hdr[8:], uint64(now.UnixNano()))
	if _, err := f.WriteAt(hdr[:], 0); err != nil {
		f.Close()
		_ = os.Remove(segPath(s.dir, id))
		return fmt.Errorf("spool: write segment header: %w", err)
	}
	// The header itself is made durable by the first record's fsync.
	sp.syncDir(s.dir)
	sp.used.Add(headerSize)
	s.active = &segMeta{id: id, bytes: headerSize, created: now}
	s.f = f
	s.timer = time.AfterFunc(sp.opts.SegmentMaxAge, func() {
		defer safe.Recover("spool-seal")
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closed && !s.dropped {
			sp.sealLocked(s, id)
		}
	})
	return nil
}

// sealLocked syncs, closes and queues the active segment if it is still id.
func (sp *Spool) sealLocked(s *stream, id uint64) {
	for s.syncing {
		s.cond.Wait()
	}
	if s.active == nil || s.active.id != id {
		return
	}
	a, f := s.active, s.f
	s.active, s.f = nil, nil
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	var err error
	if sp.opts.SyncMode == SyncAlways {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.failed = fmt.Errorf("spool: sealing segment failed, stream %s stops accepting data until restart: %w", s.name, err)
		slog.Error("Spool seal failed", "stream", s.name, "segment", id, "error", err)
	} else {
		s.syncSeq = s.writeSeq
	}
	// Queue it either way: records acknowledged before a failure are durable.
	s.segs = append(s.segs, a)
	s.signal()
	s.cond.Broadcast()
}

func (s *stream) signal() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func (sp *Spool) stream(name string) *stream {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	s := sp.streams[name]
	if s == nil {
		s = sp.newStream(name)
		sp.streams[name] = s
	}
	return s
}

// Claim blocks until stream has a sealed, unclaimed segment, then claims and
// returns the oldest one. It returns ctx.Err() when ctx is done and ErrClosed
// once the spool is closed.
func (sp *Spool) Claim(ctx context.Context, name string) (*Segment, error) {
	if err := validStream(name); err != nil {
		return nil, err
	}
	for {
		select {
		case <-sp.done:
			return nil, ErrClosed
		default:
		}
		s := sp.stream(name)
		s.mu.Lock()
		var m *segMeta
		for _, c := range s.segs {
			if !c.claimed {
				m = c
				break
			}
		}
		if m != nil {
			m.claimed = true
			s.mu.Unlock()
			seg, err := sp.load(s, m)
			if err != nil {
				return nil, err
			}
			if seg == nil {
				continue
			}
			return seg, nil
		}
		ch := s.notify
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-sp.done:
			return nil, ErrClosed
		case <-ch:
			s.mu.Lock()
			dropped := s.dropped
			s.mu.Unlock()
			if dropped {
				return nil, ErrStreamDropped
			}
		}
	}
}

// load reads a claimed segment. It returns nil, nil when the segment turned
// out to hold nothing valid and was removed.
func (sp *Spool) load(s *stream, m *segMeta) (*Segment, error) {
	data, err := os.ReadFile(segPath(s.dir, m.id))
	if err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.dropped {
			return nil, ErrStreamDropped
		}
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("Spool segment file disappeared", "stream", s.name, "segment", m.id)
			sp.forgetLocked(s, m)
			return nil, nil
		}
		m.claimed = false
		return nil, fmt.Errorf("spool: read segment: %w", err)
	}
	recs, end, prob := parseSegment(data)
	if prob != problemNone {
		// Sealed segments were fully synced, so any damage is corruption.
		nm, err := sp.checkSegment(s, m.id, data, false)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			m.claimed = false
			return nil, err
		}
		if nm == nil {
			sp.forgetLocked(s, m)
			return nil, nil
		}
		sp.used.Add(int64(end) - m.bytes)
		m.bytes, m.records = int64(end), int64(len(recs))
	}
	return &Segment{
		Stream:  s.name,
		ID:      m.id,
		Bytes:   int64(end),
		records: recs,
		token:   fmt.Sprintf("chui-spool:%s:%s:%020d", sp.instanceID, s.name, m.id),
		s:       s,
		m:       m,
	}, nil
}

func (sp *Spool) forgetLocked(s *stream, m *segMeta) {
	if i := slices.Index(s.segs, m); i >= 0 {
		s.segs = slices.Delete(s.segs, i, i+1)
		sp.used.Add(-m.bytes)
	}
}

// Ack deletes a claimed segment and frees its space.
func (sp *Spool) Ack(g *Segment) error {
	s := g.s
	s.mu.Lock()
	if s.dropped {
		g.settled = true
		s.mu.Unlock()
		return nil
	}
	if g.settled || !g.m.claimed {
		s.mu.Unlock()
		return ErrNotClaimed
	}
	g.settled = true
	s.mu.Unlock()

	err := os.Remove(segPath(s.dir, g.ID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		g.m.claimed = false
		s.signal()
		s.mu.Unlock()
		return fmt.Errorf("spool: delete segment: %w", err)
	}
	sp.syncDir(s.dir)
	s.mu.Lock()
	sp.forgetLocked(s, g.m)
	s.mu.Unlock()
	return nil
}

// Release returns a claimed segment so a later Claim retries it.
func (sp *Spool) Release(g *Segment) error {
	s := g.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped {
		g.settled = true
		return nil
	}
	if g.settled || !g.m.claimed {
		return ErrNotClaimed
	}
	g.settled = true
	g.m.claimed = false
	s.signal()
	return nil
}

// Stats reports the unacked data of one stream.
func (sp *Spool) Stats(name string) Stats {
	sp.mu.Lock()
	s := sp.streams[name]
	sp.mu.Unlock()
	if s == nil {
		return Stats{}
	}
	return s.stats(time.Now())
}

// TotalStats sums Stats over all streams. OldestAge is the maximum.
func (sp *Spool) TotalStats() Stats {
	sp.mu.Lock()
	all := make([]*stream, 0, len(sp.streams))
	for _, s := range sp.streams {
		all = append(all, s)
	}
	sp.mu.Unlock()
	now := time.Now()
	var t Stats
	for _, s := range all {
		st := s.stats(now)
		t.Bytes += st.Bytes
		t.Segments += st.Segments
		t.Records += st.Records
		t.OldestAge = max(t.OldestAge, st.OldestAge)
	}
	return t
}

func (s *stream) stats(now time.Time) Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st Stats
	var oldest time.Time
	add := func(m *segMeta) {
		st.Bytes += m.bytes
		st.Segments++
		st.Records += m.records
		if oldest.IsZero() || m.created.Before(oldest) {
			oldest = m.created
		}
	}
	for _, m := range s.segs {
		add(m)
	}
	if s.active != nil {
		add(s.active)
	}
	if !oldest.IsZero() {
		st.OldestAge = max(now.Sub(oldest), 0)
	}
	return st
}

// Streams lists the streams that have a directory on disk, sorted.
func (sp *Spool) Streams() []string {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	var out []string
	for name, s := range sp.streams {
		s.mu.Lock()
		ready := s.dirReady
		s.mu.Unlock()
		if ready {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// DropStream deletes all data of a stream, including quarantine files and its
// segment id reservation. Claims blocked on it return ErrStreamDropped and
// outstanding Segments become no-ops for Ack and Release.
func (sp *Spool) DropStream(name string) error {
	if err := validStream(name); err != nil {
		return err
	}
	// Holding sp.mu keeps a concurrent Append from recreating the directory
	// while it is being removed.
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if s := sp.streams[name]; s != nil {
		delete(sp.streams, name)
		s.mu.Lock()
		for s.syncing {
			s.cond.Wait()
		}
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
		if s.f != nil {
			s.f.Close()
			s.f = nil
		}
		var freed int64
		for _, m := range s.segs {
			freed += m.bytes
		}
		if s.active != nil {
			freed += s.active.bytes
		}
		s.active, s.segs = nil, nil
		s.dropped = true
		s.signal()
		s.cond.Broadcast()
		s.mu.Unlock()
		sp.used.Add(-freed)
	}
	if err := os.RemoveAll(filepath.Join(sp.dir, name)); err != nil {
		return fmt.Errorf("spool: remove stream: %w", err)
	}
	sp.syncDir(sp.dir)
	return nil
}

// Close waits for in-flight Appends, then seals and syncs every active
// segment. Blocked Claims return ErrClosed.
func (sp *Spool) Close() error {
	sp.closeMu.Lock()
	if sp.closed {
		sp.closeMu.Unlock()
		return nil
	}
	sp.closed = true
	close(sp.done)
	sp.closeMu.Unlock()

	sp.mu.Lock()
	defer sp.mu.Unlock()
	var errs []error
	for _, s := range sp.streams {
		s.mu.Lock()
		s.closed = true
		if s.active != nil {
			sp.sealLocked(s, s.active.id)
		}
		if s.failed != nil {
			errs = append(errs, s.failed)
		}
		s.mu.Unlock()
	}
	return errors.Join(errs...)
}

func validStream(name string) error {
	if name == "" || len(name) > maxName || name[0] == '.' {
		return ErrInvalidStream
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return ErrInvalidStream
		}
	}
	return nil
}

func segPath(dir string, id uint64) string {
	return filepath.Join(dir, fmt.Sprintf("%020d%s", id, segExt))
}

// parseID reads the 20-digit id prefix of a .seg or .bad file name.
func parseID(name string) (uint64, bool) {
	if len(name) < 21 || name[20] != '.' || !(strings.HasSuffix(name, segExt) || strings.HasSuffix(name, badExt)) {
		return 0, false
	}
	id, err := strconv.ParseUint(name[:20], 10, 64)
	return id, err == nil
}

func (sp *Spool) syncDir(dir string) {
	if sp.opts.SyncMode == SyncNone {
		return
	}
	if err := syncDir(dir); err != nil {
		slog.Warn("Spool directory fsync failed", "dir", dir, "error", err)
	}
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

func truncateSync(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = f.Truncate(size)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// writeFileAtomic writes name in dir through a synced temp file and rename.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp := filepath.Join(dir, name+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}
