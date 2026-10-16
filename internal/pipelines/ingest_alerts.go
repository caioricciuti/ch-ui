package pipelines

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

// Thresholds of the ingest alerts. The stall threshold is per pipeline
// (stall_after_minutes); the others are fixed.
const (
	DefaultStallAfterMinutes = 15
	// backlogAfter is how old a pipeline's oldest spooled data may get.
	backlogAfter = 5 * time.Minute
	// spoolWarnRatio and spoolCritRatio are shares of the spool cap.
	spoolWarnRatio = 0.80
	spoolCritRatio = 0.95
	// rejectWindow is the window of the rejection rates.
	rejectWindow = 5 * time.Minute
	// rejectRatio is the share of requests rejected (not counting auth)
	// that fires ingest.rejecting; rejectErrorRatio raises it to error.
	rejectRatio      = 0.10
	rejectErrorRatio = 0.50
	// rejectMinRequests keeps a handful of requests from firing it.
	rejectMinRequests = 20
	// authFailuresPerWindow fires ingest.auth_failures: one a second.
	authFailuresPerWindow = 300
	// resolveAfter is how many evaluations in a row must be fine before a
	// rate or backlog alert resolves.
	resolveAfter = 2
	// alertsInterval is how often the watcher evaluates.
	alertsInterval = 30 * time.Second
	// resumeGrace is how long after boot an open incident of a pipeline
	// that is "running" in the database waits for the pipeline to resume
	// before it resolves as stopped.
	resumeGrace = 2 * time.Minute
	// openIncidentScan is how many recent events per type the watcher
	// reads at boot to find the incidents left open.
	openIncidentScan = 500
)

// Alert kinds, the last part of an alert key.
const (
	kindStalled      = "stalled"
	kindSpoolBacklog = "spool_backlog"
	kindRejecting    = "rejecting"
	kindAuthFailures = "auth_failures"
	kindSinkFailing  = "sink_failing"
)

// runWatch is what the watcher reads about a run, set when it starts.
type runWatch struct {
	name, connectionID, sourceType, signal string
	// stallAfter is 0 when stall alerts are off.
	stallAfter time.Duration
}

func newRunWatch(p *preparedPipeline) runWatch {
	w := runWatch{sourceType: p.sourceCfg.NodeType}
	if p.pipeline != nil {
		w.name, w.connectionID = p.pipeline.Name, p.pipeline.ConnectionID
	}
	if isPushSource(w.sourceType) {
		w.signal = IngestSignal(p.sourceCfg.Fields)
		if w.sourceType == "source_webhook" {
			w.signal = "webhook"
		}
		w.stallAfter = time.Duration(max(intField(p.sourceCfg.Fields, "stall_after_minutes", DefaultStallAfterMinutes), 0)) * time.Minute
	}
	return w
}

func isPushSource(nodeType string) bool {
	return nodeType == "source_ingest" || nodeType == "source_webhook"
}

// watchAlert is one alert the watcher knows: open, or tracked towards a
// resolve.
type watchAlert struct {
	key, eventType, sourceRef      string
	pipelineID, connectionID, name string
	firing                         bool
	severity, incident             string
	firedAt                        time.Time
	okCount                        int
}

// reading is one evaluation of one alert. sev is the breach severity, ""
// when within thresholds; clear says the condition is over (a reading with
// neither holds an open incident as it is). title and message describe a
// breach, resolved the end of one.
type reading struct {
	sev            string
	clear          bool
	title, message string
	resolved       string
	facts          map[string]interface{}
}

// rejectLabels name the rejection counters of a sample, in order.
var rejectLabels = [...]string{"quota", "rate limit", "too large", "malformed", "spool full"}

// sample is a pipeline's counters at one evaluation, for the rates.
type sample struct {
	at             time.Time
	accepted, auth int64
	rejected       [len(rejectLabels)]int64
}

func takeSample(st *IngestStats, now time.Time) sample {
	return sample{at: now, accepted: st.Requests.Load(), auth: st.RejectedAuth.Load(), rejected: [len(rejectLabels)]int64{
		st.RejectedQuota.Load(), st.RejectedRateLimit.Load(), st.RejectedTooLarge.Load(), st.RejectedMalformed.Load(), st.RejectedSpoolFull.Load(),
	}}
}

// ingestWatch raises the ingest and pipeline alert events. It reads only
// in-memory counters and spool stats; the database is read once at boot
// (the incidents left open) and once per run for a pipeline without data
// in this process (whether earlier runs received any).
type ingestWatch struct {
	r      *Runner
	bootAt time.Time
	loaded bool

	alerts  map[string]*watchAlert
	samples map[string][]sample
	// receivedBefore caches, per run id, whether earlier runs wrote rows.
	receivedBefore map[string]bool
}

func newIngestWatch(r *Runner) *ingestWatch {
	return &ingestWatch{r: r, alerts: map[string]*watchAlert{}, samples: map[string][]sample{}, receivedBefore: map[string]bool{}}
}

// startAlerts runs the watcher until Stop.
func (r *Runner) startAlerts() {
	r.mu.Lock()
	if r.alertsStop != nil || r.stopping {
		r.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	r.alertsStop = stop
	r.mu.Unlock()
	r.alerts.bootAt = time.Now()
	go func() {
		defer safe.Recover("pipeline-alerts")
		t := time.NewTicker(alertsInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				r.alerts.tick(now)
			}
		}
	}()
}

// watchedRuns returns the running pipelines, or nil while the runner stops (a
// shutdown must not resolve every incident).
func (r *Runner) watchedRuns() ([]*RunningPipeline, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopping {
		return nil, false
	}
	out := make([]*RunningPipeline, 0, len(r.pipelines))
	for _, rp := range r.pipelines {
		// A run still starting has no RunID and no health yet.
		if rp.watchReady.Load() && !rp.userStopped.Load() && !rp.shutdown.Load() {
			out = append(out, rp)
		}
	}
	return out, true
}

// tick evaluates every alert once.
func (w *ingestWatch) tick(now time.Time) {
	if !w.loaded {
		w.loadOpen()
	}
	running, ok := w.r.watchedRuns()
	if !ok {
		return
	}
	if len(running) == 0 && len(w.alerts) == 0 {
		return
	}
	hub := w.r.Hub()
	seen := map[string]bool{}
	pushRunning := false
	for _, rp := range running {
		seen[rp.PipelineID] = true
		w.step(rp.PipelineID, rp.watch, kindSinkFailing, alerts.EventTypePipelineSinkFailing, w.sinkReading(rp), 1, now)
		if !isPushSource(rp.watch.sourceType) || !hub.Available() {
			continue
		}
		pushRunning = true
		st := hub.Stats(rp.PipelineID)
		w.step(rp.PipelineID, rp.watch, kindStalled, alerts.EventTypeIngestStalled, w.stallReading(rp, st, now), 1, now)
		sp := hub.SpoolStats(rp.PipelineID)
		w.step(rp.PipelineID, rp.watch, kindSpoolBacklog, alerts.EventTypeIngestSpoolBacklog, backlogReading(rp, sp.Bytes, sp.OldestAge), resolveAfter, now)
		rej, auth := w.rateReadings(rp, st, now)
		w.step(rp.PipelineID, rp.watch, kindRejecting, alerts.EventTypeIngestRejecting, rej, resolveAfter, now)
		w.step(rp.PipelineID, rp.watch, kindAuthFailures, alerts.EventTypeIngestAuthFailures, auth, resolveAfter, now)
	}
	if _, open := w.alerts[alerts.SpoolAlertKey]; pushRunning || open {
		if capBytes := w.spoolCap(); capBytes > 0 && hub.Available() {
			w.step("", runWatch{}, "", alerts.EventTypeIngestSpoolBacklog, spoolReading(hub.SpoolTotal().Bytes, capBytes), resolveAfter, now)
		}
	}
	// Incidents of pipelines that stopped, crashed or were deleted resolve.
	for key, a := range w.alerts {
		if a.pipelineID == "" || seen[a.pipelineID] {
			continue
		}
		if !w.stoppedFor(a, now) {
			continue
		}
		w.resolve(a, "Pipeline "+a.name+" is no longer running.", now)
		delete(w.alerts, key)
	}
	for id := range w.samples {
		if !seen[id] {
			delete(w.samples, id)
		}
	}
	for runID := range w.receivedBefore {
		found := false
		for _, rp := range running {
			if rp.RunID == runID {
				found = true
				break
			}
		}
		if !found {
			delete(w.receivedBefore, runID)
		}
	}
}

// loadOpen picks up the incidents a previous process left open, so a
// restart neither opens them again nor floods resolves.
func (w *ingestWatch) loadOpen() {
	open, err := alerts.OpenIncidents(w.r.db, alerts.PipelineEventTypes, openIncidentScan)
	if err != nil {
		slog.Warn("Pipeline alerts: failed to load open incidents, retrying", "error", err)
		return
	}
	w.loaded = true
	for _, inc := range open {
		str := func(k string) string { s, _ := inc.Payload[k].(string); return s }
		a := &watchAlert{
			key: inc.AlertKey, eventType: inc.Event.EventType, pipelineID: str("pipeline_id"), name: str("pipeline_name"),
			firing: true, severity: inc.Severity, incident: inc.IncidentID,
		}
		if inc.Event.SourceRef != nil {
			a.sourceRef = *inc.Event.SourceRef
		}
		if inc.Event.ConnectionID != nil {
			a.connectionID = *inc.Event.ConnectionID
		}
		if t, err := time.Parse(time.RFC3339, inc.IncidentID); err == nil {
			a.firedAt = t
		}
		if a.name == "" {
			a.name = a.pipelineID
		}
		w.alerts[a.key] = a
	}
}

// stoppedFor reports whether an open incident's pipeline is gone for good:
// not running here, and not about to resume after a boot.
func (w *ingestWatch) stoppedFor(a *watchAlert, now time.Time) bool {
	if now.Sub(w.bootAt) >= resumeGrace {
		return true
	}
	p, err := w.r.db.GetPipelineByID(a.pipelineID)
	if err != nil {
		return false
	}
	return p == nil || p.Status != "running"
}

func (w *ingestWatch) spoolCap() int64 {
	if w.r.cfg == nil {
		return 0
	}
	return w.r.cfg.IngestSpoolBytes()
}

// step applies one reading to one alert and writes the event it calls for.
// pipelineID "" is the instance-wide spool alert.
func (w *ingestWatch) step(pipelineID string, rw runWatch, kind, eventType string, rd reading, resolveN int, now time.Time) {
	key := alerts.SpoolAlertKey
	if pipelineID != "" {
		key = alerts.PipelineAlertKey(pipelineID, kind)
	}
	a := w.alerts[key]
	switch {
	case rd.sev != "":
		if a == nil {
			a = &watchAlert{key: key, eventType: eventType, pipelineID: pipelineID}
			w.alerts[key] = a
		}
		a.okCount = 0
		a.name, a.connectionID = rw.name, rw.connectionID
		if a.name == "" {
			a.name = pipelineID
		}
		a.sourceRef = alerts.SpoolSourceRef
		if pipelineID != "" {
			a.sourceRef = alerts.PipelineSourceRef(pipelineID)
		}
		switch {
		case !a.firing:
			a.firing, a.severity, a.incident, a.firedAt = true, rd.sev, now.UTC().Format(time.RFC3339), now
			w.emit(a, rd.sev, rd.title, rd.message, rd.facts, "firing", false)
		case alerts.SeverityRank(rd.sev) > alerts.SeverityRank(a.severity):
			a.severity = rd.sev
			w.emit(a, rd.sev, rd.title, rd.message, rd.facts, "firing", true)
		}
	case a == nil || !a.firing:
		delete(w.alerts, key)
	case rd.clear:
		a.okCount++
		if a.okCount >= resolveN {
			w.resolve(a, rd.resolved, now)
			delete(w.alerts, key)
		}
	default:
		a.okCount = 0
	}
}

// resolve writes the resolved event of an open incident.
func (w *ingestWatch) resolve(a *watchAlert, message string, now time.Time) {
	facts := map[string]interface{}{}
	if !a.firedAt.IsZero() {
		facts["duration_seconds"] = int64(now.Sub(a.firedAt).Seconds())
	}
	w.emit(a, a.severity, "[RESOLVED] "+alertTitle(a.eventType, a.name), message, facts, "resolved", false)
}

// emit writes one event. Failures are logged; the state moves on, so a
// broken database cannot make the watcher repeat an event every tick.
func (w *ingestWatch) emit(a *watchAlert, sev, title, message string, facts map[string]interface{}, status string, escalated bool) {
	payload := map[string]interface{}{
		"alert_key": a.key, "incident_id": a.incident, "status": status, "incident_severity": a.severity,
		"since": a.incident,
	}
	if a.pipelineID != "" {
		payload["pipeline_id"], payload["pipeline_name"] = a.pipelineID, a.name
		payload["link_path"] = alerts.PipelineLinkPath(a.pipelineID)
	} else {
		payload["link_path"] = "/pipelines"
	}
	for k, v := range facts {
		payload[k] = v
	}
	fp := a.key + ":firing:" + a.incident
	switch {
	case status == "resolved":
		fp = a.key + ":resolved:" + a.incident
	case escalated:
		payload["escalated"] = true
		fp = a.key + ":escalated:" + sev + ":" + a.incident
	}
	var conn *string
	if a.connectionID != "" {
		c := a.connectionID
		conn = &c
	}
	if _, err := w.r.db.CreateAlertEvent(conn, a.eventType, sev, title, message, payload, fp, a.sourceRef); err != nil {
		slog.Warn("Pipeline alerts: failed to create event", "alert", a.key, "error", err)
	}
}

// alertTitle is the title of an alert's events.
func alertTitle(eventType, name string) string {
	switch eventType {
	case alerts.EventTypeIngestStalled:
		return "Ingest stalled: " + name
	case alerts.EventTypeIngestSpoolBacklog:
		if name == "" {
			return "Ingest spool almost full"
		}
		return "Ingest spool backlog: " + name
	case alerts.EventTypeIngestRejecting:
		return "Ingest rejecting requests: " + name
	case alerts.EventTypeIngestAuthFailures:
		return "Ingest auth failures: " + name
	case alerts.EventTypePipelineSinkFailing:
		return "Pipeline sink failing: " + name
	}
	return eventType + ": " + name
}

// sinkReading is error while the pipeline's writes keep failing; it clears
// once a write succeeded after the incident began.
func (w *ingestWatch) sinkReading(rp *RunningPipeline) reading {
	failing, msg, lastOK := rp.writeHealth.State()
	if failing {
		return reading{sev: alerts.SeverityError, title: alertTitle(alerts.EventTypePipelineSinkFailing, rp.watch.name),
			message: fmt.Sprintf("Pipeline %s cannot write to ClickHouse and keeps retrying: %s. The source is paused until a write succeeds.", rp.watch.name, msg),
			facts:   map[string]interface{}{"error": msg}}
	}
	a := w.alerts[alerts.PipelineAlertKey(rp.PipelineID, kindSinkFailing)]
	if a != nil && !lastOK.After(a.firedAt) {
		return reading{}
	}
	return reading{clear: true, resolved: fmt.Sprintf("Pipeline %s writes to ClickHouse again.", rp.watch.name)}
}

// stallReading is a warning when a push pipeline that received data before
// got nothing for stall_after_minutes. The quiet time counts from the
// last request, or from the run's start when there was none in this
// process. It clears only when data arrives (or stall alerts are turned
// off), so a restart keeps a stall open until the shippers are back.
func (w *ingestWatch) stallReading(rp *RunningPipeline, st *IngestStats, now time.Time) reading {
	name := rp.watch.name
	if rp.watch.stallAfter <= 0 {
		return reading{clear: true, resolved: fmt.Sprintf("Stall alerts were turned off for pipeline %s.", name)}
	}
	last := st.LastAccepted()
	a := w.alerts[alerts.PipelineAlertKey(rp.PipelineID, kindStalled)]
	if a != nil && a.firing && last.After(a.firedAt) {
		return reading{clear: true, resolved: fmt.Sprintf("Data is arriving again on pipeline %s (last request %s).", name, formatWhen(last))}
	}
	baseline := rp.StartedAt
	if last.After(baseline) {
		baseline = last
	}
	quiet := now.Sub(baseline)
	if quiet < rp.watch.stallAfter {
		if a != nil && a.firing {
			return reading{}
		}
		return reading{clear: true}
	}
	// An open stall proves earlier data, as do rows of earlier runs.
	if last.IsZero() && (a == nil || !a.firing) && !w.hasReceivedBefore(rp) {
		return reading{clear: true}
	}
	since := "since the pipeline started at " + formatWhen(rp.StartedAt)
	if !last.IsZero() {
		since = "since " + formatWhen(last)
	}
	return reading{
		sev: alerts.SeverityWarn, title: alertTitle(alerts.EventTypeIngestStalled, name),
		message: fmt.Sprintf("Pipeline %s (%s) has received no data for %s, %s. It fires after %s of silence; check the shippers that push to it.",
			name, rp.watch.signal, formatDuration(quiet), since, formatDuration(rp.watch.stallAfter)),
		facts: map[string]interface{}{"quiet_seconds": int64(quiet.Seconds()), "stall_after_minutes": int64(rp.watch.stallAfter.Minutes()), "last_data_at": formatRFC(last)},
	}
}

// hasReceivedBefore reports whether an earlier run of the pipeline wrote
// rows, read once per run.
func (w *ingestWatch) hasReceivedBefore(rp *RunningPipeline) bool {
	if v, ok := w.receivedBefore[rp.RunID]; ok {
		return v
	}
	runs, err := w.r.db.GetPipelineRuns(rp.PipelineID, 10, 0)
	if err != nil {
		return false
	}
	v := false
	for _, run := range runs {
		if run.ID != rp.RunID && run.RowsIngested > 0 {
			v = true
			break
		}
	}
	w.receivedBefore[rp.RunID] = v
	return v
}

// backlogReading is a warning when a pipeline's oldest spooled data is
// older than backlogAfter: its sink is not keeping up or failing.
func backlogReading(rp *RunningPipeline, bytes int64, oldest time.Duration) reading {
	if oldest < backlogAfter {
		return reading{clear: true, resolved: fmt.Sprintf("The spool of pipeline %s has drained (%s pending).", rp.watch.name, formatBytes(bytes))}
	}
	return reading{
		sev: alerts.SeverityWarn, title: alertTitle(alerts.EventTypeIngestSpoolBacklog, rp.watch.name),
		message: fmt.Sprintf("Pipeline %s has %s in its spool waiting to be written; the oldest data is %s old (alert at %s). Its ClickHouse sink is not keeping up or is failing.",
			rp.watch.name, formatBytes(bytes), formatDuration(oldest), formatDuration(backlogAfter)),
		facts: map[string]interface{}{"spool_bytes": bytes, "oldest_age_seconds": int64(oldest.Seconds()), "threshold_seconds": int64(backlogAfter.Seconds())},
	}
}

// spoolReading is a warning above spoolWarnRatio of the cap and critical
// above spoolCritRatio.
func spoolReading(bytes, capBytes int64) reading {
	ratio := float64(bytes) / float64(capBytes)
	pct := int(ratio * 100)
	facts := map[string]interface{}{"spool_bytes": bytes, "spool_max_bytes": capBytes, "percent": pct}
	if ratio < spoolWarnRatio {
		return reading{clear: true, resolved: fmt.Sprintf("The ingest spool is back below %d%% of its cap (%s of %s).", int(spoolWarnRatio*100), formatBytes(bytes), formatBytes(capBytes)), facts: facts}
	}
	sev := alerts.SeverityWarn
	if ratio >= spoolCritRatio {
		sev = alerts.SeverityCritical
	}
	return reading{
		sev: sev, title: fmt.Sprintf("Ingest spool %d%% full", pct),
		message: fmt.Sprintf("The ingest spool holds %s of %s (%d%%). When it is full the receivers answer 503 and shippers have to retry; check the pipelines whose sinks are behind.",
			formatBytes(bytes), formatBytes(capBytes), pct),
		facts: facts,
	}
}

// rateReadings compute the rejection and auth failure rates over the last
// rejectWindow from the samples taken each tick. Without two samples there
// is no rate yet and both readings hold.
func (w *ingestWatch) rateReadings(rp *RunningPipeline, st *IngestStats, now time.Time) (reading, reading) {
	cur := takeSample(st, now)
	list := append(w.samples[rp.PipelineID], cur)
	// Keep the newest sample at or before the window start as the base.
	cut := 0
	for i := range list {
		if !list[i].at.After(now.Add(-rejectWindow)) {
			cut = i
		}
	}
	list = list[cut:]
	w.samples[rp.PipelineID] = list
	if len(list) < 2 {
		return reading{}, reading{}
	}
	base := list[0]
	span := now.Sub(base.at)
	name := rp.watch.name
	var rejected int64
	var parts []string
	for i, label := range rejectLabels {
		if n := cur.rejected[i] - base.rejected[i]; n > 0 {
			rejected += n
			parts = append(parts, fmt.Sprintf("%s %d", label, n))
		}
	}
	accepted, auth := cur.accepted-base.accepted, cur.auth-base.auth

	rej := reading{clear: true, resolved: fmt.Sprintf("Pipeline %s accepts requests again.", name)}
	if total := accepted + rejected; total >= rejectMinRequests {
		ratio := float64(rejected) / float64(total)
		if ratio > rejectRatio {
			sev := alerts.SeverityWarn
			if ratio >= rejectErrorRatio {
				sev = alerts.SeverityError
			}
			rej = reading{
				sev: sev, title: alertTitle(alerts.EventTypeIngestRejecting, name),
				message: fmt.Sprintf("Pipeline %s rejected %d%% of requests in the last %s (%d of %d): %s.",
					name, int(ratio*100), formatDuration(span), rejected, total, strings.Join(parts, ", ")),
				facts: map[string]interface{}{"rejected": rejected, "requests": total, "percent": int(ratio * 100), "window_seconds": int64(span.Seconds())},
			}
		}
	}
	authR := reading{clear: true, resolved: fmt.Sprintf("Requests with a wrong ingest key on pipeline %s are back to a low rate.", name)}
	if auth >= authFailuresPerWindow {
		authR = reading{
			sev: alerts.SeverityInfo, title: alertTitle(alerts.EventTypeIngestAuthFailures, name),
			message: fmt.Sprintf("Pipeline %s refused %d requests with a missing or wrong key in the last %s. A shipper may use a revoked key, or something is probing the endpoint.",
				name, auth, formatDuration(span)),
			facts: map[string]interface{}{"auth_failures": auth, "window_seconds": int64(span.Seconds())},
		}
	}
	return rej, authR
}

func formatWhen(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func formatRFC(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// formatDuration renders minutes, or seconds under a minute.
func formatDuration(d time.Duration) string {
	switch m := int(d.Minutes()); {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case m == 1:
		return "1 minute"
	case m < 120:
		return fmt.Sprintf("%d minutes", m)
	default:
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
