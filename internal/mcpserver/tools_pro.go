// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/costs"
	"github.com/caioricciuti/ch-ui/internal/queryinsights"
)

// Pro tools expose the analytics CH-UI already computes — Query Insights and
// Cost Center — as structured MCP tools, so an AI can answer "what was slow
// yesterday" or "why was last week expensive" without reinventing query_log
// SQL. Registered only when a Pro license is active.

type insightsArgs struct {
	Section string `json:"section" jsonschema:"which ranking to return: slow (by p95 latency), memory (by peak memory), or frequent (by run count)"`
	Range   string `json:"range,omitempty" jsonschema:"time range: 1h, 6h, 24h (default), 7d, or 30d"`
}

type costsArgs struct {
	Range string `json:"range,omitempty" jsonschema:"time range: 24h, 7d (default), or 30d"`
}

var insightSections = map[string]func(cluster string, rng queryinsights.Range, f queryinsights.Filters) string{
	"slow":     queryinsights.SlowQueriesQuery,
	"memory":   queryinsights.MemoryQuery,
	"frequent": queryinsights.FrequentQuery,
}

func registerProTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("Top query patterns")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "query_insights_top",
		Title:       title,
		Annotations: ann,
		Description: "Top query patterns from system.query_log, grouped by normalized query: slowest (p95), most memory-hungry, or most frequent. Requires the ClickHouse user to have access to system.query_log.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args insightsArgs) (*mcp.CallToolResult, any, error) {
		builder, ok := insightSections[args.Section]
		if !ok {
			return errResult("section must be one of: slow, memory, frequent"), nil, nil
		}
		rng, ok := queryinsights.RangeSpec(args.Range)
		if !ok {
			rng = queryinsights.DefaultRange
		}
		sql := builder("", rng, queryinsights.Filters{})
		rows, err := runCH(deps, ak, sql, map[string]string{"log_comment": queryinsights.LogComment}, metaTimeout)
		if err != nil {
			return errResult("query_insights_top failed (is system.query_log accessible to this key's ClickHouse user?): %v", err), nil, nil
		}
		return jsonResult(map[string]any{"section": args.Section, "range": rng.Name, "patterns": rows}), nil, nil
	})

	title, ann = readOnlyTool("Cost summary")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "costs_summary",
		Title:       title,
		Annotations: ann,
		Description: "Cost Center summary for this connection: compute spend from real CPU consumption, spend on failed queries, core-hours, and scan volume, priced with the connection's configured rates.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args costsArgs) (*mcp.CallToolResult, any, error) {
		rng, ok := costs.RangeSpec(args.Range)
		if !ok {
			rng = costs.DefaultRange
		}
		cfg, isDefault := loadCostsConfig(deps, ak.key.ConnectionID)
		sql := costs.SummaryQuery("", rng, cfg)
		rows, err := runCH(deps, ak, sql, map[string]string{"log_comment": costs.LogComment}, metaTimeout)
		if err != nil {
			return errResult("costs_summary failed (is system.query_log accessible to this key's ClickHouse user?): %v", err), nil, nil
		}
		out := map[string]any{
			"range":    rng.Name,
			"currency": cfg.Currency,
			"summary":  rows,
		}
		if isDefault {
			out["note"] = "rates are CH-UI defaults; configure real rates in Cost Center for accurate pricing"
		}
		return jsonResult(out), nil, nil
	})
}

// loadCostsConfig mirrors the Cost Center handler: stored config or defaults.
func loadCostsConfig(deps Deps, connectionID string) (costs.Config, bool) {
	stored, _ := deps.DB.GetCostsConfig(connectionID)
	if stored == nil || stored.ConfigJSON == "" {
		return costs.DefaultConfig(), true
	}
	var cfg costs.Config
	if err := json.Unmarshal([]byte(stored.ConfigJSON), &cfg); err != nil {
		return costs.DefaultConfig(), true
	}
	return cfg.Sanitize(), false
}
