// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"fmt"
	"strings"
)

// LoadCatalog reads the live lists the ops are validated against: the
// privilege names, the referenced setting names and a parse check of every
// row policy condition, all as the session user.
func LoadCatalog(ctx context.Context, ex Executor, ops []Op) (Catalog, error) {
	cat := Catalog{PolicyErrors: map[string]string{}}
	needPrivileges := false
	for _, op := range ops {
		if op.Kind == OpGrantPrivilege || op.Kind == OpRevokePrivilege {
			needPrivileges = true
		}
	}
	if needPrivileges {
		rows, err := ex.Query(ctx, `SELECT toString(privilege) AS p FROM system.privileges`, nil)
		if err != nil {
			return cat, fmt.Errorf("read system.privileges: %w", err)
		}
		cat.Privileges = map[string]bool{}
		for _, r := range rows {
			cat.Privileges[str(r, "p")] = true
		}
	}
	if names := SettingNames(ops); len(names) > 0 {
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = quoteLit(n)
		}
		rows, err := ex.Query(ctx, `SELECT name FROM system.settings WHERE name IN (`+strings.Join(quoted, ", ")+`)`, nil)
		if err != nil {
			return cat, fmt.Errorf("read system.settings: %w", err)
		}
		cat.Settings = map[string]bool{}
		for _, r := range rows {
			cat.Settings[str(r, "name")] = true
		}
	}
	for _, c := range PolicyChecks(ops) {
		db, table, expr := c[0], c[1], c[2]
		key := PolicyKey(db, table, expr)
		if _, done := cat.PolicyErrors[key]; done {
			continue
		}
		if _, err := ex.Query(ctx, PolicyCheckSQL(db, table, expr), nil); err != nil {
			cat.PolicyErrors[key] = shortError(err)
			continue
		}
		// With the analyzer the same check also resolves columns and the
		// table. Servers without it fail on the setting; that is not the
		// expression's fault, so only name-resolution errors count.
		analyzed := strings.Replace(PolicyCheckSQL(db, table, expr), "EXPLAIN SYNTAX ", "EXPLAIN SYNTAX run_query_tree_passes = 1 ", 1)
		if _, err := ex.Query(ctx, analyzed, nil); err != nil && isResolutionError(err) {
			cat.PolicyErrors[key] = shortError(err)
		}
	}
	return cat, nil
}

func isResolutionError(err error) bool {
	msg := err.Error()
	for _, code := range []string{"UNKNOWN_IDENTIFIER", "UNKNOWN_TABLE", "UNKNOWN_DATABASE", "UNKNOWN_FUNCTION", "TYPE_MISMATCH", "ILLEGAL_TYPE_OF_ARGUMENT"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// shortError trims ClickHouse's error to its first line without the
// version suffix.
func shortError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "DB::Exception: "); i >= 0 {
		msg = msg[i+len("DB::Exception: "):]
	}
	if i := strings.Index(msg, " (version "); i >= 0 {
		msg = msg[:i]
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return strings.TrimSpace(msg)
}

// PlanLive reads the snapshot and catalog, then composes the plan.
func PlanLive(ctx context.Context, ex Executor, ops []Op, cluster string, gctx GuardContext) (*Plan, *Snapshot, error) {
	snap, err := ReadSnapshot(ctx, ex)
	if err != nil {
		return nil, nil, err
	}
	cat, err := LoadCatalog(ctx, ex, ops)
	if err != nil {
		return nil, snap, err
	}
	plan, err := Compose(ops, snap, cat, cluster, gctx)
	if err != nil {
		return nil, snap, &ValidationError{Err: err}
	}
	return plan, snap, nil
}

// ValidationError marks a client mistake (bad ops) rather than a server or
// ClickHouse failure.
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// clusterSettings make ON CLUSTER statements return one row per host and
// never throw for a failed host, so each host's outcome can be shown.
var clusterSettings = map[string]string{
	"distributed_ddl_output_mode":  "never_throw",
	"distributed_ddl_task_timeout": "25",
	"wait_end_of_query":            "1",
}

// Apply runs the statements in order and stops at the first failure; the
// rest are reported as skipped. onResult is called after each statement.
func Apply(ctx context.Context, ex Executor, plan *Plan, onResult func(Result)) []Result {
	results := make([]Result, 0, len(plan.Statements))
	failed := false
	for _, s := range plan.Statements {
		r := Result{Seq: s.Seq, SQL: s.SQL}
		if failed {
			r.Status = StatusSkipped
			results = append(results, r)
			continue
		}
		var settings map[string]string
		if plan.Cluster != "" {
			settings = clusterSettings
		}
		rows, err := ex.Query(ctx, s.SQL, settings)
		if err != nil {
			r.Status, r.Error = StatusFailed, shortError(err)
			if IsAlreadyExists(err) {
				r.Code = "already_exists"
			}
		} else {
			r.Status = StatusApplied
			if plan.Cluster != "" {
				r.Hosts = hostResults(rows)
				for _, h := range r.Hosts {
					// A host that did not finish leaves the outcome unknown,
					// so it stops the run like a failure.
					if h.Status == StatusFailed || h.Status == StatusPending {
						r.Status = StatusFailed
						if r.Error == "" {
							r.Error = h.Host + ": " + h.Error
						}
					}
				}
			}
		}
		if r.Status == StatusFailed {
			failed = true
		}
		results = append(results, r)
		if onResult != nil {
			onResult(r)
		}
	}
	return results
}

// hostResults parses the distributed DDL status rows.
func hostResults(rows []map[string]any) []HostResult {
	out := []HostResult{}
	remaining := uint64(0)
	for _, row := range rows {
		host := str(row, "host")
		if host == "" {
			continue
		}
		if p := str(row, "port"); p != "" {
			host += ":" + p
		}
		h := HostResult{Host: host, Status: StatusApplied}
		if row["status"] == nil {
			h.Status, h.Error = StatusPending, "did not finish within the timeout"
		} else if st := str(row, "status"); st != "0" {
			h.Status = StatusFailed
			h.Error = shortError(fmt.Errorf("%s", str(row, "error")))
			if h.Error == "" {
				h.Error = "status " + st
			}
		}
		remaining = u64(row, "num_hosts_remaining")
		out = append(out, h)
	}
	if remaining > 0 {
		out = append(out, HostResult{Host: fmt.Sprintf("%d more", remaining), Status: StatusPending, Error: "did not finish within the timeout"})
	}
	return out
}

// IsAlreadyExists reports ClickHouse error 493 (ACCESS_ENTITY_ALREADY_EXISTS).
func IsAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "ACCESS_ENTITY_ALREADY_EXISTS") || strings.Contains(msg, "Code: 493.")
}
