// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/brain/tools"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const proPromptIntro = "You are Brain, an expert ClickHouse assistant for analytics teams.\n\n" +
	"You can inspect the user's CH-UI workspace through tools. You are an agent: you DO things, you don't just describe them.\n\n"

const proPromptRead = "Read-only tools (auto-execute, no approval needed):\n" +
	"  Schema/data:\n" +
	"  - list_tables(database?): databases or tables.\n" +
	"  - describe_table(database, table): columns + sample + keys.\n" +
	"  - run_query(sql, limit?): read-only SQL. You see at most 100 rows as CSV; the user sees the full result.\n" +
	"  - get_insights(database, table): full table profile (row count, nulls, distincts, min/max).\n" +
	"  Existing artifacts:\n" +
	"  - list_dashboards() / get_dashboard(id): dashboards (+ their panels).\n" +
	"  - list_saved_queries(): saved queries.\n" +
	"  - list_models(): models.\n" +
	"  - list_pipelines(): ingestion pipelines.\n" +
	"  Telemetry (OpenTelemetry, requires user to have enabled it):\n" +
	"  - list_services(): distinct services seen in logs+traces (24h).\n" +
	"  - query_logs(service?, severity_min?, search?, since_minutes?): recent log entries.\n" +
	"  - query_traces(service?, errors_only?, min_duration_ms?, since_minutes?): spans.\n" +
	"  - find_trace(trace_id): full waterfall + correlated logs for one trace.\n" +
	"  - list_metrics(): available metric names + type (sum / gauge / histogram) + services.\n" +
	"  - query_metrics(metric_name, group_by?, since_minutes?, bucket_seconds?): timeseries; for histograms returns count/sum/avg/min/max per bucket.\n\n"

const proPromptWrite = "Mutating tools (the user must approve each one; they see a card with the full arguments and Approve/Decline):\n" +
	"  Create (ALWAYS call the matching list_* first to avoid duplicates):\n" +
	"  - create_saved_query(name, sql, description?)\n" +
	"  - create_model(name, target_database, materialization, sql_body, ...): view | table | incremental | materialized_view\n" +
	"  - create_dashboard(name, description?): creates an empty dashboard; CHAIN add_dashboard_panel right after.\n" +
	"  - add_dashboard_panel(dashboard_id, name, panel_type, sql, ...): timeseries | bar | pie | stat | gauge | table | text. THIS is how charts get on a dashboard. Always call get_dashboard first to see what's there.\n" +
	"    Dashboard SQL supports template variables for time-range filtering (the dashboard time picker fills these in):\n" +
	"      $__timestamp(col)  : DateTime range filter: col BETWEEN <from> AND <to>\n" +
	"      $__timeFilter(col) : Epoch range filter (for UInt32/Int64 unix timestamps)\n" +
	"      $__interval        : Aggregation bucket in seconds (auto-calculated from range + panel width)\n" +
	"      $__timeFrom / $__timeTo : Raw epoch boundaries as integers\n" +
	"    ALWAYS use $__timestamp(col) or $__timeFilter(col) in WHERE clauses and toStartOfInterval(col, INTERVAL $__interval second) for time-bucketed GROUP BY in timeseries/bar panels. This makes panels respond to the dashboard time picker.\n" +
	"  - create_pipeline(name, description?): creates the container. ALWAYS chain configure_pipeline + start_pipeline so the user has a working pipeline, not a stub.\n" +
	"  Configure / run pipelines:\n" +
	"  - get_pipeline_graph(pipeline_id): see if it's already wired (call before configure_pipeline).\n" +
	"  - configure_pipeline(pipeline_id, source: {node_type, config}, sink: {node_type, config}): ONE-SHOT source + sink + wire. Source types: source_webhook | source_kafka | source_database | source_s3. Sink: sink_clickhouse. Replaces any existing graph.\n" +
	"  - start_pipeline(pipeline_id): begin ingestion.\n" +
	"  Run / schedule models:\n" +
	"  - run_model(model_id): materialize a model. Models have no data tests; build_model is the same as run_model.\n" +
	"  - schedule_model(model_id, cron): cron is 5-field UTC (e.g. '0 6 * * *').\n" +
	"  Update:\n" +
	"  - update_saved_query(id, ...) / update_model(id, ...): fix existing things instead of recreating. Unspecified fields keep their current value.\n" +
	"  Delete (irreversible):\n" +
	"  - delete_dashboard / delete_dashboard_panel / delete_model / delete_saved_query / delete_pipeline. Use only when explicitly asked.\n\n" +
	"CRITICAL behavior rules:\n" +
	"1. ACT, don't promise. If the user says 'go', 'yes', 'do it', 'sure', or 'be creative', immediately call tools. NEVER say 'hold on', 'please wait', 'I'll set that up'. Just do it in this turn.\n" +
	"2. Chain tools to complete the task end-to-end in ONE turn. Examples:\n" +
	"   - 'build me a dashboard' = list_dashboards, describe_table, create_dashboard, add_dashboard_panel x 4-6.\n" +
	"   - 'create a pipeline that ingests X via webhook into Y' = create_pipeline, configure_pipeline, start_pipeline. NEVER stop after create_pipeline.\n" +
	"   - 'create a model and run it daily' = create_model, run_model, schedule_model.\n" +
	"3. Before every create_* call, run the matching list_* first. If something already exists with that name, USE it instead of creating a duplicate.\n" +
	"4. After create_model, propose run_model in the same turn so the user sees data immediately.\n" +
	"5. If the user declines a tool, acknowledge briefly and offer one different approach; don't re-propose the same thing.\n" +
	"6. Don't ask the user to confirm; the approval card already does that. Just propose with good defaults.\n"

const proPromptViewer = "This person has the viewer role: read-only access. You cannot create, change, run, schedule or delete " +
	"dashboards, models, saved queries or pipelines, and those tools are not available to you. If they ask for a change, say that " +
	"an admin or analyst has to make it, and give them the SQL or the steps to hand over.\n\n" +
	"Behavior rules:\n"

const proPromptCommonRules = "- SQL: always reference real columns from describe_table results. Default LIMIT 100 for exploration. Show the final SQL in a fenced sql block.\n" +
	"- If a tool errors with the same args twice, change the approach; don't retry identically. After 3 identical failures the server blocks the call.\n" +
	"- Governance policies may block a query; when that happens, explain the block and do not try to work around it.\n\n"

// proBrainPrompt is the agent's system prompt. Viewers get no mention of
// the tools that change the workspace.
func proBrainPrompt(canWrite bool) string {
	var sb strings.Builder
	sb.WriteString(proPromptIntro)
	sb.WriteString(proPromptRead)
	if canWrite {
		sb.WriteString(proPromptWrite)
	} else {
		sb.WriteString(proPromptViewer)
	}
	sb.WriteString(proPromptCommonRules)
	sb.WriteString(tools.ToolOutputPreamble)
	return sb.String()
}

type approvalDecision struct {
	Approved bool
	By       string
}

func (h *BrainHandler) registerApproval(id string) chan approvalDecision {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	if h.approvals == nil {
		h.approvals = make(map[string]chan approvalDecision)
	}
	ch := make(chan approvalDecision, 1)
	h.approvals[id] = ch
	return ch
}

func (h *BrainHandler) deregisterApproval(id string) {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	delete(h.approvals, id)
}

func (h *BrainHandler) signalApproval(id string, decision approvalDecision) bool {
	h.approvalMu.Lock()
	ch, ok := h.approvals[id]
	h.approvalMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- decision:
		return true
	default:
		return false
	}
}

func (h *BrainHandler) workspaceOrigin(r *http.Request) string {
	scheme := "https"
	if h.Config != nil && strings.HasPrefix(strings.ToLower(h.Config.AppURL), "http://") {
		scheme = "http"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		if h.Config != nil {
			return strings.TrimRight(h.Config.AppURL, "/")
		}
		return ""
	}
	if strings.HasPrefix(strings.ToLower(host), "localhost") || strings.HasPrefix(strings.ToLower(host), "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + host
}

// registerAgentRoutes mounts the Pro approval queue and audit log routes.
func (h *BrainHandler) registerAgentRoutes(r chi.Router) {
	// Approvals only exist on the Pro tool path, which runs with an active
	// license (Config.IsPro). Deciding one is a write, so RequirePro answers
	// 402 both without a license and in the read-only grace window.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequirePro(h.Config))
		r.Post("/approvals/{approvalID}/approve", h.ApprovePendingAction)
		r.Post("/approvals/{approvalID}/decline", h.DeclinePendingAction)
	})
	// The audit log lists every person's approvals, so it is admin only.
	r.With(middleware.RequireAdmin(h.DB)).Get("/audit", h.ListAudit)
}

func (h *BrainHandler) ApprovePendingAction(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	// Approving runs a tool that changes the workspace: writers only, like
	// the REST routes' RequireWriter.
	if !isWriterRole(session.UserRole) {
		writeError(w, http.StatusForbidden, "Write access required (viewer is read-only)")
		return
	}
	id := chi.URLParam(r, "approvalID")
	actor := middleware.Actor(session)
	ok, err := h.DB.DecideBrainApprovalAs(id, "approved", actor)
	if err != nil {
		slog.Error("failed to mark approval decided", "approvalID", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to record decision")
		return
	}
	if !ok {
		existing, _ := h.DB.GetBrainApprovalByID(id)
		if existing == nil || existing.RequestedBy == nil || *existing.RequestedBy != actor {
			slog.Warn("approval not found in DB", "approvalID", id)
			writeError(w, http.StatusNotFound, "Approval not found")
		} else {
			slog.Warn("approval already decided", "approvalID", id, "currentStatus", existing.Status)
			writeError(w, http.StatusConflict, fmt.Sprintf("Approval already %s", existing.Status))
		}
		return
	}
	if !h.signalApproval(id, approvalDecision{Approved: true, By: actor}) {
		slog.Warn("approval channel not found — stream may have ended", "approvalID", id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *BrainHandler) DeclinePendingAction(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "approvalID")
	actor := middleware.Actor(session)
	ok, err := h.DB.DecideBrainApprovalAs(id, "declined", actor)
	if err != nil {
		slog.Error("failed to mark approval declined", "approvalID", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to record decision")
		return
	}
	if !ok {
		existing, _ := h.DB.GetBrainApprovalByID(id)
		if existing == nil || existing.RequestedBy == nil || *existing.RequestedBy != actor {
			slog.Warn("approval not found in DB", "approvalID", id)
			writeError(w, http.StatusNotFound, "Approval not found")
		} else {
			slog.Warn("approval already decided", "approvalID", id, "currentStatus", existing.Status)
			writeError(w, http.StatusConflict, fmt.Sprintf("Approval already %s", existing.Status))
		}
		return
	}
	_ = h.signalApproval(id, approvalDecision{Approved: false, By: actor})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *BrainHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := h.DB.ListBrainApprovals(status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list approvals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": rows})
}

// isWriterRole matches middleware.RequireWriter.
func isWriterRole(role string) bool {
	return role == "admin" || role == "analyst"
}

// newBrainRegistry registers every Brain tool.
func newBrainRegistry() *tools.Registry {
	registry := tools.New()
	tools.RegisterRead(registry)
	tools.RegisterInsights(registry)
	tools.RegisterAwareness(registry)
	tools.RegisterWrite(registry)
	tools.RegisterPanel(registry)
	tools.RegisterModelActions(registry)
	tools.RegisterPipeline(registry)
	tools.RegisterUpdates(registry)
	tools.RegisterDeletes(registry)
	tools.RegisterSchedules(registry)
	tools.RegisterTelemetry(registry)
	return registry
}

// updateBrainToolCall settles the row written when a call started waiting
// for approval, so a reload shows one card per call, not two.
// TODO: move to internal/database next to CreateBrainToolCall.
func (h *BrainHandler) updateBrainToolCall(id, outputJSON, status, errText string) {
	if id == "" || h.DB == nil {
		return
	}
	var errVal any
	if errText != "" {
		errVal = errText
	}
	if _, err := h.DB.Conn().Exec(`UPDATE brain_tool_calls SET output_json = ?, status = ?, error = ? WHERE id = ?`, outputJSON, status, errVal, id); err != nil {
		slog.Error("failed to update brain tool call", "id", id, "error", err)
	}
}

func (h *BrainHandler) streamMessagePro(r *http.Request, stream *sseStream, t brainTurn) {
	session := t.session
	chatID, assistantMessageID := t.chatID, t.assistantMessageID
	canWrite := isWriterRole(session.UserRole)

	systemPrompt := h.buildSystemPrompt(t.contexts, t.entities, true, canWrite) + historyOmittedNote(t.omitted)
	chatMessages := append([]braincore.ChatMessage{{Role: "system", Content: systemPrompt}}, toChatMessages(t.history)...)

	registry := newBrainRegistry()

	chPassword, decryptErr := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if decryptErr != nil {
		_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", "Failed to decrypt credentials")
		_ = stream.send(map[string]interface{}{"type": "error", "error": "Failed to decrypt credentials", "messageId": assistantMessageID})
		return
	}
	tctx := tools.Context{
		Ctx:          r.Context(),
		ConnectionID: session.ConnectionID,
		Username:     middleware.Actor(session),
		CHUser:       session.ClickhouseUser,
		CHPassword:   chPassword,
		Role:         session.UserRole,
		Guardrail:    h.guardrailCheck(session, r.URL.Path),
		RecordQuery:  h.queryRecorder(session),
		WorkspaceURL: h.workspaceOrigin(r),
		ChatID:       chatID,
		MessageID:    assistantMessageID,
		DB:           h.DB,
		Gateway:      h.Gateway,
	}
	if h.ModelRunner != nil {
		runner := h.ModelRunner
		connID := session.ConnectionID
		user := middleware.Actor(session)
		run := func(modelID string) (string, error) {
			return runner.RunSingle(connID, modelID, user)
		}
		// Models have no data tests yet, so build_model runs the same thing.
		tctx.RunModel = run
		tctx.BuildModel = run
	}
	if h.PipelineRunner != nil {
		runner := h.PipelineRunner
		tctx.StartPipeline = func(pipelineID string) error {
			return runner.StartPipeline(pipelineID)
		}
	}
	toolDefs := registry.DefinitionsFor(canWrite)

	const maxIterations = 20
	const maxRetriesPerCall = 3
	var built strings.Builder
	var streamErr error
	var legacyFallback bool
	var stopReason string
	failureCounts := make(map[string]int)

	// toolResult reports one finished call to the person and to the model.
	toolResult := func(tc braincore.ToolCallEmit, status string, resultJSON json.RawMessage) {
		_ = stream.send(map[string]interface{}{
			"type":       "tool_call_result",
			"toolCallId": tc.ID,
			"tool":       tc.Function.Name,
			"status":     status,
			"result":     resultJSON,
			"messageId":  assistantMessageID,
		})
		chatMessages = append(chatMessages, braincore.ChatMessage{
			Role:       "tool",
			ToolCallID: tc.ID,
			Name:       tc.Function.Name,
			Content:    tools.ModelView(tc.Function.Name, resultJSON),
			IsError:    status == "error",
		})
	}
	errorJSON := func(msg string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"error": msg})
		return b
	}

	for iter := 0; iter < maxIterations; iter++ {
		compactTurn(chatMessages, brainTurnMaxChars)
		res, err := braincore.CallWithTools(t.provider, r.Context(), t.providerCfg, t.runtimeModel.ModelName, chatMessages, toolDefs, func(delta string) error {
			if delta == "" {
				return nil
			}
			built.WriteString(delta)
			return stream.send(map[string]interface{}{"type": "delta", "delta": delta, "messageId": assistantMessageID})
		})
		if res != nil {
			// Sum every iteration, also one that failed midway.
			t.usage.Add(&res.ChatResult)
		}
		if errors.Is(err, braincore.ErrToolsUnsupported) {
			legacyFallback = true
			break
		}
		if err != nil {
			streamErr = err
			break
		}
		if res == nil || res.FinishReason != "tool_calls" || len(res.ToolCalls) == 0 {
			if res != nil {
				stopReason = res.StopReason
			}
			break
		}

		chatMessages = append(chatMessages, braincore.ChatMessage{
			Role:           "assistant",
			Content:        res.Content,
			ToolCalls:      res.ToolCalls,
			ProviderBlocks: res.ProviderBlocks,
		})
		for _, tc := range res.ToolCalls {
			argsRaw := json.RawMessage(tc.Function.Arguments)
			if !json.Valid(argsRaw) {
				argsRaw = json.RawMessage("{}")
			}
			toolDef, knownTool := registry.Get(tc.Function.Name)
			pendingRowID := ""

			if knownTool && toolDef.IsWrite() {
				// Viewers may not propose or run a write: refuse before any
				// approval card is shown.
				if !canWrite {
					msg := tools.ErrWriteForbidden.Error()
					if _, err := h.DB.CreateBrainToolCall(chatID, assistantMessageID, tc.Function.Name, tc.Function.Arguments, string(errorJSON(msg)), "error", msg); err != nil {
						slog.Error("failed to persist refused tool call", "tool", tc.Function.Name, "error", err)
					}
					toolResult(tc, "error", errorJSON(msg))
					continue
				}

				approvalID := uuid.NewString()
				ch := h.registerApproval(approvalID)
				rowID, err := h.DB.CreateBrainToolCall(chatID, assistantMessageID, tc.Function.Name, tc.Function.Arguments, "", "pending_approval", "")
				if err != nil {
					slog.Error("failed to persist tool call", "tool", tc.Function.Name, "error", err)
				}
				pendingRowID = rowID
				// The gate fails closed: without a stored approval the
				// tool does not run.
				if err := h.DB.CreateBrainApproval(approvalID, chatID, assistantMessageID, tc.ID, tc.Function.Name, tc.Function.Arguments, middleware.Actor(session)); err != nil {
					slog.Error("failed to create brain approval, not running the tool", "approvalID", approvalID, "error", err)
					h.deregisterApproval(approvalID)
					msg := "Could not record the approval request, so this action was not run. Tell the user and do not retry it in this turn."
					h.updateBrainToolCall(pendingRowID, string(errorJSON(msg)), "error", msg)
					toolResult(tc, "error", errorJSON(msg))
					continue
				}

				_ = stream.send(map[string]interface{}{
					"type":       "tool_call_pending_approval",
					"toolCallId": tc.ID,
					"approvalId": approvalID,
					"tool":       tc.Function.Name,
					"args":       argsRaw,
					"messageId":  assistantMessageID,
				})

				var decision approvalDecision
				var decisionStatus string
				select {
				case decision = <-ch:
					if decision.Approved {
						decisionStatus = "approved"
					} else {
						decisionStatus = "declined"
					}
				case <-time.After(5 * time.Minute):
					decision = approvalDecision{Approved: false, By: "system"}
					decisionStatus = "timeout"
				case <-r.Context().Done():
					// Stopped while waiting: settle the approval, the card
					// and the message so nothing stays pending.
					h.deregisterApproval(approvalID)
					_, _ = h.DB.MarkBrainApprovalDecided(approvalID, "abandoned", "system")
					stopped, _ := json.Marshal(map[string]any{"declined": true, "message": "Stopped before approval."})
					h.updateBrainToolCall(pendingRowID, string(stopped), "declined", "")
					h.persistStopped(assistantMessageID, built.String())
					return
				}
				h.deregisterApproval(approvalID)
				_, _ = h.DB.MarkBrainApprovalDecided(approvalID, decisionStatus, decision.By)

				if !decision.Approved {
					declinedJSON, _ := json.Marshal(map[string]any{"declined": true, "message": "User declined this action. Acknowledge and ask if they'd like a different approach."})
					h.updateBrainToolCall(pendingRowID, string(declinedJSON), "declined", "")
					toolResult(tc, "declined", declinedJSON)
					continue
				}
			}

			callKey := tc.Function.Name + "|" + tc.Function.Arguments
			if failureCounts[callKey] >= maxRetriesPerCall {
				blocked := errorJSON(fmt.Sprintf("This call has failed %d times in this turn, refusing to retry. Try different arguments or a different tool.", maxRetriesPerCall))
				h.updateBrainToolCall(pendingRowID, string(blocked), "error", "retry limit")
				toolResult(tc, "error", blocked)
				continue
			}

			_ = stream.send(map[string]interface{}{
				"type":       "tool_call_start",
				"toolCallId": tc.ID,
				"tool":       tc.Function.Name,
				"args":       argsRaw,
				"messageId":  assistantMessageID,
			})
			resultJSON, toolErr := registry.Execute(tctx, tc.Function.Name, argsRaw)
			status := "success"
			errText := ""
			if toolErr != nil {
				status = "error"
				errText = toolErr.Error()
				failureCounts[callKey]++
				if len(resultJSON) == 0 {
					resultJSON = errorJSON(errText)
				}
			} else {
				delete(failureCounts, callKey)
			}
			if pendingRowID != "" {
				h.updateBrainToolCall(pendingRowID, string(resultJSON), status, errText)
			} else if _, err := h.DB.CreateBrainToolCall(chatID, assistantMessageID, tc.Function.Name, tc.Function.Arguments, string(resultJSON), status, errText); err != nil {
				slog.Error("failed to persist tool call result", "tool", tc.Function.Name, "error", err)
			}
			toolResult(tc, status, resultJSON)
		}
	}

	if legacyFallback {
		simplePrompt := h.buildSystemPrompt(t.contexts, t.entities, false, canWrite) + historyOmittedNote(t.omitted)
		legacyMsgs := []braincore.Message{{Role: "system", Content: simplePrompt}}
		for _, m := range chatMessages {
			if m.Role != "user" && m.Role != "assistant" {
				continue
			}
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			legacyMsgs = append(legacyMsgs, braincore.Message{Role: m.Role, Content: m.Content})
		}
		legacyRes, err := t.provider.StreamChat(r.Context(), t.providerCfg, t.runtimeModel.ModelName, legacyMsgs, func(delta string) error {
			if delta == "" {
				return nil
			}
			built.WriteString(delta)
			return stream.send(map[string]interface{}{"type": "delta", "delta": delta, "messageId": assistantMessageID})
		})
		if legacyRes != nil {
			t.usage.Add(legacyRes)
			stopReason = legacyRes.StopReason
		}
		if err != nil {
			streamErr = err
		}
	}

	if streamErr != nil {
		if r.Context().Err() != nil {
			h.persistStopped(assistantMessageID, built.String())
			return
		}
		errMessage := streamErr.Error()
		if errMessage == "" {
			errMessage = "Unknown provider error"
		}
		_ = h.DB.UpdateBrainMessage(assistantMessageID, built.String(), "error", errMessage)
		_ = stream.send(map[string]interface{}{"type": "error", "error": errMessage, "messageId": assistantMessageID})
		return
	}

	h.sendStopNote(stream, t, &built, stopReason)
	h.finishTurn(r, stream, t, built.String(), true)
}
