// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func historyBase() *Snapshot {
	s := reconcileBase()
	s.Roles = append(s.Roles, Entity{Name: "analyst", Storage: "local_directory"}, Entity{Name: "team_lead", Storage: "local_directory"})
	s.RoleGrants = append(s.RoleGrants, RoleGrant{UserName: "alice", GrantedRole: "team_lead"}, RoleGrant{RoleName: "team_lead", GrantedRole: "analyst"})
	return s
}

func findChange(changes []StateChange, kind, change, grantee, detailPart string) bool {
	for _, c := range changes {
		if c.Kind == kind && c.Change == change && c.Grantee == grantee && strings.Contains(c.Detail, detailPart) {
			return true
		}
	}
	return false
}

func TestDiffStatesEffectiveThroughRoleChain(t *testing.T) {
	before := historyBase()
	after := Simulate(before, []Op{
		{Kind: OpGrantPrivilege, Target: &Target{Type: "role", Name: "analyst"}, Access: []string{"SELECT"}, Database: "sales"},
		{Kind: OpCreateRole, Name: "auditor"},
		{Kind: OpGrantRole, Target: &Target{Type: "user", Name: "bob"}, Roles: []string{"auditor"}},
		{Kind: OpSetDefaultRoles, Target: &Target{Type: "user", Name: "bob"}, Roles: []string{"auditor"}},
	})
	d := DiffStates(FromSnapshot(before), FromSnapshot(after))

	for _, want := range []struct{ kind, change, grantee, detail string }{
		{ChangeGrant, "added", "analyst", "GRANT SELECT ON `sales`.* TO `analyst`"},
		{ChangeEntity, "added", "auditor", "role created"},
		{ChangeRoleGrant, "added", "bob", "role auditor"},
		{ChangeDefaultRoles, "changed", "bob", "to auditor"},
	} {
		if !findChange(d.Changes, want.kind, want.change, want.grantee, want.detail) {
			t.Errorf("missing change %+v in %+v", want, d.Changes)
		}
	}

	gained := map[string]string{}
	for _, e := range d.Effective {
		if e.Change == "gained" {
			gained[e.User+": "+e.What] = e.Via
		}
	}
	if via := gained["alice: SELECT ON `sales`.*"]; via != "analyst" {
		t.Errorf("alice should gain SELECT on sales via analyst, got %q (%v)", via, gained)
	}
	if via := gained["bob: role auditor"]; via != "direct" {
		t.Errorf("bob should gain role auditor directly, got %q", via)
	}
	if len(d.Effective) != 2 {
		t.Errorf("effective = %+v", d.Effective)
	}

	var roleDiff *Diff
	for i := range d.Entities {
		if d.Entities[i].Kind == KindRole && d.Entities[i].Entity == "analyst" {
			roleDiff = &d.Entities[i]
		}
	}
	if roleDiff == nil || !strings.Contains(roleDiff.Proposed, "GRANT SELECT ON `sales`.* TO `analyst`") || strings.Contains(roleDiff.Current, "sales") {
		t.Errorf("analyst entity diff = %+v", roleDiff)
	}

	counts := d.Counts()
	if counts["added"][ChangeGrant] != 1 || counts["added"][ChangeEntity] != 1 || counts["changed"][ChangeDefaultRoles] != 1 {
		t.Errorf("counts = %v", counts)
	}

	// The way back reports losses, and a partial revoke reads as a loss.
	back := DiffStates(FromSnapshot(after), FromSnapshot(before))
	lost := false
	for _, e := range back.Effective {
		if e.User == "alice" && e.Change == "lost" && e.What == "SELECT ON `sales`.*" && e.Via == "analyst" {
			lost = true
		}
	}
	if !lost {
		t.Errorf("reverse diff should show alice losing SELECT: %+v", back.Effective)
	}
	revoked := Simulate(before, []Op{
		{Kind: OpGrantPrivilege, Target: &Target{Type: "user", Name: "carol"}, Access: []string{"SELECT"}, Database: "sales"},
	})
	partial := Simulate(revoked, []Op{
		{Kind: OpRevokePrivilege, Target: &Target{Type: "user", Name: "carol"}, Access: []string{"SELECT"}, Database: "sales", Table: "secret"},
	})
	pd := DiffStates(FromSnapshot(revoked), FromSnapshot(partial))
	if len(pd.Effective) != 1 || pd.Effective[0].Change != "lost" || !strings.Contains(pd.Effective[0].What, "partial revoke") {
		t.Errorf("partial revoke effective = %+v", pd.Effective)
	}
}

func TestDiffStatesSameIsEmpty(t *testing.T) {
	s := historyBase()
	d := DiffStates(FromSnapshot(s), FromSnapshot(cloneSnapshot(s)))
	if len(d.Changes) != 0 || len(d.Entities) != 0 || len(d.Effective) != 0 {
		t.Fatalf("expected no difference, got %+v", d)
	}
	if first := DiffStates(nil, FromSnapshot(s)); len(first.Changes) == 0 {
		t.Fatal("a nil side should read as empty")
	}
}

func TestUnexplainedChangesClassifiesMixed(t *testing.T) {
	pre := historyBase()
	ops := []Op{{Kind: OpCreateRole, Name: "chui_new"}, {Kind: OpGrantRole, Target: &Target{Type: "user", Name: "bob"}, Roles: []string{"chui_new"}}}
	post := Simulate(pre, ops)
	if extra := UnexplainedChanges(pre, post, ops); len(extra) != 0 {
		t.Fatalf("apply alone should explain post: %+v", extra)
	}
	// Someone else granted carol a privilege during the apply.
	mixed := Simulate(post, []Op{{Kind: OpGrantPrivilege, Target: &Target{Type: "user", Name: "carol"}, Access: []string{"INSERT"}, Database: "d1"}})
	extra := UnexplainedChanges(pre, mixed, ops)
	if len(extra) != 1 || extra[0].Grantee != "carol" || extra[0].Kind != ChangeGrant {
		t.Fatalf("unexplained = %+v", extra)
	}
	// A failed statement is not counted as applied.
	results := []Result{{Seq: 1, Status: StatusApplied}, {Seq: 2, Status: StatusFailed}}
	applied := AppliedOps(ops, results)
	if len(applied) != 1 || applied[0].Kind != OpCreateRole {
		t.Fatalf("applied = %+v", applied)
	}
	if extra := UnexplainedChanges(pre, Simulate(pre, applied), applied); len(extra) != 0 {
		t.Fatalf("partial apply: %+v", extra)
	}
}

// reported is the set of entity names a revert plan says it leaves alone.
func reported(p RevertPlan) map[string]bool {
	out := map[string]bool{}
	for _, s := range p.Skipped {
		out[s.Name] = true
	}
	for _, c := range p.Remaining {
		out[c.Grantee] = true
	}
	return out
}

// Reverting to a stored state with every kind pruned gets there, except
// for what the plan reports as unrevertable.
func TestRevertRoundTrip(t *testing.T) {
	exact := 0
	for seed := int64(0); seed < 120; seed++ {
		base := baseFor(t, seed)
		rng := rand.New(rand.NewSource(seed))
		target := FromSnapshot(randomize(base, rng, 10+rng.Intn(40)))
		live := randomize(base, rng, 10+rng.Intn(40))

		p := PlanRevert(live, target, nil)
		after := FromSnapshot(Simulate(live, p.Ops))
		skip := reported(p)
		for _, c := range DiffStates(target.CanonicalWith(live), after).Changes {
			if !skip[c.Grantee] {
				t.Fatalf("seed %d: %s %s %s %q differs but was not reported\nops:\n%s", seed, c.Kind, c.Change, c.Grantee, c.Detail, renderOps(p.Ops))
			}
		}
		if len(skip) == 0 {
			exact++
			if got, want := stateJSON(after), stateJSON(target.CanonicalWith(live)); got != want {
				t.Fatalf("seed %d: nothing reported but states differ\n got %s\nwant %s", seed, got, want)
			}
		}
	}
	if exact == 0 {
		t.Fatal("no seed reverted exactly")
	}
}

// Reverting only some entities changes those and leaves the rest as live.
func TestRevertOnlyLimitsEntities(t *testing.T) {
	for seed := int64(0); seed < 60; seed++ {
		base := baseFor(t, seed)
		rng := rand.New(rand.NewSource(seed))
		target := FromSnapshot(randomize(base, rng, 10+rng.Intn(40)))
		live := randomize(base, rng, 10+rng.Intn(40))
		full := PlanRevert(live, target, nil)
		if len(full.Ops) == 0 {
			continue
		}
		pickName := OpEntities(full.Ops[rng.Intn(len(full.Ops))])
		if len(pickName) == 0 {
			continue
		}
		only := map[string]bool{pickName[0]: true}
		p := PlanRevert(live, target, only)
		skip := reported(p)
		after := FromSnapshot(Simulate(live, p.Ops))
		for _, c := range DiffStates(FromSnapshot(live), after).Changes {
			if !only[c.Grantee] && !skip[c.Grantee] {
				t.Fatalf("seed %d only %v: %s changed (%s %s)\nops:\n%s", seed, only, c.Grantee, c.Kind, c.Detail, renderOps(p.Ops))
			}
		}
		for _, c := range DiffStates(FilterState(target.CanonicalWith(live), only), FilterState(after, only)).Changes {
			if !skip[c.Grantee] {
				t.Fatalf("seed %d only %v: %s not reverted (%s %s)", seed, only, c.Grantee, c.Kind, c.Detail)
			}
		}
		if len(p.Ops) > len(full.Ops) {
			t.Fatalf("seed %d: subset has more ops (%d) than the full revert (%d)", seed, len(p.Ops), len(full.Ops))
		}
	}
}

func TestMergeStateKeepsOthers(t *testing.T) {
	live := FromSnapshot(historyBase())
	target := FromSnapshot(Simulate(historyBase(), []Op{
		{Kind: OpDropRole, Name: "analyst"},
		{Kind: OpGrantPrivilege, Target: &Target{Type: "user", Name: "bob"}, Access: []string{"SELECT"}},
	}))
	merged := MergeState(live, target, map[string]bool{"bob": true})
	if !strings.Contains(stateJSON(merged), `"analyst"`) {
		t.Fatal("analyst should stay: it is not selected")
	}
	if !strings.Contains(fmt.Sprint(FilterState(merged, map[string]bool{"bob": true}).Grants), "SELECT") {
		t.Fatal("bob's grant should come from the target")
	}
}
