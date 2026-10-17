// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package querylogactor

import (
	"strconv"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestExprWithoutAccountsFallsBackToUser(t *testing.T) {
	got := Expr(nil)
	want := `if(if(startsWith(log_comment, '{"chui":'), JSONExtractString(log_comment, 'person'), '') != '', if(startsWith(log_comment, '{"chui":'), JSONExtractString(log_comment, 'person'), ''), user)`
	if got != want {
		t.Fatalf("Expr(nil) =\n%s\nwant\n%s", got, want)
	}
	// The prefix must be the one the gateway writes, or tagged rows would
	// fall through to the ClickHouse account.
	if !strings.HasPrefix(tunnel.QueryTag{Surface: "editor", Person: "a@x"}.LogComment(), tagPrefix) {
		t.Fatalf("tag prefix drifted from tunnel.QueryTag")
	}
	if !strings.Contains(ObjectExpr, "JSONExtractString(log_comment, 'object')") {
		t.Fatalf("ObjectExpr = %s", ObjectExpr)
	}
}

func TestExprRelabelsKnownAccounts(t *testing.T) {
	got := Expr([]Account{
		{User: "sso:bob@x", Person: "bob@x"},
		{User: "sso:a'l@x", Person: "a'l@x"},
		{User: "sso:bob@x", Person: "other"}, // duplicate user: first wins
		{User: "same", Person: "same"},       // identity: dropped
		{User: "", Person: "nobody"},         // empty: dropped
	})
	want := `transform(user, ['sso:a\'l@x', 'sso:bob@x'], ['a\'l@x', 'bob@x'], user)`
	if !strings.HasSuffix(got, want+")") {
		t.Fatalf("Expr =\n%s\nwant suffix\n%s", got, want)
	}
	if strings.Contains(got, "'same'") || strings.Contains(got, "nobody") || strings.Contains(got, "other") {
		t.Fatalf("dropped accounts leaked into SQL:\n%s", got)
	}
}

func TestExprCapsAccounts(t *testing.T) {
	accounts := make([]Account, 0, MaxAccounts+50)
	for i := 0; i < MaxAccounts+50; i++ {
		accounts = append(accounts, Account{User: "u" + strconv.Itoa(i), Person: "p"})
	}
	users, _ := literalLists(accounts)
	if len(users) != MaxAccounts {
		t.Fatalf("len = %d, want %d", len(users), MaxAccounts)
	}
}
