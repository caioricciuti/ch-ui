package governance

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Regex patterns for table references ─────────────────────────────────────

// tableRef matches a possibly qualified table name: [db.]table
// Handles both backtick-quoted and unquoted identifiers.
const tableRefPattern = `((?:` + "`" + `[^` + "`" + `]+` + "`" + `|[a-zA-Z_][a-zA-Z0-9_]*)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|[a-zA-Z_][a-zA-Z0-9_]*))?)`

var (
	fromRe   = regexp.MustCompile(`(?i)\bFROM\s+` + tableRefPattern)
	joinRe   = regexp.MustCompile(`(?i)\bJOIN\s+` + tableRefPattern)
	insertRe = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+` + tableRefPattern)
	createRe = regexp.MustCompile(`(?i)\bCREATE\s+(?:TABLE|MATERIALIZED\s+VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?` + tableRefPattern)
)

// tableRefParsed holds a parsed database.table reference.
type tableRefParsed struct {
	Database string
	Table    string
}

// ── Public API ──────────────────────────────────────────────────────────────

// ExtractLineage analyses a query log entry and returns any lineage edges
// that can be inferred from the SQL text.  Only INSERT INTO ... SELECT and
// CREATE TABLE/MATERIALIZED VIEW ... AS SELECT produce edges; plain SELECTs
// are reads and do not generate edges.
func ExtractLineage(connectionID string, entry QueryLogEntry) []LineageEdge {
	query := normaliseWhitespace(entry.QueryText)

	// Determine target table (INSERT INTO / CREATE TABLE|MV).
	target := extractTarget(query)
	if target == nil {
		// Plain SELECT or DDL without a target — no lineage edges.
		return nil
	}

	// Determine edge type.
	edgeType := classifyEdgeType(query)

	// Collect source tables (FROM / JOIN), excluding the target itself and
	// system tables.
	sources := extractSourceTables(query)
	if len(sources) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var edges []LineageEdge
	for _, src := range sources {
		// Skip self-references and system tables.
		if src.Database == target.Database && src.Table == target.Table {
			continue
		}
		if isSystemTable(src.Database, src.Table) {
			continue
		}

		edges = append(edges, LineageEdge{
			ID:             uuid.New().String(),
			ConnectionID:   connectionID,
			SourceDatabase: src.Database,
			SourceTable:    src.Table,
			TargetDatabase: target.Database,
			TargetTable:    target.Table,
			QueryID:        entry.QueryID,
			User:           entry.User,
			EdgeType:       string(edgeType),
			DetectedAt:     now,
		})
	}

	return edges
}

// ── Internal helpers ────────────────────────────────────────────────────────

// extractTarget returns the target table for INSERT INTO or CREATE TABLE/MV
// statements.  Returns nil when none is found.
func extractTarget(query string) *tableRefParsed {
	// Try INSERT INTO first.
	if m := insertRe.FindStringSubmatch(query); len(m) > 1 {
		db, tbl := parseTableRef(m[1:])
		return &tableRefParsed{Database: db, Table: tbl}
	}
	// Try CREATE TABLE / MATERIALIZED VIEW.
	if m := createRe.FindStringSubmatch(query); len(m) > 1 {
		db, tbl := parseTableRef(m[1:])
		return &tableRefParsed{Database: db, Table: tbl}
	}
	return nil
}

// classifyEdgeType returns the edge type string based on the SQL verb.
func classifyEdgeType(query string) EdgeType {
	upper := strings.ToUpper(query)
	if strings.Contains(upper, "INSERT") {
		return EdgeInsertSelect
	}
	if strings.Contains(upper, "CREATE") {
		return EdgeCreateAsSelect
	}
	return EdgeSelectFrom
}

// extractSourceTables finds all FROM and JOIN table references in the query.
func extractSourceTables(query string) []tableRefParsed {
	seen := map[string]bool{}
	var results []tableRefParsed

	addMatches := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(query, -1) {
			if len(m) < 2 {
				continue
			}
			db, tbl := parseTableRef(m[1:])
			if isSystemTable(db, tbl) {
				continue
			}
			key := db + "." + tbl
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, tableRefParsed{Database: db, Table: tbl})
		}
	}

	addMatches(fromRe)
	addMatches(joinRe)

	return results
}

// parseTableRef takes the captured groups from a table-reference regex match
// and splits them into (database, table).  If no database qualifier is
// present, database is returned as an empty string.
func parseTableRef(groups []string) (database, table string) {
	if len(groups) == 0 {
		return "", ""
	}
	raw := groups[0]
	raw = stripBackticks(raw)

	parts := strings.SplitN(raw, ".", 2)
	if len(parts) == 2 {
		return stripBackticks(parts[0]), stripBackticks(parts[1])
	}
	return "", stripBackticks(parts[0])
}

// stripBackticks removes surrounding backticks from an identifier.
func stripBackticks(s string) string {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// isSystemTable returns true for ClickHouse system and information_schema
// databases that should be excluded from lineage graphs.
func isSystemTable(db, table string) bool {
	lower := strings.ToLower(db)
	switch lower {
	case "system", "information_schema", "information_schema_upper", "INFORMATION_SCHEMA":
		return true
	}
	// Also filter tables that look like system tables when no db is specified.
	if db == "" {
		lowerT := strings.ToLower(table)
		if strings.HasPrefix(lowerT, "system.") || strings.HasPrefix(lowerT, "information_schema.") {
			return true
		}
	}
	return false
}

// normaliseWhitespace collapses runs of whitespace into single spaces and
// trims the result.  This simplifies regex matching.
func normaliseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
