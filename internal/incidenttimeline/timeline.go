// SPDX-License-Identifier: BUSL-1.1
// Package incidenttimeline assembles timestamped observations, never causal conclusions.
package incidenttimeline

import (
	"fmt"
	"sort"
	"time"
)

const Limit = 500

type Window struct{ From, To time.Time }

func ParseWindow(from, to string) (Window, error) {
	f, e1 := time.Parse(time.RFC3339Nano, from)
	t, e2 := time.Parse(time.RFC3339Nano, to)
	if e1 != nil || e2 != nil || !t.After(f) || t.Sub(f) > 7*24*time.Hour {
		return Window{}, fmt.Errorf("from and to must be RFC3339 timestamps spanning more than zero and at most 7 days")
	}
	return Window{From: f.UTC(), To: t.UTC()}, nil
}

type Event struct {
	ID         string                 `json:"id"`
	At         string                 `json:"at"`
	Source     string                 `json:"source"`
	Kind       string                 `json:"kind"`
	Title      string                 `json:"title"`
	Details    string                 `json:"details,omitempty"`
	Severity   string                 `json:"severity"`
	IncidentID string                 `json:"incident_id,omitempty"`
	Actor      string                 `json:"actor,omitempty"`
	Values     map[string]interface{} `json:"values,omitempty"`
}

type Coverage struct {
	Source    string `json:"source"`
	Available bool   `json:"available"`
	Message   string `json:"message"`
	Events    int    `json:"events"`
	Truncated bool   `json:"truncated"`
}

func Sort(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, events[i].At)
		b, _ := time.Parse(time.RFC3339Nano, events[j].At)
		if a.Equal(b) {
			return events[i].ID < events[j].ID
		}
		return a.Before(b)
	})
}

func (w Window) BucketSeconds() int {
	if w.To.Sub(w.From) <= 6*time.Hour {
		return 60
	}
	if w.To.Sub(w.From) <= 24*time.Hour {
		return 300
	}
	return 1800
}

func (w Window) bounds() string {
	return fmt.Sprintf("event_time >= parseDateTime64BestEffort('%s') AND event_time < parseDateTime64BestEffort('%s') AND event_date >= toDate('%s')",
		w.From.Format(time.RFC3339Nano), w.To.Format(time.RFC3339Nano), w.From.Format("2006-01-02"))
}

// QuerySQL aggregates terminal events from initial queries on the connected node.
// No raw query text, users or data values are returned or persisted.
func (w Window) QuerySQL() string {
	return fmt.Sprintf(`SELECT formatDateTime(toStartOfInterval(event_time, INTERVAL %d SECOND, 'UTC'), '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS t,
	count() AS queries, countIf(type != 'QueryFinish') AS failures,
	if(countIf(type = 'QueryFinish') = 0, 0, round(quantileIf(0.95)(query_duration_ms, type = 'QueryFinish'), 1)) AS p95_ms,
	max(query_duration_ms) AS max_ms
	FROM system.query_log WHERE %s AND is_initial_query = 1
	AND type IN ('QueryFinish','ExceptionBeforeStart','ExceptionWhileProcessing')
	AND log_comment != 'ch-ui:incident-timeline'
	GROUP BY t ORDER BY t LIMIT %d FORMAT JSON`, w.BucketSeconds(), w.bounds(), Limit+1)
}

func (w Window) PartsSQL() string {
	return fmt.Sprintf(`SELECT formatDateTime(event_time, '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC') AS t,
	toString(event_type) AS event_type, database, table, duration_ms, error
	FROM system.part_log WHERE %s AND event_type IN ('MergeParts', 'MutatePart')
	ORDER BY event_time, database, table LIMIT %d FORMAT JSON`, w.bounds(), Limit+1)
}
