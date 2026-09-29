// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// checkGuardrails runs governance policies when available (Pro). A nil service
// or non-Pro license means no policy evaluation.
func checkGuardrails(deps Deps, ak *authedKey, sql, endpoint string) *mcp.CallToolResult {
	if deps.Guardrails == nil || deps.Config == nil || !deps.Config.IsPro() {
		return nil
	}
	decision, err := deps.Guardrails.EvaluateQuery(ak.key.ConnectionID, ak.key.CHUser, sql, endpoint)
	if err != nil || decision.Allowed {
		return nil // guardrails soft-fail open, same as the editor
	}
	b := decision.Block
	return errResult("query blocked by governance policy %q (severity %s): %s", b.PolicyName, b.Severity, b.Detail)
}
