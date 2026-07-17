// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package github

import (
	"reflect"
	"sort"
	"testing"
)

func TestFilterSQLFiles(t *testing.T) {
	// A dbt-style nested tree plus some noise (non-sql, directory entries,
	// a sibling path that shares a prefix).
	tree := []TreeEntry{
		{Path: "dbt/models", Type: "tree"},
		{Path: "dbt/models/sources.yml", Type: "blob"},
		{Path: "dbt/models/top_level.sql", Type: "blob"},
		{Path: "dbt/models/staging", Type: "tree"},
		{Path: "dbt/models/staging/stg_orders.sql", Type: "blob"},
		{Path: "dbt/models/marts/core/fct_orders.SQL", Type: "blob"}, // deep + uppercase ext
		{Path: "dbt/models/marts/core/schema.yml", Type: "blob"},
		{Path: "dbt/models2/other.sql", Type: "blob"}, // sibling sharing the prefix string
		{Path: "dbt/macros/helper.sql", Type: "blob"}, // outside the models path
		{Path: "root.sql", Type: "blob"},
	}

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{
			name:   "recurses into subdirectories under the path",
			prefix: "dbt/models",
			want: []string{
				"dbt/models/marts/core/fct_orders.SQL",
				"dbt/models/staging/stg_orders.sql",
				"dbt/models/top_level.sql",
			},
		},
		{
			name:   "trailing slash is equivalent",
			prefix: "dbt/models/",
			want: []string{
				"dbt/models/marts/core/fct_orders.SQL",
				"dbt/models/staging/stg_orders.sql",
				"dbt/models/top_level.sql",
			},
		},
		{
			name:   "prefix boundary does not leak into sibling dir",
			prefix: "dbt/macros",
			want:   []string{"dbt/macros/helper.sql"},
		},
		{
			name:   "empty prefix matches the whole repo",
			prefix: "",
			want: []string{
				"dbt/macros/helper.sql",
				"dbt/models/marts/core/fct_orders.SQL",
				"dbt/models/staging/stg_orders.sql",
				"dbt/models/top_level.sql",
				"dbt/models2/other.sql",
				"root.sql",
			},
		},
		{
			name:   "no matches under an unrelated path",
			prefix: "nope",
			want:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := paths(FilterSQLFiles(tree, tc.prefix))
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if len(got) == 0 && len(want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("FilterSQLFiles(%q):\n got  %v\n want %v", tc.prefix, got, want)
			}
		})
	}
}

func paths(entries []TreeEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}
