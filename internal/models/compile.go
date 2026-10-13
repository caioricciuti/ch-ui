package models

import (
	"fmt"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// PlanStep is one statement a run would execute, with when it applies.
type PlanStep struct {
	// When is "first build", "rebuild", "incremental run", "full refresh"
	// or "every run".
	When string `json:"when"`
	SQL  string `json:"sql"`
}

// Compile resolves the model's SQL and lists the statements a run would
// execute, without touching ClickHouse. Ownership checks, leftover cleanup and
// the database-engine-dependent swap (EXCHANGE TABLES on Atomic databases) are
// summarized, not listed.
func Compile(m database.Model, modelTargets map[string]string) (string, []PlanStep, error) {
	resolved, err := ResolveModelSQL(m.SQLBody, modelTargets, m.TargetDatabase, m.Name)
	if err != nil {
		return resolved, nil, err
	}
	db, name := m.TargetDatabase, m.Name
	tmp, mv := name+tmpSuffix, name+mvSuffix
	ref := quoteIdent(db) + "." + quoteIdent(name)
	tmpRef := quoteIdent(db) + "." + quoteIdent(tmp)
	mvRef := quoteIdent(db) + "." + quoteIdent(mv)
	swap := fmt.Sprintf("EXCHANGE TABLES %s AND %s", tmpRef, ref)
	spec := specOf(m)

	var steps []PlanStep
	add := func(when, sql string) { steps = append(steps, PlanStep{When: when, SQL: sql}) }
	switch m.Materialization {
	case MaterializationView:
		add("first build", createViewSQL(db, name, resolved, false))
		add("rebuild", createViewSQL(db, name, resolved, true))
	case MaterializationTable:
		add("first build", createTableFromSpec(db, name, spec, resolved, false))
		add("rebuild", createTableFromSpec(db, tmp, spec, resolved, false))
		add("rebuild", swap)
	case MaterializationIncremental:
		add("first build", createTableFromSpec(db, name, spec, resolved, false))
		src := incrementalSource(m, ref, resolved)
		if m.IncrementalStrategy == StrategyDeleteInsert {
			key := keyTuple(m.UniqueKey)
			add("incremental run", fmt.Sprintf("CREATE TABLE %s AS %s", tmpRef, ref))
			add("incremental run", "INSERT INTO "+tmpRef+" "+src)
			add("incremental run", fmt.Sprintf("DELETE FROM %s WHERE %s IN (SELECT %s FROM %s)", ref, key, key, tmpRef))
			add("incremental run", "INSERT INTO "+ref+" SELECT * FROM "+tmpRef)
		} else {
			add("incremental run", "INSERT INTO "+ref+" "+src)
		}
		add("full refresh", createTableFromSpec(db, tmp, spec, resolved, false))
		add("full refresh", swap)
	case MaterializationMaterializedView:
		add("first build", createTableFromSpec(db, name, spec, resolved, true))
		add("first build", createMVSQL(db, mv, name, resolved))
		add("first build", "INSERT INTO "+ref+" "+cleanSelect(resolved))
		add("every run", "ALTER TABLE "+mvRef+" MODIFY QUERY "+cleanSelect(resolved))
		add("full refresh", createTableFromSpec(db, tmp, spec, resolved, true))
		add("full refresh", "INSERT INTO "+tmpRef+" "+cleanSelect(resolved))
		add("full refresh", "DROP VIEW "+mvRef)
		add("full refresh", swap)
		add("full refresh", createMVSQL(db, mv, name, resolved))
	default:
		return resolved, nil, ValidateMaterialization(m.Materialization)
	}
	return resolved, steps, nil
}

// PreviewSQL wraps a resolved SELECT for a capped, read-only preview.
func PreviewSQL(resolved string, limit int) string {
	return fmt.Sprintf("SELECT * FROM %s LIMIT %d", wrapSelect(resolved), limit)
}
