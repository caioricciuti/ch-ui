// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/reports"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
)

// Pro lifecycle actions: run a scheduled job or send a report now, the
// MCP counterparts of the "Run now" and "Send now" buttons. Registered only
// for keys with the actions permission while a Pro license is active.
//
// Both are refused for keys with a database allow-list: a schedule runs any
// statement its saved query holds and a report reads whatever its query or
// dashboard does, and neither can be checked against the list.

type runScheduleArgs struct {
	Schedule string `json:"schedule" jsonschema:"scheduled job name or id"`
}

type runReportArgs struct {
	Report string `json:"report" jsonschema:"report name or id"`
}

// refuseActionForAllowList refuses a Pro action for an allow-listed key.
func refuseActionForAllowList(ak *authedKey, tool, why string) *mcp.CallToolResult {
	if allowedDBs(ak.key) == nil {
		return nil
	}
	return errResult("%s is not available to this key: %s, which cannot be checked against this key's allowlist (%s). Use a key without a database allow-list.", tool, why, allowList(ak.key.AllowedDatabases).describe())
}

// scheduleConnection is the connection a schedule runs on: its own, else
// its saved query's, else the caller's (the UI's "Run now" uses the
// session's connection for an unbound job).
func scheduleConnection(s *database.Schedule, q *database.SavedQuery, fallback string) string {
	if s.ConnectionID != nil && *s.ConnectionID != "" {
		return *s.ConnectionID
	}
	if q != nil && q.ConnectionID != nil && *q.ConnectionID != "" {
		return *q.ConnectionID
	}
	return fallback
}

func registerProActionTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := actionTool("Run scheduled job now", true, false, false)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_schedule_now",
		Title:       title,
		Annotations: ann,
		Description: "Run a scheduled job's saved query once, now, on this connection as this key's ClickHouse user, like Run now on the Schedules page. Returns status, rows and elapsed time.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args runScheduleArgs) (*mcp.CallToolResult, any, error) {
		if res := refuseActionForAllowList(ak, "run_schedule_now", "a scheduled job runs whatever statement its saved query holds"); res != nil {
			return res, nil, nil
		}
		ref := strings.TrimSpace(args.Schedule)
		if ref == "" {
			return errResult("schedule is required"), nil, nil
		}
		all, err := deps.DB.GetSchedules()
		if err != nil {
			return errResult("failed to load schedules: %v", err), nil, nil
		}
		var match []database.Schedule
		var query *database.SavedQuery
		for _, s := range all {
			if s.ID != ref && s.Name != ref {
				continue
			}
			q, err := deps.DB.GetSavedQueryByID(s.SavedQueryID)
			if err != nil {
				return errResult("failed to load the saved query of %q: %v", s.Name, err), nil, nil
			}
			if scheduleConnection(&s, q, ak.key.ConnectionID) != ak.key.ConnectionID {
				continue
			}
			match = append(match, s)
			query = q
		}
		switch {
		case len(match) == 0:
			return errResult("scheduled job %q not found on this connection", ref), nil, nil
		case len(match) > 1:
			return errResult("%d scheduled jobs are named %q; pass the id", len(match), ref), nil, nil
		case query == nil:
			return errResult("the saved query of scheduled job %q no longer exists", match[0].Name), nil, nil
		}
		s := match[0]
		msg := fmt.Sprintf("Run scheduled job %q now in ClickHouse as user %q? It executes saved query %q.", s.Name, ak.key.CHUser, query.Name)
		if res := confirmWrite(deps, ak, req, msg); res != nil {
			return res, nil, nil
		}
		out := scheduler.RunNow(ctx, deps.DB, deps.Gateway, &s, query.Query, ak.key.ConnectionID, ak.key.CHUser, ak.chPassword)
		audit(deps, ak, "schedule.manual_run", fmt.Sprintf("mcp key: %s, schedule: %s, status=%s elapsed=%dms", ak.key.Name, s.Name, out.Status, out.ElapsedMs))
		res := map[string]any{"run_id": out.RunID, "schedule": s.Name, "status": out.Status, "rows": out.Rows, "elapsed_ms": out.ElapsedMs}
		if out.Status == "error" {
			res["error"] = out.Error
			if h := grantsHint(ak, out.Error); h != "" {
				res["hint"] = h
			}
			r := jsonResult(res)
			r.IsError = true
			return r, nil, nil
		}
		return jsonResult(res), nil, nil
	})

	if deps.ReportRunner == nil {
		return
	}
	title, ann = actionTool("Send report now", false, false, true)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_saved_report_now",
		Title:       title,
		Annotations: ann,
		Description: "Run one of your own reports on this connection now and deliver it to all its channels (email or webhook), like Send now on the Reports page. OAuth sign-ins only. Returns the delivery status.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args runReportArgs) (*mcp.CallToolResult, any, error) {
		// Reports belong to people (same rule as list_reports): an OAuth
		// sign-in reaches its own, an API key has no person and none.
		if ak.key.Kind != "oauth" || strings.TrimSpace(ak.key.Subject) == "" {
			return errResult("run_saved_report_now needs an OAuth sign-in: reports belong to the person who created them, and an API key is not a person"), nil, nil
		}
		if res := refuseActionForAllowList(ak, "run_saved_report_now", "a report reads whatever its query or dashboard reads"); res != nil {
			return res, nil, nil
		}
		ref := strings.TrimSpace(args.Report)
		if ref == "" {
			return errResult("report is required"), nil, nil
		}
		all, err := deps.DB.ListReportSubscriptions(false)
		if err != nil {
			return errResult("failed to load reports: %v", err), nil, nil
		}
		// Same rule as the Reports page for a non-admin: own reports only.
		actor := ak.key.Subject
		var match []database.ReportSubscription
		for _, s := range all {
			if s.ConnectionID != ak.key.ConnectionID || s.CreatedBy == nil || *s.CreatedBy != actor {
				continue
			}
			if s.ID == ref || s.Name == ref {
				match = append(match, s)
			}
		}
		switch len(match) {
		case 0:
			return errResult("report %q not found among your reports on this connection", ref), nil, nil
		case 1:
		default:
			return errResult("%d reports are named %q; pass the id", len(match), ref), nil, nil
		}
		full := &match[0]
		msg := fmt.Sprintf("Send report %q now to its %d channel(s)? It runs with the connection's report account and delivers to every recipient.", full.Name, len(full.Targets))
		if res := confirmWrite(deps, ak, req, msg); res != nil {
			return res, nil, nil
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(full.TimeoutSeconds)*time.Second+3*time.Minute)
		defer cancel()
		d, err := deps.ReportRunner.SendNow(rctx, full)
		if errors.Is(err, reports.ErrBusy) {
			return errResult("report %q is already running; try again shortly", full.Name), nil, nil
		}
		details := fmt.Sprintf("mcp key: %s, report: %s, trigger: manual", ak.key.Name, full.Name)
		out := map[string]any{"report": full.Name}
		if d != nil {
			details += ", delivery: " + d.ID + ", status: " + d.Status
			out["delivery_id"] = d.ID
			out["status"] = d.Status
			out["rows"] = d.Rows
		}
		audit(deps, ak, "report.sent_manual", details)
		if err != nil {
			out["error"] = err.Error()
			r := jsonResult(out)
			r.IsError = true
			return r, nil, nil
		}
		return jsonResult(out), nil, nil
	})
}
