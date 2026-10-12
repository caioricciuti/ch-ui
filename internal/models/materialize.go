package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Supported materializations. Anything else is rejected when a model is
// created, updated, synced from GitHub or run.
const (
	MaterializationView  = "view"
	MaterializationTable = "table"
)

const (
	defaultTableEngine = "MergeTree"
	defaultOrderBy     = "tuple()"
	// A table rebuild writes into <name>__chui_tmp and swaps it in; the
	// rename fallback parks the previous table at <name>__chui_old.
	tmpSuffix = "__chui_tmp"
	oldSuffix = "__chui_old"
)

// ValidateMaterialization accepts only the materializations the runner builds.
func ValidateMaterialization(m string) error {
	switch m {
	case MaterializationView, MaterializationTable:
		return nil
	}
	return fmt.Errorf("materialization must be 'view' or 'table' (got %q)", m)
}

// TableDefaults fills the engine (MergeTree) and, for engines that take one,
// the ORDER BY (tuple()) when they are empty.
func TableDefaults(engine, orderBy string) (string, string) {
	engine = strings.TrimSpace(engine)
	orderBy = strings.TrimSpace(orderBy)
	if engine == "" {
		engine = defaultTableEngine
	}
	if orderBy == "" && engineTakesOrderBy(engine) {
		orderBy = defaultOrderBy
	}
	return engine, orderBy
}

// engineTakesOrderBy reports whether ClickHouse accepts an ORDER BY clause for
// the engine. Only the MergeTree family does; Memory, Log, TinyLog, StripeLog
// and the rest refuse it.
func engineTakesOrderBy(engine string) bool {
	return strings.Contains(engine, "MergeTree")
}

// OwnerComment is the ClickHouse COMMENT CH-UI stamps on every table and view
// it builds for a model. It is how a run tells its own objects apart from
// tables someone made by hand.
func OwnerComment(modelID string) string {
	return "ch-ui model " + modelID
}

func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "`", "\\`") + "`"
}

func quoteString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// cleanSelect trims whitespace and trailing semicolons so the body can be
// embedded after AS.
func cleanSelect(sql string) string {
	return strings.TrimRight(strings.TrimSpace(sql), "; \t\r\n")
}

func createTableSQL(db, name, engine, orderBy, selectSQL string) string {
	engine, orderBy = TableDefaults(engine, orderBy)
	order := ""
	if engineTakesOrderBy(engine) {
		order = " ORDER BY " + orderBy
	}
	return fmt.Sprintf("CREATE TABLE %s.%s ENGINE = %s%s AS %s",
		quoteIdent(db), quoteIdent(name), engine, order, cleanSelect(selectSQL))
}

func createViewSQL(db, name, selectSQL string, replace bool) string {
	verb := "CREATE VIEW"
	if replace {
		verb = "CREATE OR REPLACE VIEW"
	}
	return fmt.Sprintf("%s %s.%s AS %s", verb, quoteIdent(db), quoteIdent(name), cleanSelect(selectSQL))
}

// chExec runs one statement against the model's ClickHouse connection.
type chExec func(sql string) (*tunnel.QueryResult, error)

type chObject struct {
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	Comment string `json:"comment"`
}

// kind groups ClickHouse engines into what a model can be.
func (o chObject) kind() string {
	switch o.Engine {
	case "View":
		return MaterializationView
	case "MaterializedView", "LiveView", "WindowView", "Dictionary":
		return "other"
	}
	return MaterializationTable
}

// materializer builds one model into ClickHouse.
type materializer struct {
	exec  chExec
	model database.Model
	// priorSuccess reports whether this model has a successful run on record.
	priorSuccess func() (bool, error)
}

// ownsTarget decides whether the existing object at the model's name may be
// replaced by this model.
//
// The rule:
//   - COMMENT 'ch-ui model <this id>': ours, replace it.
//   - Any other comment (someone's description, another model's marker): not
//     ours, refuse.
//   - No comment: CH-UI versions before the marker existed built tables and
//     views without one. Such an object counts as ours only when its kind
//     (table or view) matches the model AND this model has a successful run
//     recorded, which means CH-UI built it. Otherwise refuse, so a model named
//     like a hand-made table can never drop that table on its first run.
func (mz *materializer) ownsTarget(o chObject) (bool, error) {
	marker := OwnerComment(mz.model.ID)
	if o.Comment == marker {
		return true, nil
	}
	if o.Comment != "" || o.kind() != mz.model.Materialization {
		return false, nil
	}
	return mz.priorSuccess()
}

func (mz *materializer) notOwnedErr(name string) error {
	return fmt.Errorf("%s.%s: a table with this name already exists and was not created by this model; rename the model or drop the table yourself",
		mz.model.TargetDatabase, name)
}

func (mz *materializer) lookup(names ...string) (map[string]chObject, error) {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = quoteString(n)
	}
	res, err := mz.exec(fmt.Sprintf("SELECT name, engine, comment FROM system.tables WHERE database = %s AND name IN (%s)",
		quoteString(mz.model.TargetDatabase), strings.Join(quoted, ", ")))
	if err != nil {
		return nil, fmt.Errorf("inspect existing tables: %w", err)
	}
	var rows []chObject
	if res != nil && len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &rows); err != nil {
			return nil, fmt.Errorf("inspect existing tables: %w", err)
		}
	}
	out := make(map[string]chObject, len(rows))
	for _, r := range rows {
		out[r.Name] = r
	}
	return out, nil
}

func (mz *materializer) databaseEngine() (string, error) {
	res, err := mz.exec("SELECT engine FROM system.databases WHERE name = " + quoteString(mz.model.TargetDatabase))
	if err != nil {
		return "", fmt.Errorf("inspect database engine: %w", err)
	}
	var rows []struct {
		Engine string `json:"engine"`
	}
	if res != nil && len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &rows); err != nil {
			return "", fmt.Errorf("inspect database engine: %w", err)
		}
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].Engine, nil
}

func (mz *materializer) ref(name string) string {
	return quoteIdent(mz.model.TargetDatabase) + "." + quoteIdent(name)
}

// drop removes an object CH-UI created, logging (not returning) failures:
// it only runs while cleaning up after another error or after a swap.
func (mz *materializer) drop(name string) {
	if _, err := mz.exec("DROP TABLE IF EXISTS " + mz.ref(name)); err != nil {
		slog.Warn("Model cleanup failed", "model", mz.model.Name, "table", name, "error", err)
	}
}

func (mz *materializer) mark(name string) error {
	_, err := mz.exec(fmt.Sprintf("ALTER TABLE %s MODIFY COMMENT %s", mz.ref(name), quoteString(OwnerComment(mz.model.ID))))
	if err == nil {
		return nil
	}
	// The marker is a convenience, not a safety requirement: without it the
	// object still counts as ours through a recorded successful run (see
	// ownsTarget). An account without ALTER MODIFY COMMENT, common on installs
	// that predate the marker, must not turn every model run into a failure.
	if isPrivilegeError(err) {
		slog.Warn("Model account cannot set the ownership comment; relying on run history", "model", mz.model.Name, "object", name, "error", err)
		return nil
	}
	return fmt.Errorf("mark %s as built by this model: %w", name, err)
}

// isPrivilegeError reports ClickHouse refusing a statement for lack of grants.
func isPrivilegeError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "ACCESS_DENIED") || strings.Contains(msg, "Not enough privileges") || strings.Contains(msg, "NOT_ENOUGH_PRIVILEGES")
}

func (mz *materializer) createSQL(name, selectSQL string, replaceView bool) string {
	m := mz.model
	if m.Materialization == MaterializationTable {
		return createTableSQL(m.TargetDatabase, name, m.TableEngine, m.OrderBy, selectSQL)
	}
	return createViewSQL(m.TargetDatabase, name, selectSQL, replaceView)
}

// run builds the model and returns the main CREATE statement for the run log.
//
// A view replacing a view uses CREATE OR REPLACE VIEW, which ClickHouse does
// atomically. A table (or a change between table and view) is built under
// <name>__chui_tmp first and only swapped in once it exists, so a failing
// SELECT leaves the previous table untouched.
func (mz *materializer) run(selectSQL string) (string, error) {
	m := mz.model
	if err := ValidateMaterialization(m.Materialization); err != nil {
		return "", err
	}
	name, tmp, old := m.Name, m.Name+tmpSuffix, m.Name+oldSuffix
	objs, err := mz.lookup(name, tmp, old)
	if err != nil {
		return "", err
	}

	target, exists := objs[name]
	if exists {
		owned, err := mz.ownsTarget(target)
		if err != nil {
			return "", err
		}
		if !owned {
			return "", mz.notOwnedErr(name)
		}
	}
	// Leftovers from an interrupted earlier run: drop them only when marked.
	for _, aux := range []string{tmp, old} {
		if o, ok := objs[aux]; ok {
			if o.Comment != OwnerComment(m.ID) {
				return "", mz.notOwnedErr(aux)
			}
			if _, err := mz.exec("DROP TABLE " + mz.ref(aux)); err != nil {
				return "", fmt.Errorf("drop leftover %s: %w", aux, err)
			}
		}
	}

	// Nothing there yet, or a view replacing a view: build under the final name.
	if !exists || (m.Materialization == MaterializationView && target.kind() == MaterializationView) {
		ddl := mz.createSQL(name, selectSQL, exists)
		if _, err := mz.exec(ddl); err != nil {
			return ddl, err
		}
		if err := mz.mark(name); err != nil {
			if !exists {
				mz.drop(name)
			}
			return ddl, err
		}
		return ddl, nil
	}

	ddl := mz.createSQL(tmp, selectSQL, false)
	if _, err := mz.exec(ddl); err != nil {
		mz.drop(tmp)
		return ddl, err
	}
	if err := mz.mark(tmp); err != nil {
		mz.drop(tmp)
		return ddl, err
	}
	if err := mz.swap(name, tmp, old); err != nil {
		return ddl, err
	}
	return ddl, nil
}

// swap replaces name with tmp. Atomic databases swap both names in one
// EXCHANGE TABLES; other engines rename the old table aside first and put it
// back if the second rename fails.
func (mz *materializer) swap(name, tmp, old string) error {
	engine, err := mz.databaseEngine()
	if err != nil {
		mz.drop(tmp)
		return err
	}
	if engine == "Atomic" {
		if _, err := mz.exec(fmt.Sprintf("EXCHANGE TABLES %s AND %s", mz.ref(tmp), mz.ref(name))); err != nil {
			mz.drop(tmp)
			return fmt.Errorf("swap in new %s: %w", name, err)
		}
		mz.drop(tmp) // now holds the previous version
		return nil
	}
	if _, err := mz.exec(fmt.Sprintf("RENAME TABLE %s TO %s", mz.ref(name), mz.ref(old))); err != nil {
		mz.drop(tmp)
		return fmt.Errorf("move previous %s aside: %w", name, err)
	}
	if _, err := mz.exec(fmt.Sprintf("RENAME TABLE %s TO %s", mz.ref(tmp), mz.ref(name))); err != nil {
		var restoreErr error
		if _, restoreErr = mz.exec(fmt.Sprintf("RENAME TABLE %s TO %s", mz.ref(old), mz.ref(name))); restoreErr == nil {
			mz.drop(tmp)
		}
		return errors.Join(fmt.Errorf("swap in new %s: %w", name, err), restoreErr)
	}
	mz.drop(old)
	return nil
}
