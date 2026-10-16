package pipelines

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

// alertRig is a runner with a spool and fake running pipelines; the tests
// drive the watcher's ticks with their own clock.
type alertRig struct {
	t    *testing.T
	db   *database.DB
	conn string
	r    *Runner
}

func newAlertRig(t *testing.T) *alertRig {
	t.Helper()
	db, conn, cfg := testRunnerDB(t)
	cfg.IngestSpoolMaxBytes = 1 << 20
	sp, err := spool.Open(t.TempDir(), spool.Options{MaxBytes: cfg.IngestSpoolMaxBytes})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	r := NewRunner(db, nil, cfg)
	r.SetHub(NewHub(sp))
	rig := &alertRig{t: t, db: db, conn: conn, r: r}
	rig.reboot(time.Now().Add(-time.Hour))
	return rig
}

// reboot replaces the watcher, as a server restart does.
func (g *alertRig) reboot(bootAt time.Time) {
	g.r.alerts = newIngestWatch(g.r)
	g.r.alerts.bootAt = bootAt
}

// run adds a running pipeline with a database row.
func (g *alertRig) run(name, sourceType string, stallAfter time.Duration, started time.Time) *RunningPipeline {
	g.t.Helper()
	id, err := g.db.CreatePipeline(name, "", g.conn, "admin")
	if err != nil {
		g.t.Fatal(err)
	}
	g.db.UpdatePipelineStatus(id, "running", "")
	runID, _ := g.db.CreatePipelineRun(id, "running")
	rp := &RunningPipeline{PipelineID: id, RunID: runID, StartedAt: started, Metrics: &Metrics{}, Done: make(chan struct{}), writeHealth: &Health{},
		watch: runWatch{name: name, connectionID: g.conn, sourceType: sourceType, signal: "logs", stallAfter: stallAfter}}
	rp.watchReady.Store(true)
	g.r.mu.Lock()
	g.r.pipelines[id] = rp
	g.r.mu.Unlock()
	return rp
}

func (g *alertRig) stop(rp *RunningPipeline) {
	g.r.mu.Lock()
	delete(g.r.pipelines, rp.PipelineID)
	g.r.mu.Unlock()
}

type gotEvent struct {
	database.AlertEvent
	payload map[string]interface{}
}

// events returns the events of a type in the order they were written.
func (g *alertRig) events(eventType string) []gotEvent {
	g.t.Helper()
	rows, err := g.db.Conn().Query(`SELECT id FROM alert_events WHERE event_type = ? ORDER BY rowid`, eventType)
	if err != nil {
		g.t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]gotEvent, 0, len(ids))
	for _, id := range ids {
		ev, err := g.db.GetAlertEvent(id)
		if err != nil || ev == nil {
			g.t.Fatal(err)
		}
		var p map[string]interface{}
		if ev.PayloadJSON != nil {
			_ = json.Unmarshal([]byte(*ev.PayloadJSON), &p)
		}
		out = append(out, gotEvent{*ev, p})
	}
	return out
}

func (g *alertRig) total() int {
	n := 0
	for _, t := range alerts.PipelineEventTypes {
		n += len(g.events(t))
	}
	return n
}

func (g *alertRig) wantStatuses(eventType string, want ...string) []gotEvent {
	g.t.Helper()
	evs := g.events(eventType)
	got := make([]string, len(evs))
	for i, e := range evs {
		got[i], _ = e.payload["status"].(string)
		if e.payload["escalated"] == true {
			got[i] = "escalated"
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		g.t.Fatalf("%s events: got %v, want %v", eventType, got, want)
	}
	return evs
}

func TestIngestAlertsNothingRunning(t *testing.T) {
	g := newAlertRig(t)
	now := time.Now()
	for i := 0; i < 3; i++ {
		g.r.alerts.tick(now.Add(time.Duration(i) * alertsInterval))
	}
	if n := g.total(); n != 0 {
		t.Fatalf("want no events, got %d", n)
	}
}

// A stall opens once after stall_after_minutes of silence, is not repeated,
// and resolves when data arrives.
func TestIngestStalledOpensDedupsResolves(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("otel-logs", "source_ingest", 15*time.Minute, t0)
	st := g.r.Hub().Stats(rp.PipelineID)
	st.Accepted(10, 100, t0.Add(time.Minute))

	g.r.alerts.tick(t0.Add(10 * time.Minute))
	g.wantStatuses(alerts.EventTypeIngestStalled)
	g.r.alerts.tick(t0.Add(16 * time.Minute))
	g.r.alerts.tick(t0.Add(17 * time.Minute))
	evs := g.wantStatuses(alerts.EventTypeIngestStalled, "firing")
	ev := evs[0]
	if ev.Severity != alerts.SeverityWarn || ev.Title != "Ingest stalled: otel-logs" || !strings.Contains(ev.Message, "15 minutes") ||
		ev.payload["link_path"] != "/pipelines/"+rp.PipelineID || ev.payload["alert_key"] != alerts.PipelineAlertKey(rp.PipelineID, kindStalled) ||
		ev.ConnectionID == nil || *ev.ConnectionID != g.conn || ev.SourceRef == nil || *ev.SourceRef != alerts.PipelineSourceRef(rp.PipelineID) {
		t.Fatalf("unexpected firing event: %+v %v", ev.AlertEvent, ev.payload)
	}

	st.Accepted(1, 10, t0.Add(18*time.Minute))
	g.r.alerts.tick(t0.Add(18*time.Minute + 30*time.Second))
	evs = g.wantStatuses(alerts.EventTypeIngestStalled, "firing", "resolved")
	if evs[1].payload["incident_id"] != ev.payload["incident_id"] || !strings.HasPrefix(evs[1].Title, "[RESOLVED]") {
		t.Fatalf("resolve does not close the incident: %v", evs[1].payload)
	}
	g.r.alerts.tick(t0.Add(19 * time.Minute))
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing", "resolved")
}

// No stall without earlier data, with stall alerts off, and an earlier
// run's rows count as earlier data.
func TestIngestStalledThresholds(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	never := g.run("never", "source_ingest", 15*time.Minute, t0)
	off := g.run("off", "source_webhook", 0, t0)
	g.r.Hub().Stats(off.PipelineID).Accepted(1, 1, t0)
	before := g.run("before", "source_ingest", 15*time.Minute, t0)
	oldRun, _ := g.db.CreatePipelineRun(before.PipelineID, "running")
	g.db.UpdatePipelineRun(oldRun, "stopped", 42, 100, 0, "", "{}")

	g.r.alerts.tick(t0.Add(20 * time.Minute))
	evs := g.events(alerts.EventTypeIngestStalled)
	if len(evs) != 1 || evs[0].payload["pipeline_id"] != before.PipelineID {
		t.Fatalf("want one stall for the pipeline with earlier rows, got %d (never %s)", len(evs), never.PipelineID)
	}
	if !strings.Contains(evs[0].Message, "since the pipeline started") {
		t.Fatalf("message should count from the start: %s", evs[0].Message)
	}
}

// Turning stall alerts off resolves an open stall.
func TestIngestStalledDisableResolves(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("p", "source_ingest", 15*time.Minute, t0)
	g.r.Hub().Stats(rp.PipelineID).Accepted(1, 1, t0)
	g.r.alerts.tick(t0.Add(16 * time.Minute))
	rp.watch.stallAfter = 0
	g.r.alerts.tick(t0.Add(17 * time.Minute))
	evs := g.wantStatuses(alerts.EventTypeIngestStalled, "firing", "resolved")
	if !strings.Contains(evs[1].Message, "turned off") {
		t.Fatalf("resolve message: %s", evs[1].Message)
	}
}

// After a restart an open stall stays open without new events until data
// arrives, and resolves once.
func TestIngestStalledSurvivesRestart(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-2 * time.Hour)
	rp := g.run("p", "source_ingest", 15*time.Minute, t0)
	g.r.Hub().Stats(rp.PipelineID).Accepted(1, 1, t0)
	g.r.alerts.tick(t0.Add(16 * time.Minute))
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing")

	// Restart: fresh watcher and counters, the pipeline resumes.
	boot := t0.Add(30 * time.Minute)
	g.stop(rp)
	sp, err := spool.Open(t.TempDir(), spool.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	g.r.hub = NewHub(sp)
	g.reboot(boot)
	g.r.mu.Lock()
	rp2 := &RunningPipeline{PipelineID: rp.PipelineID, RunID: "run-2", StartedAt: boot, Metrics: &Metrics{}, writeHealth: &Health{}, watch: rp.watch}
	rp2.watchReady.Store(true)
	g.r.pipelines[rp.PipelineID] = rp2
	g.r.mu.Unlock()

	for m := 1; m <= 40; m++ {
		g.r.alerts.tick(boot.Add(time.Duration(m) * time.Minute))
	}
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing")
	g.r.Hub().Stats(rp.PipelineID).Accepted(1, 1, boot.Add(41*time.Minute))
	g.r.alerts.tick(boot.Add(42 * time.Minute))
	g.r.alerts.tick(boot.Add(43 * time.Minute))
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing", "resolved")
}

// An open incident whose pipeline does not come back after a restart
// resolves once the resume grace is over, not before.
func TestIngestAlertsStoppedPipelineResolves(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("p", "source_ingest", 15*time.Minute, t0)
	g.r.Hub().Stats(rp.PipelineID).Accepted(1, 1, t0)
	g.r.alerts.tick(t0.Add(16 * time.Minute))

	boot := t0.Add(20 * time.Minute)
	g.stop(rp)
	g.reboot(boot)
	g.r.alerts.tick(boot.Add(30 * time.Second))
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing")
	g.r.alerts.tick(boot.Add(resumeGrace))
	evs := g.wantStatuses(alerts.EventTypeIngestStalled, "firing", "resolved")
	if !strings.Contains(evs[1].Message, "no longer running") {
		t.Fatalf("resolve message: %s", evs[1].Message)
	}
	g.r.alerts.tick(boot.Add(resumeGrace + time.Minute))
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing", "resolved")
}

// A user stop resolves at once: the pipeline row is not "running".
func TestIngestAlertsUserStopResolves(t *testing.T) {
	g := newAlertRig(t)
	g.reboot(time.Now())
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("p", "source_ingest", 15*time.Minute, t0)
	rp.writeHealth.Fail("ClickHouse write", errors.New("down"))
	rp.writeHealth.Fail("ClickHouse write", errors.New("down"))
	rp.writeHealth.Fail("ClickHouse write", errors.New("down"))
	g.r.alerts.tick(time.Now())
	g.stop(rp)
	g.db.UpdatePipelineStatus(rp.PipelineID, "stopped", "")
	g.r.alerts.tick(time.Now().Add(alertsInterval))
	g.wantStatuses(alerts.EventTypePipelineSinkFailing, "firing", "resolved")
}

// A shutdown evaluates nothing, so it resolves nothing.
func TestIngestAlertsShutdownQuiet(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("p", "source_ingest", 15*time.Minute, t0)
	g.r.Hub().Stats(rp.PipelineID).Accepted(1, 1, t0)
	g.r.alerts.tick(t0.Add(16 * time.Minute))
	g.r.mu.Lock()
	g.r.stopping = true
	g.r.mu.Unlock()
	g.r.alerts.tick(t0.Add(17 * time.Minute))
	g.wantStatuses(alerts.EventTypeIngestStalled, "firing")
}

func TestPipelineSinkFailing(t *testing.T) {
	g := newAlertRig(t)
	now := time.Now().Add(-time.Hour)
	rp := g.run("kafka-orders", "source_kafka", 0, now.Add(-time.Hour))
	rp.writeHealth.Fail("ClickHouse write", errors.New("connection refused"))
	g.r.alerts.tick(now)
	g.wantStatuses(alerts.EventTypePipelineSinkFailing)
	rp.writeHealth.Fail("ClickHouse write", errors.New("connection refused"))
	rp.writeHealth.Fail("ClickHouse write", errors.New("connection refused"))
	g.r.alerts.tick(now.Add(alertsInterval))
	g.r.alerts.tick(now.Add(2 * alertsInterval))
	evs := g.wantStatuses(alerts.EventTypePipelineSinkFailing, "firing")
	if evs[0].Severity != alerts.SeverityError || !strings.Contains(evs[0].Message, "connection refused") {
		t.Fatalf("firing event: %+v", evs[0].AlertEvent)
	}
	rp.writeHealth.OK()
	g.r.alerts.tick(now.Add(3 * alertsInterval))
	g.wantStatuses(alerts.EventTypePipelineSinkFailing, "firing", "resolved")
	// Pull sources get no ingest alerts.
	if n := len(g.events(alerts.EventTypeIngestStalled)) + len(g.events(alerts.EventTypeIngestRejecting)); n != 0 {
		t.Fatalf("a kafka pipeline raised %d ingest events", n)
	}
}

// Rejections over 10% open ingest.rejecting, more than half escalate it,
// and it resolves after the window is clean; auth failures are their own
// alert and do not count.
func TestIngestRejecting(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("p", "source_ingest", 0, t0)
	st := g.r.Hub().Stats(rp.PipelineID)
	at := func(i int) time.Time { return t0.Add(time.Duration(i) * alertsInterval) }
	accept := func(n int) {
		for range n {
			st.Accepted(1, 1, t0)
		}
	}

	g.r.alerts.tick(at(0))
	accept(95)
	st.RejectedQuota.Add(5)
	st.RejectedAuth.Add(200)
	g.r.alerts.tick(at(1)) // 5%: fine, auth not counted
	g.wantStatuses(alerts.EventTypeIngestRejecting)

	accept(80)
	st.RejectedRateLimit.Add(20)
	g.r.alerts.tick(at(2)) // 25 of 200: 12%
	evs := g.wantStatuses(alerts.EventTypeIngestRejecting, "firing")
	if evs[0].Severity != alerts.SeverityWarn || !strings.Contains(evs[0].Message, "quota 5, rate limit 20") {
		t.Fatalf("firing event: %s", evs[0].Message)
	}
	st.RejectedMalformed.Add(400)
	g.r.alerts.tick(at(3))
	g.r.alerts.tick(at(4))
	evs = g.wantStatuses(alerts.EventTypeIngestRejecting, "firing", "escalated")
	if evs[1].Severity != alerts.SeverityError {
		t.Fatalf("escalation severity %s", evs[1].Severity)
	}
	// Clean traffic until the rejections leave the window.
	for i := 5; i < 30; i++ {
		accept(50)
		g.r.alerts.tick(at(i))
	}
	g.wantStatuses(alerts.EventTypeIngestRejecting, "firing", "escalated", "resolved")

	// 300 auth failures in the window fire the low-severity auth alert.
	g.wantStatuses(alerts.EventTypeIngestAuthFailures)
	st.RejectedAuth.Add(400)
	g.r.alerts.tick(at(30))
	evs = g.wantStatuses(alerts.EventTypeIngestAuthFailures, "firing")
	if evs[0].Severity != alerts.SeverityInfo {
		t.Fatalf("auth severity %s", evs[0].Severity)
	}
}

// Too few requests never fire, whatever the share.
func TestIngestRejectingMinimumVolume(t *testing.T) {
	g := newAlertRig(t)
	t0 := time.Now().Add(-time.Hour)
	rp := g.run("p", "source_ingest", 0, t0)
	st := g.r.Hub().Stats(rp.PipelineID)
	g.r.alerts.tick(t0)
	st.RejectedTooLarge.Add(rejectMinRequests - 1)
	g.r.alerts.tick(t0.Add(alertsInterval))
	g.wantStatuses(alerts.EventTypeIngestRejecting)
}

// The spool alert is warning above 80% of the cap, escalates to critical
// above 95%, and resolves after two readings below 80%.
func TestSpoolCapacityReadings(t *testing.T) {
	g := newAlertRig(t)
	w, now := g.r.alerts, time.Now()
	step := func(bytes int64) {
		now = now.Add(alertsInterval)
		w.step("", runWatch{}, "", alerts.EventTypeIngestSpoolBacklog, spoolReading(bytes, 1000), resolveAfter, now)
	}
	step(700)
	g.wantStatuses(alerts.EventTypeIngestSpoolBacklog)
	step(850)
	step(860)
	evs := g.wantStatuses(alerts.EventTypeIngestSpoolBacklog, "firing")
	if evs[0].Severity != alerts.SeverityWarn || evs[0].ConnectionID != nil || evs[0].payload["alert_key"] != alerts.SpoolAlertKey || evs[0].Title != "Ingest spool 85% full" {
		t.Fatalf("firing event: %+v %v", evs[0].AlertEvent, evs[0].payload)
	}
	step(970)
	evs = g.wantStatuses(alerts.EventTypeIngestSpoolBacklog, "firing", "escalated")
	if evs[1].Severity != alerts.SeverityCritical {
		t.Fatalf("escalated to %s", evs[1].Severity)
	}
	step(500)
	g.wantStatuses(alerts.EventTypeIngestSpoolBacklog, "firing", "escalated")
	step(500)
	evs = g.wantStatuses(alerts.EventTypeIngestSpoolBacklog, "firing", "escalated", "resolved")
	if evs[2].Severity != alerts.SeverityCritical || evs[2].Title != "[RESOLVED] Ingest spool almost full" {
		t.Fatalf("resolve: %+v", evs[2].AlertEvent)
	}
}

func TestSpoolBacklogReading(t *testing.T) {
	rp := &RunningPipeline{watch: runWatch{name: "p"}}
	if rd := backlogReading(rp, 100, 4*time.Minute); rd.sev != "" || !rd.clear {
		t.Fatalf("4 minutes old should be fine: %+v", rd)
	}
	rd := backlogReading(rp, 3<<20, 7*time.Minute)
	if rd.sev != alerts.SeverityWarn || !strings.Contains(rd.message, "3.0 MiB") || !strings.Contains(rd.message, "7 minutes old") {
		t.Fatalf("backlog reading: %+v", rd)
	}
}

// The per-pipeline backlog reads the real spool: fresh data is no backlog.
func TestSpoolBacklogFreshDataQuiet(t *testing.T) {
	g := newAlertRig(t)
	rp := g.run("p", "source_ingest", 0, time.Now())
	if err := g.r.Hub().Append(rp.PipelineID, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	g.r.alerts.tick(time.Now())
	g.r.alerts.tick(time.Now().Add(alertsInterval))
	if n := g.total(); n != 0 {
		t.Fatalf("want no events, got %d", n)
	}
}

func TestNewRunWatchStallSetting(t *testing.T) {
	cases := []struct {
		fields map[string]interface{}
		want   time.Duration
	}{
		{map[string]interface{}{}, DefaultStallAfterMinutes * time.Minute},
		{map[string]interface{}{"stall_after_minutes": float64(5)}, 5 * time.Minute},
		{map[string]interface{}{"stall_after_minutes": float64(0)}, 0},
	}
	for _, c := range cases {
		w := newRunWatch(&preparedPipeline{pipeline: &database.Pipeline{Name: "n"}, sourceCfg: ConnectorConfig{NodeType: "source_ingest", Fields: c.fields}})
		if w.stallAfter != c.want {
			t.Errorf("%v: got %v, want %v", c.fields, w.stallAfter, c.want)
		}
	}
	if w := newRunWatch(&preparedPipeline{sourceCfg: ConnectorConfig{NodeType: "source_kafka", Fields: map[string]interface{}{}}}); w.stallAfter != 0 {
		t.Fatal("pull sources have no stall setting")
	}
	if err := (&IngestSource{}).Validate(ConnectorConfig{Fields: map[string]interface{}{"signal": "logs", "stall_after_minutes": float64(-1)}}); err == nil {
		t.Fatal("negative stall_after_minutes accepted")
	}
}
