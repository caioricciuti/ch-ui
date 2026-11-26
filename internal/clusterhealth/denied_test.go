// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// A user without grants on system.replicas and system.disks gets those two
// tables back as denied; a timeout on merges is a failure, not a grant.
func TestDeniedTablesFromCollection(t *testing.T) {
	denied := func(table string) error {
		return errors.New("ClickHouse error: Code: 497. DB::Exception: restricted: Not enough privileges. To execute this query, it's necessary to have the grant SELECT ON " + table + ". (ACCESS_DENIED)")
	}
	exec := func(sql string) ([]map[string]interface{}, error) {
		switch {
		case strings.Contains(sql, "system.replicas"):
			return nil, denied("system.replicas")
		case strings.Contains(sql, "system.disks"):
			return nil, denied("system.disks")
		case strings.Contains(sql, "system.merges"):
			return nil, errors.New("ClickHouse error: Code: 159. DB::Exception: Timeout exceeded. (TIMEOUT_EXCEEDED)")
		}
		return []map[string]interface{}{{"node": "ch-1"}}, nil
	}

	col, _ := CollectSamples(exec, "", 30, "2026-11-20T00:00:00Z")
	got := DeniedTables(col.Errors)
	want := []string{"system.disks", "system.replicas"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DeniedTables = %v, want %v (errors %v)", got, want, col.Errors)
	}
	if _, ok := col.Errors[MetricMerges]; !ok {
		t.Fatalf("merges timeout should still be recorded as an error")
	}
}

// The probe reports the tables the summary never reads: a denied one is a
// missing grant, a table the deployment does not have is not.
func TestProbeDenied(t *testing.T) {
	exec := func(sql string) ([]map[string]interface{}, error) {
		switch {
		case strings.Contains(sql, "system.clusters"), strings.Contains(sql, "system.replication_queue"):
			return nil, errors.New("ClickHouse error: Code: 497. DB::Exception: restricted: Not enough privileges. (ACCESS_DENIED)")
		case strings.Contains(sql, "system.zookeeper_connection"):
			return nil, errors.New("ClickHouse error: Code: 60. DB::Exception: Unknown table expression identifier 'system.zookeeper_connection'. (UNKNOWN_TABLE)")
		}
		return nil, nil
	}
	got := ProbeDenied(exec)
	want := []string{"system.clusters", "system.replication_queue"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProbeDenied = %v, want %v", got, want)
	}
}

// A denied read of system.clusters must reach the caller as an error, not
// as a single node.
func TestResolveClusterErrKeepsDenial(t *testing.T) {
	exec := func(string) ([]map[string]interface{}, error) {
		return nil, errors.New("ClickHouse error: Code: 497. DB::Exception: restricted: Not enough privileges. (ACCESS_DENIED)")
	}
	name, err := ResolveClusterErr(exec)
	if name != "" || err == nil || !IsAccessDenied(err.Error()) {
		t.Fatalf("ResolveClusterErr = %q, %v; want an access denied error", name, err)
	}
	if _, ok := ResolveCluster(exec); ok {
		t.Fatalf("ResolveCluster should report the failed lookup")
	}
}

func TestDeniedTablesEmpty(t *testing.T) {
	if got := DeniedTables(nil); got == nil || len(got) != 0 {
		t.Fatalf("DeniedTables(nil) = %#v, want empty non-nil slice", got)
	}
}
