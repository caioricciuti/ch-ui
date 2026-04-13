package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/google/uuid"
)

// viewEntry holds a row from system.tables for a View or MaterializedView.
type viewEntry struct {
	Database         string `json:"database"`
	Name             string `json:"name"`
	Engine           string `json:"engine"`
	CreateTableQuery string `json:"create_table_query"`
}

// Regex patterns for parsing SQL table references in view definitions.
// These mirror the patterns in governance/lineage.go but are local to avoid
// exporting internal regex details.
const vgTableRef = "(" +
	"(?:`[^`]+`|[a-zA-Z_][a-zA-Z0-9_]*)" +
	"(?:\\.(?:`[^`]+`|[a-zA-Z_][a-zA-Z0-9_]*))?)"

var (
	vgFromRe = regexp.MustCompile(`(?i)\bFROM\s+` + vgTableRef)
	vgJoinRe = regexp.MustCompile(`(?i)\bJOIN\s+` + vgTableRef)
	vgToRe   = regexp.MustCompile(`(?i)\bTO\s+` + vgTableRef)
)

// GetViewGraph queries ClickHouse for all materialized views and views,
// parses their CREATE statements to build a structural dependency graph,
// and returns it in the same LineageGraph format used by the lineage endpoints.
func (h *GovernanceHandler) GetViewGraph(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	sql := `SELECT database, name, engine, create_table_query
		FROM system.tables
		WHERE engine IN ('MaterializedView', 'View')
		  AND database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		FORMAT JSON`

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID, sql,
		session.ClickhouseUser, password,
		60*time.Second,
	)
	if err != nil {
		slog.Warn("Failed to query system.tables for views", "error", err, "connection", session.ConnectionID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	// Parse ClickHouse JSON response
	raw, err := json.Marshal(result.Data)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to marshal result"})
		return
	}

	var rows []viewEntry
	if err := json.Unmarshal(raw, &rows); err != nil {
		slog.Error("Failed to parse view entries", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse view data"})
		return
	}

	graph := buildViewGraph(rows)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"graph": graph,
	})
}

// buildViewGraph parses CREATE statements from view entries and constructs
// a LineageGraph with nodes (tables, views, MVs) and edges (data flow).
func buildViewGraph(rows []viewEntry) governance.LineageGraph {
	nodeMap := make(map[string]governance.LineageNode)
	var edges []governance.LineageEdge

	for _, row := range rows {
		viewKey := row.Database + "." + row.Name
		nodeType := "materialized_view"
		if strings.EqualFold(row.Engine, "View") {
			nodeType = "view"
		}

		nodeMap[viewKey] = governance.LineageNode{
			ID:       viewKey,
			Database: row.Database,
			Table:    row.Name,
			Type:     nodeType,
		}

		query := normaliseWS(row.CreateTableQuery)

		// For materialized views: extract the TO target table
		var toTarget *parsedRef
		if strings.EqualFold(row.Engine, "MaterializedView") {
			toTarget = extractToTarget(query)
		}

		// Extract source tables from FROM/JOIN clauses in the AS SELECT part
		sources := extractViewSources(query)

		// Create edges: source → view
		for _, src := range sources {
			if isSystemDB(src.db) {
				continue
			}
			srcKey := src.key()
			if srcKey == viewKey {
				continue // skip self-reference
			}

			// Ensure source node exists
			if _, ok := nodeMap[srcKey]; !ok {
				nodeMap[srcKey] = governance.LineageNode{
					ID:       srcKey,
					Database: src.db,
					Table:    src.table,
					Type:     "source",
				}
			}

			edges = append(edges, governance.LineageEdge{
				ID:             uuid.New().String(),
				ConnectionID:   "",
				SourceDatabase: src.db,
				SourceTable:    src.table,
				TargetDatabase: row.Database,
				TargetTable:    row.Name,
				EdgeType:       "view_dependency",
			})
		}

		// Create edge: MV → TO target
		if toTarget != nil && !isSystemDB(toTarget.db) {
			tgtKey := toTarget.key()
			if tgtKey != viewKey {
				if _, ok := nodeMap[tgtKey]; !ok {
					nodeMap[tgtKey] = governance.LineageNode{
						ID:       tgtKey,
						Database: toTarget.db,
						Table:    toTarget.table,
						Type:     "target",
					}
				}

				edges = append(edges, governance.LineageEdge{
					ID:             uuid.New().String(),
					ConnectionID:   "",
					SourceDatabase: row.Database,
					SourceTable:    row.Name,
					TargetDatabase: toTarget.db,
					TargetTable:    toTarget.table,
					EdgeType:       "materialized_to",
				})
			}
		}
	}

	nodes := make([]governance.LineageNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}

	if edges == nil {
		edges = []governance.LineageEdge{}
	}

	return governance.LineageGraph{
		Nodes: nodes,
		Edges: edges,
	}
}

// parsedRef is a database.table reference extracted from SQL.
type parsedRef struct {
	db    string
	table string
}

func (r parsedRef) key() string {
	if r.db == "" {
		return r.table
	}
	return r.db + "." + r.table
}

// extractToTarget extracts the TO target table from a materialized view definition.
func extractToTarget(query string) *parsedRef {
	m := vgToRe.FindStringSubmatch(query)
	if len(m) < 2 {
		return nil
	}
	db, tbl := splitRef(m[1])
	return &parsedRef{db: db, table: tbl}
}

// extractViewSources finds all FROM and JOIN table references in a view definition.
func extractViewSources(query string) []parsedRef {
	seen := map[string]bool{}
	var results []parsedRef

	collect := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(query, -1) {
			if len(m) < 2 {
				continue
			}
			db, tbl := splitRef(m[1])
			if isSystemDB(db) {
				continue
			}
			key := db + "." + tbl
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, parsedRef{db: db, table: tbl})
		}
	}

	collect(vgFromRe)
	collect(vgJoinRe)

	return results
}

// splitRef splits a possibly qualified table reference into (database, table).
func splitRef(raw string) (string, string) {
	raw = stripBT(raw)
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) == 2 {
		return stripBT(parts[0]), stripBT(parts[1])
	}
	return "", stripBT(parts[0])
}

// stripBT removes surrounding backticks.
func stripBT(s string) string {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// normaliseWS collapses whitespace runs into single spaces.
func normaliseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// isSystemDB returns true for ClickHouse system databases.
func isSystemDB(db string) bool {
	switch strings.ToLower(db) {
	case "system", "information_schema":
		return true
	}
	return false
}
