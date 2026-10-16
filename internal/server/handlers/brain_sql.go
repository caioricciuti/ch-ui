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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/brain/tools"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ── Text-to-SQL grounded in workspace metadata ─────────────────────────────
// POST /api/brain/generate-sql  Body: { question, model_id?, current_sql? }
// POST /api/brain/fix-sql       Body: { sql, error, params?, model_id? }
// Both retrieve the relevant ClickHouse schema (tables/columns) for the
// active connection plus any documented models, ground one LLM call with
// that context, and return a single ClickHouse SQL query. The answer is then
// checked with EXPLAIN as the session user; when ClickHouse rejects it with
// an error a model can fix, the error goes back to the model for up to
// sqlMaxRepairs repairs. Reuses the Brain provider/model resolution, so it
// honors the admin-configured AI providers. Pro features, like Brain's
// agentic tools.

const (
	tts2sqlMaxTables  = 16
	tts2sqlSchemaWait = 20 * time.Second
	tts2sqlGenWait    = 60 * time.Second
	// tts2sqlMaxCurrentSQL bounds the editor SQL sent along.
	tts2sqlMaxCurrentSQL = 20000
	// tts2sqlColumnScan bounds the column names read for ranking.
	tts2sqlColumnScan = 20000
	// sqlValidateWait bounds the EXPLAIN that checks generated SQL.
	sqlValidateWait = 10 * time.Second
	// sqlMaxRepairs is how many times a rejected query goes back to the model.
	sqlMaxRepairs = 2
	// sqlErrorClip bounds a ClickHouse error put into a prompt.
	sqlErrorClip = 1500
)

// Validation statuses of generated SQL.
const (
	validationPassed  = "passed"
	validationFailed  = "failed"
	validationSkipped = "skipped"
)

// repairableCodes are ClickHouse errors a model can fix by editing the SQL:
// syntax, unknown names, types, aggregation and parameter mistakes.
var repairableCodes = map[int]bool{
	62: true, 47: true, 46: true, 60: true, 81: true, 43: true, 42: true,
	215: true, 184: true, 53: true, 386: true, 352: true, 10: true, 36: true,
}

// terminalCodes are errors no SQL edit fixes (access, read-only profile,
// limits, missing parameter values). The check is skipped for them.
var terminalCodes = map[int]bool{497: true, 164: true, 201: true, 159: true, 241: true, 456: true}

// sqlRequest is what one quick SQL request asks for.
type sqlRequest struct {
	ModelID string
	// Question is the Ask AI question, or a summary of the fix request.
	Question string
	// SQL is the editor SQL (Ask AI) or the query that failed (fix).
	SQL string
	// Error is the ClickHouse error of the query to fix.
	Error string
	// Params are the query parameter values the query ran with.
	Params map[string]string
	// rankText picks the schema tables for the prompt.
	rankText string
}

// sqlTask is one kind of quick SQL request: Ask AI or Fix with AI. Both
// share the model resolution, the schema grounding and the validate and
// repair loop; they differ in the request body and the prompt.
type sqlTask struct {
	label         string
	purpose       string
	repairPurpose string
	auditAction   string
	// withReason strips a leading "-- fix: <reason>" line into the
	// explanation.
	withReason bool
	parse      func(r *http.Request) (sqlRequest, string)
	prompt     func(req sqlRequest, schema, models string) (system, user string)
}

// sqlGenJob is one prepared quick SQL request.
type sqlGenJob struct {
	task         *sqlTask
	actor        string
	ident        budgetIdentity
	req          sqlRequest
	settings     brainSettings
	runtimeModel *database.BrainModelRuntime
	provider     braincore.Provider
	providerCfg  braincore.ProviderConfig
	messages     []braincore.Message
	tables       []string
	prices       braincore.PriceTable

	connID     string
	chUser     string
	chPassword string
	guardrail  func(sql string) error
}

var paramNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

var askAITask = &sqlTask{
	label:         "Ask AI (text-to-SQL)",
	purpose:       "ask_ai",
	repairPurpose: "ask_ai_repair",
	auditAction:   "brain.generate_sql",
	parse: func(r *http.Request) (sqlRequest, string) {
		var body struct {
			Question string `json:"question"`
			ModelID  string `json:"model_id"`
			// CurrentSQL is the editor's SQL, sent when the person asks to
			// change the query they are looking at.
			CurrentSQL string `json:"current_sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return sqlRequest{}, "Invalid request body"
		}
		question := strings.TrimSpace(body.Question)
		if question == "" {
			return sqlRequest{}, "Question is required"
		}
		current := clipMiddle(strings.TrimSpace(body.CurrentSQL), tts2sqlMaxCurrentSQL)
		return sqlRequest{ModelID: strings.TrimSpace(body.ModelID), Question: question, SQL: current, rankText: question + "\n" + current}, ""
	},
	prompt: func(req sqlRequest, schema, models string) (string, string) {
		sysPrompt := "You are an expert ClickHouse SQL engineer. Write ONE correct ClickHouse SQL query that answers the user's question.\n" +
			"Rules:\n" +
			"- Use ONLY the tables and columns provided in the schema below. Never invent names.\n" +
			"- Prefer fully-qualified `database.table` names.\n" +
			"- Use ClickHouse syntax and functions (not Postgres/MySQL).\n" +
			"- If the question can't be answered from the schema, return a comment explaining what's missing.\n" +
			"- When a current query is given and the question asks to change it, return the full edited query.\n" +
			"- Output ONLY the SQL, with no prose and no markdown fences."
		var user strings.Builder
		writeSchemaContext(&user, schema, models)
		if req.SQL != "" {
			user.WriteString("\n# Current query in the editor\n\n")
			user.WriteString(req.SQL)
			user.WriteString("\n")
		}
		user.WriteString("\n# Question\n\n")
		user.WriteString(req.Question)
		user.WriteString("\n\nReturn only the ClickHouse SQL.")
		return sysPrompt, user.String()
	},
}

var fixSQLTask = &sqlTask{
	label:         "Fix with AI",
	purpose:       "fix_sql",
	repairPurpose: "fix_sql_repair",
	auditAction:   "brain.fix_sql",
	withReason:    true,
	parse: func(r *http.Request) (sqlRequest, string) {
		var body struct {
			SQL     string            `json:"sql"`
			Error   string            `json:"error"`
			Params  map[string]string `json:"params"`
			ModelID string            `json:"model_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return sqlRequest{}, "Invalid request body"
		}
		sql := strings.TrimSpace(body.SQL)
		errText := strings.TrimSpace(body.Error)
		if sql == "" {
			return sqlRequest{}, "The query to fix is required"
		}
		if errText == "" {
			return sqlRequest{}, "The error to fix is required"
		}
		if len(sql) > tts2sqlMaxCurrentSQL {
			return sqlRequest{}, fmt.Sprintf("The query is too long to fix (over %d characters)", tts2sqlMaxCurrentSQL)
		}
		params := map[string]string{}
		for k, v := range body.Params {
			if !paramNameRE.MatchString(k) {
				return sqlRequest{}, fmt.Sprintf("Invalid query parameter name %q", k)
			}
			params[k] = v
		}
		errText = clipRunes(errText, sqlErrorClip)
		return sqlRequest{
			ModelID:  strings.TrimSpace(body.ModelID),
			Question: "Fix: " + clipRunes(oneLine(errText), 300),
			SQL:      sql,
			Error:    errText,
			Params:   params,
			rankText: sql + "\n" + errText,
		}, ""
	},
	prompt: func(req sqlRequest, schema, models string) (string, string) {
		sysPrompt := "You are an expert ClickHouse SQL engineer. A query failed in ClickHouse. Fix it with the smallest edit that makes it run and keeps its meaning.\n" +
			"Rules:\n" +
			"- Change only what the error requires. Keep the columns, filters, grouping, ordering, limits, formatting and comments of the original.\n" +
			"- Use ONLY the tables and columns provided in the schema below. Never invent names.\n" +
			"- Keep query parameters such as {name:Type} as they are.\n" +
			"- Use ClickHouse syntax and functions (not Postgres/MySQL).\n" +
			"- The first line of your answer must be `-- fix: <one short sentence saying what you changed and why>`, followed by the full corrected query.\n" +
			"- Output ONLY that line and the SQL, with no other prose and no markdown fences."
		var user strings.Builder
		writeSchemaContext(&user, schema, models)
		user.WriteString("\n# Query that failed\n\n")
		user.WriteString(req.SQL)
		user.WriteString("\n\n# ClickHouse error\n\n")
		user.WriteString(req.Error)
		user.WriteString("\n")
		if len(req.Params) > 0 {
			names := make([]string, 0, len(req.Params))
			for k := range req.Params {
				names = append(names, k)
			}
			sort.Strings(names)
			user.WriteString("\n# Query parameter values it ran with\n\n")
			for _, k := range names {
				fmt.Fprintf(&user, "- %s = %s\n", k, clipRunes(oneLine(req.Params[k]), 200))
			}
		}
		user.WriteString("\nReturn the `-- fix:` line and the full corrected query.")
		return sysPrompt, user.String()
	},
}

// writeSchemaContext renders the grounding shared by every quick SQL prompt.
func writeSchemaContext(sb *strings.Builder, schema, models string) {
	sb.WriteString("# Available schema\n\n")
	if schema == "" {
		sb.WriteString("(no user tables found)\n")
	} else {
		sb.WriteString(schema)
	}
	if models != "" {
		sb.WriteString("\n# Documented models (curated, prefer these when relevant)\n\n")
		sb.WriteString(models)
	}
}

// prepareGenerateSQL validates a quick SQL request, checks the budget,
// resolves the quick model and builds the grounded prompt with the task's
// prompt builder. It writes the error response itself and returns nil when
// the request cannot go on.
func (h *BrainHandler) prepareGenerateSQL(w http.ResponseWriter, r *http.Request, task *sqlTask) *sqlGenJob {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil
	}
	// Gate like Brain's agentic tools: schema-grounded generation is Pro-only.
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, task.label+" requires a Pro license")
		return nil
	}

	req, msg := task.parse(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return nil
	}

	settings := loadBrainSettings(h.DB)
	actor := middleware.Actor(session)
	ident := identityOf(session)
	preferred := settings.QuickModelID
	if preferred == "" {
		preferred = settings.AgentModelID
	}
	runtimeModel, rmErr := h.resolveRuntimeModel(nil, req.ModelID, preferred)
	if rmErr != nil {
		writeError(w, http.StatusBadRequest, rmErr.Error())
		return nil
	}
	if msg := h.checkBudget(ident, runtimeModel.ModelName, settings.Prices, h.live.others(ident.subject, "")); msg != "" {
		writeError(w, http.StatusTooManyRequests, msg)
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

	schema, tables := h.buildSchemaContext(r.Context(), session.ConnectionID, session.ClickhouseUser, chPassword, req.rankText)
	sysPrompt, userPrompt := task.prompt(req, schema, h.buildModelContext(session.ConnectionID))

	return &sqlGenJob{
		task:         task,
		actor:        actor,
		ident:        ident,
		req:          req,
		settings:     settings,
		runtimeModel: runtimeModel,
		provider:     provider,
		providerCfg:  providerCfg,
		messages: []braincore.Message{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
		tables:     tables,
		prices:     settings.Prices,
		connID:     session.ConnectionID,
		chUser:     session.ClickhouseUser,
		chPassword: chPassword,
		guardrail:  h.guardrailCheck(session, r.URL.Path),
	}
}

func (h *BrainHandler) auditGenerateSQL(r *http.Request, job *sqlGenJob) {
	session := middleware.GetSession(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         job.task.auditAction,
		Username:       strPtr(job.actor),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(job.req.Question),
		IPAddress:      strPtr(getClientIP(r)),
	})
}

// ── Validation and repair ──────────────────────────────────────────────────

// sqlValidation is how generated SQL fared against ClickHouse.
type sqlValidation struct {
	// Status is passed, failed (ClickHouse still rejects it) or skipped (it
	// could not be checked; Reason says why).
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Rounds is how many repairs were asked of the model.
	Rounds int `json:"rounds"`
}

// validationOutcome is the result of one EXPLAIN check.
type validationOutcome struct {
	status     string // passed, failed or skipped
	repairable bool
	err        string
	reason     string
}

var leadingFixRE = regexp.MustCompile(`(?i)^--\s*fix:\s*(.*)$`)

// splitFixReason takes a leading "-- fix: <reason>" line off sql.
func splitFixReason(sql string) (reason, rest string) {
	lines := strings.Split(sql, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if m := leadingFixRE.FindStringSubmatch(t); m != nil {
			return strings.TrimSpace(m[1]), strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
		break
	}
	return "", sql
}

// sqlCommentRE finds comments, to see whether any SQL is left without them.
var sqlCommentRE = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)

// validateGeneratedSQL checks sql with EXPLAIN as the session user, without
// running it: EXPLAIN PLAN for a SELECT or WITH query (which resolves
// tables, columns and types), EXPLAIN AST for anything else (syntax only).
// It runs with Brain's read-only settings, a 10 s cap and the governance
// guardrail.
func (h *BrainHandler) validateGeneratedSQL(ctx context.Context, job *sqlGenJob, sql string) validationOutcome {
	body := tools.StripTrailingFormat(sql)
	if strings.TrimSpace(sqlCommentRE.ReplaceAllString(body, " ")) == "" {
		return validationOutcome{status: validationSkipped, reason: "no_sql"}
	}
	if h.Gateway == nil || !h.Gateway.IsTunnelOnline(job.connID) {
		return validationOutcome{status: validationSkipped, reason: "tunnel_offline"}
	}
	if job.guardrail != nil {
		if err := job.guardrail(body); err != nil {
			return validationOutcome{status: validationSkipped, reason: "guardrail", err: err.Error()}
		}
	}
	stmt := "EXPLAIN AST " + body
	if tools.IsSelectStatement(body) {
		stmt = "EXPLAIN PLAN " + body
	}
	settings := tools.ReadOnlySettings(1000, sqlValidateWait)
	for k, v := range job.req.Params {
		settings["param_"+k] = v
	}
	vctx, cancel := context.WithTimeout(ctx, sqlValidateWait)
	defer cancel()
	_, err := h.Gateway.ExecuteQueryWithSettingsCtx(vctx, job.connID, stmt, job.chUser, job.chPassword, settings, sqlValidateWait)
	if err == nil {
		return validationOutcome{status: validationPassed}
	}
	msg := clipRunes(err.Error(), sqlErrorClip)
	code := tools.ErrorCode(err)
	switch {
	case repairableCodes[code]:
		return validationOutcome{status: validationFailed, repairable: true, err: msg}
	case code == 0:
		// Timeouts, a dropped tunnel or a cancelled request: not the SQL.
		return validationOutcome{status: validationSkipped, reason: "unavailable", err: msg}
	case terminalCodes[code]:
		return validationOutcome{status: validationSkipped, reason: "not_checkable", err: msg}
	default:
		return validationOutcome{status: validationFailed, err: msg}
	}
}

// sqlGenEvents streams the progress of a generation; nil fields are skipped.
type sqlGenEvents struct {
	delta      func(string) error
	validating func(round int)
	repair     func(round int, errText string)
	reset      func()
}

// sqlGenResult is a finished generation.
type sqlGenResult struct {
	sql         string
	explanation string
	usage       *braincore.ChatResult
	truncated   bool
	validation  sqlValidation
}

// generateOnce runs one completion and stores its usage in the ledger under
// purpose. The usage is returned also when the call failed partway.
func (h *BrainHandler) generateOnce(parent context.Context, job *sqlGenJob, messages []braincore.Message, purpose string, delta func(string) error) (string, *braincore.ChatResult, error) {
	ctx, cancel := context.WithTimeout(parent, tts2sqlGenWait)
	defer cancel()
	var sb strings.Builder
	usage, err := job.provider.StreamChat(ctx, job.providerCfg, job.runtimeModel.ModelName, messages, func(d string) error {
		sb.WriteString(d)
		if delta != nil {
			return delta(d)
		}
		return nil
	})
	h.recordQuickUsage(job.ident, job.runtimeModel, purpose, usage, job.prices)
	return sb.String(), usage, err
}

// runSQLGeneration writes the SQL, checks it with EXPLAIN, and sends each
// repairable rejection back to the model, at most sqlMaxRepairs times and
// only while the person's daily budget lasts. An error means the first
// completion failed; a failed repair ends the loop with the last SQL.
func (h *BrainHandler) runSQLGeneration(ctx context.Context, job *sqlGenJob, ev sqlGenEvents) (*sqlGenResult, error) {
	raw, usage, err := h.generateOnce(ctx, job, job.messages, job.task.purpose, ev.delta)
	if err != nil {
		return nil, err
	}
	res := &sqlGenResult{}
	addUsage := func(u *braincore.ChatResult) {
		if u == nil {
			return
		}
		if res.usage == nil {
			c := *u
			res.usage = &c
		} else {
			res.usage.Add(u)
			res.usage.StopReason = u.StopReason
		}
	}
	addUsage(usage)
	clean := func(raw string) {
		sql := stripSQLFences(raw)
		if job.task.withReason {
			if reason, rest := splitFixReason(sql); reason != "" {
				res.explanation, sql = reason, rest
			}
		}
		res.sql = sql
	}
	clean(raw)
	res.truncated = usage != nil && usage.StopReason == "length"

	messages := append([]braincore.Message(nil), job.messages...)
	for {
		if ev.validating != nil {
			ev.validating(res.validation.Rounds)
		}
		v := h.validateGeneratedSQL(ctx, job, res.sql)
		res.validation.Status, res.validation.Error, res.validation.Reason = v.status, v.err, v.reason
		if !v.repairable {
			return res, nil
		}
		if res.validation.Rounds >= sqlMaxRepairs || ctx.Err() != nil {
			return res, nil
		}
		if h.checkBudget(job.ident, "", job.prices, h.live.others(job.ident.subject, "")) != "" {
			res.validation.Reason = "budget"
			return res, nil
		}
		res.validation.Rounds++
		if ev.repair != nil {
			ev.repair(res.validation.Rounds, v.err)
		}
		if ev.reset != nil {
			ev.reset()
		}
		messages = append(messages,
			braincore.Message{Role: "assistant", Content: raw},
			braincore.Message{Role: "user", Content: "ClickHouse rejected it: " + v.err + ". Return the full corrected query only."},
		)
		next, usage, err := h.generateOnce(ctx, job, messages, job.task.repairPurpose, ev.delta)
		addUsage(usage)
		if err != nil {
			res.validation.Reason = "repair_failed"
			return res, nil
		}
		raw = next
		clean(raw)
		res.truncated = usage != nil && usage.StopReason == "length"
	}
}

// ── Handlers ───────────────────────────────────────────────────────────────

// GenerateSQL handles text-to-SQL generation and answers with JSON.
func (h *BrainHandler) GenerateSQL(w http.ResponseWriter, r *http.Request) {
	h.serveSQLTask(w, r, askAITask)
}

// GenerateSQLStream is GenerateSQL as Server-Sent Events.
func (h *BrainHandler) GenerateSQLStream(w http.ResponseWriter, r *http.Request) {
	h.streamSQLTask(w, r, askAITask)
}

// FixSQL proposes a minimal fix for a query ClickHouse rejected.
func (h *BrainHandler) FixSQL(w http.ResponseWriter, r *http.Request) {
	h.serveSQLTask(w, r, fixSQLTask)
}

// FixSQLStream is FixSQL as Server-Sent Events.
func (h *BrainHandler) FixSQLStream(w http.ResponseWriter, r *http.Request) {
	h.streamSQLTask(w, r, fixSQLTask)
}

// sqlResultPayload is the shared body of the JSON answer and the done event.
func sqlResultPayload(job *sqlGenJob, res *sqlGenResult) map[string]any {
	out := map[string]any{
		"sql":         res.sql,
		"tables_used": job.tables,
		"model":       job.runtimeModel.ModelName,
		"validation":  res.validation,
	}
	if job.task.withReason {
		out["explanation"] = res.explanation
	}
	if res.usage != nil {
		out["usage"] = usagePayload(providerLabel(job.runtimeModel), job.runtimeModel.ModelName, *res.usage, costPtr(*res.usage, job.runtimeModel.ModelName, job.prices))
	}
	if res.truncated {
		out["truncated"] = true
	}
	return out
}

func (h *BrainHandler) serveSQLTask(w http.ResponseWriter, r *http.Request, task *sqlTask) {
	job := h.prepareGenerateSQL(w, r, task)
	if job == nil {
		return
	}
	res, err := h.runSQLGeneration(r.Context(), job, sqlGenEvents{})
	if err != nil {
		writeError(w, http.StatusBadGateway, "AI generation failed: "+err.Error())
		return
	}
	h.auditGenerateSQL(r, job)
	out := sqlResultPayload(job, res)
	if b := h.budgetWarningFor(job.ident); b != nil {
		out["budget"] = b
	}
	out["success"] = true
	writeJSON(w, http.StatusOK, out)
}

// streamSQLTask answers with Server-Sent Events: "delta" while the SQL is
// written, "validating" before each EXPLAIN check, "repair" {round, error}
// and "reset" before a repair writes the query again, then one "done" with
// the cleaned SQL and its validation, or an "error".
func (h *BrainHandler) streamSQLTask(w http.ResponseWriter, r *http.Request, task *sqlTask) {
	job := h.prepareGenerateSQL(w, r, task)
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

	res, err := h.runSQLGeneration(r.Context(), job, sqlGenEvents{
		delta: func(d string) error { return stream.send(map[string]any{"type": "delta", "delta": d}) },
		validating: func(round int) {
			_ = stream.send(map[string]any{"type": "validating", "round": round})
		},
		repair: func(round int, errText string) {
			_ = stream.send(map[string]any{"type": "repair", "round": round, "error": errText})
		},
		reset: func() { _ = stream.send(map[string]any{"type": "reset"}) },
	})
	if err != nil {
		_ = stream.send(map[string]any{"type": "error", "error": "AI generation failed: " + err.Error()})
		return
	}
	h.auditGenerateSQL(r, job)
	done := sqlResultPayload(job, res)
	if b := h.budgetWarningFor(job.ident); b != nil {
		done["budget"] = b
	}
	done["type"] = "done"
	_ = stream.send(done)
}

// clipRunes shortens s to at most n runes, marking the cut.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
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
		conds = append(conds, fmt.Sprintf("(database='%s' AND table='%s')", safe.Literal(p.db), safe.Literal(p.name)))
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
