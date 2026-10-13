// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

// CHCredentials carries the ClickHouse credentials the harvester borrows from an
// active session to run monitoring queries through the tunnel gateway.
type CHCredentials struct {
	ConnectionID string
	User         string
	Password     string
}

// Settings is the per-connection monitoring configuration. Retention defaults to
// 7 days and is user-adjustable; the harvester prunes samples older than this.
type Settings struct {
	ConnectionID           string `json:"connection_id"`
	Enabled                bool   `json:"enabled"`
	RetentionDays          int    `json:"retention_days"`
	PollIntervalSeconds    int    `json:"poll_interval_seconds"`
	LongQueryThresholdSecs int    `json:"long_query_threshold_seconds"`
	UpdatedAt              string `json:"updated_at"`
}

// DefaultSettings returns the baseline configuration applied when a connection
// has no stored settings row yet.
func DefaultSettings(connectionID string) Settings {
	return Settings{
		ConnectionID:           connectionID,
		Enabled:                true,
		RetentionDays:          7,
		PollIntervalSeconds:    60,
		LongQueryThresholdSecs: 30,
	}
}

// Bounds for user-supplied settings, enforced on write.
const (
	MinRetentionDays       = 1
	MaxRetentionDays       = 365
	MinPollIntervalSeconds = 15
	MaxPollIntervalSeconds = 3600
	MinLongQuerySeconds    = 1
	MaxLongQuerySeconds    = 3600
)

// Clamp returns a copy of s with all fields forced into their valid ranges and
// sensible defaults substituted for zero values.
func (s Settings) Clamp() Settings {
	out := s
	if out.RetentionDays == 0 {
		out.RetentionDays = 7
	}
	out.RetentionDays = clampInt(out.RetentionDays, MinRetentionDays, MaxRetentionDays)
	if out.PollIntervalSeconds == 0 {
		out.PollIntervalSeconds = 60
	}
	out.PollIntervalSeconds = clampInt(out.PollIntervalSeconds, MinPollIntervalSeconds, MaxPollIntervalSeconds)
	if out.LongQueryThresholdSecs == 0 {
		out.LongQueryThresholdSecs = 30
	}
	out.LongQueryThresholdSecs = clampInt(out.LongQueryThresholdSecs, MinLongQuerySeconds, MaxLongQuerySeconds)
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Sample is one time-series row: aggregated health for a single node at a single
// poll. Only these compact numerics are stored; detailed lists are fetched live.
type Sample struct {
	Node                  string  `json:"node"`
	CapturedAt            string  `json:"captured_at"`
	ReplicationMaxDelay   float64 `json:"replication_max_delay"`
	ReplicationQueueTotal int64   `json:"replication_queue_total"`
	ReplicasReadonly      int64   `json:"replicas_readonly"`
	MergesRunning         int64   `json:"merges_running"`
	MutationsPending      int64   `json:"mutations_pending"`
	PartsMaxActive        int64   `json:"parts_max_active"`
	PartsPressurePct      float64 `json:"parts_pressure_pct"`
	LongQueries           int64   `json:"long_queries"`

	// Live-only fields (not stored in history).
	// MutationsOldestAgeSec is the age of the oldest unfinished mutation.
	MutationsOldestAgeSec int64 `json:"mutations_oldest_age_seconds"`
	// DiskUsedPct is the fullest disk on the node, nil when unknown.
	DiskUsedPct *float64 `json:"disk_used_pct,omitempty"`

	// Unknown lists the metric groups (see Metric*) whose collection failed
	// for this node. Their numeric fields read 0 but must be shown as unknown,
	// never as healthy.
	Unknown []string `json:"unknown,omitempty"`
	// Down marks a node listed in system.clusters that returned no data at
	// all: unreachable, or skipped as unavailable by the fan-out.
	Down bool `json:"down,omitempty"`
}

// Metric groups reported in Sample.Unknown and Collection.Errors.
const (
	MetricReplication = "replication"
	MetricMerges      = "merges"
	MetricMutations   = "mutations"
	MetricParts       = "parts"
	MetricLongQueries = "long_queries"
	MetricDisk        = "disk"
)

// storedMetrics are the groups persisted in ch_health_samples.
var storedMetrics = []string{MetricReplication, MetricMerges, MetricMutations, MetricParts, MetricLongQueries}

// IsUnknown reports whether metric failed to collect for this sample.
func (s Sample) IsUnknown(metric string) bool {
	for _, m := range s.Unknown {
		if m == metric {
			return true
		}
	}
	return false
}

func (s *Sample) markUnknown(metric string) {
	if !s.IsUnknown(metric) {
		s.Unknown = append(s.Unknown, metric)
	}
}
