// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package accesshistory records point-in-time copies of a connection's
// ClickHouse access control and classifies each change: made through
// CH-UI (apply, revert), made outside it (drift), or both at once (mixed).
package accesshistory

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/rbac"
)

// Sources of a history row.
const (
	SourceApply  = "apply"
	SourceRevert = "revert"
	SourceSync   = "sync"
	SourceView   = "view"
	SourceManual = "manual"
)

// Drift classes of a history row.
const (
	DriftNone    = "none"
	DriftOutside = "outside"
	DriftMixed   = "mixed"
)

// maxEntities bounds the entity lists kept in a summary.
const maxEntities = 500

// Summary is the JSON stored with each row: change counts per kind against
// the row it is compared with, and what the drift decision rests on.
type Summary struct {
	Added   map[string]int `json:"added,omitempty"`
	Removed map[string]int `json:"removed,omitempty"`
	Changed map[string]int `json:"changed,omitempty"`
	// Entities are the changed entities as "kind:name".
	Entities  []string `json:"entities,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	// Unexplained are the entities of an apply row that the applied ops do
	// not account for.
	Unexplained []string `json:"unexplained,omitempty"`
	// Local are the entities kept in local (not replicated) storage on a
	// multi-node cluster, so they differ between hosts.
	Local        []string `json:"local,omitempty"`
	HostChanged  bool     `json:"host_changed,omitempty"`
	PreviousHost string   `json:"previous_host,omitempty"`
	RevertOf     string   `json:"revert_of,omitempty"`
	First        bool     `json:"first,omitempty"`
	Note         string   `json:"note,omitempty"`
	Incomplete   []string `json:"incomplete,omitempty"`
}

// Capture is one observation to record.
type Capture struct {
	ConnectionID string
	Source       string
	Actor        string
	CHUser       string
	Snapshot     *rbac.Snapshot
	// Pre, for apply and revert rows, is the snapshot the plan was made on
	// and Applied the ops that ran; together they say what the apply alone
	// would have produced.
	Pre      *rbac.Snapshot
	Applied  []rbac.Op
	PlanHash string
	RevertOf string
	Note     string
}

// Record stores c, or bumps the newest row when nothing changed. It returns
// the row and whether it is new.
func Record(db *database.DB, c Capture) (*database.RBACSnapshot, bool, error) {
	st := rbac.FromSnapshot(c.Snapshot)
	body, err := EncodeState(st)
	if err != nil {
		return nil, false, err
	}
	incomplete := incompleteReasons(c.Snapshot)
	in := database.RBACSnapshotInput{
		ConnectionID: c.ConnectionID, Hash: st.Hash(), Source: c.Source, Actor: c.Actor, CHUser: c.CHUser,
		Host: c.Snapshot.Host, MultiNode: c.Snapshot.MultiNode, PlanHash: c.PlanHash, Complete: len(incomplete) == 0, Body: body,
	}
	classify := func(hc database.RBACHistoryContext) (string, string, error) {
		drift, sum, err := classify(c, st, in, hc)
		if err != nil {
			return "", "", err
		}
		sum.Incomplete = incomplete
		b, err := json.Marshal(sum)
		if err != nil {
			return "", "", err
		}
		return drift, string(b), nil
	}
	return db.RecordRBACSnapshot(in, classify)
}

func classify(c Capture, st *rbac.State, in database.RBACSnapshotInput, hc database.RBACHistoryContext) (string, Summary, error) {
	sum := Summary{RevertOf: c.RevertOf, Note: c.Note, Local: localEntities(c.Snapshot)}
	if (c.Source == SourceApply || c.Source == SourceRevert) && c.Pre != nil {
		setCounts(&sum, rbac.DiffStates(rbac.FromSnapshot(c.Pre), st))
		if !in.Complete {
			return DriftNone, sum, nil
		}
		if extra := rbac.UnexplainedChanges(c.Pre, c.Snapshot, c.Applied); len(extra) > 0 {
			sum.Unexplained = capList(entityKeys(extra))
			return DriftMixed, sum, nil
		}
		return DriftNone, sum, nil
	}
	if hc.Previous == nil {
		sum.First = true
		return DriftNone, sum, nil
	}
	prev, err := DecodeState(hc.PreviousBody)
	if err != nil {
		return "", sum, fmt.Errorf("previous snapshot: %w", err)
	}
	diff := rbac.DiffStates(prev, st)
	setCounts(&sum, diff)
	if !in.Complete || len(diff.Changes) == 0 {
		return DriftNone, sum, nil
	}
	if !sum.Truncated && coveredByApplies(sum.Entities, hc.Applies) {
		sum.Note = joinNote(sum.Note, "changes match an apply made through CH-UI by another ClickHouse user")
		return DriftNone, sum, nil
	}
	if in.MultiNode && hc.Previous.Host != "" && in.Host != hc.Previous.Host {
		sum.HostChanged, sum.PreviousHost = true, hc.Previous.Host
		if !sum.Truncated && onlyLocal(sum.Entities, sum.Local, previousLocal(hc.Previous)) {
			sum.Note = joinNote(sum.Note, "host changed; only entities in local storage differ")
			return DriftNone, sum, nil
		}
	}
	return DriftOutside, sum, nil
}

func setCounts(sum *Summary, d *rbac.StateDiff) {
	counts := d.Counts()
	sum.Added, sum.Removed, sum.Changed = counts["added"], counts["removed"], counts["changed"]
	keys := d.EntityKeys()
	sum.Truncated = len(keys) > maxEntities
	sum.Entities = capList(keys)
}

func capList(list []string) []string {
	if len(list) > maxEntities {
		return list[:maxEntities]
	}
	return list
}

func entityKeys(changes []rbac.StateChange) []string {
	return (&rbac.StateDiff{Changes: changes}).EntityKeys()
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// coveredByApplies reports whether every changed entity was changed by one
// of the applies.
func coveredByApplies(entities []string, applies []database.RBACSnapshot) bool {
	if len(applies) == 0 {
		return false
	}
	touched := map[string]bool{}
	for _, a := range applies {
		var s Summary
		if json.Unmarshal([]byte(a.Summary), &s) != nil || s.Truncated {
			return false
		}
		for _, e := range s.Entities {
			touched[e] = true
		}
	}
	for _, e := range entities {
		if !touched[e] {
			return false
		}
	}
	return true
}

func previousLocal(prev *database.RBACSnapshot) []string {
	var s Summary
	_ = json.Unmarshal([]byte(prev.Summary), &s)
	return s.Local
}

// onlyLocal reports whether every changed entity is in local storage on
// either host. A grantee's grants count as part of the grantee.
func onlyLocal(entities, cur, prev []string) bool {
	local := map[string]bool{}
	for _, e := range cur {
		local[e] = true
	}
	for _, e := range prev {
		local[e] = true
	}
	for _, e := range entities {
		if !local[e] {
			return false
		}
	}
	return true
}

// localEntities lists, on a multi-node cluster, the entities not kept in
// replicated storage, as "kind:name".
func localEntities(s *rbac.Snapshot) []string {
	if !s.MultiNode {
		return nil
	}
	var out []string
	add := func(kind, name, storage string) {
		if storage != "" && storage != "replicated" {
			out = append(out, kind+":"+name)
		}
	}
	for _, r := range s.Roles {
		add(rbac.KindRole, r.Name, r.Storage)
	}
	for _, u := range s.Users {
		add(rbac.KindUser, u.Name, u.Storage)
	}
	for _, p := range s.RowPolicies {
		add(rbac.KindRowPolicy, p.Name+" ON "+p.Database+"."+p.Table, p.Storage)
	}
	for _, q := range s.Quotas {
		add(rbac.KindQuota, q.Name, q.Storage)
	}
	for _, p := range s.SettingsProfiles {
		add(rbac.KindSettingsProfile, p.Name, p.Storage)
	}
	return capList(out)
}

// incompleteReasons says why a snapshot may not show everything: a failed
// section, or a login without SHOW ACCESS that only sees its own entries.
// Incomplete rows are kept but never used to judge drift.
func incompleteReasons(s *rbac.Snapshot) []string {
	out := append([]string{}, s.Errors...)
	if !s.Capabilities.ShowAccess {
		out = append(out, "login lacks SHOW ACCESS")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EncodeState returns the gzip JSON of st.
func EncodeState(st *rbac.State) ([]byte, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// maxStateBytes bounds a decoded body.
const maxStateBytes = 256 << 20

// DecodeState reads a body written by EncodeState.
func DecodeState(body []byte) (*rbac.State, error) {
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(io.LimitReader(zr, maxStateBytes))
	if err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	var st rbac.State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	return &st, nil
}
