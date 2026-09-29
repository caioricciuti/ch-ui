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
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const proBrainPrompt = "You are Brain, an expert ClickHouse assistant for analytics teams.\n\n" +
	"You can inspect AND act on the user's CH-UI workspace through tools. " +
	"You are an agent — you DO things, you don't just describe them.\n\n" +
	"Read-only tools (auto-execute, no approval needed):\n" +
	"  Schema/data:\n" +
	"  - list_tables(database?) — databases or tables.\n" +
	"  - describe_table(database, table) — columns + sample + keys.\n" +
	"  - run_query(sql, limit?) — read-only SQL.\n" +
	"  - get_insights(database, table) — full table profile (row count, nulls, distincts, min/max).\n" +
	"  Existing artifacts (ALWAYS call these before creating to avoid duplicates):\n" +
	"  - list_dashboards() / get_dashboard(id) — dashboards (+ their panels).\n" +
	"  - list_saved_queries() — saved queries.\n" +
	"  - list_models() — dbt-style models (returns ids you pass to run_model/build_model).\n" +
	"  - list_pipelines() — ingestion pipelines.\n" +
	"  Telemetry (OpenTelemetry, requires user to have enabled it):\n" +
	"  - list_services() — distinct services seen in logs+traces (24h).\n" +
	"  - query_logs(service?, severity_min?, search?, since_minutes?) — recent log entries.\n" +
	"  - query_traces(service?, errors_only?, min_duration_ms?, since_minutes?) — spans.\n" +
	"  - find_trace(trace_id) — full waterfall + correlated logs for one trace.\n" +
	"  - list_metrics() — available metric names + type (sum / gauge / histogram) + services.\n" +
	"  - query_metrics(metric_name, group_by?, since_minutes?, bucket_seconds?) — timeseries; for histograms returns count/sum/avg/min/max per bucket.\n\n" +
	"Mutating tools (the user must approve each one — they see a card with Approve/Decline):\n" +
	"  Create:\n" +
	"  - create_saved_query(name, sql, description?)\n" +
	"  - create_model(name, target_database, materialization, sql_body, ...) — view | table | incremental | materialized_view\n" +
	"  - create_dashboard(name, description?) — creates an empty dashboard; CHAIN add_dashboard_panel right after.\n" +
	"  - add_dashboard_panel(dashboard_id, name, panel_type, sql, ...) — timeseries | bar | pie | stat | gauge | table | text. THIS is how charts get on a dashboard. Always call get_dashboard first to see what's there.\n" +
	"    Dashboard SQL supports template variables for time-range filtering (the dashboard time picker fills these in):\n" +
	"      $__timestamp(col)  — DateTime range filter: col BETWEEN <from> AND <to>\n" +
	"      $__timeFilter(col)  — Epoch range filter (for UInt32/Int64 unix timestamps)\n" +
	"      $__interval         — Aggregation bucket in seconds (auto-calculated from range + panel width)\n" +
	"      $__timeFrom / $__timeTo — Raw epoch boundaries as integers\n" +
	"    ALWAYS use $__timestamp(col) or $__timeFilter(col) in WHERE clauses and toStartOfInterval(col, INTERVAL $__interval second) for time-bucketed GROUP BY in timeseries/bar panels. This makes panels respond to the dashboard time picker.\n" +
	"  - create_pipeline(name, description?) — creates the container. ALWAYS chain configure_pipeline + start_pipeline so the user has a working pipeline, not a stub.\n" +
	"  Configure / run pipelines:\n" +
	"  - get_pipeline_graph(pipeline_id) — see if it's already wired (call before configure_pipeline).\n" +
	"  - configure_pipeline(pipeline_id, source: {node_type, config}, sink: {node_type, config}) — ONE-SHOT source + sink + wire. Source types: source_webhook | source_kafka | source_database | source_s3. Sink: sink_clickhouse. Replaces any existing graph.\n" +
	"  - start_pipeline(pipeline_id) — begin ingestion.\n" +
	"  Run / schedule models:\n" +
	"  - run_model(model_id) / build_model(model_id) — materialize a model. build_model also runs tests.\n" +
	"  - schedule_model(model_id, cron) — cron is 5-field UTC (e.g. '0 6 * * *').\n" +
	"  Update:\n" +
	"  - update_saved_query(id, ...) / update_model(id, ...) — fix existing things instead of recreating. Unspecified fields keep their current value.\n" +
	"  Delete (irreversible):\n" +
	"  - delete_dashboard / delete_dashboard_panel / delete_model / delete_saved_query / delete_pipeline. Use only when explicitly asked.\n\n" +
	"CRITICAL behavior rules:\n" +
	"1. ACT, don't promise. If the user says 'go', 'yes', 'do it', 'sure', or 'be creative' — immediately call tools. NEVER say 'hold on', 'please wait', 'I'll set that up', 'let me know if...'. Just do it in this turn.\n" +
	"2. Chain tools to complete the task end-to-end in ONE turn. Examples:\n" +
	"   - 'build me a dashboard' = list_dashboards → describe_table → create_dashboard → add_dashboard_panel × 4-6.\n" +
	"   - 'create a pipeline that ingests X via webhook into Y' = create_pipeline → configure_pipeline → start_pipeline. NEVER stop after create_pipeline — that's a useless stub.\n" +
	"   - 'create a model and run it daily' = create_model → build_model → schedule_model.\n" +
	"3. Before every create_* call, run the matching list_* first. If something already exists with that name, USE it (add_dashboard_panel to the existing one, update_model on the existing one) instead of creating a duplicate.\n" +
	"4. After create_model, propose build_model in the same turn so the user sees data immediately.\n" +
	"5. If the user declines a tool, acknowledge briefly and offer one different approach — don't re-propose the same thing.\n" +
	"6. SQL: always reference real columns from describe_table results. Default LIMIT 100 for exploration. Show the final SQL in a fenced sql block.\n" +
	"7. Don't ask the user to confirm — the approval card already does that. Just propose with good defaults.\n" +
	"8. If a tool errors with the same args twice, change the approach — don't retry identically. After 3 identical failures the server blocks the call."

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
	r.Post("/approvals/{approvalID}/approve", h.ApprovePendingAction)
	r.Post("/approvals/{approvalID}/decline", h.DeclinePendingAction)
	r.Get("/audit", h.ListAudit)
}

func (h *BrainHandler) ApprovePendingAction(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
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

func (h *BrainHandler) streamMessagePro(
	w http.ResponseWriter,
	r *http.Request,
	flusher http.Flusher,
	session *middleware.SessionInfo,
	chat *database.BrainChat,
	chatID, prompt, userMessageID, assistantMessageID string,
	provider braincore.Provider,
	providerCfg braincore.ProviderConfig,
	runtimeModel *database.BrainModelRuntime,
	history []database.BrainMessage,
	contexts []schemaContext,
	entities []entityContext,
) {
	systemPrompt := h.buildSystemPrompt(contexts, entities, true)
	chatMessages := make([]braincore.ChatMessage, 0, len(history)+2)
	chatMessages = append(chatMessages, braincore.ChatMessage{Role: "system", Content: systemPrompt})
	for _, msg := range history {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if msg.Status == "error" {
			continue
		}
		chatMessages = append(chatMessages, braincore.ChatMessage{Role: role, Content: msg.Content})
	}

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

	chPassword, decryptErr := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if decryptErr != nil {
		_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", "Failed to decrypt credentials")
		_ = writeSSE(w, flusher, map[string]interface{}{"type": "error", "error": "Failed to decrypt credentials", "messageId": assistantMessageID})
		return
	}
	tctx := tools.Context{
		Ctx:          r.Context(),
		ConnectionID: session.ConnectionID,
		Username:     middleware.Actor(session),
		CHUser:       session.ClickhouseUser,
		CHPassword:   chPassword,
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
		tctx.RunModel = func(modelID string) (string, error) {
			return runner.RunSingle(connID, modelID, user)
		}
		tctx.BuildModel = func(modelID string) (string, error) {
			return runner.RunSingle(connID, modelID, user)
		}
	}
	if h.PipelineRunner != nil {
		runner := h.PipelineRunner
		tctx.StartPipeline = func(pipelineID string) error {
			return runner.StartPipeline(pipelineID)
		}
	}
	toolDefs := registry.Definitions()

	const maxIterations = 20
	const maxRetriesPerCall = 3
	var built strings.Builder
	var streamErr error
	var legacyFallback bool
	failureCounts := make(map[string]int)

	for iter := 0; iter < maxIterations; iter++ {
		res, err := braincore.CallWithTools(provider, r.Context(), providerCfg, runtimeModel.ModelName, chatMessages, toolDefs, func(delta string) error {
			if delta == "" {
				return nil
			}
			built.WriteString(delta)
			return writeSSE(w, flusher, map[string]interface{}{"type": "delta", "delta": delta, "messageId": assistantMessageID})
		})
		if errors.Is(err, braincore.ErrToolsUnsupported) {
			legacyFallback = true
			break
		}
		if err != nil {
			streamErr = err
			break
		}
		if res == nil || res.FinishReason != "tool_calls" || len(res.ToolCalls) == 0 {
			break
		}

		chatMessages = append(chatMessages, braincore.ChatMessage{
			Role:      "assistant",
			Content:   res.Content,
			ToolCalls: res.ToolCalls,
		})
		for _, tc := range res.ToolCalls {
			argsRaw := json.RawMessage(tc.Function.Arguments)
			toolDef, knownTool := registry.Get(tc.Function.Name)

			if knownTool && toolDef.RequiresApproval {
				approvalID := uuid.NewString()
				ch := h.registerApproval(approvalID)
				if _, err := h.DB.CreateBrainToolCall(chatID, assistantMessageID, tc.Function.Name, tc.Function.Arguments, "", "pending_approval", ""); err != nil {
					slog.Error("failed to persist tool call", "tool", tc.Function.Name, "error", err)
				}
				approvalCreated := true
				if err := h.DB.CreateBrainApproval(approvalID, chatID, assistantMessageID, tc.ID, tc.Function.Name, tc.Function.Arguments, middleware.Actor(session)); err != nil {
					slog.Error("failed to create brain approval — executing without approval gate", "approvalID", approvalID, "error", err)
					h.deregisterApproval(approvalID)
					approvalCreated = false
				}

				if approvalCreated {
					_ = writeSSE(w, flusher, map[string]interface{}{
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
						h.deregisterApproval(approvalID)
						_, _ = h.DB.MarkBrainApprovalDecided(approvalID, "abandoned", "system")
						return
					}
					h.deregisterApproval(approvalID)
					_, _ = h.DB.MarkBrainApprovalDecided(approvalID, decisionStatus, decision.By)

					if !decision.Approved {
						declinedJSON, _ := json.Marshal(map[string]any{"declined": true, "message": "User declined this action. Acknowledge and ask if they'd like a different approach."})
						if _, err := h.DB.CreateBrainToolCall(chatID, assistantMessageID, tc.Function.Name, tc.Function.Arguments, string(declinedJSON), "declined", ""); err != nil {
							slog.Error("failed to persist declined tool call", "tool", tc.Function.Name, "error", err)
						}
						_ = writeSSE(w, flusher, map[string]interface{}{
							"type":       "tool_call_result",
							"toolCallId": tc.ID,
							"tool":       tc.Function.Name,
							"status":     "declined",
							"result":     json.RawMessage(declinedJSON),
							"messageId":  assistantMessageID,
						})
						chatMessages = append(chatMessages, braincore.ChatMessage{
							Role:       "tool",
							ToolCallID: tc.ID,
							Name:       tc.Function.Name,
							Content:    string(declinedJSON),
						})
						continue
					}
				}
			}

			callKey := tc.Function.Name + "|" + tc.Function.Arguments
			if failureCounts[callKey] >= maxRetriesPerCall {
				blocked, _ := json.Marshal(map[string]any{
					"error": fmt.Sprintf("This call has failed %d times in this turn — refusing to retry. Try different arguments or a different tool.", maxRetriesPerCall),
				})
				_ = writeSSE(w, flusher, map[string]interface{}{
					"type":       "tool_call_result",
					"toolCallId": tc.ID,
					"tool":       tc.Function.Name,
					"status":     "error",
					"result":     json.RawMessage(blocked),
					"messageId":  assistantMessageID,
				})
				chatMessages = append(chatMessages, braincore.ChatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    string(blocked),
				})
				continue
			}

			_ = writeSSE(w, flusher, map[string]interface{}{
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
			} else {
				delete(failureCounts, callKey)
			}
			if _, err := h.DB.CreateBrainToolCall(chatID, assistantMessageID, tc.Function.Name, tc.Function.Arguments, string(resultJSON), status, errText); err != nil {
				slog.Error("failed to persist tool call result", "tool", tc.Function.Name, "error", err)
			}
			_ = writeSSE(w, flusher, map[string]interface{}{
				"type":       "tool_call_result",
				"toolCallId": tc.ID,
				"tool":       tc.Function.Name,
				"status":     status,
				"result":     json.RawMessage(resultJSON),
				"messageId":  assistantMessageID,
			})
			chatMessages = append(chatMessages, braincore.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    string(resultJSON),
			})
		}
	}

	if legacyFallback {
		simplePrompt := h.buildSystemPrompt(contexts, entities, false)
		legacyMsgs := []braincore.Message{{Role: "system", Content: simplePrompt}}
		for _, m := range chatMessages {
			if m.Role == "system" {
				continue
			}
			if m.Role != "user" && m.Role != "assistant" {
				continue
			}
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			legacyMsgs = append(legacyMsgs, braincore.Message{Role: m.Role, Content: m.Content})
		}
		_, err := provider.StreamChat(r.Context(), providerCfg, runtimeModel.ModelName, legacyMsgs, func(delta string) error {
			if delta == "" {
				return nil
			}
			built.WriteString(delta)
			return writeSSE(w, flusher, map[string]interface{}{"type": "delta", "delta": delta, "messageId": assistantMessageID})
		})
		if err != nil {
			streamErr = err
		}
	}

	if streamErr != nil {
		errMessage := streamErr.Error()
		if errMessage == "" {
			errMessage = "Unknown provider error"
		}
		_ = h.DB.UpdateBrainMessage(assistantMessageID, built.String(), "error", errMessage)
		_ = writeSSE(w, flusher, map[string]interface{}{"type": "error", "error": errMessage, "messageId": assistantMessageID})
		return
	}

	assistantText := built.String()
	if strings.TrimSpace(assistantText) == "" {
		assistantText = "I could not generate a response for that prompt."
	}

	if err := h.DB.UpdateBrainMessage(assistantMessageID, assistantText, "complete", ""); err != nil {
		slog.Warn("Failed to persist assistant message", "error", err)
	}
	if err := h.DB.TouchBrainChat(chatID); err != nil {
		slog.Warn("Failed to update chat activity", "error", err)
	}

	title := chat.Title
	if title == "New Chat" || strings.TrimSpace(title) == "" {
		title = autoTitle(prompt)
	}
	streamCtxDB := ""
	if chat.ContextDatabase != nil {
		streamCtxDB = *chat.ContextDatabase
	}
	streamCtxTable := ""
	if chat.ContextTable != nil {
		streamCtxTable = *chat.ContextTable
	}
	streamCtxTables := ""
	if chat.ContextTables != nil {
		streamCtxTables = *chat.ContextTables
	}
	if err := h.DB.UpdateBrainChat(chatID, title, runtimeModel.ProviderID, runtimeModel.ModelID, chat.Archived, streamCtxDB, streamCtxTable, streamCtxTables); err != nil {
		slog.Warn("Failed to update chat title/model", "error", err)
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "brain.chat",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(fmt.Sprintf("chat=%s user_msg=%s pro=true", chatID, userMessageID)),
		IPAddress:      strPtr(r.RemoteAddr),
	})

	_ = writeSSE(w, flusher, map[string]interface{}{"type": "done", "messageId": assistantMessageID, "chatId": chatID})
}
