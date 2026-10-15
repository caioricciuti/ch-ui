package spool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// crash simulates a process death: file handles are closed without sealing
// or syncing and timers stop. Data already written stays in the files, as it
// would in the page cache of a killed process.
func (sp *Spool) crash() {
	sp.closeMu.Lock()
	sp.closed = true
	close(sp.done)
	sp.closeMu.Unlock()
	sp.mu.Lock()
	defer sp.mu.Unlock()
	for _, s := range sp.streams {
		s.mu.Lock()
		s.closed = true
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.f != nil {
			s.f.Close()
			s.f = nil
		}
		s.mu.Unlock()
	}
}

func open(t *testing.T, dir string, opts Options) *Spool {
	t.Helper()
	sp, err := Open(dir, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return sp
}

func rec(i int) []byte { return fmt.Appendf(nil, "record-%05d", i) }

func claim(t *testing.T, sp *Spool, stream string, wait time.Duration) *Segment {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	seg, err := sp.Claim(ctx, stream)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return seg
}

// drain claims and acks until n records were read, returning them in order.
func drain(t *testing.T, sp *Spool, stream string, n int) ([][]byte, []uint64) {
	t.Helper()
	var out [][]byte
	var ids []uint64
	for len(out) < n {
		seg := claim(t, sp, stream, 3*time.Second)
		ids = append(ids, seg.ID)
		for _, r := range seg.Records() {
			out = append(out, bytes.Clone(r))
		}
		if err := sp.Ack(seg); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}
	return out, ids
}

func segFiles(t *testing.T, dir, ext string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "*"+ext))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAppendClaimAckOrder(t *testing.T) {
	dir := t.TempDir()
	sp := open(t, dir, Options{SegmentMaxBytes: 200, SegmentMaxAge: 30 * time.Millisecond})
	defer sp.Close()
	const n = 50
	for i := range n {
		if err := sp.Append("p1", rec(i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	got, ids := drain(t, sp, "p1", n)
	if len(got) != n {
		t.Fatalf("got %d records, want %d", len(got), n)
	}
	for i, r := range got {
		if !bytes.Equal(r, rec(i)) {
			t.Fatalf("record %d = %q, want %q", i, r, rec(i))
		}
	}
	if len(ids) < 2 {
		t.Fatalf("expected several segments, got %d", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("segment ids not ascending: %v", ids)
		}
	}
	if st := sp.Stats("p1"); st != (Stats{}) {
		t.Fatalf("stats after draining = %+v, want zero", st)
	}
	if u := sp.used.Load(); u != 0 {
		t.Fatalf("used = %d after draining, want 0", u)
	}
	if left := segFiles(t, filepath.Join(dir, "p1"), segExt); len(left) != 0 {
		t.Fatalf("segment files left after ack: %v", left)
	}
}

func TestReleaseReclaimAndSingleClaim(t *testing.T) {
	sp := open(t, t.TempDir(), Options{SegmentMaxBytes: 16 + 8 + 12 + 1, SegmentMaxAge: time.Hour})
	defer sp.Close()
	for i := range 3 { // one record per segment; the third stays active
		if err := sp.Append("p", rec(i)); err != nil {
			t.Fatal(err)
		}
	}
	a := claim(t, sp, "p", time.Second)
	b := claim(t, sp, "p", time.Second)
	if a.ID == b.ID {
		t.Fatalf("same segment claimed twice: %d", a.ID)
	}
	if a.ID > b.ID {
		t.Fatalf("claims out of order: %d then %d", a.ID, b.ID)
	}
	// Both sealed segments are claimed, the third is active: nothing to claim.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := sp.Claim(ctx, "p"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Claim with all segments claimed = %v, want deadline exceeded", err)
	}
	if err := sp.Release(a); err != nil {
		t.Fatal(err)
	}
	if err := sp.Release(a); !errors.Is(err, ErrNotClaimed) {
		t.Fatalf("second Release = %v, want ErrNotClaimed", err)
	}
	again := claim(t, sp, "p", time.Second)
	if again.ID != a.ID || again.DedupToken() != a.DedupToken() {
		t.Fatalf("reclaimed %d (%s), want %d (%s)", again.ID, again.DedupToken(), a.ID, a.DedupToken())
	}
	if !bytes.Equal(again.Records()[0], rec(0)) {
		t.Fatalf("reclaimed record = %q", again.Records()[0])
	}
	if err := sp.Ack(again); err != nil {
		t.Fatal(err)
	}
	if err := sp.Ack(again); !errors.Is(err, ErrNotClaimed) {
		t.Fatalf("second Ack = %v, want ErrNotClaimed", err)
	}
	if err := sp.Ack(a); !errors.Is(err, ErrNotClaimed) {
		t.Fatalf("Ack of released handle = %v, want ErrNotClaimed", err)
	}
	if err := sp.Ack(b); err != nil {
		t.Fatal(err)
	}
}

func TestSealBySize(t *testing.T) {
	recSize := 8 + len(rec(0))
	sp := open(t, t.TempDir(), Options{SegmentMaxBytes: int64(16 + 3*recSize), SegmentMaxAge: time.Hour})
	defer sp.Close()
	for i := range 2 {
		if err := sp.Append("p", rec(i)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := sp.Claim(ctx, "p"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Claim before the segment is full = %v, want deadline exceeded", err)
	}
	if err := sp.Append("p", rec(2)); err != nil {
		t.Fatal(err)
	}
	seg := claim(t, sp, "p", 50*time.Millisecond)
	if len(seg.Records()) != 3 {
		t.Fatalf("sealed segment has %d records, want 3", len(seg.Records()))
	}
	if seg.Bytes != int64(16+3*recSize) {
		t.Fatalf("segment bytes = %d, want %d", seg.Bytes, 16+3*recSize)
	}
}

func TestSealByAgeWithoutNewAppends(t *testing.T) {
	sp := open(t, t.TempDir(), Options{SegmentMaxAge: 100 * time.Millisecond})
	defer sp.Close()
	start := time.Now()
	if err := sp.Append("quiet", rec(0)); err != nil {
		t.Fatal(err)
	}
	seg := claim(t, sp, "quiet", 3*time.Second)
	if el := time.Since(start); el < 100*time.Millisecond {
		t.Fatalf("segment sealed after %v, before SegmentMaxAge", el)
	}
	if len(seg.Records()) != 1 || !bytes.Equal(seg.Records()[0], rec(0)) {
		t.Fatalf("records = %q", seg.Records())
	}
}

func TestErrFullAndAckFreesSpace(t *testing.T) {
	// Header 16 plus 8 frames of 8+12 bytes = 176.
	sp := open(t, t.TempDir(), Options{MaxBytes: 176, SegmentMaxBytes: 176, SegmentMaxAge: time.Hour})
	defer sp.Close()
	var n int
	for ; ; n++ {
		err := sp.Append("p", rec(n))
		if errors.Is(err, ErrFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if n > 100 {
			t.Fatal("never got ErrFull")
		}
	}
	if n != 8 {
		t.Fatalf("accepted %d records before ErrFull, want 8", n)
	}
	st := sp.Stats("p")
	if st.Bytes > 176 || st.Records != 8 {
		t.Fatalf("stats = %+v, want 8 records within 176 bytes", st)
	}
	if err := sp.Append("other", rec(0)); !errors.Is(err, ErrFull) {
		t.Fatalf("Append to another stream = %v, want ErrFull (cap is global)", err)
	}
	seg := claim(t, sp, "p", 3*time.Second)
	if err := sp.Ack(seg); err != nil {
		t.Fatal(err)
	}
	if err := sp.Append("p", rec(99)); err != nil {
		t.Fatalf("Append after Ack freed space: %v", err)
	}
}

func TestRecoveryTornTail(t *testing.T) {
	dir := t.TempDir()
	sp := open(t, dir, Options{SegmentMaxAge: time.Hour})
	for i := range 5 {
		if err := sp.Append("p", rec(i)); err != nil {
			t.Fatal(err)
		}
	}
	sp.crash()
	files := segFiles(t, filepath.Join(dir, "p"), segExt)
	if len(files) != 1 {
		t.Fatalf("segment files = %v", files)
	}
	fi, _ := os.Stat(files[0])
	// Cut the last frame in the middle of its payload.
	if err := os.Truncate(files[0], fi.Size()-5); err != nil {
		t.Fatal(err)
	}

	sp2 := open(t, dir, Options{SegmentMaxAge: time.Hour})
	defer sp2.Close()
	if st := sp2.Stats("p"); st.Records != 4 || st.Segments != 1 {
		t.Fatalf("stats after recovery = %+v, want 4 records in 1 segment", st)
	}
	if bad := segFiles(t, filepath.Join(dir, "p"), badExt); len(bad) != 0 {
		t.Fatalf("torn tail should be truncated, not quarantined: %v", bad)
	}
	seg := claim(t, sp2, "p", time.Second)
	if len(seg.Records()) != 4 {
		t.Fatalf("recovered %d records, want 4", len(seg.Records()))
	}
	for i, r := range seg.Records() {
		if !bytes.Equal(r, rec(i)) {
			t.Fatalf("record %d = %q", i, r)
		}
	}
	// New data goes to a new segment with a higher id.
	if err := sp2.Append("p", rec(5)); err != nil {
		t.Fatal(err)
	}
	sp2.mu.Lock()
	activeID := sp2.streams["p"].active.id
	sp2.mu.Unlock()
	if activeID <= seg.ID {
		t.Fatalf("new segment id %d not after recovered %d", activeID, seg.ID)
	}
}

func TestRecoveryCorruptCRCQuarantines(t *testing.T) {
	dir := t.TempDir()
	recSize := 8 + len(rec(0))
	opts := Options{SegmentMaxBytes: int64(16 + 5*recSize), SegmentMaxAge: time.Hour}
	sp := open(t, dir, opts)
	for i := range 10 { // two sealed segments of 5 records
		if err := sp.Append("p", rec(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sp.Close(); err != nil {
		t.Fatal(err)
	}
	files := segFiles(t, filepath.Join(dir, "p"), segExt)
	if len(files) != 2 {
		t.Fatalf("segment files = %v", files)
	}
	// Flip one payload byte of the third record of the first segment.
	data, _ := os.ReadFile(files[0])
	off := 16 + 2*recSize + 8 + 3
	data[off] ^= 0xff
	if err := os.WriteFile(files[0], data, 0o600); err != nil {
		t.Fatal(err)
	}

	sp2 := open(t, dir, opts)
	defer sp2.Close()
	bad := segFiles(t, filepath.Join(dir, "p"), badExt)
	if len(bad) != 1 {
		t.Fatalf("quarantine files = %v, want 1", bad)
	}
	badData, _ := os.ReadFile(bad[0])
	if want := data[16+2*recSize:]; !bytes.Equal(badData, want) {
		t.Fatalf("quarantine holds %d bytes, want the %d bytes from the corrupt frame on", len(badData), len(want))
	}
	got, _ := drain(t, sp2, "p", 7)
	want := [][]byte{rec(0), rec(1), rec(5), rec(6), rec(7), rec(8), rec(9)}
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestClaimQuarantinesLateCorruption(t *testing.T) {
	dir := t.TempDir()
	sp := open(t, dir, Options{SegmentMaxBytes: int64(16 + 3*(8+len(rec(0)))), SegmentMaxAge: time.Hour})
	defer sp.Close()
	for i := range 3 { // the third record seals the segment by size
		if err := sp.Append("p", rec(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Damage the second frame's CRC on disk after sealing.
	files := segFiles(t, filepath.Join(dir, "p"), segExt)
	data, _ := os.ReadFile(files[0])
	data[16+(8+len(rec(0)))+4] ^= 0x01
	if err := os.WriteFile(files[0], data, 0o600); err != nil {
		t.Fatal(err)
	}
	seg := claim(t, sp, "p", time.Second)
	if len(seg.Records()) != 1 || !bytes.Equal(seg.Records()[0], rec(0)) {
		t.Fatalf("records = %q, want only the first", seg.Records())
	}
	if bad := segFiles(t, filepath.Join(dir, "p"), badExt); len(bad) != 1 {
		t.Fatalf("quarantine files = %v, want 1", bad)
	}
	if err := sp.Ack(seg); err != nil {
		t.Fatal(err)
	}
	if u := sp.used.Load(); u != 0 {
		t.Fatalf("used = %d after ack, want 0", u)
	}
}

func TestDedupTokenStableAndIDsNeverReused(t *testing.T) {
	dir := t.TempDir()
	opts := Options{SegmentMaxAge: 20 * time.Millisecond}
	sp := open(t, dir, opts)
	if err := sp.Append("p", rec(0)); err != nil {
		t.Fatal(err)
	}
	seg := claim(t, sp, "p", 3*time.Second)
	tok, id := seg.DedupToken(), seg.ID
	if !strings.Contains(tok, sp.instanceID) || !strings.Contains(tok, "p") {
		t.Fatalf("token %q lacks instance id or stream", tok)
	}
	if err := sp.Close(); err != nil { // close without ack: an insert that crashed
		t.Fatal(err)
	}

	sp2 := open(t, dir, opts)
	seg2 := claim(t, sp2, "p", time.Second)
	if seg2.ID != id || seg2.DedupToken() != tok {
		t.Fatalf("after reopen got %d %q, want %d %q", seg2.ID, seg2.DedupToken(), id, tok)
	}
	if err := sp2.Ack(seg2); err != nil {
		t.Fatal(err)
	}
	if err := sp2.Close(); err != nil {
		t.Fatal(err)
	}

	// Everything is acked and deleted. A new segment must still get a fresh
	// id, or ClickHouse would drop it as a duplicate of the old token.
	sp3 := open(t, dir, opts)
	defer sp3.Close()
	if err := sp3.Append("p", rec(1)); err != nil {
		t.Fatal(err)
	}
	seg3 := claim(t, sp3, "p", 3*time.Second)
	if seg3.ID <= id || seg3.DedupToken() == tok {
		t.Fatalf("segment id reused after restart: %d (%q) vs %d (%q)", seg3.ID, seg3.DedupToken(), id, tok)
	}
}

func TestConcurrentAppendersSurviveCrash(t *testing.T) {
	dir := t.TempDir()
	opts := Options{SegmentMaxBytes: 4 << 10, SegmentMaxAge: 20 * time.Millisecond}
	sp := open(t, dir, opts)
	const writers, perWriter = 8, 150
	var (
		mu    sync.Mutex
		acked = map[string]bool{}
		wg    sync.WaitGroup
	)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream := fmt.Sprintf("s%d", w%3)
			for i := range perWriter {
				r := fmt.Appendf(nil, "%s/w%d/%d", stream, w, i)
				if err := sp.Append(stream, r); err != nil {
					if errors.Is(err, ErrClosed) {
						return
					}
					t.Errorf("Append: %v", err)
					return
				}
				mu.Lock()
				acked[string(r)] = true
				mu.Unlock()
			}
		}()
	}
	// Drain one stream concurrently so Claim/Ack race with Append.
	consumed := map[string]bool{}
	ctx, cancel := context.WithCancel(context.Background())
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for {
			seg, err := sp.Claim(ctx, "s0")
			if err != nil {
				return
			}
			for _, r := range seg.Records() {
				consumed[string(r)] = true
			}
			if err := sp.Ack(seg); err != nil {
				t.Errorf("Ack: %v", err)
			}
		}
	}()
	time.Sleep(150 * time.Millisecond)
	sp.crash()
	wg.Wait()
	cancel()
	<-consumerDone

	sp2 := open(t, dir, opts)
	defer sp2.Close()
	found := map[string]bool{}
	for _, name := range sp2.Streams() {
		for {
			cctx, ccancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			seg, err := sp2.Claim(cctx, name)
			ccancel()
			if err != nil {
				break
			}
			for _, r := range seg.Records() {
				found[string(r)] = true
			}
			if err := sp2.Ack(seg); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(acked) == 0 {
		t.Fatal("no appends were acknowledged before the crash")
	}
	for r := range acked {
		if !found[r] && !consumed[r] {
			t.Fatalf("acknowledged record %q lost after crash", r)
		}
	}
	t.Logf("acked %d records, %d consumed before crash, %d recovered", len(acked), len(consumed), len(found))
}

func TestDropStream(t *testing.T) {
	dir := t.TempDir()
	sp := open(t, dir, Options{SegmentMaxBytes: 64, SegmentMaxAge: time.Hour})
	defer sp.Close()
	for i := range 5 {
		if err := sp.Append("gone", rec(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sp.Append("kept", rec(0)); err != nil {
		t.Fatal(err)
	}
	held := claim(t, sp, "gone", time.Second)

	// A second consumer takes the other sealed segment, then blocks and wakes on drop.
	waitErr := make(chan error, 1)
	go func() {
		for {
			seg, err := sp.Claim(context.Background(), "gone")
			if err != nil {
				waitErr <- err
				return
			}
			_ = seg // keep claiming until nothing is left and we block
		}
	}()
	time.Sleep(50 * time.Millisecond)
	keptBytes := sp.Stats("kept").Bytes
	if err := sp.DropStream("gone"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waitErr:
		if !errors.Is(err, ErrStreamDropped) {
			t.Fatalf("blocked Claim = %v, want ErrStreamDropped", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Claim did not wake on DropStream")
	}
	if _, err := os.Stat(filepath.Join(dir, "gone")); !os.IsNotExist(err) {
		t.Fatalf("stream dir still exists: %v", err)
	}
	if err := sp.Ack(held); err != nil {
		t.Fatalf("Ack of a dropped segment = %v, want nil", err)
	}
	if got := sp.Streams(); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("Streams = %v, want [kept]", got)
	}
	if u := sp.used.Load(); u != keptBytes {
		t.Fatalf("used = %d, want %d (only the kept stream)", u, keptBytes)
	}
	// The name can be used again.
	if err := sp.Append("gone", rec(9)); err != nil {
		t.Fatalf("Append after drop: %v", err)
	}
}

func TestValidationAndClose(t *testing.T) {
	sp := open(t, t.TempDir(), Options{MaxRecordBytes: 10, SegmentMaxAge: time.Hour})
	for _, name := range []string{"", ".", "..", ".hidden", "a/b", `a\b`, "a b", strings.Repeat("x", 129)} {
		if err := sp.Append(name, []byte("x")); !errors.Is(err, ErrInvalidStream) {
			t.Errorf("Append(%q) = %v, want ErrInvalidStream", name, err)
		}
	}
	if err := sp.Append("p", make([]byte, 11)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized Append = %v, want ErrTooLarge", err)
	}
	if err := sp.Append("p", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() {
		_, err := sp.Claim(context.Background(), "idle")
		blocked <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := sp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-blocked; !errors.Is(err, ErrClosed) {
		t.Fatalf("Claim blocked over Close = %v, want ErrClosed", err)
	}
	if err := sp.Append("p", []byte("late")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Append after Close = %v, want ErrClosed", err)
	}
	if st := sp.TotalStats(); st.Segments != 1 || st.Records != 1 {
		t.Fatalf("TotalStats after Close = %+v, want the sealed segment", st)
	}
}

func BenchmarkAppendParallel1KiB(b *testing.B) {
	sp, err := Open(b.TempDir(), Options{MaxBytes: 64 << 30})
	if err != nil {
		b.Fatal(err)
	}
	defer sp.Close()
	payload := bytes.Repeat([]byte("x"), 1024)
	b.SetParallelism(16)
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := sp.Append("bench", payload); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "records/s")
}
