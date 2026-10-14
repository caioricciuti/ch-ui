// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// richSnapshot holds one of everything State carries, in the shapes
// ReadSnapshot produces.
func richSnapshot() *Snapshot {
	return &Snapshot{
		Roles: []Entity{{Name: "analyst", Storage: "local_directory"}, {Name: "admin", Storage: "replicated"}, {Name: "xml_role", Storage: "users_xml"}},
		Users: []UserEntity{
			{Name: "alice", Storage: "local_directory", DefaultRoles: []string{"analyst"}, DefaultExcpt: []string{}},
			{Name: "bob", Storage: "local_directory", DefaultAll: true, DefaultRoles: []string{}, DefaultExcpt: []string{"admin"}},
			{Name: "default", Storage: "users_xml", DefaultAll: true},
		},
		RoleGrants: []RoleGrant{
			{UserName: "alice", GrantedRole: "analyst", IsDefault: true},
			{UserName: "bob", GrantedRole: "admin", AdminOption: true},
			{RoleName: "admin", GrantedRole: "analyst"},
		},
		Grants: []GrantRow{
			{RoleName: "analyst", Access: "SELECT", Database: "sales"},
			{RoleName: "analyst", Access: "SHOW TABLES", Database: "sales"},
			{RoleName: "analyst", Access: "SELECT", Database: "sales", Table: "secrets", PartialRevoke: true},
			{RoleName: "analyst", Access: "SELECT", Database: "sales", Table: "orders", Column: "total", GrantOption: true},
			{RoleName: "admin", Access: "ALL", GrantOption: true},
			{RoleName: "admin", Access: "ALTER", Database: "system", PartialRevoke: true, GrantOption: true},
			{UserName: "alice", Access: "INSERT", Database: "sales", Table: "orders"},
			{UserName: "default", Access: "ALL", GrantOption: true},
		},
		RowPolicies: []RowPolicy{{FullName: "tenant ON sales.orders", Storage: "local_directory", RowPolicySpec: RowPolicySpec{
			Name: "tenant", Database: "sales", Table: "orders", Using: "tenant = 'a'", Restrictive: true, To: Assignees{All: true, Except: []string{"admin", "bob"}},
		}}},
		Quotas: []Quota{{Storage: "local_directory", QuotaSpec: QuotaSpec{Name: "q", KeyedBy: "user_name", Intervals: []QuotaInterval{
			{Duration: 1, Unit: "day", Limits: map[string]uint64{"queries": 1000, "read_rows": 0}},
			{Duration: 60, Unit: "minute", Randomized: true, Limits: map[string]uint64{"queries": 100, "errors": 5}},
			{Duration: 1, Unit: "week", Limits: map[string]uint64{}},
		}, To: Assignees{Names: []string{"analyst", "alice"}}}}},
		SettingsProfiles: []SettingsProfile{
			{Storage: "users_xml", ProfileSpec: ProfileSpec{Name: "default", Settings: []ProfileSetting{{Name: "max_threads", Value: sp("8")}}}},
			{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "limited", Inherit: []string{"default"}, Settings: []ProfileSetting{
				{Name: "max_memory_usage", Value: sp("1000000"), Max: sp("2000000"), Writability: "WRITABLE"},
				{Name: "readonly", Value: sp(""), Writability: "CONST"},
			}, To: Assignees{Names: []string{"bob"}}}},
		},
		RoleProfiles: map[string][]string{"analyst": {"limited"}, "ghost": {"limited"}},
		Capabilities: Capabilities{User: "default", AccessManagement: true},
		Privileges:   []Privilege{{Name: "SELECT", Level: "COLUMN"}},
		QuotaUsage:   []QuotaUsage{{Quota: "q", Key: "alice", Duration: 3600}},
		Host:         "node-1",
	}
}

func TestStateFromSnapshot(t *testing.T) {
	st := FromSnapshot(richSnapshot())
	if st.Version != StateVersion {
		t.Fatalf("version = %d", st.Version)
	}
	if got := []string{st.Roles[0].Name, st.Roles[1].Name, st.Roles[2].Name}; strings.Join(got, ",") != "admin,analyst,xml_role" {
		t.Fatalf("roles = %v", got)
	}
	if !reflect.DeepEqual(st.Roles[1].Profiles, []string{"limited"}) || st.Roles[0].Profiles != nil {
		t.Fatalf("role profiles = %+v", st.Roles)
	}
	// DEFAULT ROLE ALL keeps only the exceptions, a list keeps only the list.
	if u := st.Users[1]; u.Name != "bob" || !u.DefaultAll || u.DefaultRoles != nil || !reflect.DeepEqual(u.DefaultExcept, []string{"admin"}) {
		t.Fatalf("bob = %+v", u)
	}
	if u := st.Users[0]; u.DefaultExcept != nil || !reflect.DeepEqual(u.DefaultRoles, []string{"analyst"}) {
		t.Fatalf("alice = %+v", u)
	}
	// Grantees sort by type then name; rows group by object, broad first.
	var grantees []string
	for _, g := range st.Grants {
		grantees = append(grantees, g.Type+":"+g.Name)
	}
	if strings.Join(grantees, ",") != "role:admin,role:analyst,user:alice,user:default" {
		t.Fatalf("grantees = %v", grantees)
	}
	analyst := st.Grants[1].Privileges
	want := []StateGrant{
		{Access: []string{"SELECT", "SHOW TABLES"}, Database: "sales"},
		{Access: []string{"SELECT"}, Database: "sales", Table: "secrets", Revoke: true},
		{Access: []string{"SELECT"}, Database: "sales", Table: "orders", Column: "total", GrantOption: true},
	}
	if !reflect.DeepEqual(analyst, want) {
		t.Fatalf("analyst grants\n got %+v\nwant %+v", analyst, want)
	}
	// Quota intervals sort by length in their largest unit, zero limits go.
	q := st.Quotas[0]
	if q.Intervals[0].Duration != 1 || q.Intervals[0].Unit != "hour" || q.Intervals[1].Unit != "day" || q.Intervals[2].Limits != nil {
		t.Fatalf("intervals = %+v", q.Intervals)
	}
	if _, ok := q.Intervals[1].Limits["read_rows"]; ok {
		t.Fatal("zero limit kept")
	}
	if !reflect.DeepEqual(q.To.Names, []string{"alice", "analyst"}) {
		t.Fatalf("quota to = %+v", q.To)
	}
	// Profile settings and inherits keep their order.
	if p := st.SettingsProfiles[1]; p.Settings[0].Name != "max_memory_usage" || p.Settings[1].Name != "readonly" || *p.Settings[1].Value != "" {
		t.Fatalf("profile = %+v", p)
	}
}

func TestStateRoundTrips(t *testing.T) {
	st := FromSnapshot(richSnapshot())

	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON State
	if err := json.Unmarshal(b, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&fromJSON, st) {
		t.Fatalf("JSON round trip\n got %+v\nwant %+v", fromJSON, *st)
	}

	y, err := yaml.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML State
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&fromYAML, st) {
		t.Fatalf("YAML round trip\n got %+v\nwant %+v\n%s", fromYAML, *st, y)
	}
	for _, key := range []string{"role_grants:", "row_policies:", "settings_profiles:", "keyed_by:", "default_roles_except:", "grant_option:", "admin_option:"} {
		if !strings.Contains(string(y), key) {
			t.Errorf("YAML has no %s key:\n%s", key, y)
		}
	}
	if st.Hash() != fromJSON.Hash() || st.Hash() != fromYAML.Hash() {
		t.Fatal("hash changed in a round trip")
	}
	// A canonical state is its own canonical form.
	if !reflect.DeepEqual(st.Canonical(), st) {
		t.Fatal("Canonical is not idempotent")
	}
}

func TestStateHashStable(t *testing.T) {
	base := richSnapshot()
	want := FromSnapshot(base).Hash()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		s := cloneSnapshot(base)
		shuffle := func(n int, swap func(i, j int)) { rng.Shuffle(n, swap) }
		shuffle(len(s.Roles), func(i, j int) { s.Roles[i], s.Roles[j] = s.Roles[j], s.Roles[i] })
		shuffle(len(s.Users), func(i, j int) { s.Users[i], s.Users[j] = s.Users[j], s.Users[i] })
		shuffle(len(s.Grants), func(i, j int) { s.Grants[i], s.Grants[j] = s.Grants[j], s.Grants[i] })
		shuffle(len(s.RoleGrants), func(i, j int) { s.RoleGrants[i], s.RoleGrants[j] = s.RoleGrants[j], s.RoleGrants[i] })
		iv := s.Quotas[0].Intervals
		shuffle(len(iv), func(i, j int) { iv[i], iv[j] = iv[j], iv[i] })
		to := s.Quotas[0].To.Names
		shuffle(len(to), func(i, j int) { to[i], to[j] = to[j], to[i] })
		// Rebuild the limits maps so their insertion order differs.
		for k := range iv {
			m := map[string]uint64{}
			keys := make([]string, 0, len(iv[k].Limits))
			for res := range iv[k].Limits {
				keys = append(keys, res)
			}
			shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
			for _, res := range keys {
				m[res] = iv[k].Limits[res]
			}
			iv[k].Limits = m
		}
		if got := FromSnapshot(s).Hash(); got != want {
			t.Fatalf("shuffle %d changed the hash", i)
		}
	}
	// The same interval in other units, duplicated entries and an
	// uncanonical grant grouping hash the same.
	st := FromSnapshot(base)
	st.Quotas[0].Intervals[0] = QuotaInterval{Duration: 3600, Unit: "second", Randomized: true, Limits: map[string]uint64{"queries": 100, "errors": 5, "result_rows": 0}}
	st.Roles = append(st.Roles, st.Roles[0])
	st.Grants = append(st.Grants, GranteeGrants{Type: "role", Name: "analyst", Privileges: []StateGrant{{Access: []string{"SHOW TABLES", "SELECT"}, Database: "sales"}}})
	if st.Hash() != want {
		t.Fatal("equivalent state hashes differently")
	}
	st.Users[0].DefaultRoles = nil
	if st.Hash() == want {
		t.Fatal("hash ignores default roles")
	}
}

// catalogSnapshot carries system.privileges of ClickHouse 25.8, read from
// testdata.
func catalogSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	b, err := os.ReadFile("testdata/privileges_25_8.tsv")
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{RoleProfiles: map[string][]string{}, PrivilegeParents: map[string]string{}, PrivilegeLevels: map[string]string{}}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Split(line, "\t")
		if f[1] != "" {
			s.PrivilegeLevels[f[0]] = f[1]
		}
		if f[2] != "" {
			s.PrivilegeParents[f[0]] = f[2]
		}
	}
	return s
}

// printedGrants reads system.grants rows of role r as ClickHouse 25.8
// printed them (access, database, table, column, partial, grant option).
func printedGrants(t *testing.T, file string) []GrantRow {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var rows []GrantRow
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Split(line, "\t")
		rows = append(rows, GrantRow{RoleName: "r", Access: f[0], Database: f[1], Table: f[2], Column: f[3], PartialRevoke: f[4] == "1", GrantOption: f[5] == "1"})
	}
	return rows
}

func renderRows(rows []GrantRow) string {
	var lines []string
	for _, g := range rows {
		lines = append(lines, renderGrantRow(g))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// The cases, each with what ClickHouse printed after the statements, and
// the same grants written as groups with partial revokes.
var printedCases = []struct {
	file  string
	sql   []Op
	group []GrantRow
}{
	{"grants_all_db.tsv",
		[]Op{privOp(Target{Type: "role", Name: "r"}, []string{"ALL"}, scope{db: "db"}, false, false)},
		[]GrantRow{{RoleName: "r", Access: "ALL", Database: "db"}}},
	{"grants_all_db_minus_select.tsv",
		[]Op{privOp(Target{Type: "role", Name: "r"}, []string{"ALL"}, scope{db: "db"}, false, false), privOp(Target{Type: "role", Name: "r"}, []string{"SELECT"}, scope{db: "db"}, true, false)},
		[]GrantRow{{RoleName: "r", Access: "ALL", Database: "db"}, {RoleName: "r", Access: "SELECT", Database: "db", PartialRevoke: true}}},
	{"grants_alter_db_minus_update.tsv",
		[]Op{privOp(Target{Type: "role", Name: "r"}, []string{"ALTER"}, scope{db: "db"}, false, false), privOp(Target{Type: "role", Name: "r"}, []string{"ALTER UPDATE"}, scope{db: "db"}, true, false)},
		[]GrantRow{{RoleName: "r", Access: "ALTER", Database: "db"}, {RoleName: "r", Access: "ALTER UPDATE", Database: "db", PartialRevoke: true}}},
	{"grants_alter_db_revoke_update_table.tsv",
		[]Op{privOp(Target{Type: "role", Name: "r"}, []string{"ALTER"}, scope{db: "db"}, false, false), privOp(Target{Type: "role", Name: "r"}, []string{"ALTER UPDATE"}, scope{db: "db", table: "t"}, true, false)},
		[]GrantRow{{RoleName: "r", Access: "ALTER", Database: "db"}, {RoleName: "r", Access: "ALTER UPDATE", Database: "db", Table: "t", PartialRevoke: true}}},
	{"grants_show_tables_db_all_table.tsv",
		[]Op{privOp(Target{Type: "role", Name: "r"}, []string{"SHOW TABLES"}, scope{db: "db"}, false, false), privOp(Target{Type: "role", Name: "r"}, []string{"ALL"}, scope{db: "db", table: "t"}, false, false)},
		[]GrantRow{{RoleName: "r", Access: "SHOW TABLES", Database: "db"}, {RoleName: "r", Access: "ALL", Database: "db", Table: "t"}}},
	{"grants_grant_option_column.tsv",
		[]Op{
			privOp(Target{Type: "role", Name: "r"}, []string{"SELECT"}, scope{db: "db"}, false, true),
			privOp(Target{Type: "role", Name: "r"}, []string{"SELECT"}, scope{db: "db", table: "t"}, true, true),
			privOp(Target{Type: "role", Name: "r"}, []string{"INSERT"}, scope{db: "db", table: "t", column: "c"}, false, false),
		},
		[]GrantRow{{RoleName: "r", Access: "SELECT", Database: "db", GrantOption: true}, {RoleName: "r", Access: "SELECT", Database: "db", Table: "t", PartialRevoke: true, GrantOption: true}, {RoleName: "r", Access: "INSERT", Database: "db", Table: "t", Column: "c"}}},
}

// Grants that mean the same hash the same however they were printed, and
// the canonical form is what ClickHouse prints.
func TestCanonicalWithNormalisesGrants(t *testing.T) {
	cat := catalogSnapshot(t)
	stateOf := func(rows []GrantRow) *State {
		s := cloneSnapshot(cat)
		s.Roles = []Entity{{Name: "r", Storage: "local_directory"}}
		s.Grants = rows
		return FromSnapshot(s)
	}
	for _, c := range printedCases {
		printed := printedGrants(t, c.file)
		a, b := stateOf(printed), stateOf(c.group)
		if a.Hash() != b.Hash() {
			t.Errorf("%s: printed and group forms hash differently\nprinted %s\ngroup   %s", c.file, stateJSON(a), stateJSON(b))
		}
		if got := renderRows(b.Grants[0].rows()); got != renderRows(printed) {
			t.Errorf("%s: canonical form is not what ClickHouse prints\n got\n%s\nwant\n%s", c.file, got, renderRows(printed))
		}
	}
	// Without a catalog the forms stay apart: there is nothing to expand
	// ALL with.
	plain := &Snapshot{RoleProfiles: map[string][]string{}, Grants: printedCases[1].group}
	if FromSnapshot(plain).Hash() == stateOf(printedCases[1].group).Hash() {
		t.Error("catalog made no difference")
	}
}

// The simulator, given the catalog, produces the rows ClickHouse printed
// after the same statements.
func TestApplyGrantMatchesClickHousePrinting(t *testing.T) {
	for _, c := range printedCases {
		s := catalogSnapshot(t)
		for _, op := range c.sql {
			applyOp(s, op)
		}
		if got, want := renderRows(s.Grants), renderRows(printedGrants(t, c.file)); got != want {
			t.Errorf("%s:\n got\n%s\nwant\n%s", c.file, got, want)
		}
	}
}
