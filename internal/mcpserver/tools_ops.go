// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package mcpserver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/alerts/firing"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
	"github.com/caioricciuti/ch-ui/internal/sqlalerts"
)

// Ops tools are read-only views of CH-UI's Pro operations features (health
// checks, SQL alerts, open incidents, alert events, scheduled jobs, report
// delivery, cluster health) for the key's connection, limited to what a
// non-admin sees in the UI. Admin-only views (incident timeline, governance
// incidents, access drift, operations reports, audit) are not exposed, nor
// are channel secrets and report recipients. Registered only when a Pro
// license is active.

type alertEventsArgs struct {
	Range string `json:"range,omitempty" jsonschema:"how far back to look: 1h, 6h, 24h (default), 7d, or 30d"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum events to return, newest first (default 50, max 500)"`
}

// opsRanges are the look-back windows of alert_events.
var opsRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// alertEventScan is how many events alert_events reads before filtering
// for an allow-listed key, so the filter rarely leaves the page short.
const alertEventScan = 1000

func registerOpsTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	conn := ak.key.ConnectionID

	title, ann := readOnlyTool("List alerts")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_alerts",
		Title:       title,
		Annotations: ann,
		Description: "List the alerting configured on this connection: the built-in health checks (thresholds, enabled, nodes currently pending or firing) and the SQL threshold alerts with their state (ok, pending, firing, error), last value and last run. Use firing_alerts for what is open right now.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pageArgs) (*mcp.CallToolResult, any, error) {
		settings, err := clusterhealth.LoadHealthSettings(deps.DB, conn)
		if err != nil {
			return errResult("failed to load health checks: %v", err), nil, nil
		}
		states, err := deps.DB.ListHealthAlertStates(conn, true)
		if err != nil {
			return errResult("failed to load health check state: %v", err), nil, nil
		}
		active := map[string][]map[string]any{}
		for _, st := range states {
			active[st.CheckKind] = append(active[st.CheckKind], map[string]any{
				"node": st.Node, "state": st.State, "severity": st.Severity, "value": st.LastValue, "since": st.Since,
			})
		}
		checks := make([]map[string]any, 0, len(settings))
		for _, s := range settings {
			label := s.CheckKind
			if c, ok := clusterhealth.LookupHealthCheck(s.CheckKind); ok {
				label = c.Label
			}
			nodes := active[s.CheckKind]
			if nodes == nil {
				nodes = []map[string]any{}
			}
			checks = append(checks, map[string]any{
				"check_kind": s.CheckKind, "label": label, "enabled": s.Enabled, "unit": s.Unit,
				"warn_threshold": s.WarnThreshold, "crit_threshold": s.CritThreshold, "free_bytes_floor": s.FreeBytesFloor,
				"for_evaluations": s.ForEvaluations, "resolve_evaluations": s.ResolveEvaluations, "active": nodes,
			})
		}

		list, err := visibleSQLAlerts(deps, ak)
		if err != nil {
			return errResult("failed to list SQL alerts: %v", err), nil, nil
		}
		items := make([]map[string]any, 0, len(list))
		for _, a := range list {
			a = sqlalerts.View(a)
			items = append(items, map[string]any{
				"id": a.ID, "name": a.Name, "sql": a.SQL, "mode": a.Mode, "comparator": a.Comparator, "threshold": a.Threshold,
				"severity": a.Severity, "interval_seconds": a.IntervalSeconds, "enabled": a.Enabled, "state": a.State,
				"last_run_at": a.LastRunAt, "last_value": a.LastValue, "last_error": a.LastError, "created_by": a.CreatedBy,
			})
		}
		page, next, err := pageOf(items, args.Cursor, clampPageSize(args.PageSize))
		if err != nil {
			return errResult("invalid cursor; pass the next_cursor value from the previous page"), nil, nil
		}
		out := map[string]any{"health_checks": checks, "sql_alerts": page}
		if next != "" {
			out["next_cursor"] = next
		}
		if allowedDBs(ak.key) != nil {
			out["note"] = "SQL alerts whose SQL reads outside this key's databases, or names a table without its database, are not listed"
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Firing alerts")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "firing_alerts",
		Title:       title,
		Annotations: ann,
		Description: "List the incidents open right now on this connection: firing health checks, firing SQL alerts, and stalled or failing ingest pipelines, with severity, when each started and whether someone acknowledged it. Start here when asked whether anything is wrong.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pageArgs) (*mcp.CallToolResult, any, error) {
		acks, err := firing.LoadAcks(deps.DB)
		if err != nil {
			return errResult("failed to load open incidents: %v", err), nil, nil
		}
		open, err := firing.Open(deps.DB, acks, conn)
		if err != nil {
			return errResult("failed to load open incidents: %v", err), nil, nil
		}
		restricted := allowedDBs(ak.key) != nil
		var sqlOK map[string]bool
		if restricted {
			if sqlOK, err = visibleSQLAlertIDs(deps, ak); err != nil {
				return errResult("failed to load open incidents: %v", err), nil, nil
			}
		}
		items := make([]map[string]any, 0, len(open))
		for _, inc := range open {
			if restricted {
				// Pipeline incidents name tables in any database; SQL
				// alerts only when their SQL stays inside the allow-list.
				if inc.Source == "ingest" || (inc.Source == "sql" && (inc.SQLAlertID == nil || !sqlOK[*inc.SQLAlertID])) {
					continue
				}
			}
			item := map[string]any{
				"source": inc.Source, "name": inc.Name, "severity": inc.Severity, "since": inc.Since,
			}
			for k, v := range map[string]*string{
				"check_kind": inc.CheckKind, "node": inc.Node, "sql_alert_id": inc.SQLAlertID, "pipeline_id": inc.PipelineID,
				"event_type": inc.EventType, "summary": inc.Summary, "last_seen": inc.LastSeen, "link_path": inc.LinkPath,
			} {
				if v != nil {
					item[k] = *v
				}
			}
			if inc.Ack != nil {
				item["acknowledged"] = map[string]any{"at": inc.Ack.CreatedAt, "expires_at": inc.Ack.ExpiresAt, "comment": inc.Ack.Comment}
			}
			items = append(items, item)
		}
		return pagedResult("incidents", items, args), nil, nil
	})

	title, ann = readOnlyTool("Alert events")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "alert_events",
		Title:       title,
		Annotations: ann,
		Description: "Recent alert events on this connection, newest first: health check, SQL alert, ingest, scheduled job and report failures, firing and resolved, with whether delivery was held back by a silence or an acknowledgement. Use it to see what happened over a time range.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args alertEventsArgs) (*mcp.CallToolResult, any, error) {
		rng := args.Range
		window, ok := opsRanges[rng]
		if !ok {
			rng, window = "24h", opsRanges["24h"]
		}
		limit := clampPageSize(args.Limit)
		restricted := allowedDBs(ak.key) != nil
		fetch := limit
		var sqlOK map[string]bool
		if restricted {
			fetch = alertEventScan
			var err error
			if sqlOK, err = visibleSQLAlertIDs(deps, ak); err != nil {
				return errResult("failed to list alert events: %v", err), nil, nil
			}
		}
		since := time.Now().UTC().Add(-window).Format(time.RFC3339)
		events, err := deps.DB.ListConnectionAlertEvents(conn, since, alerts.AdminOnlyEventTypes, fetch)
		if err != nil {
			return errResult("failed to list alert events: %v", err), nil, nil
		}
		items := make([]map[string]any, 0, limit)
		for _, ev := range events {
			if len(items) == limit {
				break
			}
			isSQL := ev.EventType == alerts.EventTypeSQLAlert
			if restricted {
				// Health events name only checks and nodes; everything else
				// can name tables in any database.
				if !(strings.HasPrefix(ev.EventType, "health.") || (isSQL && ev.SourceRef != nil && sqlOK[*ev.SourceRef])) {
					continue
				}
			}
			item := map[string]any{
				"id": ev.ID, "event_type": ev.EventType, "severity": ev.Severity, "title": ev.Title,
				"message": ev.Message, "status": ev.Status, "created_at": ev.CreatedAt, "suppressed": ev.SuppressedBy != nil,
			}
			if isSQL && ev.SourceRef != nil {
				item["sql_alert_id"] = *ev.SourceRef
			}
			items = append(items, item)
		}
		return jsonResult(map[string]any{"range": rng, "events": items}), nil, nil
	})

	title, ann = readOnlyTool("List scheduled jobs")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_schedules",
		Title:       title,
		Annotations: ann,
		Description: "List the scheduled query jobs that run on this connection: saved query, cron and timezone, enabled, last run status and error, next run.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pageArgs) (*mcp.CallToolResult, any, error) {
		schedules, err := deps.DB.GetSchedules()
		if err != nil {
			return errResult("failed to list schedules: %v", err), nil, nil
		}
		queries, err := deps.DB.GetSavedQueries()
		if err != nil {
			return errResult("failed to list schedules: %v", err), nil, nil
		}
		byID := make(map[string]*database.SavedQuery, len(queries))
		for i := range queries {
			byID[queries[i].ID] = &queries[i]
		}
		restricted := allowedDBs(ak.key) != nil
		items := []map[string]any{}
		for i := range schedules {
			s := &schedules[i]
			sq := byID[s.SavedQueryID]
			if scheduler.ConnectionFor(s, sq) != conn {
				continue
			}
			item := map[string]any{
				"id": s.ID, "name": s.Name, "saved_query_id": s.SavedQueryID, "cron": s.Cron, "timezone": s.Timezone,
				"enabled": s.Enabled, "last_run_at": s.LastRunAt, "next_run_at": s.NextRunAt, "last_status": s.LastStatus,
				"created_by": s.CreatedBy,
			}
			if sq != nil {
				// Name only, as in the UI: the query may be private.
				item["saved_query_name"] = sq.Name
			}
			if !restricted {
				item["last_error"] = s.LastError
			}
			items = append(items, item)
		}
		return pagedResult("schedules", items, args), nil, nil
	})

	title, ann = readOnlyTool("List reports")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_reports",
		Title:       title,
		Annotations: ann,
		Description: "List your own scheduled report deliveries on this connection (OAuth sign-ins and keys that run as a person only): source, cron, format, channels, last delivery status and next run. Recipients are not included.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pageArgs) (*mcp.CallToolResult, any, error) {
		// Reports belong to people. A service key on a ClickHouse account stands for no one, so it
		// sees none rather than those of whoever shares its ClickHouse user.
		actor := strings.TrimSpace(ak.key.Subject)
		if actor == "" {
			return jsonResult(map[string]any{
				"reports": []map[string]any{},
				"note":    "reports belong to people; this service key runs as a ClickHouse account, so none are listed. Sign in with OAuth, or use a key that runs as you, to see yours.",
			}), nil, nil
		}
		all, err := deps.DB.ListReportSubscriptions(false)
		if err != nil {
			return errResult("failed to list reports: %v", err), nil, nil
		}
		restricted := allowedDBs(ak.key) != nil
		items := []map[string]any{}
		for _, s := range all {
			// The UI shows a report to its creator and to admins only.
			if s.ConnectionID != conn || s.CreatedBy == nil || *s.CreatedBy != actor {
				continue
			}
			channels := make([]map[string]any, 0, len(s.Targets))
			for _, t := range s.Targets {
				channels = append(channels, map[string]any{"name": t.ChannelName, "type": t.ChannelType})
			}
			item := map[string]any{
				"id": s.ID, "name": s.Name, "source_type": s.SourceType, "saved_query_id": s.SavedQueryID,
				"dashboard_id": s.DashboardID, "cron": s.Cron, "timezone": s.Timezone, "enabled": s.Enabled,
				"format": s.Format, "next_run_at": s.NextRunAt, "last_run_at": s.LastRunAt, "last_status": s.LastStatus,
				"consecutive_failures": s.ConsecutiveFailures, "channels": channels,
			}
			if !restricted {
				item["last_error"] = s.LastError
			}
			items = append(items, item)
		}
		return pagedResult("reports", items, args), nil, nil
	})

	title, ann = readOnlyTool("Cluster health")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cluster_health",
		Title:       title,
		Annotations: ann,
		Description: "Live per-node health of this connection's cluster: replication delay and queue, read-only replicas, running merges, pending mutations, parts pressure, long-running queries and disk usage. Requires the key's ClickHouse user to read the system tables involved.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		if allowedDBs(ak.key) != nil {
			return errResult("cluster_health is not available to this key: it aggregates system tables across all databases, and this key is limited to: %s. Use a key without a database allow-list.", allowList(ak.key.AllowedDatabases).describe()), nil, nil
		}
		exec := clusterhealth.ExecFunc(func(sql string) ([]map[string]interface{}, error) {
			return runCH(ctx, deps, ak, sql, clusterhealth.QuerySettings(), metaTimeout)
		})
		threshold := clusterhealth.DefaultSettings(conn).LongQueryThresholdSecs
		if st, err := clusterhealth.NewStore(deps.DB).GetSettings(conn); err == nil {
			threshold = st.LongQueryThresholdSecs
		}
		cluster, _ := clusterhealth.ResolveCluster(exec)
		capturedAt := time.Now().UTC().Format(time.RFC3339)
		col, degraded, err := clusterhealth.LiveSummary(exec, cluster, threshold, capturedAt)
		if err != nil && len(col.Samples) == 0 {
			return errResult("cluster_health failed (are system tables readable by this key's ClickHouse user?): %v", err), nil, nil
		}
		errs := col.Errors
		if errs == nil {
			errs = map[string]string{}
		}
		return jsonResult(map[string]any{
			"cluster": cluster, "is_cluster": cluster != "", "degraded": degraded, "captured_at": capturedAt,
			"long_query_threshold_seconds": threshold, "parts_limits": col.Limits, "nodes": col.Samples, "errors": errs,
		}), nil, nil
	})
}

// visibleSQLAlerts returns the SQL alerts of the key's connection. For a key
// with a database allow-list, only alerts whose SQL provably reads inside it.
func visibleSQLAlerts(deps Deps, ak *authedKey) ([]database.SQLAlert, error) {
	list, err := deps.DB.ListSQLAlerts(ak.key.ConnectionID)
	if err != nil {
		return nil, err
	}
	if allowedDBs(ak.key) == nil {
		return list, nil
	}
	out := list[:0]
	for _, a := range list {
		if sqlAlertAllowed(ak, a.SQL) {
			out = append(out, a)
		}
	}
	return out, nil
}

func visibleSQLAlertIDs(deps Deps, ak *authedKey) (map[string]bool, error) {
	list, err := visibleSQLAlerts(deps, ak)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(list))
	for _, a := range list {
		ids[a.ID] = true
	}
	return ids, nil
}

// errUnqualified refuses to resolve unqualified tables: an alert runs with
// another account whose current database the key cannot know.
var errUnqualified = errors.New("unqualified table")

// sqlAlertAllowed reports whether an alert's SQL reads only databases in the
// key's allow-list, failing closed on anything the analysis cannot resolve.
func sqlAlertAllowed(ak *authedKey, sql string) bool {
	return checkAllowedDatabases(func() (string, error) { return "", errUnqualified }, allowList(ak.key.AllowedDatabases), sql, nil) == nil
}
