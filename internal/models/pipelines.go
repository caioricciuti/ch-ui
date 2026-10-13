package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// LoadTimezone resolves a pipeline timezone; empty means UTC.
func LoadTimezone(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" || tz == "UTC" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q", tz)
	}
	return loc, nil
}

// NextRuns returns up to n run times after from for a cron read in tz (UTC
// results). An invalid cron or timezone returns nothing.
func NextRuns(cron, tz string, from time.Time, n int) []time.Time {
	loc, err := LoadTimezone(tz)
	if err != nil || strings.TrimSpace(cron) == "" {
		return nil
	}
	var out []time.Time
	t := from
	for len(out) < n {
		next := cronexpr.ComputeNextRunIn(cron, t, loc)
		if next == nil {
			break
		}
		out = append(out, *next)
		t = *next
	}
	return out
}

// MigrateLegacySchedules copies every per-anchor model schedule into a named
// pipeline, once (model_pipelines.legacy_schedule_id remembers the copy). The
// pipeline is named after the anchor model and selects the models the
// schedule ran before: "+anchor+" when that covers the anchor's whole
// connected component, otherwise the component's model names. model_schedules
// itself is kept. Returns the number of pipelines created.
func MigrateLegacySchedules(db *database.DB) (int, error) {
	schedules, err := db.GetAllModelSchedules()
	if err != nil {
		return 0, err
	}
	created := 0
	graphs := make(map[string]*connGraph)
	for _, s := range schedules {
		if s.AnchorModelID == nil {
			continue
		}
		done, err := db.ModelPipelineForLegacySchedule(s.ID)
		if err != nil {
			return created, err
		}
		if done {
			continue
		}
		g, ok := graphs[s.ConnectionID]
		if !ok {
			g, err = loadConnGraph(db, s.ConnectionID)
			if err != nil {
				return created, err
			}
			graphs[s.ConnectionID] = g
		}
		anchor, ok := g.idToName[*s.AnchorModelID]
		if !ok || g.dag == nil {
			continue
		}
		selector := "+" + anchor + "+"
		component := g.dag.ComponentContaining(*s.AnchorModelID)
		if closure, err := g.dag.Select(selector, g.nameToID); err != nil || len(closure) != len(component) {
			names := make([]string, len(component))
			for i, id := range component {
				names[i] = g.idToName[id]
			}
			selector = strings.Join(names, ",")
		}
		name := anchor
		for i := 2; ; i++ {
			existing, err := db.GetModelPipelineByName(s.ConnectionID, name)
			if err != nil {
				return created, err
			}
			if existing == nil {
				break
			}
			name = fmt.Sprintf("%s_%d", anchor, i)
		}
		legacy := s.ID
		p := database.ModelPipeline{
			ConnectionID: s.ConnectionID, Name: name, Selector: selector, Cron: s.Cron,
			Enabled: s.Enabled, Timezone: "UTC", NextRunAt: s.NextRunAt, LegacyScheduleID: &legacy,
			CreatedBy: s.CreatedBy,
		}
		if _, err := db.CreateModelPipeline(p); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

type connGraph struct {
	dag      *DepGraph
	nameToID map[string]string
	idToName map[string]string
}

func loadConnGraph(db *database.DB, connectionID string) (*connGraph, error) {
	all, err := db.GetModelsByConnection(connectionID)
	if err != nil {
		return nil, err
	}
	g := &connGraph{nameToID: map[string]string{}, idToName: map[string]string{}}
	var ids []string
	refs := map[string][]string{}
	for _, m := range all {
		g.nameToID[m.Name] = m.ID
		g.idToName[m.ID] = m.Name
		ids = append(ids, m.ID)
		refs[m.ID] = ExtractRefs(m.SQLBody)
	}
	// A broken graph (unknown reference, cycle) leaves dag nil; those
	// schedules are retried on the next start.
	g.dag, _ = BuildDAG(ids, refs, g.nameToID)
	return g, nil
}
