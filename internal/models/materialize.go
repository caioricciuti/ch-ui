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
	MaterializationView             = "view"
	MaterializationTable            = "table"
	MaterializationIncremental      = "incremental"
	MaterializationMaterializedView = "materialized_view"
)

const (
	defaultTableEngine = "MergeTree"
	defaultOrderBy     = "tuple()"
	// A table rebuild writes into <name>__chui_tmp and swaps it in; the
	// rename fallback parks the previous table at <name>__chui_old.
	tmpSuffix = "__chui_tmp"
	oldSuffix = "__chui_old"
	// A materialized_view model keeps its trigger at <name>__mv, writing TO
	// the table <name>.
	mvSuffix = "__mv"
)

// ValidateMaterialization accepts only the materializations the runner builds.
func ValidateMaterialization(m string) error {
	switch m {
	case MaterializationView, MaterializationTable, MaterializationIncremental, MaterializationMaterializedView:
		return nil
	}
	return fmt.Errorf("materialization must be 'view', 'table', 'incremental' or 'materialized_view' (got %q)", m)
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

// wrapSelect embeds a SELECT as a subquery. The newlines keep a trailing line
// comment in the body from swallowing the closing parenthesis.
func wrapSelect(selectSQL string) string {
	return "(\n" + cleanSelect(selectSQL) + "\n)"
}

// tableSpec is everything after the table name in a CREATE TABLE.
type tableSpec struct {
	engine, engineArgs, partitionBy, orderBy string
}

func specOf(m database.Model) tableSpec {
	return tableSpec{engine: m.TableEngine, engineArgs: m.EngineArgs, partitionBy: m.PartitionBy, orderBy: m.OrderBy}
}

// clause renders ENGINE = ... [PARTITION BY ...] [ORDER BY ...]. PARTITION BY
// and ORDER BY are only sent for MergeTree-family engines.
func (s tableSpec) clause() string {
	engine, orderBy := TableDefaults(s.engine, s.orderBy)
	out := "ENGINE = " + engine
	if args := strings.TrimSpace(s.engineArgs); args != "" {
		out += "(" + args + ")"
	}
	if engineTakesOrderBy(engine) {
		if p := strings.TrimSpace(s.partitionBy); p != "" {
			out += " PARTITION BY " + p
		}
		out += " ORDER BY " + orderBy
	}
	return out
}

func createTableSQL(db, name, engine, orderBy, selectSQL string) string {
	return createTableFromSpec(db, name, tableSpec{engine: engine, orderBy: orderBy}, selectSQL, false)
}

// createTableFromSpec renders CREATE TABLE ... AS <select>; empty creates the
// table with the SELECT's columns but no rows.
func createTableFromSpec(db, name string, s tableSpec, selectSQL string, empty bool) string {
	emptyKw := ""
	if empty {
		emptyKw = " EMPTY"
	}
	return fmt.Sprintf("CREATE TABLE %s.%s %s%s AS %s",
		quoteIdent(db), quoteIdent(name), s.clause(), emptyKw, cleanSelect(selectSQL))
}

func createViewSQL(db, name, selectSQL string, replace bool) string {
	verb := "CREATE VIEW"
	if replace {
		verb = "CREATE OR REPLACE VIEW"
	}
	return fmt.Sprintf("%s %s.%s AS %s", verb, quoteIdent(db), quoteIdent(name), cleanSelect(selectSQL))
}

func createMVSQL(db, mv, target, selectSQL string) string {
	return fmt.Sprintf("CREATE MATERIALIZED VIEW %s.%s TO %s.%s AS %s",
		quoteIdent(db), quoteIdent(mv), quoteIdent(db), quoteIdent(target), cleanSelect(selectSQL))
}

// keyTuple renders a comma-separated column list as a quoted tuple.
func keyTuple(cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = quoteIdent(strings.TrimSpace(p))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// incrementalSource is the SELECT an incremental run inserts: the model's
// rows, limited to those past the target's watermark when one is set.
// delete_insert uses >= so rows at the boundary are replaced, not skipped.
func incrementalSource(m database.Model, target, selectSQL string) string {
	src := "SELECT * FROM " + wrapSelect(selectSQL)
	if wm := m.WatermarkColumn; wm != "" {
		op := ">"
		if m.IncrementalStrategy == StrategyDeleteInsert {
			op = ">="
		}
		src += fmt.Sprintf(" WHERE %s %s (SELECT max(%s) FROM %s)", quoteIdent(wm), op, quoteIdent(wm), target)
	}
	return src
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
	case "MaterializedView":
		return MaterializationMaterializedView
	case "LiveView", "WindowView", "Dictionary":
		return "other"
	}
	return MaterializationTable
}

// targetKind is the kind of object a materialization leaves at the model's
// name: a view for views, a table for everything else.
func targetKind(materialization string) string {
	if materialization == MaterializationView {
		return MaterializationView
	}
	return MaterializationTable
}

// materializer builds one model into ClickHouse.
type materializer struct {
	exec  chExec
	model database.Model
	// priorSuccess reports whether this model has a successful run on record.
	priorSuccess func() (bool, error)
	// fullRefresh rebuilds incremental and materialized_view models from
	// scratch through the safe swap.
	fullRefresh bool
	// usesThis is set when the SQL reads $this (the model's own table).
	usesThis bool
	// log collects the statements that changed ClickHouse, for the run log.
	log []string
}

// do runs a statement that changes ClickHouse and records it.
func (mz *materializer) do(sql string) error {
	mz.log = append(mz.log, sql)
	_, err := mz.exec(sql)
	return err
}

func (mz *materializer) statements() string {
	return strings.Join(mz.log, ";\n\n")
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
	return mz.owns(o, targetKind(mz.model.Materialization))
}

func (mz *materializer) owns(o chObject, wantKind string) (bool, error) {
	if o.Comment == OwnerComment(mz.model.ID) {
		return true, nil
	}
	if o.Comment != "" || o.kind() != wantKind {
		return false, nil
	}
	return mz.priorSuccess()
}

func (mz *materializer) notOwnedErr(name string) error {
	return fmt.Errorf("%s.%s: a table with this name already exists and was not created by this model; rename the model or drop the table yourself",
		mz.model.TargetDatabase, name)
}

// drop removes an object CH-UI created, logging (not returning) failures:
// it only runs while cleaning up after another error or after a swap.
func (mz *materializer) drop(name string) {
	if _, err := mz.exec("DROP TABLE IF EXISTS " + mz.ref(name)); err != nil {
		slog.Warn("Model cleanup failed", "model", mz.model.Name, "table", name, "error", err)
	}
}

// checkExpressions has ClickHouse parse every user expression that goes into
// DDL, so a malformed one fails before anything is created or dropped.
func (mz *materializer) checkExpressions() error {
	m := mz.model
	if !BuildsTable(m.Materialization) {
		return nil
	}
	checks := []struct{ label, value, probe string }{
		{"PARTITION BY", m.PartitionBy, "EXPLAIN SYNTAX SELECT (%s)"},
		{"engine arguments", m.EngineArgs, "EXPLAIN SYNTAX SELECT tuple(%s)"},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.value) == "" {
			continue
		}
		if err := ValidateExpression(c.label, c.value); err != nil {
			return err
		}
		if _, err := mz.exec(fmt.Sprintf(c.probe, c.value)); err != nil {
			return fmt.Errorf("%s is not a valid expression: %w", c.label, err)
		}
	}
	return nil
}

// run builds the model and returns the statements that changed ClickHouse,
// for the run log.
//
// A view replacing a view uses CREATE OR REPLACE VIEW, which ClickHouse does
// atomically. A table (or a change between table and view) is built under
// <name>__chui_tmp first and only swapped in once it exists, so a failing
// SELECT leaves the previous table untouched. Incremental models insert into
// their existing table; materialized views keep their table and update the
// trigger's query.
func (mz *materializer) run(selectSQL string) (string, error) {
	err := mz.build(selectSQL)
	return mz.statements(), err
}

func (mz *materializer) build(selectSQL string) error {
	m := mz.model
	if err := ValidateMaterialization(m.Materialization); err != nil {
		return err
	}
	if err := mz.checkExpressions(); err != nil {
		return err
	}
	name, tmp, old, mv := m.Name, m.Name+tmpSuffix, m.Name+oldSuffix, m.Name+mvSuffix
	objs, err := mz.lookup(name, tmp, old, mv)
	if err != nil {
		return err
	}

	target, exists := objs[name]
	if exists {
		owned, err := mz.ownsTarget(target)
		if err != nil {
			return err
		}
		if !owned {
			return mz.notOwnedErr(name)
		}
	}
	// Leftovers from an interrupted earlier run: drop them only when marked.
	for _, aux := range []string{tmp, old} {
		if o, ok := objs[aux]; ok {
			if o.Comment != OwnerComment(m.ID) {
				return mz.notOwnedErr(aux)
			}
			if err := mz.do("DROP TABLE " + mz.ref(aux)); err != nil {
				return fmt.Errorf("drop leftover %s: %w", aux, err)
			}
		}
	}
	// The trigger of a materialized_view model. Another materialization
	// must not leave it writing into the table, so it goes first.
	mvObj, mvExists := objs[mv]
	if mvExists {
		owned, err := mz.owns(mvObj, MaterializationMaterializedView)
		if err != nil {
			return err
		}
		if !owned {
			return mz.notOwnedErr(mv)
		}
		if m.Materialization != MaterializationMaterializedView {
			if err := mz.do("DROP VIEW " + mz.ref(mv)); err != nil {
				return fmt.Errorf("drop materialized view %s: %w", mv, err)
			}
			mvExists = false
		}
	}

	switch m.Materialization {
	case MaterializationIncremental:
		return mz.incremental(selectSQL, target, exists)
	case MaterializationMaterializedView:
		return mz.materializedView(selectSQL, target, exists, mvExists)
	}
	return mz.fullBuild(selectSQL, target, exists)
}

// fullBuild creates the view or table from scratch: directly under its name
// when nothing is there (or a view replaces a view), otherwise under
// <name>__chui_tmp and swapped in.
func (mz *materializer) fullBuild(selectSQL string, target chObject, exists bool) error {
	m := mz.model
	name, tmp, old := m.Name, m.Name+tmpSuffix, m.Name+oldSuffix
	if m.Materialization == MaterializationView {
		if !exists || target.kind() == MaterializationView {
			if err := mz.do(createViewSQL(m.TargetDatabase, name, selectSQL, exists)); err != nil {
				return err
			}
			if err := mz.mark(name); err != nil {
				if !exists {
					mz.drop(name)
				}
				return err
			}
			return nil
		}
		if err := mz.do(createViewSQL(m.TargetDatabase, tmp, selectSQL, false)); err != nil {
			mz.drop(tmp)
			return err
		}
	} else {
		if !exists {
			if mz.usesThis {
				return fmt.Errorf("the SQL reads $this, but %s.%s does not exist yet; build it once without $this (use watermark_column for incremental filters)", m.TargetDatabase, name)
			}
			if err := mz.do(createTableFromSpec(m.TargetDatabase, name, specOf(m), selectSQL, false)); err != nil {
				return err
			}
			if err := mz.mark(name); err != nil {
				mz.drop(name)
				return err
			}
			return nil
		}
		if err := mz.do(createTableFromSpec(m.TargetDatabase, tmp, specOf(m), selectSQL, false)); err != nil {
			mz.drop(tmp)
			return err
		}
	}
	if err := mz.mark(tmp); err != nil {
		mz.drop(tmp)
		return err
	}
	return mz.swap(name, tmp, old)
}

// incremental adds new rows to the model's table. The first run (no table
// yet), a full refresh, or a table that is not a table (the model was a view)
// builds it in full instead.
func (mz *materializer) incremental(selectSQL string, target chObject, exists bool) error {
	m := mz.model
	if !exists || mz.fullRefresh || target.kind() != MaterializationTable {
		return mz.fullBuild(selectSQL, target, exists)
	}
	ref := mz.ref(m.Name)
	src := incrementalSource(m, ref, selectSQL)
	hint := func(err error) error {
		return fmt.Errorf("%w (if the model's columns changed, run a full refresh)", err)
	}
	if m.IncrementalStrategy != StrategyDeleteInsert {
		if err := mz.do("INSERT INTO " + ref + " " + src); err != nil {
			return hint(err)
		}
		return nil
	}

	// delete_insert: stage the new rows, delete the keys they replace, then
	// insert them. The staging table has the target's exact structure.
	tmp := m.Name + tmpSuffix
	tmpRef := mz.ref(tmp)
	if err := mz.do(fmt.Sprintf("CREATE TABLE %s AS %s", tmpRef, ref)); err != nil {
		return err
	}
	if err := mz.mark(tmp); err != nil {
		mz.drop(tmp)
		return err
	}
	defer mz.drop(tmp)
	if err := mz.do("INSERT INTO " + tmpRef + " " + src); err != nil {
		return hint(err)
	}
	key := keyTuple(m.UniqueKey)
	cond := fmt.Sprintf("%s IN (SELECT %s FROM %s)", key, key, tmpRef)
	if err := mz.do("DELETE FROM " + ref + " WHERE " + cond); err != nil {
		// Lightweight DELETE is not available everywhere (older servers,
		// tables with projections); a synchronous mutation does the same.
		if err2 := mz.do(fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s SETTINGS mutations_sync = 2", ref, cond)); err2 != nil {
			return errors.Join(fmt.Errorf("delete replaced rows: %w", err), err2)
		}
	}
	if err := mz.do("INSERT INTO " + ref + " SELECT * FROM " + tmpRef); err != nil {
		return fmt.Errorf("insert new rows: %w", err)
	}
	return nil
}

// materializedView keeps <name> (a table) fed by the trigger <name>__mv.
//
//   - No table yet: create it empty, create the trigger, then backfill it
//     with the SELECT over the existing data.
//   - Full refresh (or <name> is not a table): build a new table, backfill it,
//     drop the trigger, swap the table in and recreate the trigger.
//   - Otherwise: the SELECT must still produce the table's columns; the
//     trigger's query is replaced in place (ALTER TABLE ... MODIFY QUERY).
func (mz *materializer) materializedView(selectSQL string, target chObject, exists, mvExists bool) error {
	m := mz.model
	name, tmp, old, mv := m.Name, m.Name+tmpSuffix, m.Name+oldSuffix, m.Name+mvSuffix
	ref, mvRef := mz.ref(name), mz.ref(mv)
	createMV := func() error {
		if err := mz.do(createMVSQL(m.TargetDatabase, mv, name, selectSQL)); err != nil {
			return fmt.Errorf("create materialized view %s: %w", mv, err)
		}
		return mz.mark(mv)
	}

	if !exists {
		if mvExists {
			if err := mz.do("DROP VIEW " + mvRef); err != nil {
				return err
			}
		}
		if err := mz.do(createTableFromSpec(m.TargetDatabase, name, specOf(m), selectSQL, true)); err != nil {
			return err
		}
		if err := mz.mark(name); err != nil {
			mz.drop(name)
			return err
		}
		if err := createMV(); err != nil {
			return err
		}
		if err := mz.do("INSERT INTO " + ref + " " + cleanSelect(selectSQL)); err != nil {
			return fmt.Errorf("backfill %s: %w", name, err)
		}
		return nil
	}

	if mz.fullRefresh || target.kind() != MaterializationTable {
		if err := mz.do(createTableFromSpec(m.TargetDatabase, tmp, specOf(m), selectSQL, true)); err != nil {
			mz.drop(tmp)
			return err
		}
		if err := mz.mark(tmp); err != nil {
			mz.drop(tmp)
			return err
		}
		if err := mz.do("INSERT INTO " + mz.ref(tmp) + " " + cleanSelect(selectSQL)); err != nil {
			mz.drop(tmp)
			return fmt.Errorf("backfill %s: %w", tmp, err)
		}
		if mvExists {
			if err := mz.do("DROP VIEW " + mvRef); err != nil {
				mz.drop(tmp)
				return err
			}
		}
		if err := mz.swap(name, tmp, old); err != nil {
			return err
		}
		return createMV()
	}

	same, err := mz.sameColumns(name, selectSQL)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("the SELECT no longer produces the columns of %s.%s; run a full refresh to rebuild it", m.TargetDatabase, name)
	}
	if !mvExists {
		return createMV()
	}
	if err := mz.do("ALTER TABLE " + mvRef + " MODIFY QUERY " + cleanSelect(selectSQL)); err != nil {
		// Servers without MODIFY QUERY: recreate the trigger.
		if dropErr := mz.do("DROP VIEW " + mvRef); dropErr != nil {
			return errors.Join(err, dropErr)
		}
		return createMV()
	}
	return mz.mark(mv)
}

type columnDef struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func decodeColumns(res *tunnel.QueryResult) ([]columnDef, error) {
	var cols []columnDef
	if res != nil && len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &cols); err != nil {
			return nil, err
		}
	}
	return cols, nil
}

// sameColumns compares the SELECT's result columns with the table's, by name
// and type (a materialized view inserts by name, so order does not matter).
func (mz *materializer) sameColumns(table, selectSQL string) (bool, error) {
	res, err := mz.exec("DESCRIBE TABLE " + wrapSelect(selectSQL))
	if err != nil {
		return false, fmt.Errorf("describe the SELECT: %w", err)
	}
	want, err := decodeColumns(res)
	if err != nil {
		return false, fmt.Errorf("describe the SELECT: %w", err)
	}
	res, err = mz.exec(fmt.Sprintf("SELECT name, type FROM system.columns WHERE database = %s AND table = %s",
		quoteString(mz.model.TargetDatabase), quoteString(table)))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	have, err := decodeColumns(res)
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	if len(want) != len(have) {
		return false, nil
	}
	types := make(map[string]string, len(have))
	for _, c := range have {
		types[c.Name] = c.Type
	}
	for _, c := range want {
		if t, ok := types[c.Name]; !ok || t != c.Type {
			return false, nil
		}
	}
	return true, nil
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
		if err := mz.do(fmt.Sprintf("EXCHANGE TABLES %s AND %s", mz.ref(tmp), mz.ref(name))); err != nil {
			mz.drop(tmp)
			return fmt.Errorf("swap in new %s: %w", name, err)
		}
		mz.drop(tmp) // now holds the previous version
		return nil
	}
	if err := mz.do(fmt.Sprintf("RENAME TABLE %s TO %s", mz.ref(name), mz.ref(old))); err != nil {
		mz.drop(tmp)
		return fmt.Errorf("move previous %s aside: %w", name, err)
	}
	if err := mz.do(fmt.Sprintf("RENAME TABLE %s TO %s", mz.ref(tmp), mz.ref(name))); err != nil {
		var restoreErr error
		if restoreErr = mz.do(fmt.Sprintf("RENAME TABLE %s TO %s", mz.ref(old), mz.ref(name))); restoreErr == nil {
			mz.drop(tmp)
		}
		return errors.Join(fmt.Errorf("swap in new %s: %w", name, err), restoreErr)
	}
	mz.drop(old)
	return nil
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
