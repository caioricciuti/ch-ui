package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ── Text-to-SQL grounded in workspace metadata ─────────────────────────────
// POST /api/brain/generate-sql  Body: { question, model_id? }
// Retrieves the relevant ClickHouse schema (tables/columns) for the active
// connection plus any documented models, grounds an LLM call with that
// context, and returns a single ClickHouse SQL query. Reuses the Brain
// provider/model resolution, so it honors the admin-configured AI providers.
// Pro feature, like Brain's agentic tools.

const (
	tts2sqlMaxTables  = 16
	tts2sqlSchemaWait = 20 * time.Second
	tts2sqlGenWait    = 60 * time.Second
)

// GenerateSQL handles text-to-SQL generation.
func (h *BrainHandler) GenerateSQL(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	// Gate like Brain's agentic tools: schema-grounded generation is Pro-only.
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Ask AI (text-to-SQL) requires a Pro license")
		return
	}

	var body struct {
		Question string `json:"question"`
		ModelID  string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, "Question is required")
		return
	}

	runtimeModel, rmErr := h.resolveRuntimeModel(nil, strings.TrimSpace(body.ModelID))
	if rmErr != nil {
		writeError(w, http.StatusBadRequest, rmErr.Error())
		return
	}
	provider, err := braincore.NewProvider(runtimeModel.ProviderKind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	providerCfg := braincore.ProviderConfig{Kind: runtimeModel.ProviderKind}
	if runtimeModel.ProviderBaseURL != nil {
		providerCfg.BaseURL = *runtimeModel.ProviderBaseURL
	}
	if runtimeModel.ProviderEncryptedKey != nil {
		decrypted, decErr := crypto.Decrypt(*runtimeModel.ProviderEncryptedKey, h.Config.AppSecretKey)
		if decErr != nil {
			writeError(w, http.StatusInternalServerError, "Failed to decrypt provider API key")
			return
		}
		providerCfg.APIKey = decrypted
	}

	chPassword, decErr := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if decErr != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	schema, tables := h.buildSchemaContext(session.ConnectionID, session.ClickhouseUser, chPassword, question)
	modelCtx := h.buildModelContext(session.ConnectionID)

	sysPrompt := "You are an expert ClickHouse SQL engineer. Write ONE correct ClickHouse SQL query that answers the user's question.\n" +
		"Rules:\n" +
		"- Use ONLY the tables and columns provided in the schema below. Never invent names.\n" +
		"- Prefer fully-qualified `database.table` names.\n" +
		"- Use ClickHouse syntax and functions (not Postgres/MySQL).\n" +
		"- If the question can't be answered from the schema, return a comment explaining what's missing.\n" +
		"- Output ONLY the SQL — no prose, no markdown fences."

	var user strings.Builder
	user.WriteString("# Available schema\n\n")
	if schema == "" {
		user.WriteString("(no user tables found)\n")
	} else {
		user.WriteString(schema)
	}
	if modelCtx != "" {
		user.WriteString("\n# Documented models (curated, prefer these when relevant)\n\n")
		user.WriteString(modelCtx)
	}
	user.WriteString("\n# Question\n\n")
	user.WriteString(question)
	user.WriteString("\n\nReturn only the ClickHouse SQL.")

	ctx, cancel := context.WithTimeout(r.Context(), tts2sqlGenWait)
	defer cancel()
	raw, genErr := completeOnce(ctx, provider, providerCfg, runtimeModel.ModelName, []braincore.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: user.String()},
	})
	if genErr != nil {
		writeError(w, http.StatusBadGateway, "AI generation failed: "+genErr.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "brain.generate_sql",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(question),
		IPAddress:      strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"sql":         stripSQLFences(raw),
		"tables_used": tables,
		"model":       runtimeModel.ModelName,
	})
}

// completeOnce runs a non-streaming completion by collecting stream deltas.
func completeOnce(ctx context.Context, p braincore.Provider, cfg braincore.ProviderConfig, model string, messages []braincore.Message) (string, error) {
	var sb strings.Builder
	_, err := p.StreamChat(ctx, cfg, model, messages, func(delta string) error {
		sb.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", err
	}
	return sb.String(), nil
}

// buildSchemaContext picks the tables most relevant to the question and
// renders their columns as compact DDL-ish text. Returns the rendered schema
// and the list of qualified table names included. Introspection goes through
// the same tunnel query path Brain's tools use (Gateway.ExecuteQuery).
func (h *BrainHandler) buildSchemaContext(connID, chUser, chPassword, question string) (string, []string) {
	runSelect := func(sql string, timeout time.Duration) []map[string]any {
		res, err := h.Gateway.ExecuteQuery(connID, sql, chUser, chPassword, timeout)
		if err != nil || res == nil {
			return nil
		}
		var rows []map[string]any
		if json.Unmarshal(res.Data, &rows) != nil {
			return nil
		}
		return rows
	}

	tableRows := runSelect(
		`SELECT database, name FROM system.tables
		 WHERE database NOT IN ('system','information_schema','INFORMATION_SCHEMA')
		   AND engine NOT LIKE '%View' ORDER BY database, name`, tts2sqlSchemaWait)
	if len(tableRows) == 0 {
		return "", nil
	}

	type tbl struct{ db, name string }
	var all []tbl
	for _, row := range tableRows {
		d, _ := row["database"].(string)
		n, _ := row["name"].(string)
		if d != "" && n != "" {
			all = append(all, tbl{d, n})
		}
	}

	// Rank by keyword overlap between the question and the qualified name.
	tokens := tokenize(question)
	scored := make([]struct {
		t     tbl
		score int
	}, len(all))
	for i, t := range all {
		q := strings.ToLower(t.db + "." + t.name)
		s := 0
		for tok := range tokens {
			if strings.Contains(q, tok) {
				s += 2
			} else if strings.Contains(q, singularize(tok)) {
				s++
			}
		}
		scored[i].t = t
		scored[i].score = s
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

	limit := tts2sqlMaxTables
	if len(scored) < limit {
		limit = len(scored)
	}
	picked := scored[:limit]

	// Fetch columns for the picked tables in one query.
	var conds []string
	qualified := make([]string, 0, limit)
	for _, p := range picked {
		conds = append(conds, fmt.Sprintf("(database='%s' AND table='%s')",
			sqlSingleQuote(p.t.db), sqlSingleQuote(p.t.name)))
		qualified = append(qualified, p.t.db+"."+p.t.name)
	}
	if len(conds) == 0 {
		return "", nil
	}
	colRows := runSelect(
		`SELECT database, table, name, type FROM system.columns WHERE `+
			strings.Join(conds, " OR ")+` ORDER BY database, table, position`, tts2sqlSchemaWait)

	cols := map[string][]string{}
	for _, row := range colRows {
		d, _ := row["database"].(string)
		t, _ := row["table"].(string)
		n, _ := row["name"].(string)
		ty, _ := row["type"].(string)
		key := d + "." + t
		cols[key] = append(cols[key], fmt.Sprintf("  %s %s", n, ty))
	}

	var sb strings.Builder
	for _, q := range qualified {
		c := cols[q]
		if len(c) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s (\n%s\n)\n\n", q, strings.Join(c, ",\n")))
	}
	return sb.String(), qualified
}

// buildModelContext adds curated model names + descriptions for the connection.
func (h *BrainHandler) buildModelContext(connID string) string {
	models, err := h.DB.GetModelsByConnection(connID)
	if err != nil || len(models) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, m := range models {
		desc := strings.TrimSpace(m.Description)
		if desc == "" {
			desc = "(no description)"
		}
		sb.WriteString(fmt.Sprintf("- %s.%s — %s\n", m.TargetDatabase, m.Name, desc))
	}
	return sb.String()
}

// ── helpers ────────────────────────────────────────────────────────────────

func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(w) >= 3 && !sqlStopWords[w] {
			out[w] = true
		}
	}
	return out
}

func singularize(w string) string {
	if len(w) > 3 && strings.HasSuffix(w, "s") {
		return w[:len(w)-1]
	}
	return w
}

func sqlSingleQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

// stripSQLFences removes ```sql fences and surrounding whitespace the model
// may add despite instructions.
func stripSQLFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl != -1 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

var sqlStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "show": true,
	"get": true, "list": true, "all": true, "how": true, "many": true, "what": true,
	"which": true, "are": true, "was": true, "were": true, "per": true, "top": true,
	"count": true, "number": true, "total": true, "give": true, "find": true, "that": true,
}
