// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package querylogactor defines, once, how a system.query_log row is
// attributed to a person. Cost Center, Query Insights and the governance
// harvest all read the same column (log_comment) the gateway writes
// (tunnel.QueryTag), so they must agree on who ran a query.
//
// A row CH-UI tagged names the person in the tag: the SSO email, the
// ClickHouse user of a password login, or the owner of a background job.
// Any other row (clickhouse-client, Grafana, an untagged tool) only has the
// ClickHouse account, which is then the actor. Per-person SSO accounts can
// be mapped back to their email with Accounts, so a person's queries from
// outside CH-UI land on the same row as those run through it.
package querylogactor

import (
	"sort"
	"strings"
)

// Account is a ClickHouse account known to belong to one person, typically
// a per-person SSO user and the email behind it.
type Account struct {
	User   string
	Person string
}

// MaxAccounts bounds the transform() literal: every account adds two string
// literals to every query_log aggregation.
const MaxAccounts = 1000

// tagPrefix is the start of every log_comment CH-UI writes
// (tunnel.LogCommentPrefix without the surface).
const tagPrefix = `{"chui":`

// personExpr reads the person field of a CH-UI tag, empty for a foreign row.
const personExpr = `if(startsWith(log_comment, '` + tagPrefix + `'), JSONExtractString(log_comment, 'person'), '')`

// ObjectExpr is the workspace object ("dash:42", "schedule:7") a tagged row
// ran for, empty for ad-hoc and foreign queries.
const ObjectExpr = `if(startsWith(log_comment, '` + tagPrefix + `'), JSONExtractString(log_comment, 'object'), '')`

// Expr is the actor of a query_log row: the tag's person when present and
// non-empty, else the ClickHouse user, mapped through accounts when that
// user is a known per-person account. The expression names only columns,
// never aliases, so it is safe in WHERE, GROUP BY and subselects on every
// ClickHouse analyzer.
func Expr(accounts []Account) string {
	return `if(` + personExpr + ` != '', ` + personExpr + `, ` + fallbackExpr(accounts) + `)`
}

// fallbackExpr is the ClickHouse user, relabeled for known accounts.
func fallbackExpr(accounts []Account) string {
	users, persons := literalLists(accounts)
	if len(users) == 0 {
		return "user"
	}
	return "transform(user, [" + strings.Join(users, ", ") + "], [" + strings.Join(persons, ", ") + "], user)"
}

// literalLists renders the account mapping as two parallel lists of quoted
// literals: deduplicated by user, sorted so the SQL is stable, capped at
// MaxAccounts, and without identity or empty pairs.
func literalLists(accounts []Account) (users, persons []string) {
	byUser := make(map[string]string, len(accounts))
	for _, a := range accounts {
		u, p := strings.TrimSpace(a.User), strings.TrimSpace(a.Person)
		if u == "" || p == "" || u == p {
			continue
		}
		if _, dup := byUser[u]; !dup {
			byUser[u] = p
		}
	}
	keys := make([]string, 0, len(byUser))
	for u := range byUser {
		keys = append(keys, u)
	}
	sort.Strings(keys)
	if len(keys) > MaxAccounts {
		keys = keys[:MaxAccounts]
	}
	for _, u := range keys {
		users = append(users, Quote(u))
		persons = append(persons, Quote(byUser[u]))
	}
	return users, persons
}

// Quote returns s as a ClickHouse single-quoted string literal.
func Quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}
