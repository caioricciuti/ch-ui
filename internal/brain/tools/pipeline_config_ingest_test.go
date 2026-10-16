// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import "testing"

func TestConfigurePipelineIngestValidation(t *testing.T) {
	if !allowedSourceTypes["source_ingest"] {
		t.Fatal("source_ingest is not an allowed source")
	}
	if m := missingFields("source_ingest", map[string]interface{}{}); len(m) != 1 || m[0] != "signal" {
		t.Fatalf("missing fields: %v", m)
	}
	if err := validateIngestSource("source_ingest", map[string]interface{}{"signal": "traces", "ttl_days": float64(7)}); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	for _, bad := range []map[string]interface{}{
		{"signal": "metrics"},
		{"signal": "logs", "rate_limit_rps": float64(-1)},
	} {
		if err := validateIngestSource("source_ingest", bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if err := validateIngestSource("source_webhook", map[string]interface{}{"signal": "nope"}); err != nil {
		t.Fatalf("other sources are not checked here: %v", err)
	}
}
