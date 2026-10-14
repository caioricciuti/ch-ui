// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

// Health check and SQL alert event types. Resolved events reuse the type of
// the incident they close.
const (
	EventTypeHealthReplicationLag        = "health.replication_lag"
	EventTypeHealthReplicaReadonly       = "health.replica_readonly"
	EventTypeHealthDisk                  = "health.disk"
	EventTypeHealthParts                 = "health.parts"
	EventTypeHealthMutationStuck         = "health.mutation_stuck"
	EventTypeHealthNodeUnreachable       = "health.node_unreachable"
	EventTypeHealthConnectionUnreachable = "health.connection_unreachable"
	EventTypeHealthSummary               = "health.summary"
	EventTypeSQLAlert                    = "sql.alert"
)

// States of a threshold alert.
const (
	StateOK      = "ok"
	StatePending = "pending"
	StateFiring  = "firing"
)

// Transitions Step reports. TransitionNone means no event.
const (
	TransitionNone        = ""
	TransitionFiring      = "firing"
	TransitionEscalated   = "escalated"
	TransitionDeescalated = "deescalated"
	TransitionResolved    = "resolved"
)

// Hysteresis is the state of one threshold alert between evaluations.
// BreachCount counts consecutive breaches while pending; while firing it
// counts consecutive breaches below the incident's severity, towards a
// de-escalation.
type Hysteresis struct {
	State       string
	Severity    string
	BreachCount int
	OkCount     int
}

// Step advances a threshold alert by one evaluation.
//
// breach is the severity the evaluation breached at, "" when it is within
// thresholds. unknown means the value could not be read: the state is
// frozen, neither a breach nor a recovery. An alert fires after forN
// consecutive breaches and resolves after resolveN consecutive OKs; a
// firing alert that breaches at a higher severity escalates at once, and one
// that breaches at a lower severity for resolveN consecutive evaluations
// de-escalates. The returned transition names the event to raise, if any. On
// TransitionResolved the returned Severity is the incident's, so the
// resolved event carries it; the caller clears it once the event is written.
func Step(prev Hysteresis, breach string, unknown bool, forN, resolveN int) (Hysteresis, string) {
	if forN < 1 {
		forN = 1
	}
	if resolveN < 1 {
		resolveN = 1
	}
	if prev.State == "" {
		prev.State = StateOK
	}
	if unknown {
		return prev, TransitionNone
	}
	next := prev
	if breach != "" {
		next.OkCount = 0
		if prev.State == StateFiring {
			switch rank, current := SeverityRank(breach), SeverityRank(prev.Severity); {
			case rank > current:
				next.Severity = breach
				next.BreachCount = 0
				return next, TransitionEscalated
			case rank < current:
				next.BreachCount++
				if next.BreachCount >= resolveN {
					next.Severity = breach
					next.BreachCount = 0
					return next, TransitionDeescalated
				}
				return next, TransitionNone
			default:
				next.BreachCount = 0
				return next, TransitionNone
			}
		}
		next.BreachCount++
		next.Severity = breach
		if next.BreachCount >= forN {
			next.State = StateFiring
			next.BreachCount = 0
			return next, TransitionFiring
		}
		next.State = StatePending
		return next, TransitionNone
	}
	next.BreachCount = 0
	switch prev.State {
	case StateFiring:
		next.OkCount++
		if next.OkCount >= resolveN {
			next.State = StateOK
			next.OkCount = 0
			return next, TransitionResolved
		}
		return next, TransitionNone
	default:
		next.State = StateOK
		next.Severity = ""
		next.OkCount = 0
		return next, TransitionNone
	}
}

// SeverityRank orders severities: info < warn < error < critical; 0 for
// anything else.
func SeverityRank(s string) int {
	return severityRank(s)
}
