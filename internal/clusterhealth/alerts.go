// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// Built-in health checks, evaluated on every harvester poll from the samples
// the poll already collected. Each (connection, check, node) runs its own
// alerts.Step state machine.
const (
	CheckReplicationLag  = "replication_lag"
	CheckReplicaReadonly = "replica_readonly"
	CheckDisk            = "disk"
	CheckParts           = "parts"
	CheckMutationStuck   = "mutation_stuck"
	CheckNodeUnreachable = "node_unreachable"
	// CheckConnectionUnreachable is not read from samples: the harvester
	// probes the connection itself (tunnel, then SELECT 1) and keeps its
	// state on the node ConnectionNode.
	CheckConnectionUnreachable = "connection_unreachable"
)

// ConnectionNode is the node of the connection-level check's state.
const ConnectionNode = "*"

const (
	// maxHealthEventsPerPoll caps the events one poll of one connection may
	// raise; the rest are folded into one summary event.
	maxHealthEventsPerPoll = 20
	// historySampleInterval is how often a steady state is still recorded.
	historySampleInterval = time.Hour
	// detailRows bounds the top offenders attached to a firing event.
	detailRows = 5
	// ghostExpiry is how long a node may be absent from every poll before
	// its incidents are closed, when the cluster's node list is not known.
	ghostExpiry = 24 * time.Hour
)

// HealthCheck describes one built-in check.
type HealthCheck struct {
	Kind         string
	Label        string
	EventType    string
	Unit         string // seconds, count, percent or down
	SupportsWarn bool
	DefaultWarn  *float64
	DefaultCrit  *float64
	// DefaultFor and DefaultResolve are the default for and resolve
	// evaluations; 0 means 2.
	DefaultFor     int
	DefaultResolve int
}

func f64(v float64) *float64 { return &v }

// HealthChecks lists the built-in checks in display order.
var HealthChecks = []HealthCheck{
	{Kind: CheckReplicationLag, Label: "Replication lag", EventType: alerts.EventTypeHealthReplicationLag, Unit: "seconds", SupportsWarn: true, DefaultWarn: f64(60), DefaultCrit: f64(300)},
	{Kind: CheckReplicaReadonly, Label: "Read-only replicas", EventType: alerts.EventTypeHealthReplicaReadonly, Unit: "count", DefaultCrit: f64(0)},
	{Kind: CheckDisk, Label: "Disk usage", EventType: alerts.EventTypeHealthDisk, Unit: "percent", SupportsWarn: true, DefaultWarn: f64(80), DefaultCrit: f64(90)},
	{Kind: CheckParts, Label: "Parts pressure", EventType: alerts.EventTypeHealthParts, Unit: "percent", SupportsWarn: true, DefaultWarn: f64(50), DefaultCrit: f64(80)},
	{Kind: CheckMutationStuck, Label: "Stuck mutation", EventType: alerts.EventTypeHealthMutationStuck, Unit: "seconds", SupportsWarn: true, DefaultWarn: f64(1800), DefaultCrit: f64(7200)},
	{Kind: CheckNodeUnreachable, Label: "Node unreachable", EventType: alerts.EventTypeHealthNodeUnreachable, Unit: "down", DefaultCrit: f64(0)},
	{Kind: CheckConnectionUnreachable, Label: "Connection unreachable", EventType: alerts.EventTypeHealthConnectionUnreachable, Unit: "down", DefaultCrit: f64(0), DefaultFor: 3, DefaultResolve: 2},
}

// LookupHealthCheck returns the check of a kind.
func LookupHealthCheck(kind string) (HealthCheck, bool) {
	for _, c := range HealthChecks {
		if c.Kind == kind {
			return c, true
		}
	}
	return HealthCheck{}, false
}

// HealthCheckSetting is the effective configuration of one check on a
// connection: the stored override, or the defaults.
type HealthCheckSetting struct {
	CheckKind          string   `json:"check_kind"`
	Enabled            bool     `json:"enabled"`
	WarnThreshold      *float64 `json:"warn_threshold"`
	CritThreshold      *float64 `json:"crit_threshold"`
	FreeBytesFloor     *float64 `json:"free_bytes_floor"`
	ForEvaluations     int      `json:"for_evaluations"`
	ResolveEvaluations int      `json:"resolve_evaluations"`
	Unit               string   `json:"unit"`
	SupportsWarn       bool     `json:"supports_warn"`
	UpdatedAt          *string  `json:"updated_at"`
}

// DefaultHealthSetting returns a check's built-in configuration.
func DefaultHealthSetting(c HealthCheck) HealthCheckSetting {
	forN, resolveN := c.DefaultFor, c.DefaultResolve
	if forN < 1 {
		forN = 2
	}
	if resolveN < 1 {
		resolveN = 2
	}
	return HealthCheckSetting{
		CheckKind: c.Kind, Enabled: true, WarnThreshold: c.DefaultWarn, CritThreshold: c.DefaultCrit,
		ForEvaluations: forN, ResolveEvaluations: resolveN, Unit: c.Unit, SupportsWarn: c.SupportsWarn,
	}
}

// LoadHealthSettings returns the effective settings of every check.
func LoadHealthSettings(db *database.DB, connectionID string) ([]HealthCheckSetting, error) {
	stored, err := db.ListHealthAlertSettings(connectionID)
	if err != nil {
		return nil, err
	}
	out := make([]HealthCheckSetting, 0, len(HealthChecks))
	for _, c := range HealthChecks {
		s := DefaultHealthSetting(c)
		if row, ok := stored[c.Kind]; ok {
			s.Enabled = row.Enabled
			s.WarnThreshold = row.WarnThreshold
			s.CritThreshold = row.CritThreshold
			s.FreeBytesFloor = row.FreeBytesFloor
			s.ForEvaluations = row.ForEvaluations
			s.ResolveEvaluations = row.ResolveEvaluations
			s.UpdatedAt = row.UpdatedAt
		}
		out = append(out, normalizeSetting(c, s))
	}
	return out, nil
}

// normalizeSetting forces the fields a check does not use to their fixed
// values.
func normalizeSetting(c HealthCheck, s HealthCheckSetting) HealthCheckSetting {
	s.Unit, s.SupportsWarn = c.Unit, c.SupportsWarn
	if !c.SupportsWarn {
		s.WarnThreshold = nil
	}
	if c.Kind == CheckNodeUnreachable || c.Kind == CheckConnectionUnreachable {
		s.CritThreshold = f64(0)
	}
	if c.Kind != CheckDisk {
		s.FreeBytesFloor = nil
	}
	return s
}

// ValidateHealthSetting checks an edited setting and returns it normalized.
func ValidateHealthSetting(s HealthCheckSetting) (HealthCheckSetting, error) {
	c, ok := LookupHealthCheck(s.CheckKind)
	if !ok {
		return s, fmt.Errorf("unknown check %q", s.CheckKind)
	}
	if s.ForEvaluations < 1 || s.ForEvaluations > 20 || s.ResolveEvaluations < 1 || s.ResolveEvaluations > 20 {
		return s, fmt.Errorf("%s: for and resolve evaluations must be between 1 and 20", c.Label)
	}
	for _, v := range []*float64{s.WarnThreshold, s.CritThreshold, s.FreeBytesFloor} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return s, fmt.Errorf("%s: thresholds must be zero or positive numbers", c.Label)
		}
	}
	if c.Unit == "percent" {
		for _, v := range []*float64{s.WarnThreshold, s.CritThreshold} {
			if v != nil && *v > 100 {
				return s, fmt.Errorf("%s: percent thresholds must be at most 100", c.Label)
			}
		}
	}
	s = normalizeSetting(c, s)
	if s.WarnThreshold != nil && s.CritThreshold != nil && *s.WarnThreshold >= *s.CritThreshold {
		return s, fmt.Errorf("%s: the warn threshold must be below the critical threshold", c.Label)
	}
	if s.Enabled && s.WarnThreshold == nil && s.CritThreshold == nil && s.FreeBytesFloor == nil {
		return s, fmt.Errorf("%s: set at least one threshold or disable the check", c.Label)
	}
	return s, nil
}

// checkValue reads a check's value from a sample. ok is false when the
// metric is unknown for the node.
func checkValue(kind string, s Sample) (float64, bool) {
	switch kind {
	case CheckReplicationLag:
		return s.ReplicationMaxDelay, !s.IsUnknown(MetricReplication)
	case CheckReplicaReadonly:
		return float64(s.ReplicasReadonly), !s.IsUnknown(MetricReplication)
	case CheckDisk:
		if s.IsUnknown(MetricDisk) || s.DiskUsedPct == nil {
			return 0, false
		}
		return *s.DiskUsedPct, true
	case CheckParts:
		return s.PartsPressurePct, !s.IsUnknown(MetricParts)
	case CheckMutationStuck:
		return float64(s.MutationsOldestAgeSec), !s.IsUnknown(MetricMutations)
	case CheckNodeUnreachable:
		if s.Down {
			return 1, true
		}
		// A node with nothing known that is not flagged down was not
		// reached by a real fan-out: its state is unknown.
		if len(s.Unknown) > len(storedMetrics) {
			return 0, false
		}
		return 0, true
	}
	return 0, false
}

// breachSeverity returns the severity a value breaches at, "" when within
// thresholds.
func breachSeverity(set HealthCheckSetting, value float64, s Sample) string {
	if set.CritThreshold != nil && value > *set.CritThreshold {
		return alerts.SeverityCritical
	}
	if set.CheckKind == CheckDisk && set.FreeBytesFloor != nil && s.DiskFreeMinBytes != nil && *s.DiskFreeMinBytes < *set.FreeBytesFloor {
		return alerts.SeverityCritical
	}
	if set.WarnThreshold != nil && value > *set.WarnThreshold {
		return alerts.SeverityWarn
	}
	return ""
}

// healthEvent is a transition waiting to become an alert event.
type healthEvent struct {
	check      HealthCheck
	setting    HealthCheckSetting
	node       string
	value      float64
	transition string
	severity   string
	// incident is the start of the incident (its firing time), part of the
	// firing and resolved fingerprints. at is the time of this transition.
	incident, at string
	// announced is true for a resolve whose firing went out as its own
	// event; the resolve of a summarized firing is summarized too.
	announced bool
	// reason is set on a resolve without a recovery: the check was turned
	// off or the node left the cluster.
	reason string
	// detail says why the connection check breached (tunnel offline, the
	// probe's error).
	detail string
	// stateIdx and ghostIdx point at the state the event belongs to, -1
	// for none.
	stateIdx, ghostIdx int

	// Set by raiseHealthEvents: created is true once the event (or the
	// summary holding it) is written; eventID is "" for a summarized one.
	created bool
	eventID string
}

// HealthFingerprint is the dispatcher de-dupe key of a health event. The
// suffix names the transition and the incident, so a new incident or a
// severity change is never taken for a repeat inside a rule's cooldown.
func HealthFingerprint(connectionID, check, node, suffix string) string {
	return HealthAlertKey(connectionID, check, node) + ":" + suffix
}

// HealthAlertKey names one check on one node of a connection: the alert an
// acknowledgement applies to.
func HealthAlertKey(connectionID, check, node string) string {
	return "health:" + connectionID + ":" + check + ":" + node
}

// evaluateHealthAlerts runs every enabled check over one poll's samples,
// advances the state machines and raises events for transitions. Nodes
// absent from the samples keep their state: a node only reachable through
// a failed fan-out is unknown, not recovered. With membersKnown the samples
// list every node of the cluster, and the state of a node no longer in it
// is closed; otherwise that happens after ghostExpiry without data.
func (h *Harvester) evaluateHealthAlerts(connID, cluster string, samples []Sample, membersKnown bool, exec ExecFunc, now time.Time) {
	settings, err := LoadHealthSettings(h.db, connID)
	if err != nil {
		slog.Warn("Health alerts: failed to load settings", "connection", connID, "error", err)
		return
	}
	stored, err := h.db.ListHealthAlertStates(connID, false)
	if err != nil {
		slog.Warn("Health alerts: failed to load state", "connection", connID, "error", err)
		return
	}
	prevByKey := map[string]database.HealthAlertState{}
	for _, st := range stored {
		prevByKey[st.CheckKind+"\x00"+st.Node] = st
	}
	nowStr := now.UTC().Format(time.RFC3339)

	var states []database.HealthAlertState
	// Per state: goes into the history as a change; was not readable.
	var changed, unknown []bool
	var events []healthEvent
	enabled := map[string]HealthCheckSetting{}
	present := map[string]bool{}
	for _, s := range samples {
		present[s.Node] = true
	}
	for _, set := range settings {
		// The connection check is evaluated by evaluateReachability; leaving
		// it out of enabled also keeps its state out of the ghost sweep.
		if !set.Enabled || set.CheckKind == CheckConnectionUnreachable {
			continue
		}
		enabled[set.CheckKind] = set
		check, _ := LookupHealthCheck(set.CheckKind)
		for _, s := range samples {
			prev, hasPrev := prevByKey[check.Kind+"\x00"+s.Node]
			value, known := checkValue(check.Kind, s)
			if !known {
				// Unknown neither breaches nor recovers: the stored state
				// stays as it is, only marked as seen.
				if hasPrev {
					states = append(states, prev)
					changed = append(changed, false)
					unknown = append(unknown, true)
				}
				continue
			}
			hPrev := alerts.Hysteresis{State: prev.State, BreachCount: prev.BreachCount, OkCount: prev.OkCount}
			if prev.Severity != nil {
				hPrev.Severity = *prev.Severity
			}
			next, transition := alerts.Step(hPrev, breachSeverity(set, value, s), false, set.ForEvaluations, set.ResolveEvaluations)
			st := database.HealthAlertState{
				ConnectionID: connID, CheckKind: check.Kind, Node: s.Node, State: next.State,
				BreachCount: next.BreachCount, OkCount: next.OkCount, LastValue: f64(value),
				Since: prev.Since, LastEventID: prev.LastEventID,
			}
			if next.Severity != "" {
				sev := next.Severity
				st.Severity = &sev
			}
			prevState := prev.State
			if prevState == "" {
				prevState = alerts.StateOK
			}
			if next.State != prevState || st.Since == nil {
				st.Since = &nowStr
			}
			states = append(states, st)
			changed = append(changed, next.State != prevState || transition != alerts.TransitionNone)
			unknown = append(unknown, false)
			if transition != alerts.TransitionNone {
				ev := healthEvent{check: check, setting: set, node: s.Node, value: value,
					transition: transition, severity: next.Severity, incident: *st.Since, at: nowStr,
					stateIdx: len(states) - 1, ghostIdx: -1}
				if transition == alerts.TransitionResolved {
					ev.incident = incidentOf(prev, nowStr)
					ev.announced = prev.LastEventID != nil
				}
				events = append(events, ev)
			}
			if transition == alerts.TransitionResolved {
				states[len(states)-1].Severity = nil
			}
		}
	}

	// Ghosts: states of nodes that are no longer polled.
	var ghosts []database.HealthAlertState
	for _, st := range stored {
		set, ok := enabled[st.CheckKind]
		if !ok || present[st.Node] {
			continue
		}
		reason := ""
		if membersKnown {
			reason = "the node is no longer part of the cluster"
		} else if seen, err := time.Parse(time.RFC3339, deref(st.UpdatedAt)); err == nil && now.Sub(seen) >= ghostExpiry {
			reason = "the node sent no data for 24 hours"
		}
		if reason == "" {
			continue
		}
		ghosts = append(ghosts, st)
		if st.State == alerts.StateFiring {
			check, _ := LookupHealthCheck(st.CheckKind)
			events = append(events, closingEvent(check, set, st, reason, nowStr, -1, len(ghosts)-1))
		}
	}

	// A check turned off while this poll ran keeps its events and states
	// out: the settings change closed its incidents.
	if current, err := h.db.ListHealthAlertSettings(connID); err == nil {
		kept := events[:0]
		for _, ev := range events {
			if row, ok := current[ev.check.Kind]; ok && !row.Enabled {
				continue
			}
			kept = append(kept, ev)
		}
		events = kept
	}

	raiseHealthEvents(h.db, connID, cluster, events, exec, now)

	skip := map[int]bool{}
	keepGhost := map[int]bool{}
	for _, ev := range events {
		if !ev.created {
			// Not written: the state does not advance, so the next poll
			// raises it again.
			if ev.stateIdx >= 0 {
				skip[ev.stateIdx] = true
			}
			if ev.ghostIdx >= 0 {
				keepGhost[ev.ghostIdx] = true
			}
			continue
		}
		if ev.stateIdx < 0 {
			continue
		}
		st := &states[ev.stateIdx]
		switch ev.transition {
		case alerts.TransitionFiring:
			// A summarized firing leaves no event id, so its resolve is
			// summarized too.
			st.LastEventID = nil
			if ev.eventID != "" {
				id := ev.eventID
				st.LastEventID = &id
			}
		case alerts.TransitionEscalated, alerts.TransitionDeescalated:
			if ev.eventID != "" {
				id := ev.eventID
				st.LastEventID = &id
			}
		}
	}

	save := make([]database.HealthAlertState, 0, len(states))
	var history []database.AlertEvaluation
	for i, st := range states {
		if skip[i] {
			continue
		}
		save = append(save, st)
		if unknown[i] {
			continue
		}
		if h.shouldRecord(connID, st.CheckKind, st.Node, changed[i], now) {
			history = append(history, database.AlertEvaluation{
				Source: "health", SourceID: st.CheckKind, ConnectionID: connID, Node: st.Node,
				Value: st.LastValue, State: st.State, EvaluatedAt: nowStr,
			})
		}
	}
	if err := h.db.SaveHealthAlertStates(save); err != nil {
		slog.Warn("Health alerts: failed to save state", "connection", connID, "error", err)
	}
	for i, st := range ghosts {
		if keepGhost[i] {
			continue
		}
		if err := h.db.DeleteHealthAlertState(connID, st.CheckKind, st.Node); err != nil {
			slog.Warn("Health alerts: failed to clear state of a departed node", "connection", connID, "node", st.Node, "error", err)
			continue
		}
		h.forgetHistory(connID, st.CheckKind, st.Node)
	}
	if err := h.db.InsertAlertEvaluations(history); err != nil {
		slog.Warn("Health alerts: failed to record evaluations", "connection", connID, "error", err)
	}
}

// incidentOf returns the start of the incident a state holds.
func incidentOf(st database.HealthAlertState, fallback string) string {
	if st.Since != nil && *st.Since != "" {
		return *st.Since
	}
	return fallback
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// closingEvent is the resolve of a firing state that ends without a
// recovery.
func closingEvent(check HealthCheck, set HealthCheckSetting, st database.HealthAlertState, reason, nowStr string, stateIdx, ghostIdx int) healthEvent {
	ev := healthEvent{check: check, setting: set, node: st.Node, transition: alerts.TransitionResolved,
		incident: incidentOf(st, nowStr), at: nowStr, announced: st.LastEventID != nil, reason: reason,
		stateIdx: stateIdx, ghostIdx: ghostIdx}
	if st.Severity != nil {
		ev.severity = *st.Severity
	}
	if st.LastValue != nil {
		ev.value = *st.LastValue
	}
	return ev
}

// CloseCheckIncidents ends the incidents of a check that was turned off on
// a connection: every firing node gets a resolved event (summarized like a
// poll's), then the check's state is cleared.
func CloseCheckIncidents(db *database.DB, connID, checkKind string, now time.Time) error {
	if _, ok := LookupHealthCheck(checkKind); !ok {
		return fmt.Errorf("unknown check %q", checkKind)
	}
	return closeIncidents(db, connID, func(kind string, _ HealthCheckSetting) string {
		if kind == checkKind {
			return "the check was turned off"
		}
		return ""
	}, now)
}

// CloseOrphanedIncidents ends the incidents nothing evaluates any more on a
// connection: those of a check that is turned off, and, with Cluster Health
// sampling off, those of every check that reads samples. The connection
// check runs on probes without sampling and keeps its state. It is cheap
// when there is no state, so the harvester calls it on every tick.
func CloseOrphanedIncidents(db *database.DB, connID string, samplingOn bool, now time.Time) error {
	return closeIncidents(db, connID, func(kind string, set HealthCheckSetting) string {
		switch {
		case !set.Enabled:
			return "the check was turned off"
		case !samplingOn && kind != CheckConnectionUnreachable:
			return "Cluster Health sampling was turned off"
		}
		return ""
	}, now)
}

// closeIncidents clears the state of the checks reason selects (a non-empty
// reason), raising the resolved event of every firing node first; the
// resolves clear the acks of those incidents when the dispatcher picks them
// up. A firing node whose event could not be written keeps its state, so a
// later call tries again.
func closeIncidents(db *database.DB, connID string, reason func(kind string, set HealthCheckSetting) string, now time.Time) error {
	states, err := db.ListHealthAlertStates(connID, false)
	if err != nil || len(states) == 0 {
		return err
	}
	settings := map[string]HealthCheckSetting{}
	all, err := LoadHealthSettings(db, connID)
	if err != nil {
		return err
	}
	for _, s := range all {
		settings[s.CheckKind] = s
	}
	nowStr := now.UTC().Format(time.RFC3339)
	var events []healthEvent
	var closed []database.HealthAlertState
	for _, st := range states {
		check, ok := LookupHealthCheck(st.CheckKind)
		if !ok {
			continue
		}
		set, ok := settings[st.CheckKind]
		if !ok {
			set = DefaultHealthSetting(check)
		}
		why := reason(st.CheckKind, set)
		if why == "" {
			continue
		}
		closed = append(closed, st)
		if st.State == alerts.StateFiring {
			events = append(events, closingEvent(check, set, st, why, nowStr, len(closed)-1, -1))
		}
	}
	raiseHealthEvents(db, connID, "", events, nil, now)
	keep := map[int]bool{}
	for _, ev := range events {
		if !ev.created {
			keep[ev.stateIdx] = true
			slog.Warn("Health alerts: failed to resolve a closed incident", "connection", connID, "check", ev.check.Kind, "node", ev.node)
		}
	}
	for i, st := range closed {
		if keep[i] {
			continue
		}
		if err := db.DeleteHealthAlertState(connID, st.CheckKind, st.Node); err != nil {
			return err
		}
	}
	return nil
}

// shouldRecord reports whether an evaluation goes into the history: every
// state change, plus one sample per hour of a steady state.
func (h *Harvester) shouldRecord(connID, check, node string, changed bool, now time.Time) bool {
	key := connID + "\x00" + check + "\x00" + node
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastHistory == nil {
		h.lastHistory = map[string]time.Time{}
	}
	if last, ok := h.lastHistory[key]; ok && !changed && now.Sub(last) < historySampleInterval {
		return false
	}
	h.lastHistory[key] = now
	return true
}

// forgetHistory drops the history timestamp of a state that was cleared.
func (h *Harvester) forgetHistory(connID, check, node string) {
	h.mu.Lock()
	delete(h.lastHistory, connID+"\x00"+check+"\x00"+node)
	h.mu.Unlock()
}

// raiseHealthEvents writes the transitions as alert events, at most
// maxHealthEventsPerPoll of them; the rest go into one summary event, and
// so does the resolve of an incident whose firing was summarized. It sorts
// events and sets created and eventID on each. exec may be nil when there
// are only resolves.
func raiseHealthEvents(db *database.DB, connID, cluster string, events []healthEvent, exec ExecFunc, now time.Time) {
	if len(events) == 0 {
		return
	}
	// Escalations and firings first, worst first, so the cap drops the
	// least important ones.
	sort.SliceStable(events, func(i, j int) bool {
		ri, rj := events[i].transition == alerts.TransitionResolved, events[j].transition == alerts.TransitionResolved
		if ri != rj {
			return !ri
		}
		return alerts.SeverityRank(events[i].severity) > alerts.SeverityRank(events[j].severity)
	})
	details := map[string][]map[string]interface{}{}
	conn := connID
	emitted := 0
	var rest []int
	for i := range events {
		ev := &events[i]
		if emitted >= maxHealthEventsPerPoll || (ev.transition == alerts.TransitionResolved && !ev.announced) {
			rest = append(rest, i)
			continue
		}
		emitted++
		title, message, payload, fp := healthEventContent(connID, cluster, *ev)
		if ev.transition != alerts.TransitionResolved && exec != nil {
			rows, ok := details[ev.check.Kind]
			if !ok {
				rows = detailQuery(exec, ev.check.Kind, cluster)
				details[ev.check.Kind] = rows
			}
			if top := offendersFor(rows, ev.node); len(top) > 0 {
				payload["top_offenders"] = top
			}
		}
		id, err := db.CreateAlertEvent(&conn, ev.check.EventType, ev.severity, title, message, payload, fp, "health:"+ev.check.Kind)
		if err != nil {
			slog.Warn("Health alerts: failed to create event", "connection", connID, "check", ev.check.Kind, "error", err)
			continue
		}
		ev.created, ev.eventID = true, id
	}
	if len(rest) == 0 {
		return
	}
	sev := alerts.SeverityWarn
	counts := map[string]int{}
	resolved := 0
	for _, i := range rest {
		ev := events[i]
		if alerts.SeverityRank(ev.severity) > alerts.SeverityRank(sev) {
			sev = ev.severity
		}
		counts[ev.check.Label]++
		if ev.transition == alerts.TransitionResolved {
			resolved++
		}
	}
	parts := make([]string, 0, len(counts))
	for label, n := range counts {
		parts = append(parts, fmt.Sprintf("%s: %d", label, n))
	}
	sort.Strings(parts)
	title := fmt.Sprintf("%d more health check changes", len(rest))
	message := fmt.Sprintf("%d further health check transitions on this connection were not sent one by one (%s; %d of them resolved). Open Governance, Health checks for the full state.",
		len(rest), strings.Join(parts, ", "), resolved)
	payload := map[string]interface{}{"connection_id": connID, "cluster": cluster, "suppressed": len(rest), "resolved": resolved, "by_check": counts}
	fp := "health:" + connID + ":summary:" + strconv.FormatInt(now.Unix(), 10)
	if _, err := db.CreateAlertEvent(&conn, alerts.EventTypeHealthSummary, sev, title, message, payload, fp, "health:summary"); err != nil {
		slog.Warn("Health alerts: failed to create summary event", "connection", connID, "error", err)
		return
	}
	for _, i := range rest {
		events[i].created = true
	}
}

// healthEventContent builds the title, message, payload and fingerprint of
// one transition.
func healthEventContent(connID, cluster string, ev healthEvent) (string, string, map[string]interface{}, string) {
	value := formatCheckValue(ev.check, ev.value)
	payload := map[string]interface{}{
		"connection_id": connID, "cluster": cluster, "check": ev.check.Kind, "node": ev.node,
		"value": ev.value, "severity": ev.severity,
		"warn_threshold": ev.setting.WarnThreshold, "crit_threshold": ev.setting.CritThreshold,
		"alert_key": HealthAlertKey(connID, ev.check.Kind, ev.node), "incident_id": ev.incident,
	}
	if ev.detail != "" {
		payload["error"] = ev.detail
	}
	if ev.setting.FreeBytesFloor != nil {
		payload["free_bytes_floor"] = *ev.setting.FreeBytesFloor
	}
	switch ev.transition {
	case alerts.TransitionResolved:
		payload["status"] = "resolved"
		title := fmt.Sprintf("[RESOLVED] %s on %s", ev.check.Label, ev.node)
		message := fmt.Sprintf("%s on node %s is back within thresholds (now %s).", ev.check.Label, ev.node, value)
		if ev.check.Kind == CheckConnectionUnreachable {
			title = "[RESOLVED] Connection unreachable"
			message = "The connection answers again."
		}
		if ev.reason != "" {
			payload["reason"] = ev.reason
			message = fmt.Sprintf("%s on node %s is no longer tracked: the incident was closed because %s.", ev.check.Label, ev.node, ev.reason)
			if ev.check.Kind == CheckConnectionUnreachable {
				message = fmt.Sprintf("Connection unreachable is no longer tracked: the incident was closed because %s.", ev.reason)
			}
		}
		return title, message, payload, HealthFingerprint(connID, ev.check.Kind, ev.node, "resolved:"+ev.incident)
	default:
		payload["status"] = "firing"
		suffix := ev.severity + ":" + ev.incident
		switch ev.transition {
		case alerts.TransitionEscalated:
			payload["escalated"] = true
			suffix = "escalated:" + ev.severity + ":" + ev.at
		case alerts.TransitionDeescalated:
			payload["deescalated"] = true
			suffix = "deescalated:" + ev.severity + ":" + ev.at
		}
		var title, message string
		if ev.check.Kind == CheckConnectionUnreachable {
			title = "Connection unreachable"
			message = "CH-UI cannot reach this connection."
			if ev.detail != "" {
				message = fmt.Sprintf("CH-UI cannot reach this connection: %s.", ev.detail)
			}
		} else if ev.check.Kind == CheckNodeUnreachable {
			title = fmt.Sprintf("Node %s unreachable", ev.node)
			message = fmt.Sprintf("Node %s is listed in system.clusters but did not answer the cluster fan-out.", ev.node)
		} else {
			threshold := ev.setting.CritThreshold
			if ev.severity == alerts.SeverityWarn {
				threshold = ev.setting.WarnThreshold
			}
			limit := "its threshold"
			if threshold != nil {
				limit = formatCheckValue(ev.check, *threshold)
			}
			title = fmt.Sprintf("%s on %s: %s", ev.check.Label, ev.node, value)
			message = fmt.Sprintf("%s on node %s is %s, above %s (%s).", ev.check.Label, ev.node, value, limit, ev.severity)
			if ev.transition == alerts.TransitionDeescalated {
				message = fmt.Sprintf("%s on node %s dropped to %s, still above %s (now %s).", ev.check.Label, ev.node, value, limit, ev.severity)
			}
		}
		return title, message, payload, HealthFingerprint(connID, ev.check.Kind, ev.node, suffix)
	}
}

func formatCheckValue(c HealthCheck, v float64) string {
	switch c.Unit {
	case "seconds":
		return (time.Duration(v) * time.Second).String()
	case "percent":
		return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) + "%"
	case "down":
		if v > 0 {
			return "down"
		}
		return "up"
	default:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
}

var detailLimitRE = regexp.MustCompile(`(?s)LIMIT \d+ FORMAT JSON\s*$`)

// detailQuery runs the bounded drill-down query of a check, nil when the
// check has none or it fails.
func detailQuery(exec ExecFunc, check, cluster string) []map[string]interface{} {
	var q string
	switch check {
	case CheckReplicationLag, CheckReplicaReadonly:
		q = ReplicationQuery(cluster)
	case CheckMutationStuck:
		q = MutationsQuery(cluster)
	case CheckParts:
		q = PartsPressureQuery(cluster)
	case CheckDisk:
		q = strings.Replace(DisksQuery(cluster), "FORMAT JSON", "LIMIT 300 FORMAT JSON", 1)
	default:
		return nil
	}
	q = detailLimitRE.ReplaceAllString(q, fmt.Sprintf("LIMIT %d FORMAT JSON", detailRows))
	rows, err := exec(q)
	if err != nil {
		slog.Debug("Health alerts: detail query failed", "check", check, "error", err)
		return nil
	}
	return rows
}

// offendersFor keeps the detail rows of one node, or all rows when none of
// them name it.
func offendersFor(rows []map[string]interface{}, node string) []map[string]interface{} {
	var own []map[string]interface{}
	for _, r := range rows {
		if asString(r["node"]) == node {
			own = append(own, r)
		}
	}
	if len(own) > 0 {
		return own
	}
	return rows
}

// pruneEvaluations drops evaluation history older than the configured
// retention window (database.RetentionConfig.AlertEvaluations, 0 keeps it).
func (h *Harvester) pruneEvaluations(now time.Time) {
	days := h.db.RetentionDays(func(c database.RetentionConfig) int { return c.AlertEvaluations })
	if days <= 0 {
		return
	}
	cutoff := now.UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	if n, err := h.db.PruneAlertEvaluations(cutoff); err != nil {
		slog.Warn("Alert evaluation prune failed", "error", err)
	} else if n > 0 {
		slog.Info("Alert evaluations pruned", "rows", n, "older_than", cutoff)
	}
}
