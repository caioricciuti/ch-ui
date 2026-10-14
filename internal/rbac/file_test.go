// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// roundTrip checks st survives YAML and JSON unchanged.
func roundTrip(t *testing.T, label string, st *State) {
	t.Helper()
	src := FileSource{Connection: "prod", Host: "ch-1", ExportedAt: "2026-10-14T10:00:00Z"}
	y, err := MarshalFileYAML(st, src, "header line\nsecond line")
	if err != nil {
		t.Fatalf("%s: yaml: %v", label, err)
	}
	j, err := MarshalFileJSON(st, src)
	if err != nil {
		t.Fatalf("%s: json: %v", label, err)
	}
	for format, b := range map[string][]byte{"yaml": y, "json": j} {
		f, err := ParseFile(b)
		if err != nil {
			t.Fatalf("%s: parse %s: %v\n%s", label, format, err, b)
		}
		if !reflect.DeepEqual(f.State, st) {
			t.Fatalf("%s: %s round trip differs\n got %s\nwant %s\n%s", label, format, stateJSON(f.State), stateJSON(st), b)
		}
		if !reflect.DeepEqual(f.Source, src) {
			t.Fatalf("%s: %s source = %+v", label, format, f.Source)
		}
	}
}

func TestFileRoundTripFixtures(t *testing.T) {
	roundTrip(t, "rich", FromSnapshot(richSnapshot()))
	roundTrip(t, "empty", (&State{Version: StateVersion}).Canonical())
	for _, c := range printedCases {
		s := catalogSnapshot(t)
		s.Roles = []Entity{{Name: "r", Storage: "local_directory"}}
		s.Grants = printedGrants(t, c.file)
		roundTrip(t, c.file, FromSnapshot(s))
	}
	// Names that need quoting or a qualified grantee key.
	odd := &State{
		Version: StateVersion,
		Roles:   []StateRole{{Name: "all"}, {Name: "twin"}, {Name: "role:x"}, {Name: "multi", Profiles: []string{"p1", "p2"}}, {Name: "yes"}},
		Users:   []StateUser{{Name: "twin", DefaultRoles: []string{"all"}}, {Name: "none", DefaultAll: true, DefaultExcept: []string{"yes"}}},
		Grants: []GranteeGrants{
			{Type: "role", Name: "twin", Privileges: []StateGrant{{Access: []string{"SELECT"}, Database: "my.db", Table: "t`1"}}},
			{Type: "user", Name: "twin", Privileges: []StateGrant{{Access: []string{"INSERT"}, Database: "*x", Table: " pad"}}},
			{Type: "role", Name: "role:x", Privileges: []StateGrant{{Access: []string{"SELECT"}, Database: "d", Table: "t", Column: "a"}, {Access: []string{"SELECT"}, Database: "d", Table: "t", Column: "b"}}},
			{Type: "role", Name: "unlisted", Privileges: []StateGrant{{Access: []string{"SHOW TABLES"}, Database: "d", Revoke: true, GrantOption: true}}},
		},
		RoleGrants:  []StateRoleGrant{{Type: "user", Name: "twin", Role: "all", AdminOption: true}, {Type: "role", Name: "twin", Role: "yes"}, {Type: "user", Name: "ghost", Role: "all"}},
		RowPolicies: []RowPolicySpec{{Name: "p", Database: "d.x", Table: "t", Using: "1"}, {Name: "q", Database: "d", Table: "t", Using: "on = 'off'", To: Assignees{Names: []string{"none", "all"}}}},
		Quotas:      []QuotaSpec{{Name: "q", Intervals: []QuotaInterval{{Duration: 90, Unit: "second", Limits: map[string]uint64{"queries": 1}}, {Duration: 1, Unit: "year"}}, To: Assignees{All: true}}},
		SettingsProfiles: []ProfileSpec{
			{Name: "p1"},
			{Name: "p2", Inherit: []string{"p1", "default"}, Settings: []ProfileSetting{{Name: "z", Value: sp("")}, {Name: "a", Min: sp("1"), Max: sp("no"), Writability: "CONST"}}},
		},
	}
	roundTrip(t, "odd names", odd.Canonical())
}

func TestFileRoundTripRandom(t *testing.T) {
	for seed := int64(0); seed < 120; seed++ {
		rng := rand.New(rand.NewSource(seed))
		s := randomize(baseFor(t, seed), rng, 10+rng.Intn(40))
		roundTrip(t, fmt.Sprintf("seed %d", seed), FromSnapshot(s))
	}
}

// fileExample is the format as documented: every section once.
const fileExample = `# comment lines are fine
chui_access: 1
source: {connection: prod, host: ch-1, exported_at: 2026-10-14T10:00:00Z}
roles:
  analyst: {profile: readonly_p}
  auditor:
users:
  alice: {default_roles: all, except: [auditor]}
  bob: {default_roles: [analyst]}
grants:
  analyst:
    - {access: [SELECT], on: "analytics.*"}
    - {access: SELECT, on: pii.users, columns: [id, country]}
    - {access: [SELECT], on: pii.users, columns: [email], revoke: true}
  role:auditor:
    - {access: [SHOW TABLES], on: "*.*", grant_option: true}
role_grants:
  alice: [analyst, {role: auditor, admin_option: true}]
row_policies:
  - {name: tenant, on: analytics.events, using: "tenant_id = 7", to: [analyst]}
quotas:
  daily:
    keyed_by: user_name
    intervals:
      - {every: 1 day, limits: {queries: 1000, read_rows: 1000000}}
      - {every: 60 minutes, randomized: true}
    to: all
    except: [bob]
settings_profiles:
  readonly_p:
    inherit: [default]
    settings:
      - {name: readonly, value: 1, writability: CONST}
      - {name: max_threads, max: "8"}
`

func TestFileExample(t *testing.T) {
	f, err := ParseFile([]byte(fileExample))
	if err != nil {
		t.Fatal(err)
	}
	want := (&State{
		Version: StateVersion,
		Roles:   []StateRole{{Name: "analyst", Profiles: []string{"readonly_p"}}, {Name: "auditor"}},
		Users:   []StateUser{{Name: "alice", DefaultAll: true, DefaultExcept: []string{"auditor"}}, {Name: "bob", DefaultRoles: []string{"analyst"}}},
		Grants: []GranteeGrants{
			{Type: "role", Name: "analyst", Privileges: []StateGrant{
				{Access: []string{"SELECT"}, Database: "analytics"},
				{Access: []string{"SELECT"}, Database: "pii", Table: "users", Column: "id"},
				{Access: []string{"SELECT"}, Database: "pii", Table: "users", Column: "country"},
				{Access: []string{"SELECT"}, Database: "pii", Table: "users", Column: "email", Revoke: true},
			}},
			{Type: "role", Name: "auditor", Privileges: []StateGrant{{Access: []string{"SHOW TABLES"}, GrantOption: true}}},
		},
		RoleGrants:  []StateRoleGrant{{Type: "user", Name: "alice", Role: "analyst"}, {Type: "user", Name: "alice", Role: "auditor", AdminOption: true}},
		RowPolicies: []RowPolicySpec{{Name: "tenant", Database: "analytics", Table: "events", Using: "tenant_id = 7", To: Assignees{Names: []string{"analyst"}}}},
		Quotas: []QuotaSpec{{Name: "daily", KeyedBy: "user_name", To: Assignees{All: true, Except: []string{"bob"}}, Intervals: []QuotaInterval{
			{Duration: 1, Unit: "day", Limits: map[string]uint64{"queries": 1000, "read_rows": 1000000}},
			{Duration: 60, Unit: "minute", Randomized: true},
		}}},
		SettingsProfiles: []ProfileSpec{{Name: "readonly_p", Inherit: []string{"default"}, Settings: []ProfileSetting{
			{Name: "readonly", Value: sp("1"), Writability: "CONST"}, {Name: "max_threads", Max: sp("8")},
		}}},
	}).Canonical()
	if !reflect.DeepEqual(f.State, want) {
		t.Fatalf("got  %s\nwant %s", stateJSON(f.State), stateJSON(want))
	}
	if f.Source.Connection != "prod" || f.Source.Host != "ch-1" {
		t.Fatalf("source = %+v", f.Source)
	}
	// JSON input reads the same.
	j, _ := MarshalFileJSON(f.State, f.Source)
	g, err := ParseFile(j)
	if err != nil || !reflect.DeepEqual(g.State, want) {
		t.Fatalf("json: %v\n%s", err, j)
	}
}

func TestFileStrictErrors(t *testing.T) {
	cases := []struct{ name, file, want string }{
		{"empty", "  \n", "the file is empty"},
		{"not a map", "- a\n", "must be a mapping with chui_access"},
		{"no version", "roles: {}\n", "chui_access: missing"},
		{"newer version", "chui_access: 2\n", "chui_access: version 2 is newer"},
		{"zero version", "chui_access: 0\n", "chui_access: version 0 is not supported"},
		{"text version", "chui_access: one\n", "chui_access: must be a whole number"},
		{"unknown top key", "chui_access: 1\npasswords: {}\n", "passwords: unknown key"},
		{"unknown role key", "chui_access: 1\nroles:\n  a: {profile: p, password: x}\n", "roles.a.password: unknown key"},
		{"both profile forms", "chui_access: 1\nroles:\n  a: {profile: p, profiles: [q]}\n", "roles.a: use profile or profiles"},
		{"unknown user key", "chui_access: 1\nusers:\n  u: {default_roles: all, auth: sha256}\n", "users.u.auth: unknown key"},
		{"user without defaults", "chui_access: 1\nusers:\n  u: {}\n", "users.u.default_roles: missing"},
		{"bad default roles", "chui_access: 1\nusers:\n  u: {default_roles: some}\n", "users.u.default_roles: must be all, none or a list"},
		{"except without all", "chui_access: 1\nusers:\n  u: {default_roles: [r], except: [x]}\n", "users.u.except: except needs default_roles: all"},
		{"bad on", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT], on: analytics}\n", "grants.r[0].on: \"analytics\" must be *.*, db.* or db.table"},
		{"table without db", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT], on: \"*.t\"}\n", "grants.r[0].on: \"*.t\": a table needs a database"},
		{"unclosed backtick", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT], on: \"`db.t\"}\n", "unclosed backtick"},
		{"missing on", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT]}\n", "grants.r[0]: on is required"},
		{"bad privilege", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [\"SELECT; DROP\"], on: \"*.*\"}\n", "grants.r[0].access[0]"},
		{"grant key typo", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT], on: \"*.*\", grantoption: true}\n", "grants.r[0].grantoption: unknown key"},
		{"columns without table", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT], on: \"d.*\", columns: [c]}\n", "grants.r[0].columns: columns need a table"},
		{"bool as text", "chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [SELECT], on: \"*.*\", revoke: \"yes\"}\n", "grants.r[0].revoke: must be true or false"},
		{"unknown grantee", "chui_access: 1\ngrants:\n  r:\n    - {access: [SELECT], on: \"*.*\"}\n", "grants.r: \"r\" is listed under neither roles nor users"},
		{"ambiguous grantee", "chui_access: 1\nroles: {x: {}}\nusers: {x: {default_roles: none}}\nrole_grants:\n  x: [r]\n", "role_grants.x: \"x\" is both a role and a user"},
		{"role grant typo", "chui_access: 1\nroles: {r: {}}\nrole_grants:\n  r: [{role: a, admin: true}]\n", "role_grants.r[0].admin: unknown key"},
		{"policy on db", "chui_access: 1\nrow_policies:\n  - {name: p, on: \"d.*\", using: \"1\"}\n", "row_policies[0].on: a row policy is on one table"},
		{"policy without using", "chui_access: 1\nrow_policies:\n  - {name: p, on: d.t}\n", "row_policies[0]: using is required"},
		{"policy bad to", "chui_access: 1\nrow_policies:\n  - {name: p, on: d.t, using: \"1\", to: everyone}\n", "row_policies[0].to: must be all, none or a list"},
		{"quota bad unit", "chui_access: 1\nquotas:\n  q: {intervals: [{every: 1 fortnight}]}\n", "quotas.q.intervals[0].every: unit \"fortnight\" is not supported"},
		{"quota bad limit", "chui_access: 1\nquotas:\n  q: {intervals: [{every: 1 hour, limits: {rows: 1}}]}\n", "quotas.q.intervals[0].limits.rows: unknown quota limit"},
		{"quota negative", "chui_access: 1\nquotas:\n  q: {intervals: [{every: 1 hour, limits: {queries: -1}}]}\n", "quotas.q.intervals[0].limits.queries: must be a whole number"},
		{"quota bad key", "chui_access: 1\nquotas:\n  q: {keyed_by: host}\n", "quotas.q.keyed_by: \"host\" is not supported"},
		{"profile bad setting", "chui_access: 1\nsettings_profiles:\n  p: {settings: [{name: \"max threads\"}]}\n", "settings_profiles.p.settings[0].name"},
		{"profile bad writability", "chui_access: 1\nsettings_profiles:\n  p: {settings: [{name: x, writability: frozen}]}\n", "settings_profiles.p.settings[0].writability"},
		{"duplicate key", "chui_access: 1\nroles:\n  a: {}\n  a: {}\n", "roles.a: listed twice"},
		{"dotted key path", "chui_access: 1\nroles:\n  \"a.b\": {bad: 1}\n", `roles["a.b"].bad: unknown key`},
		{"two documents", "chui_access: 1\n---\nchui_access: 1\n", "more than one YAML document"},
		{"bad json", `{"chui_access": 1, "roles": [}`, "not valid YAML or JSON"},
		{"json unknown key", `{"chui_access": 1, "roles": {"a": {"profle": "p"}}}`, "roles.a.profle: unknown key"},
	}
	for _, c := range cases {
		_, err := ParseFile([]byte(c.file))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		var ferr *FileError
		if err != nil && !errors.As(err, &ferr) {
			t.Errorf("%s: %T is not a FileError", c.name, err)
		}
	}
}

// An export of a server, imported into the same server, changes nothing,
// with or without prune.
func TestImportOwnExportIsNoOp(t *testing.T) {
	check := func(label string, s *Snapshot) {
		st, _ := ExportState(s)
		for _, format := range []string{"yaml", "json"} {
			var b []byte
			var err error
			if format == "yaml" {
				b, err = MarshalFileYAML(st, FileSource{}, ExportHeader(FileSource{}, nil))
			} else {
				b, err = MarshalFileJSON(st, FileSource{})
			}
			if err != nil {
				t.Fatal(err)
			}
			f, err := ParseFile(b)
			if err != nil {
				t.Fatalf("%s %s: %v", label, format, err)
			}
			for _, prune := range []Prune{{}, PruneAll()} {
				plan, err := PlanImport(s, f.State, ImportOpts{Prune: prune})
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.Ops) != 0 {
					t.Fatalf("%s %s prune=%v: %d ops\n%s", label, format, prune, len(plan.Ops), renderOps(plan.Ops))
				}
			}
		}
	}
	check("rich", richSnapshot())
	for seed := int64(0); seed < 60; seed++ {
		rng := rand.New(rand.NewSource(seed))
		check(fmt.Sprintf("seed %d", seed), randomize(baseFor(t, seed), rng, 10+rng.Intn(40)))
	}
}

func TestExportLeavesOutUsersXML(t *testing.T) {
	s := richSnapshot()
	st, excluded := ExportState(s)
	if got := strings.Join(excluded, ", "); got != "role xml_role, user default, settings profile default" {
		t.Fatalf("excluded = %s", got)
	}
	for _, g := range st.Grants {
		if g.Name == "default" {
			t.Fatal("grants of a users.xml user exported")
		}
	}
	header := ExportHeader(FileSource{Connection: "prod", Host: "ch-1"}, excluded)
	y, err := MarshalFileYAML(st, FileSource{Connection: "prod"}, header)
	if err != nil {
		t.Fatal(err)
	}
	text := string(y)
	for _, want := range []string{"# Left out because they live in users.xml", "#   user default", "# Passwords and authentication settings are never exported."} {
		if !strings.Contains(text, want) {
			t.Fatalf("header lacks %q:\n%s", want, text)
		}
	}
	sql := ExportSQL(s, st, header)
	for _, want := range []string{"CREATE ROLE `analyst`", "GRANT SELECT, SHOW TABLES ON `sales`.* TO `analyst`", "ALTER USER `bob` DEFAULT ROLE ALL EXCEPT `admin`", "CREATE ROW POLICY `tenant`", "-- Left out because"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("sql lacks %q:\n%s", want, sql)
		}
	}
	for _, out := range [][]byte{y, []byte(sql)} {
		low := strings.ToLower(string(out))
		for _, secret := range []string{"identified", "password_sha", "sha256_hash", "auth_type", "bcrypt"} {
			if strings.Contains(low, secret) {
				t.Fatalf("export mentions %s:\n%s", secret, out)
			}
		}
	}
}

func importFixture() *Snapshot {
	s := reconcileBase()
	s.Roles = append(s.Roles, Entity{Name: "stg_analyst", Storage: "local_directory"}, Entity{Name: "extra", Storage: "local_directory"})
	s.Grants = append(s.Grants, GrantRow{RoleName: "extra", Access: "SELECT", Database: "stg"})
	return s
}

const importFile = `chui_access: 1
roles:
  analyst: {}
  newrole: {}
users:
  alice: {default_roles: [analyst]}
  nobody: {default_roles: none}
grants:
  analyst:
    - {access: [SELECT], on: "prod.*"}
    - {access: [SELECT], on: "gone.*"}
    - {access: [SELECT], on: prod.secret, revoke: true}
    - {access: [INSERT], on: "gone.t", revoke: true}
  newrole:
    - {access: [SELECT], on: "prod.t"}
role_grants:
  alice: [analyst]
row_policies:
  - {name: tenant, on: prod.events, using: "1", to: [analyst]}
  - {name: old, on: gone.events, using: "1", to: [analyst]}
`

func parseImport(t *testing.T) *State {
	t.Helper()
	f, err := ParseFile([]byte(importFile))
	if err != nil {
		t.Fatal(err)
	}
	return f.State
}

func TestImportRenameAndDatabases(t *testing.T) {
	cur := importFixture()
	plan, err := PlanImport(cur, parseImport(t), ImportOpts{
		Rename:    Rename{Principals: map[string]string{"analyst": "stg_analyst"}, Databases: map[string]string{"prod": "stg"}},
		Databases: map[string]bool{"stg": true, "system": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql := renderOps(plan.Ops)
	for _, want := range []string{
		"CREATE ROLE `newrole`",
		"GRANT SELECT ON `stg`.* TO `stg_analyst`",
		"REVOKE SELECT ON `stg`.`secret` FROM `stg_analyst`",
		"GRANT `stg_analyst` TO `alice`",
		"ALTER USER `alice` DEFAULT ROLE `stg_analyst`",
		"CREATE ROW POLICY `tenant` ON `stg`.`events` FOR SELECT USING (1) AS PERMISSIVE TO `stg_analyst`",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("plan lacks %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "prod") || strings.Contains(sql, "`analyst`") || strings.Contains(sql, "GRANT SELECT ON `gone`") {
		t.Errorf("rename or database check missed:\n%s", sql)
	}
	reasons := map[string]string{}
	for _, s := range plan.Skipped {
		reasons[s.Kind+" "+s.Name] = s.Reason
	}
	if !strings.Contains(reasons["role stg_analyst"], `database "gone"`) || !strings.Contains(reasons["row_policy old ON gone.events"], "does not exist") || reasons["user nobody"] != "user not found" {
		t.Errorf("skipped = %+v", plan.Skipped)
	}
	// Partial revokes on a missing database stay: they only narrow access.
	kept, _ := dropMissingDatabases(parseImport(t), map[string]bool{"prod": true}, nil)
	if got := stateJSON(kept); !strings.Contains(got, `"database": "gone"`) || strings.Count(got, `"gone"`) != 1 {
		t.Errorf("missing database filter:\n%s", got)
	}
	if plan.Summary["roles"] != 1 || plan.Summary["row_policies"] != 1 || plan.OverCap {
		t.Errorf("summary = %+v", plan.Summary)
	}
	// The plan composes.
	if _, err := Compose(plan.Ops, cur, Catalog{}, "", GuardContext{}); err != nil {
		t.Fatal(err)
	}

	// Renames that merge two names, or bad names, are refused.
	for _, rn := range []Rename{
		{Principals: map[string]string{"analyst": "alice"}},
		{Principals: map[string]string{"analyst": "x", "newrole": "x"}},
		{Principals: map[string]string{"analyst": ""}},
		{Databases: map[string]string{"prod": "gone"}},
	} {
		if _, err := PlanImport(cur, parseImport(t), ImportOpts{Rename: rn}); err == nil {
			t.Errorf("rename %+v accepted", rn)
		}
	}
}

func TestImportKinds(t *testing.T) {
	cur := importFixture()
	if _, err := PlanImport(cur, parseImport(t), ImportOpts{Kinds: []string{"secrets"}}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	only := func(kinds ...string) *ImportPlan {
		plan, err := PlanImport(cur, parseImport(t), ImportOpts{Kinds: kinds, Prune: PruneAll(), Rename: Rename{Principals: map[string]string{"analyst": "stg_analyst"}}})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	// Grants alone: the existing role gets them, the new role is skipped
	// and nothing is created, dropped or revoked outside grants.
	plan := only("grants")
	for _, op := range plan.Ops {
		if OpSection(op.Kind) != "grants" {
			t.Fatalf("grants import has %s:\n%s", op.Kind, renderOps(plan.Ops))
		}
	}
	if !strings.Contains(renderOps(plan.Ops), "GRANT SELECT ON `prod`.* TO `stg_analyst`") {
		t.Fatalf("grants import:\n%s", renderOps(plan.Ops))
	}
	skipped := false
	for _, s := range plan.Skipped {
		if s.Kind == KindRole && s.Name == "newrole" && strings.Contains(s.Reason, "roles are not part of this import") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("newrole not skipped: %+v", plan.Skipped)
	}
	// Roles with prune: the new role is created and extra, not in the file,
	// is dropped; grants are untouched.
	plan = only("roles")
	if got := renderOps(plan.Ops); got != "CREATE ROLE `newrole`\nDROP ROLE IF EXISTS `extra`" {
		t.Fatalf("roles import:\n%s", got)
	}
	// Row policies and users only.
	plan = only("row_policies", "users")
	for _, op := range plan.Ops {
		if s := OpSection(op.Kind); s != "row_policies" && s != "users" {
			t.Fatalf("policies+users import has %s:\n%s", op.Kind, renderOps(plan.Ops))
		}
	}
}

func TestImportPrune(t *testing.T) {
	cur := importFixture()
	st := parseImport(t)
	off, _ := PlanImport(cur, st, ImportOpts{})
	on, _ := PlanImport(cur, st, ImportOpts{Prune: Prune{Roles: true}})
	if strings.Contains(renderOps(off.Ops), "DROP") {
		t.Fatalf("prune off drops:\n%s", renderOps(off.Ops))
	}
	got := renderOps(on.Ops)
	if !strings.Contains(got, "DROP ROLE IF EXISTS `extra`") || !strings.Contains(got, "DROP ROLE IF EXISTS `stg_analyst`") || strings.Contains(got, "xml_role") {
		t.Fatalf("prune roles:\n%s", got)
	}
}

func TestImportOverCap(t *testing.T) {
	st := &State{Version: StateVersion}
	for i := 0; i < MaxImportOps+5; i++ {
		st.Roles = append(st.Roles, StateRole{Name: fmt.Sprintf("r%03d", i)})
	}
	plan, err := PlanImport(reconcileBase(), st, ImportOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OverCap || len(plan.Ops) != MaxImportOps+5 || !strings.Contains(plan.Message, "Import fewer kinds") || !strings.Contains(plan.Message, "roles 205") {
		t.Fatalf("over cap = %v %q", plan.OverCap, plan.Message)
	}
}

func TestValidateImportOpsNamesEntry(t *testing.T) {
	ops := []Op{privOp(Target{Type: "role", Name: "analyst"}, []string{"SELEC"}, scope{db: "sales"}, false, false)}
	err := ValidateImportOps(ops, Catalog{Privileges: map[string]bool{"SELECT": true}})
	if err == nil || !strings.Contains(err.Error(), "grants of role analyst on sales.*") {
		t.Fatalf("err = %v", err)
	}
	p := ProfileSpec{Name: "p", Settings: []ProfileSetting{{Name: "no_such_setting", Value: sp("1")}}}
	err = ValidateImportOps([]Op{{Kind: OpCreateSettingsProfile, SettingsProfile: &p}}, Catalog{Settings: map[string]bool{}})
	if err == nil || !strings.Contains(err.Error(), "settings_profiles.p") {
		t.Fatalf("err = %v", err)
	}
}
