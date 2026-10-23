// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"strings"
	"testing"
)

// The model must see query values with every digit, not rounded through
// float64.
func TestModelViewKeepsNumberPrecision(t *testing.T) {
	raw := []byte(`{"sql":"SELECT amount, n FROM sales","row_count":1,` +
		`"data":[{"amount":123456789012345678.1234567891,"n":9007199254740993}],` +
		`"meta":[{"name":"amount","type":"Decimal(38, 10)"},{"name":"n","type":"Int64"}]}`)
	got := ModelView("run_query", raw)
	if !strings.Contains(got, "123456789012345678.1234567891,9007199254740993") {
		t.Errorf("query CSV lost precision:\n%s", got)
	}

	other := ModelView("get_insights", []byte(`{"stats":{"max":9007199254740993}}`))
	if !strings.Contains(other, "9007199254740993") {
		t.Errorf("non-query result lost precision:\n%s", other)
	}
}
