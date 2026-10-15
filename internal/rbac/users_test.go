// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const (
	testHash = "9bf6479634baeda1ad95722672002e2b38f95de4065b631448a2fa93fc5b7203"
	testSalt = "c0ffee5a1ted"
)

func createUserOp(name string) Op {
	return Op{Kind: OpCreateUser, Name: name, Auth: &UserAuth{Method: AuthSHA256Hash, Hash: testHash, Salt: testSalt}}
}

func TestCreateUserRenderMasksSecrets(t *testing.T) {
	op := createUserOp("ann")
	op.Hosts = []HostSpec{{Kind: HostLocal}, {Kind: HostIP, Value: "10.0.0.0/8"}, {Kind: HostLike, Value: "%.corp"}}
	op.Profile = "analysts"
	want := "CREATE USER `ann` ON CLUSTER `c1` IDENTIFIED WITH sha256_hash BY '***' SALT '***' HOST LOCAL, IP '10.0.0.0/8', LIKE '%.corp' SETTINGS PROFILE 'analysts'"
	if got := Render(op, "c1", nil); got != want {
		t.Fatalf("masked:\n got %s\nwant %s", got, want)
	}
	real := renderCreateUser(op, " ON CLUSTER `c1`", true)
	if !strings.Contains(real, "BY '"+testHash+"' SALT '"+testSalt+"'") {
		t.Fatalf("exec SQL lacks the secret: %s", real)
	}
	for _, c := range []struct {
		auth UserAuth
		want string
	}{
		{UserAuth{Method: AuthLDAP, Server: "corp"}, "CREATE USER `u` IDENTIFIED WITH ldap SERVER 'corp'"},
		{UserAuth{Method: AuthKerberos}, "CREATE USER `u` IDENTIFIED WITH kerberos"},
		{UserAuth{Method: AuthKerberos, Realm: "EX.COM"}, "CREATE USER `u` IDENTIFIED WITH kerberos REALM 'EX.COM'"},
		{UserAuth{Method: AuthSSLCertificate, CN: "u.example"}, "CREATE USER `u` IDENTIFIED WITH ssl_certificate CN 'u.example'"},
		{UserAuth{Method: AuthNoPassword}, "CREATE USER `u` IDENTIFIED WITH no_password"},
		{UserAuth{Method: AuthDoubleSHA1Hash, Hash: strings.Repeat("a", 40)}, "CREATE USER `u` IDENTIFIED WITH double_sha1_hash BY '***'"},
	} {
		a := c.auth
		if got := Render(Op{Kind: OpCreateUser, Name: "u", Auth: &a}, "", nil); got != c.want {
			t.Errorf("%s: got %s", a.Method, got)
		}
	}
	if got := Render(Op{Kind: OpDropUser, Name: "u"}, "c1", nil); got != "DROP USER IF EXISTS `u` ON CLUSTER `c1`" {
		t.Fatalf("drop: %s", got)
	}
}

func TestCreateUserValidation(t *testing.T) {
	bad := []struct {
		name string
		op   Op
		want string
	}{
		{"no auth", Op{Kind: OpCreateUser, Name: "u"}, "choose how the user signs in"},
		{"short hash", Op{Kind: OpCreateUser, Name: "u", Auth: &UserAuth{Method: AuthSHA256Hash, Hash: "abc"}}, "64 hex"},
		{"plaintext", Op{Kind: OpCreateUser, Name: "u", Auth: &UserAuth{Method: "plaintext_password", Hash: "secret"}}, "not supported"},
		{"bad salt", Op{Kind: OpCreateUser, Name: "u", Auth: &UserAuth{Method: AuthSHA256Hash, Hash: testHash, Salt: "a'b"}}, "salt"},
		{"salt on bcrypt", Op{Kind: OpCreateUser, Name: "u", Auth: &UserAuth{Method: AuthBcryptHash, Hash: "$2b$12$" + strings.Repeat("a", 53), Salt: "x"}}, "salt does not apply"},
		{"ldap needs server", Op{Kind: OpCreateUser, Name: "u", Auth: &UserAuth{Method: AuthLDAP}}, "LDAP server name is required"},
		{"redacted", Op{Kind: OpCreateUser, Name: "u", Auth: &UserAuth{Method: AuthNoPassword, Redacted: true}}, "cannot run again"},
		{"bad ip", Op{Kind: OpCreateUser, Name: "u", Hosts: []HostSpec{{Kind: HostIP, Value: "nope"}}, Auth: &UserAuth{Method: AuthNoPassword}}, "not an address"},
		{"any plus more", Op{Kind: OpCreateUser, Name: "u", Hosts: []HostSpec{{Kind: HostAny}, {Kind: HostLocal}}, Auth: &UserAuth{Method: AuthNoPassword}}, "cannot be combined"},
	}
	for _, c := range bad {
		err := validateOp(c.op, Catalog{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), testHash) {
			t.Errorf("%s: error echoes the hash", c.name)
		}
	}
	if err := validateOp(createUserOp("ok"), Catalog{}); err != nil {
		t.Fatal(err)
	}
	if err := validateOp(Op{Kind: OpCreateUser, Name: "b", Auth: &UserAuth{Method: AuthBcryptHash, Hash: "$2b$12$" + strings.Repeat("a", 53)}}, Catalog{}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateUserComposeOrderAndGuards(t *testing.T) {
	s := reconcileBase()
	s.SettingsProfiles = append(s.SettingsProfiles, SettingsProfile{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "analysts"}})
	grant := Op{Kind: OpGrantPrivilege, Target: user("ann"), Access: []string{"SELECT"}, Database: "d"}

	// A grant before the create is refused; after it is fine.
	if _, err := Compose([]Op{grant, createUserOp("ann")}, s, Catalog{}, "", GuardContext{}); err == nil || !strings.Contains(err.Error(), "before change 2 creates it") {
		t.Fatalf("use before create: %v", err)
	}
	if _, err := Compose([]Op{createUserOp("ann"), createUserOp("ann")}, s, Catalog{}, "", GuardContext{}); err == nil || !strings.Contains(err.Error(), "both create user") {
		t.Fatalf("double create: %v", err)
	}
	op := createUserOp("ann")
	op.Profile = "analysts"
	plan, err := Compose([]Op{op, grant}, s, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() || len(plan.Warnings) != 0 {
		t.Fatalf("warnings: %+v", plan.Warnings)
	}
	if plan.Statements[0].InverseSQL != "DROP USER IF EXISTS `ann`" || plan.Statements[0].execSQL == "" {
		t.Fatalf("statement = %+v", plan.Statements[0])
	}
	rb := RollbackOps(plan, []Result{{Seq: 1, Status: StatusApplied}, {Seq: 2, Status: StatusApplied}})
	if len(rb) != 2 || rb[1].Kind != OpDropUser || rb[1].Name != "ann" {
		t.Fatalf("rollback = %+v", rb)
	}

	// Existing user, missing profile, no password, read-only storage.
	nopw := Op{Kind: OpCreateUser, Name: "bob", Profile: "nope", Auth: &UserAuth{Method: AuthNoPassword}}
	s.UserStorages = []string{"users_xml"}
	plan, err = Compose([]Op{nopw}, s, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	got := codes(plan.Warnings)
	for code, sev := range map[string]string{CodeUserExists: SeverityBlock, CodeMissingProfile: SeverityBlock, CodeNoPassword: SeverityAck, CodeReadOnlyStorage: SeverityBlock} {
		if got[code] != sev {
			t.Errorf("%s = %q, want %q (all: %v)", code, got[code], sev, got)
		}
	}
	s.UserStorages = []string{"users_xml", "local_directory"}
	plan, _ = Compose([]Op{createUserOp("new")}, s, Catalog{}, "", GuardContext{})
	if len(plan.Warnings) != 0 {
		t.Fatalf("writable storage still warned: %+v", plan.Warnings)
	}

	// drop_user: self and service accounts are blocked, others need an ack.
	plan, _ = Compose([]Op{{Kind: OpDropUser, Name: "admin_session"}, {Kind: OpDropUser, Name: "bob"}, {Kind: OpDropUser, Name: "alice"}}, s, Catalog{}, "", GuardContext{ServiceAccounts: map[string]string{"bob": "MCP key k"}})
	got = codes(plan.Warnings)
	if got[CodeSelfDropUser] != SeverityBlock || got[CodeDropServiceAccount] != SeverityBlock || got[CodeDropUser] != SeverityAck {
		t.Fatalf("drop warnings = %v", got)
	}
	if inv := Inverse(Op{Kind: OpDropUser, Name: "alice"}, s); inv != nil {
		t.Fatalf("drop_user inverse = %+v", inv)
	}
}

// echoExecutor fails every statement with an error that quotes it, the way
// a ClickHouse syntax error can.
type echoExecutor struct{ ran []string }

func (e *echoExecutor) Query(_ context.Context, sql string, _ map[string]string) ([]map[string]any, error) {
	e.ran = append(e.ran, sql)
	return nil, errors.New("Code: 62. DB::Exception: Syntax error: failed at position 40 (" + sql + ") (version 25.8)")
}

func TestApplyRunsSecretButReportsMasked(t *testing.T) {
	plan, err := Compose([]Op{createUserOp("ann")}, reconcileBase(), Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	ex := &echoExecutor{}
	var seen []Result
	results := Apply(context.Background(), ex, plan, func(r Result) { seen = append(seen, r) })
	if len(ex.ran) != 1 || !strings.Contains(ex.ran[0], testHash) {
		t.Fatalf("executor did not get the real statement: %v", ex.ran)
	}
	b, _ := json.Marshal(map[string]any{"plan": plan, "results": results, "seen": seen})
	if strings.Contains(string(b), testHash) || strings.Contains(string(b), testSalt) {
		t.Fatalf("secret leaked: %s", b)
	}
	if results[0].Status != StatusFailed || !strings.Contains(results[0].Error, "***") {
		t.Fatalf("result = %+v", results[0])
	}
	if strings.Contains(plan.PlanHash, testHash) {
		t.Fatal("plan hash holds the secret")
	}
	red := RedactOps([]Op{createUserOp("ann")})
	if red[0].Auth.Hash != "" || red[0].Auth.Salt != "" || !red[0].Auth.Redacted || red[0].Auth.Method != AuthSHA256Hash {
		t.Fatalf("redacted = %+v", red[0].Auth)
	}
}

func TestCreatedUserIsNotDrift(t *testing.T) {
	pre := reconcileBase()
	ops := []Op{createUserOp("ann"), {Kind: OpGrantRole, Target: user("ann"), Roles: []string{"xml_role"}}, {Kind: OpSetDefaultRoles, Target: user("ann")}}
	post := Simulate(pre, ops)
	// What ClickHouse reports after the apply: the user, its role grant and
	// DEFAULT ROLE NONE.
	post.Users = append(append([]UserEntity{}, pre.Users...), UserEntity{Name: "ann", Storage: "local_directory", DefaultRoles: []string{}, DefaultExcpt: []string{}})
	if extra := UnexplainedChanges(pre, post, RedactOps(ops)); len(extra) != 0 {
		t.Fatalf("unexplained = %+v", extra)
	}
	dropped := Simulate(post, []Op{{Kind: OpDropUser, Name: "ann"}})
	if dropped.user("ann") != nil || len(dropped.RoleGrants) != len(pre.RoleGrants) {
		t.Fatalf("drop left %+v", dropped.RoleGrants)
	}
}

func TestImportCreatesTickedUsers(t *testing.T) {
	cur := reconcileBase()
	st := FromSnapshot(cur)
	st.Roles = append(st.Roles, StateRole{Name: "reader"})
	st.Users = append(st.Users, StateUser{Name: "ann_old", DefaultRoles: []string{"reader"}}, StateUser{Name: "zed", DefaultAll: true})
	st.Grants = append(st.Grants, GranteeGrants{Type: "user", Name: "ann_old", Privileges: []StateGrant{{Access: []string{"SELECT"}, Database: "d"}}})
	st.RoleGrants = append(st.RoleGrants, StateRoleGrant{Type: "user", Name: "ann_old", Role: "reader"})
	logins := map[string]UserLogin{"ann_old": {Auth: "ldap", Hosts: []HostSpec{{Kind: HostLocal}}}}
	opts := ImportOpts{Rename: Rename{Principals: map[string]string{"ann_old": "ann"}}, Logins: logins}

	plan, err := PlanImport(cur, st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.NewUsers) != 2 || plan.NewUsers[0].Name != "ann" || plan.NewUsers[0].Auth != "ldap" || len(plan.NewUsers[0].Hosts) != 1 {
		t.Fatalf("new users = %+v", plan.NewUsers)
	}
	for _, op := range plan.Ops {
		if op.Kind == OpCreateUser || (op.Target != nil && op.Target.Name == "ann") {
			t.Fatalf("unticked user planned: %+v", op)
		}
	}

	opts.CreateUsers = []string{"ann"}
	plan, err = PlanImport(cur, st, opts)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, op := range plan.Ops {
		kinds = append(kinds, op.Kind)
	}
	if strings.Join(kinds, ",") != "create_role,create_user,grant_privilege,grant_role,set_default_roles" {
		t.Fatalf("ops = %v", kinds)
	}
	if plan.Summary["users"] != 1 || plan.Summary["new_users"] != 1 {
		t.Fatalf("summary = %v", plan.Summary)
	}
	if err := ValidateImportOps(plan.Ops, Catalog{}); err != nil {
		t.Fatal(err)
	}
	// The client adds the authentication; then the plan composes.
	for i := range plan.Ops {
		if plan.Ops[i].Kind == OpCreateUser {
			plan.Ops[i].Auth = &UserAuth{Method: AuthSHA256Hash, Hash: testHash, Salt: testSalt}
		}
	}
	composed, err := Compose(plan.Ops, cur, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range composed.Diffs {
		if d.Kind == KindUser && d.Entity == "ann" {
			if !strings.HasPrefix(d.Proposed, "CREATE USER `ann` IDENTIFIED WITH sha256_hash BY '***' SALT '***';\n") || strings.Contains(d.Proposed, testHash) || d.Current != "" {
				t.Fatalf("diff = %+v", d)
			}
		}
	}

	opts.CreateUsers = []string{"alice"}
	if _, err := PlanImport(cur, st, opts); err == nil || !strings.Contains(err.Error(), "not a new user") {
		t.Fatalf("existing user ticked: %v", err)
	}
}

func TestRevertNeverDropsOrRecreatesUsers(t *testing.T) {
	before := reconcileBase()
	live := Simulate(before, []Op{createUserOp("ann"), {Kind: OpGrantRole, Target: user("ann"), Roles: []string{"xml_role"}}})
	p := PlanRevert(live, FromSnapshot(before), nil)
	for _, op := range p.Ops {
		if op.Kind == OpDropUser || op.Kind == OpCreateUser {
			t.Fatalf("revert touched a user: %+v", op)
		}
	}
	var reason string
	for _, s := range p.Skipped {
		if s.Kind == KindUser && s.Name == "ann" {
			reason = s.Reason
		}
	}
	if !strings.Contains(reason, "created since this snapshot") || !strings.Contains(reason, "does not drop users on revert") {
		t.Fatalf("created user not reported: %+v", p.Skipped)
	}
	// A user dropped since cannot come back.
	p = PlanRevert(before, FromSnapshot(live), nil)
	for _, op := range p.Ops {
		if op.Kind == OpCreateUser || op.Kind == OpDropUser {
			t.Fatalf("revert touched a user: %+v", op)
		}
	}
	if !reported(p)["ann"] {
		t.Fatalf("missing user not reported: %+v", p.Skipped)
	}
}

func TestFileCarriesLoginHints(t *testing.T) {
	st := &State{Version: StateVersion, Users: []StateUser{{Name: "ann", DefaultAll: true}, {Name: "bob"}}}
	logins := map[string]UserLogin{"ann": {Auth: "sha256_password", Hosts: []HostSpec{{Kind: HostIP, Value: "10.0.0.0/8"}, {Kind: HostLike, Value: "%.corp"}}}}
	for _, marshal := range []func() ([]byte, error){
		func() ([]byte, error) { return MarshalFileYAML(st, logins, FileSource{}, "h") },
		func() ([]byte, error) { return MarshalFileJSON(st, logins, FileSource{}) },
	} {
		b, err := marshal()
		if err != nil {
			t.Fatal(err)
		}
		f, err := ParseFile(b)
		if err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		if got, _ := json.Marshal(f.Logins); string(got) != `{"ann":{"auth":"sha256_password","hosts":[{"kind":"ip","value":"10.0.0.0/8"},{"kind":"like","value":"%.corp"}]}}` {
			t.Fatalf("logins = %s\n%s", got, b)
		}
	}
	if _, err := ParseFile([]byte("chui_access: 1\nusers:\n  u: {default_roles: all, hosts: [\"ip nope\"]}\n")); err == nil || !strings.Contains(err.Error(), "users.u.hosts[0]") {
		t.Fatalf("bad host: %v", err)
	}
	s := &Snapshot{UserLogins: map[string]UserLogin{"ann": {Auth: "ldap"}, "bob": {}}}
	if got := ExportLogins(s, st); len(got) != 1 || got["ann"].Auth != "ldap" {
		t.Fatalf("export logins = %+v", got)
	}
}

func TestUserLoginsFromRows(t *testing.T) {
	if got := firstAuthType("['sha256_password']"); got != "sha256_password" {
		t.Fatalf("array auth = %q", got)
	}
	if got := firstAuthType("ldap"); got != "ldap" {
		t.Fatalf("enum auth = %q", got)
	}
	anyHost := hostsFromRow(map[string]any{"host_ip": []any{"::/0"}})
	if len(anyHost) != 1 || anyHost[0].Kind != HostAny {
		t.Fatalf("any = %+v", anyHost)
	}
	mixed := hostsFromRow(map[string]any{"host_ip": []any{"10.0.0.0/8"}, "host_names": []any{"localhost"}, "host_names_like": []any{"%.corp"}})
	if hostClause(mixed) != " HOST LOCAL, IP '10.0.0.0/8', LIKE '%.corp'" {
		t.Fatalf("mixed = %+v", mixed)
	}
}
