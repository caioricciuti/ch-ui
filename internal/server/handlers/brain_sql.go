// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/brain/tools"
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
	// tts2sqlMaxCurrentSQL bounds the editor SQL sent along.
	tts2sqlMaxCurrentSQL = 20000
	// tts2sqlColumnScan bounds the column names read for ranking.
	tts2sqlColumnScan = 20000
)

// sqlGenJob is one prepared Ask AI request.
type sqlGenJob struct {
	actor        string
	question     string
	runtimeModel *database.BrainModelRuntime
	provider     braincore.Provider
	providerCfg  braincore.ProviderConfig
	messages     []braincore.Message
	tables       []string
	prices       braincore.PriceTable
}

// prepareGenerateSQL validates an Ask AI request, checks the budget,
// resolves the quick model and builds the grounded prompt. It writes the
// error response itself and returns nil when the request cannot go on.
func (h *BrainHandler) prepareGenerateSQL(w http.ResponseWriter, r *http.Request) *sqlGenJob {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil
	}
	// Gate like Brain's agentic tools: schema-grounded generation is Pro-only.
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Ask AI (text-to-SQL) requires a Pro license")
		return nil
	}

	var body struct {
		Question string `json:"question"`
		ModelID  string `json:"model_id"`
		// CurrentSQL is the editor's SQL, sent when the person asks to
		// change the query they are looking at.
		CurrentSQL string `json:"current_sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return nil
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, "Question is required")
		return nil
	}

	settings := loadBrainSettings(h.DB)
	actor := middleware.Actor(session)
	if msg := budgetExceeded(h.DB, actor, settings); msg != "" {
		writeError(w, http.StatusTooManyRequests, msg)
		return nil
	}
	preferred := settings.QuickModelID
	if preferred == "" {
		preferred = settings.AgentModelID
	}
	runtimeModel, rmErr := h.resolveRuntimeModel(nil, strings.TrimSpace(body.ModelID), preferred)
	if rmErr != nil {
		writeError(w, http.StatusBadRequest, rmErr.Error())
		return nil
	}
	provider, err := braincore.NewProvider(runtimeModel.ProviderKind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil
	}
	providerCfg := braincore.ProviderConfig{Kind: runtimeModel.ProviderKind, MaxTokens: settings.MaxOutputTokens}
	if runtimeModel.ProviderBaseURL != nil {
		providerCfg.BaseURL = *runtimeModel.ProviderBaseURL
	}
	if runtimeModel.ProviderEncryptedKey != nil {
		decrypted, decErr := crypto.Decrypt(*runtimeModel.ProviderEncryptedKey, h.Config.AppSecretKey)
		if decErr != nil {
			writeError(w, http.StatusInternalServerError, "Failed to decrypt provider API key")
			return nil
		}
		providerCfg.APIKey = decrypted
	}

	chPassword, decErr := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if decErr != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return nil
	}

	currentSQL := clipMiddle(strings.TrimSpace(body.CurrentSQL), tts2sqlMaxCurrentSQL)
	schema, tables := h.buildSchemaContext(r.Context(), session.ConnectionID, session.ClickhouseUser, chPassword, question+"\n"+currentSQL)
	modelCtx := h.buildModelContext(session.ConnectionID)

	sysPrompt := "You are an expert ClickHouse SQL engineer. Write ONE correct ClickHouse SQL query that answers the user's question.\n" +
		"Rules:\n" +
		"- Use ONLY the tables and columns provided in the schema below. Never invent names.\n" +
		"- Prefer fully-qualified `database.table` names.\n" +
		"- Use ClickHouse syntax and functions (not Postgres/MySQL).\n" +
		"- If the question can't be answered from the schema, return a comment explaining what's missing.\n" +
		"- When a current query is given and the question asks to change it, return the full edited query.\n" +
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
	if currentSQL != "" {
		user.WriteString("\n# Current query in the editor\n\n")
		user.WriteString(currentSQL)
		user.WriteString("\n")
	}
	user.WriteString("\n# Question\n\n")
	user.WriteString(question)
	user.WriteString("\n\nReturn only the ClickHouse SQL.")

	return &sqlGenJob{
		actor:        actor,
		question:     question,
		runtimeModel: runtimeModel,
		provider:     provider,
		providerCfg:  providerCfg,
		messages: []braincore.Message{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: user.String()},
		},
		tables: tables,
		prices: settings.Prices,
	}
}

func (h *BrainHandler) auditGenerateSQL(r *http.Request, job *sqlGenJob) {
	session := middleware.GetSession(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "brain.generate_sql",
		Username:       strPtr(job.actor),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(job.question),
		IPAddress:      strPtr(getClientIP(r)),
	})
}

// GenerateSQL handles text-to-SQL generation and answers with JSON.
func (h *BrainHandler) GenerateSQL(w http.ResponseWriter, r *http.Request) {
	job := h.prepareGenerateSQL(w, r)
	if job == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tts2sqlGenWait)
	defer cancel()
	raw, usage, genErr := completeOnce(ctx, job.provider, job.providerCfg, job.runtimeModel.ModelName, job.messages)
	h.recordAskAIUsage(job.actor, job.runtimeModel, usage)
	if genErr != nil {
		writeError(w, http.StatusBadGateway, "AI generation failed: "+genErr.Error())
		return
	}
	h.auditGenerateSQL(r, job)

	resp := map[string]any{
		"success":     true,
		"sql":         stripSQLFences(raw),
		"tables_used": job.tables,
		"model":       job.runtimeModel.ModelName,
	}
	if usage != nil {
		resp["usage"] = usagePayload(providerLabel(job.runtimeModel), job.runtimeModel.ModelName, *usage, job.prices)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GenerateSQLStream is GenerateSQL as Server-Sent Events: "delta" events
// while the SQL is written, then one "done" event with the cleaned SQL, or
// an "error" event.
func (h *BrainHandler) GenerateSQLStream(w http.ResponseWriter, r *http.Request) {
	job := h.prepareGenerateSQL(w, r)
	if job == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	stream := newSSEStream(w, flusher)
	stopHeartbeat := stream.startHeartbeat(r.Context(), brainHeartbeat)
	defer stopHeartbeat()

	ctx, cancel := context.WithTimeout(r.Context(), tts2sqlGenWait)
	defer cancel()
	var sb strings.Builder
	usage, genErr := job.provider.StreamChat(ctx, job.providerCfg, job.runtimeModel.ModelName, job.messages, func(delta string) error {
		sb.WriteString(delta)
		return stream.send(map[string]any{"type": "delta", "delta": delta})
	})
	h.recordAskAIUsage(job.actor, job.runtimeModel, usage)
	if genErr != nil {
		_ = stream.send(map[string]any{"type": "error", "error": "AI generation failed: " + genErr.Error()})
		return
	}
	h.auditGenerateSQL(r, job)
	done := map[string]any{
		"type":        "done",
		"sql":         stripSQLFences(sb.String()),
		"tables_used": job.tables,
		"model":       job.runtimeModel.ModelName,
	}
	if usage != nil {
		done["usage"] = usagePayload(providerLabel(job.runtimeModel), job.runtimeModel.ModelName, *usage, job.prices)
		if usage.StopReason == "length" {
			done["truncated"] = true
		}
	}
	_ = stream.send(done)
}

// completeOnce runs a non-streaming completion by collecting stream deltas.
// The usage is returned also when the call failed partway.
func completeOnce(ctx context.Context, p braincore.Provider, cfg braincore.ProviderConfig, model string, messages []braincore.Message) (string, *braincore.ChatResult, error) {
	var sb strings.Builder
	usage, err := p.StreamChat(ctx, cfg, model, messages, func(delta string) error {
		sb.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", usage, err
	}
	return sb.String(), usage, nil
}

// schemaTable is one table or view considered for the prompt.
type schemaTable struct {
	db, name, engine, comment string
}

func (t schemaTable) qualified() string { return t.db + "." + t.name }

// rankSchemaTables orders tables by how well the question (and editor SQL)
// matches their qualified name and their column names. Name matches weigh
// more; column matches count up to three per table so a wide table does not
// win on size alone. The sort is stable, so ties keep catalog order.
func rankSchemaTables(tables []schemaTable, columns map[string][]string, text string) []schemaTable {
	tokens := tokenize(text)
	type scored struct {
		t     schemaTable
		score int
	}
	out := make([]scored, len(tables))
	for i, t := range tables {
		q := strings.ToLower(t.qualified())
		s := 0
		for tok := range tokens {
			if strings.Contains(q, tok) {
				s += 4
			} else if strings.Contains(q, singularize(tok)) {
				s += 2
			}
		}
		colHits := 0
		for _, c := range columns[t.qualified()] {
			lc := strings.ToLower(c)
			for tok := range tokens {
				if lc == tok || lc == singularize(tok) || strings.Contains(lc, tok) {
					colHits++
					break
				}
			}
		}
		if colHits > 3 {
			colHits = 3
		}
		out[i] = scored{t, s + colHits}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	ranked := make([]schemaTable, len(out))
	for i, s := range out {
		ranked[i] = s.t
	}
	return ranked
}

// buildSchemaContext picks the tables and views most relevant to the
// question and renders their columns (with comments) as compact DDL-ish
// text. Returns the rendered schema and the qualified names included.
// Introspection goes through the tunnel with readonly=1 and row/time caps,
// like Brain's tools, and stops when the request is cancelled.
func (h *BrainHandler) buildSchemaContext(ctx context.Context, connID, chUser, chPassword, question string) (string, []string) {
	runSelect := func(sql string, timeout time.Duration, maxRows int) []map[string]any {
		res, err := h.Gateway.ExecuteQueryWithSettingsCtx(ctx, connID, sql, chUser, chPassword, map[string]string{
			"readonly": "1", "max_execution_time": strconv.Itoa(int(timeout.Seconds())),
			"max_result_rows": strconv.Itoa(maxRows), "result_overflow_mode": "break",
			"log_comment": tools.LogComment,
		}, timeout)
		if err != nil || res == nil {
			return nil
		}
		var rows []map[string]any
		if json.Unmarshal(res.Data, &rows) != nil {
			return nil
		}
		return rows
	}

	const userDBs = `database NOT IN ('system','information_schema','INFORMATION_SCHEMA')`
	tableRows := runSelect(
		`SELECT database, name, engine, comment FROM system.tables
		 WHERE `+userDBs+` AND NOT is_temporary AND name NOT LIKE '.inner%'
		 ORDER BY database, name`, tts2sqlSchemaWait, 10000)
	if len(tableRows) == 0 {
		return "", nil
	}
	var all []schemaTable
	for _, row := range tableRows {
		t := schemaTable{}
		t.db, _ = row["database"].(string)
		t.name, _ = row["name"].(string)
		t.engine, _ = row["engine"].(string)
		t.comment, _ = row["comment"].(string)
		if t.db != "" && t.name != "" {
			all = append(all, t)
		}
	}

	// Column names, for ranking only.
	colNames := map[string][]string{}
	for _, row := range runSelect(`SELECT database, table, name FROM system.columns WHERE `+userDBs, tts2sqlSchemaWait, tts2sqlColumnScan) {
		d, _ := row["database"].(string)
		t, _ := row["table"].(string)
		n, _ := row["name"].(string)
		colNames[d+"."+t] = append(colNames[d+"."+t], n)
	}

	picked := rankSchemaTables(all, colNames, question)
	if len(picked) > tts2sqlMaxTables {
		picked = picked[:tts2sqlMaxTables]
	}

	var conds []string
	for _, p := range picked {
		conds = append(conds, fmt.Sprintf("(database='%s' AND table='%s')", escapeLiteral(p.db), escapeLiteral(p.name)))
	}
	if len(conds) == 0 {
		return "", nil
	}
	colRows := runSelect(
		`SELECT database, table, name, type, comment FROM system.columns WHERE `+
			strings.Join(conds, " OR ")+` ORDER BY database, table, position`, tts2sqlSchemaWait, 10000)

	cols := map[string][]string{}
	for _, row := range colRows {
		d, _ := row["database"].(string)
		t, _ := row["table"].(string)
		n, _ := row["name"].(string)
		ty, _ := row["type"].(string)
		cm, _ := row["comment"].(string)
		line := fmt.Sprintf("  %s %s", n, ty)
		if cm = oneLine(cm); cm != "" {
			line += " -- " + cm
		}
		cols[d+"."+t] = append(cols[d+"."+t], line)
	}

	var sb strings.Builder
	included := make([]string, 0, len(picked))
	for _, p := range picked {
		c := cols[p.qualified()]
		if len(c) == 0 {
			continue
		}
		header := p.qualified()
		if strings.Contains(strings.ToLower(p.engine), "view") {
			header += " (" + p.engine + ")"
		}
		if cm := oneLine(p.comment); cm != "" {
			header += " -- " + cm
		}
		sb.WriteString(fmt.Sprintf("%s (\n%s\n)\n\n", header, strings.Join(c, ",\n")))
		included = append(included, p.qualified())
	}
	return sb.String(), included
}

// oneLine flattens and bounds a comment for the prompt.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "..."
	}
	return s
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
