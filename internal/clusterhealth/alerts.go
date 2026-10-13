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
)

const (
	// maxHealthEventsPerPoll caps the events one poll of one connection may
	// raise; the rest are folded into one summary event.
	maxHealthEventsPerPoll = 20
	// historySampleInterval is how often a steady state is still recorded.
	historySampleInterval = time.Hour
	// evaluationRetention bounds the evaluation history.
	evaluationRetention = 30 * 24 * time.Hour
	// detailRows bounds the top offenders attached to a firing event.
	detailRows = 5
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
	return HealthCheckSetting{
		CheckKind: c.Kind, Enabled: true, WarnThreshold: c.DefaultWarn, CritThreshold: c.DefaultCrit,
		ForEvaluations: 2, ResolveEvaluations: 2, Unit: c.Unit, SupportsWarn: c.SupportsWarn,
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
	if c.Kind == CheckNodeUnreachable {
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
	stateIdx   int
}

// HealthFingerprint is the dispatcher de-dupe key of a health incident.
// Firing and escalation carry the severity so an escalation is not swallowed
// by the rule cooldown of the warn event; the resolve has its own suffix.
func HealthFingerprint(connectionID, check, node, suffix string) string {
	return "health:" + connectionID + ":" + check + ":" + node + ":" + suffix
}

// evaluateHealthAlerts runs every enabled check over one poll's samples,
// advances the state machines and raises events for transitions. Nodes
// absent from the samples keep their state: a node only reachable through
// a failed fan-out is unknown, not recovered.
func (h *Harvester) evaluateHealthAlerts(connID, cluster string, samples []Sample, exec ExecFunc, now time.Time) {
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
	var events []healthEvent
	var history []database.AlertEvaluation
	for _, set := range settings {
		if !set.Enabled {
			continue
		}
		check, _ := LookupHealthCheck(set.CheckKind)
		for _, s := range samples {
			value, known := checkValue(check.Kind, s)
			if !known {
				// Unknown neither breaches nor recovers: the stored state
				// stays as it is.
				continue
			}
			prev := prevByKey[check.Kind+"\x00"+s.Node]
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
			if transition != alerts.TransitionNone {
				events = append(events, healthEvent{check: check, setting: set, node: s.Node, value: value,
					transition: transition, severity: next.Severity, stateIdx: len(states) - 1})
			}
			if transition == alerts.TransitionResolved {
				states[len(states)-1].Severity = nil
			}
			if h.shouldRecord(connID, check.Kind, s.Node, next.State != prevState || transition != alerts.TransitionNone, now) {
				history = append(history, database.AlertEvaluation{
					Source: "health", SourceID: check.Kind, ConnectionID: connID, Node: s.Node,
					Value: f64(value), State: next.State, EvaluatedAt: nowStr,
				})
			}
		}
	}

	h.raiseHealthEvents(connID, cluster, events, states, exec)
	if err := h.db.SaveHealthAlertStates(states); err != nil {
		slog.Warn("Health alerts: failed to save state", "connection", connID, "error", err)
	}
	if err := h.db.InsertAlertEvaluations(history); err != nil {
		slog.Warn("Health alerts: failed to record evaluations", "connection", connID, "error", err)
	}
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

// raiseHealthEvents writes the transitions as alert events, at most
// maxHealthEventsPerPoll of them; the rest go into one summary event.
// Event ids are written back into states.
func (h *Harvester) raiseHealthEvents(connID, cluster string, events []healthEvent, states []database.HealthAlertState, exec ExecFunc) {
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
	for _, ev := range events {
		if emitted >= maxHealthEventsPerPoll {
			break
		}
		emitted++
		title, message, payload, fp := healthEventContent(connID, cluster, ev)
		if ev.transition != alerts.TransitionResolved {
			rows, ok := details[ev.check.Kind]
			if !ok {
				rows = detailQuery(exec, ev.check.Kind, cluster)
				details[ev.check.Kind] = rows
			}
			if top := offendersFor(rows, ev.node); len(top) > 0 {
				payload["top_offenders"] = top
			}
		}
		id, err := h.db.CreateAlertEvent(&conn, ev.check.EventType, ev.severity, title, message, payload, fp, "health:"+ev.check.Kind)
		if err != nil {
			slog.Warn("Health alerts: failed to create event", "connection", connID, "check", ev.check.Kind, "error", err)
			continue
		}
		if ev.transition != alerts.TransitionResolved {
			states[ev.stateIdx].LastEventID = &id
		}
	}
	if rest := events[emitted:]; len(rest) > 0 {
		sev := alerts.SeverityWarn
		counts := map[string]int{}
		for _, ev := range rest {
			if alerts.SeverityRank(ev.severity) > alerts.SeverityRank(sev) {
				sev = ev.severity
			}
			counts[ev.check.Label]++
		}
		parts := make([]string, 0, len(counts))
		for label, n := range counts {
			parts = append(parts, fmt.Sprintf("%s: %d", label, n))
		}
		sort.Strings(parts)
		title := fmt.Sprintf("%d more health check changes", len(rest))
		message := fmt.Sprintf("%d further health check transitions on this connection were not sent one by one (%s). Open Governance, Health checks for the full state.", len(rest), strings.Join(parts, ", "))
		payload := map[string]interface{}{"connection_id": connID, "cluster": cluster, "suppressed": len(rest), "by_check": counts}
		fp := "health:" + connID + ":summary:" + strconv.FormatInt(time.Now().Unix()/60, 10)
		if _, err := h.db.CreateAlertEvent(&conn, alerts.EventTypeHealthSummary, sev, title, message, payload, fp, "health:summary"); err != nil {
			slog.Warn("Health alerts: failed to create summary event", "connection", connID, "error", err)
		}
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
	}
	if ev.setting.FreeBytesFloor != nil {
		payload["free_bytes_floor"] = *ev.setting.FreeBytesFloor
	}
	switch ev.transition {
	case alerts.TransitionResolved:
		payload["status"] = "resolved"
		title := fmt.Sprintf("[RESOLVED] %s on %s", ev.check.Label, ev.node)
		message := fmt.Sprintf("%s on node %s is back within thresholds (now %s).", ev.check.Label, ev.node, value)
		return title, message, payload, HealthFingerprint(connID, ev.check.Kind, ev.node, "resolved")
	default:
		payload["status"] = "firing"
		if ev.transition == alerts.TransitionEscalated {
			payload["escalated"] = true
		}
		var title, message string
		if ev.check.Kind == CheckNodeUnreachable {
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
		}
		return title, message, payload, HealthFingerprint(connID, ev.check.Kind, ev.node, ev.severity)
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

// pruneEvaluations drops evaluation history past evaluationRetention.
func (h *Harvester) pruneEvaluations(now time.Time) {
	cutoff := now.Add(-evaluationRetention).UTC().Format(time.RFC3339)
	if n, err := h.db.PruneAlertEvaluations(cutoff); err != nil {
		slog.Warn("Alert evaluation prune failed", "error", err)
	} else if n > 0 {
		slog.Info("Alert evaluations pruned", "rows", n, "older_than", cutoff)
	}
}
