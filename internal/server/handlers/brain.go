package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/brain/tools"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const baseBrainPrompt = `You are Brain, an expert ClickHouse assistant for analytics teams.

Core behavior:
- Prioritize correctness over verbosity.
- Provide SQL first when the user asks for query help.
- Keep default queries safe: LIMIT 100 for exploratory SELECT queries.
- Ask one concise clarification if schema/context is insufficient.
- Reuse prior chat context and artifacts when relevant.

Output style:
1) One sentence acknowledging intent.
2) SQL in a fenced sql block when applicable.
3) Short explanation and optional alternatives.`

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

// BrainHandler handles Brain chat, persistence, artifacts, and (Pro)
// agentic tool calling with an approval queue.
type BrainHandler struct {
	DB             *database.DB
	Gateway        *tunnel.Gateway
	Config         *config.Config
	ModelRunner    ModelRunner
	PipelineRunner PipelineRunner

	approvalMu sync.Mutex
	approvals  map[string]chan approvalDecision
}

// ModelRunner is the subset of *models.Runner Brain needs.
type ModelRunner interface {
	RunSingle(connectionID, modelID, triggeredBy string) (string, error)
}

// PipelineRunner is the subset of *pipelines.Runner Brain needs.
type PipelineRunner interface {
	StartPipeline(pipelineID string) error
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

func (h *BrainHandler) Routes(r chi.Router) {
	r.Get("/models", h.ListModels)
	r.Get("/skills", h.GetSkill)
	r.Get("/chats", h.ListChats)
	r.Post("/chats", h.CreateChat)
	r.Get("/chats/{chatID}", h.GetChat)
	r.Put("/chats/{chatID}", h.UpdateChat)
	r.Delete("/chats/{chatID}", h.DeleteChat)
	r.Get("/chats/{chatID}/messages", h.ListMessages)
	r.Post("/chats/{chatID}/messages/stream", h.StreamMessage)
	r.Get("/chats/{chatID}/artifacts", h.ListArtifacts)
	r.Post("/chats/{chatID}/artifacts/query", h.RunQueryArtifact)

	// Pro: approval queue + audit log
	r.Post("/approvals/{approvalID}/approve", h.ApprovePendingAction)
	r.Post("/approvals/{approvalID}/decline", h.DeclinePendingAction)
	r.Get("/audit", h.ListAudit)

	// Legacy endpoint kept for compatibility with older UI.
	r.Post("/chat", h.LegacyChat)
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

func (h *BrainHandler) ApprovePendingAction(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "approvalID")
	ok, err := h.DB.MarkBrainApprovalDecided(id, "approved", session.ClickhouseUser)
	if err != nil {
		slog.Error("failed to mark approval decided", "approvalID", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to record decision")
		return
	}
	if !ok {
		existing, _ := h.DB.GetBrainApprovalByID(id)
		if existing == nil {
			slog.Warn("approval not found in DB", "approvalID", id)
			writeError(w, http.StatusNotFound, "Approval not found")
		} else {
			slog.Warn("approval already decided", "approvalID", id, "currentStatus", existing.Status)
			writeError(w, http.StatusConflict, fmt.Sprintf("Approval already %s", existing.Status))
		}
		return
	}
	if !h.signalApproval(id, approvalDecision{Approved: true, By: session.ClickhouseUser}) {
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
	ok, err := h.DB.MarkBrainApprovalDecided(id, "declined", session.ClickhouseUser)
	if err != nil {
		slog.Error("failed to mark approval declined", "approvalID", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to record decision")
		return
	}
	if !ok {
		existing, _ := h.DB.GetBrainApprovalByID(id)
		if existing == nil {
			slog.Warn("approval not found in DB", "approvalID", id)
			writeError(w, http.StatusNotFound, "Approval not found")
		} else {
			slog.Warn("approval already decided", "approvalID", id, "currentStatus", existing.Status)
			writeError(w, http.StatusConflict, fmt.Sprintf("Approval already %s", existing.Status))
		}
		return
	}
	_ = h.signalApproval(id, approvalDecision{Approved: false, By: session.ClickhouseUser})
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

type schemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type schemaContext struct {
	Database   string         `json:"database"`
	Table      string         `json:"table"`
	Columns    []schemaColumn `json:"columns"`
	SampleData interface{}    `json:"sampleData"`
}

type createChatRequest struct {
	Title   string `json:"title"`
	ModelID string `json:"modelId"`
}

type updateChatRequest struct {
	Title           *string `json:"title"`
	Archived        *bool   `json:"archived"`
	ModelID         *string `json:"modelId"`
	ContextDatabase *string `json:"contextDatabase"`
	ContextTable    *string `json:"contextTable"`
	ContextTables   *string `json:"contextTables"`
}

type entityContext struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type streamMessageRequest struct {
	Content        string          `json:"content"`
	ModelID        string          `json:"modelId"`
	SchemaContext  *schemaContext  `json:"schemaContext,omitempty"`
	SchemaContexts []schemaContext `json:"schemaContexts,omitempty"`
	EntityContexts []entityContext `json:"entityContexts,omitempty"`
}

type runQueryArtifactRequest struct {
	Query     string `json:"query"`
	Title     string `json:"title"`
	MessageID string `json:"messageId"`
	Timeout   int    `json:"timeout"`
}

func (h *BrainHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.DB.GetBrainModelsWithProvider(true)
	if err != nil {
		slog.Error("Failed to list Brain models", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load models")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"models":  models,
	})
}

func (h *BrainHandler) GetSkill(w http.ResponseWriter, r *http.Request) {
	skill, err := h.DB.GetActiveBrainSkill()
	if err != nil {
		slog.Error("Failed to load active Brain skill", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load active skill")
		return
	}
	if skill == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "skill": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "skill": skill})
}

func (h *BrainHandler) ListChats(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("includeArchived")), "true")
	chats, err := h.DB.GetBrainChatsByUser(session.ClickhouseUser, session.ConnectionID, includeArchived)
	if err != nil {
		slog.Error("Failed to list Brain chats", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load chats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"chats":   chats,
	})
}

func (h *BrainHandler) CreateChat(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body createChatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "New Chat"
	}

	providerID := ""
	modelID := strings.TrimSpace(body.ModelID)
	if modelID != "" {
		rt, err := h.DB.GetBrainModelRuntimeByID(modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to resolve model")
			return
		}
		if rt == nil || !rt.ModelActive || !rt.ProviderActive {
			writeError(w, http.StatusBadRequest, "Model is not available")
			return
		}
		providerID = rt.ProviderID
	}

	chatID, err := h.DB.CreateBrainChat(session.ClickhouseUser, session.ConnectionID, title, providerID, modelID, "", "", "")
	if err != nil {
		slog.Error("Failed to create Brain chat", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create chat")
		return
	}

	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil || chat == nil {
		writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": chatID})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "chat": chat})
}

func (h *BrainHandler) GetChat(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "chat": chat})
}

func (h *BrainHandler) UpdateChat(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	var body updateChatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	title := chat.Title
	if body.Title != nil {
		if strings.TrimSpace(*body.Title) != "" {
			title = strings.TrimSpace(*body.Title)
		}
	}

	archived := chat.Archived
	if body.Archived != nil {
		archived = *body.Archived
	}

	providerID := ""
	if chat.ProviderID != nil {
		providerID = *chat.ProviderID
	}
	modelID := ""
	if chat.ModelID != nil {
		modelID = *chat.ModelID
	}

	if body.ModelID != nil {
		modelID = strings.TrimSpace(*body.ModelID)
		providerID = ""
		if modelID != "" {
			rt, err := h.DB.GetBrainModelRuntimeByID(modelID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to resolve model")
				return
			}
			if rt == nil || !rt.ModelActive || !rt.ProviderActive {
				writeError(w, http.StatusBadRequest, "Model is not available")
				return
			}
			providerID = rt.ProviderID
		}
	}

	contextDatabase := ""
	if chat.ContextDatabase != nil {
		contextDatabase = *chat.ContextDatabase
	}
	contextTable := ""
	if chat.ContextTable != nil {
		contextTable = *chat.ContextTable
	}
	contextTables := ""
	if chat.ContextTables != nil {
		contextTables = *chat.ContextTables
	}
	if body.ContextDatabase != nil {
		contextDatabase = strings.TrimSpace(*body.ContextDatabase)
	}
	if body.ContextTable != nil {
		contextTable = strings.TrimSpace(*body.ContextTable)
	}
	if body.ContextDatabase != nil && body.ContextTable == nil {
		contextTable = ""
	}
	if body.ContextTables != nil {
		contextTables = strings.TrimSpace(*body.ContextTables)
		if contextTables != "" {
			contextDatabase = ""
			contextTable = ""
		}
	}

	if err := h.DB.UpdateBrainChat(chatID, title, providerID, modelID, archived, contextDatabase, contextTable, contextTables); err != nil {
		slog.Error("Failed to update Brain chat", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update chat")
		return
	}

	updated, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil || updated == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "chat": updated})
}

func (h *BrainHandler) DeleteChat(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	if err := h.DB.DeleteBrainChat(chatID); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete chat")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *BrainHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	messages, err := h.DB.GetBrainMessages(chatID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load messages")
		return
	}

	// Re-hydrate tool cards for Pro chats: pull every tool call for this
	// chat, group by message_id, and attach as `toolCalls` on each
	// assistant message. The client renders these as completed cards.
	toolCalls, _ := h.DB.GetBrainToolCallsByChat(chatID)
	calls := make(map[string][]map[string]interface{}, len(toolCalls))
	for _, tc := range toolCalls {
		entry := map[string]interface{}{
			"id":     tc.ID,
			"tool":   tc.ToolName,
			"status": mapToolCallStatus(tc.Status),
		}
		if tc.InputJSON != "" {
			entry["args"] = json.RawMessage(tc.InputJSON)
		}
		if tc.OutputJSON != "" {
			entry["result"] = json.RawMessage(tc.OutputJSON)
		}
		calls[tc.MessageID] = append(calls[tc.MessageID], entry)
	}

	hydrated := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		entry := map[string]interface{}{
			"id":         m.ID,
			"chat_id":    m.ChatID,
			"role":       m.Role,
			"content":    m.Content,
			"status":     m.Status,
			"error":      m.Error,
			"created_at": m.CreatedAt,
			"updated_at": m.UpdatedAt,
		}
		if cs, ok := calls[m.ID]; ok && len(cs) > 0 {
			entry["toolCalls"] = cs
		}
		hydrated = append(hydrated, entry)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "messages": hydrated})
}

func mapToolCallStatus(s string) string {
	switch s {
	case "success", "error", "declined", "pending_approval":
		return s
	default:
		return "success"
	}
}

func (h *BrainHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	artifacts, err := h.DB.GetBrainArtifacts(chatID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load artifacts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "artifacts": artifacts})
}

func (h *BrainHandler) RunQueryArtifact(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	var body runQueryArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	query := strings.TrimSpace(body.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}
	if !isBrainReadOnlyQuery(query) {
		writeError(w, http.StatusBadRequest, "Only read-only queries are allowed in Brain chat artifacts")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	timeout := 30 * time.Second
	if body.Timeout > 0 {
		timeout = time.Duration(body.Timeout) * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}

	result, err := h.Gateway.ExecuteQuery(session.ConnectionID, query, session.ClickhouseUser, password, timeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	artifactPayload, _ := json.Marshal(map[string]interface{}{
		"query":      query,
		"data":       json.RawMessage(result.Data),
		"meta":       json.RawMessage(result.Meta),
		"statistics": json.RawMessage(result.Stats),
	})

	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "Query Result"
	}

	artifactID, err := h.DB.CreateBrainArtifact(chatID, body.MessageID, "query_result", title, string(artifactPayload), session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to store artifact")
		return
	}

	toolInput, _ := json.Marshal(map[string]interface{}{"query": query})
	toolOutput, _ := json.Marshal(map[string]interface{}{"artifact_id": artifactID})
	if strings.TrimSpace(body.MessageID) != "" {
		_, _ = h.DB.CreateBrainToolCall(chatID, body.MessageID, "run_readonly_query", string(toolInput), string(toolOutput), "success", "")
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "brain.query.run",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(title),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"artifact_id": artifactID,
		"result": map[string]interface{}{
			"data":  result.Data,
			"meta":  result.Meta,
			"stats": result.Stats,
		},
	})
}

func (h *BrainHandler) StreamMessage(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	chatID := chi.URLParam(r, "chatID")
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, session.ClickhouseUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load chat")
		return
	}
	if chat == nil {
		writeError(w, http.StatusNotFound, "Chat not found")
		return
	}

	var body streamMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	prompt := strings.TrimSpace(body.Content)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "Message content is required")
		return
	}

	userMessageID, err := h.DB.CreateBrainMessage(chatID, "user", prompt, "complete", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to persist user message")
		return
	}

	assistantMessageID, err := h.DB.CreateBrainMessage(chatID, "assistant", "", "streaming", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to persist assistant message")
		return
	}

	runtimeModel, err := h.resolveRuntimeModel(chat, strings.TrimSpace(body.ModelID))
	if err != nil {
		_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", err.Error())
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := braincore.NewProvider(runtimeModel.ProviderKind)
	if err != nil {
		_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", err.Error())
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
			_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", "Failed to decrypt provider API key")
			writeError(w, http.StatusInternalServerError, "Failed to decrypt provider API key")
			return
		}
		providerCfg.APIKey = decrypted
	}

	history, err := h.DB.GetBrainMessages(chatID)
	if err != nil {
		_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", "Failed to load chat history")
		writeError(w, http.StatusInternalServerError, "Failed to load chat history")
		return
	}

	var allContexts []schemaContext
	if body.SchemaContext != nil {
		allContexts = append(allContexts, *body.SchemaContext)
	}
	for _, sc := range body.SchemaContexts {
		dup := false
		for _, existing := range allContexts {
			if existing.Database == sc.Database && existing.Table == sc.Table {
				dup = true
				break
			}
		}
		if !dup {
			allContexts = append(allContexts, sc)
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		_ = h.DB.UpdateBrainMessage(assistantMessageID, "", "error", "Streaming not supported")
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Pro path: agentic loop with tools (only if the provider supports tool calling).
	if h.Config.IsPro() && braincore.SupportsTools(provider) {
		h.streamMessagePro(w, r, flusher, session, chat, chatID, prompt, userMessageID, assistantMessageID, provider, providerCfg, runtimeModel, history, allContexts, body.EntityContexts)
		return
	}

	// Community path: simple streaming chat (no tools).
	h.streamMessageCommunity(w, r, flusher, session, chat, chatID, prompt, userMessageID, assistantMessageID, provider, providerCfg, runtimeModel, history, allContexts, body.EntityContexts)
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
		Username:     session.ClickhouseUser,
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
		user := session.ClickhouseUser
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
				if err := h.DB.CreateBrainApproval(approvalID, chatID, assistantMessageID, tc.ID, tc.Function.Name, tc.Function.Arguments, session.ClickhouseUser); err != nil {
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
		Action:       "brain.chat",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("chat=%s user_msg=%s pro=true", chatID, userMessageID)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	_ = writeSSE(w, flusher, map[string]interface{}{"type": "done", "messageId": assistantMessageID, "chatId": chatID})
}

func (h *BrainHandler) streamMessageCommunity(
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
	providerMessages := make([]braincore.Message, 0, len(history)+1)
	systemPrompt := h.buildSystemPrompt(contexts, entities, false)
	providerMessages = append(providerMessages, braincore.Message{Role: "system", Content: systemPrompt})

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
		providerMessages = append(providerMessages, braincore.Message{Role: role, Content: msg.Content})
	}

	var built strings.Builder
	_, streamErr := provider.StreamChat(r.Context(), providerCfg, runtimeModel.ModelName, providerMessages, func(delta string) error {
		if delta == "" {
			return nil
		}
		built.WriteString(delta)
		return writeSSE(w, flusher, map[string]interface{}{"type": "delta", "delta": delta, "messageId": assistantMessageID})
	})

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
		Action:       "brain.chat",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("chat=%s user_msg=%s", chatID, userMessageID)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	_ = writeSSE(w, flusher, map[string]interface{}{"type": "done", "messageId": assistantMessageID, "chatId": chatID})
}

func (h *BrainHandler) LegacyChat(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		SchemaContext *schemaContext `json:"schemaContext,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "Messages are required")
		return
	}

	rt, err := h.DB.GetDefaultBrainModelRuntime()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to resolve model")
		return
	}
	if rt == nil {
		writeError(w, http.StatusBadRequest, "No active AI model configured by admin")
		return
	}

	provider, err := braincore.NewProvider(rt.ProviderKind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg := braincore.ProviderConfig{Kind: rt.ProviderKind}
	if rt.ProviderBaseURL != nil {
		cfg.BaseURL = *rt.ProviderBaseURL
	}
	if rt.ProviderEncryptedKey != nil {
		decrypted, decErr := crypto.Decrypt(*rt.ProviderEncryptedKey, h.Config.AppSecretKey)
		if decErr != nil {
			writeError(w, http.StatusInternalServerError, "Failed to decrypt provider API key")
			return
		}
		cfg.APIKey = decrypted
	}

	var legacyContexts []schemaContext
	if req.SchemaContext != nil {
		legacyContexts = append(legacyContexts, *req.SchemaContext)
	}
	messages := make([]braincore.Message, 0, len(req.Messages)+1)
	messages = append(messages, braincore.Message{Role: "system", Content: h.buildSystemPrompt(legacyContexts, nil, false)})
	for _, msg := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		messages = append(messages, braincore.Message{Role: role, Content: msg.Content})
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	_, streamErr := provider.StreamChat(r.Context(), cfg, rt.ModelName, messages, func(delta string) error {
		return writeSSE(w, flusher, map[string]interface{}{"type": "delta", "delta": delta})
	})

	if streamErr != nil {
		_ = writeSSE(w, flusher, map[string]interface{}{"type": "error", "error": streamErr.Error()})
		return
	}

	_ = writeSSE(w, flusher, map[string]interface{}{"type": "done"})
}

func (h *BrainHandler) resolveRuntimeModel(chat *database.BrainChat, requestedModelID string) (*database.BrainModelRuntime, error) {
	if requestedModelID != "" {
		rt, err := h.DB.GetBrainModelRuntimeByID(requestedModelID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve model")
		}
		if rt == nil || !rt.ModelActive || !rt.ProviderActive {
			return nil, fmt.Errorf("selected model is not active")
		}
		return rt, nil
	}

	if chat != nil && chat.ModelID != nil && strings.TrimSpace(*chat.ModelID) != "" {
		rt, err := h.DB.GetBrainModelRuntimeByID(*chat.ModelID)
		if err == nil && rt != nil && rt.ModelActive && rt.ProviderActive {
			return rt, nil
		}
	}

	rt, err := h.DB.GetDefaultBrainModelRuntime()
	if err != nil {
		return nil, fmt.Errorf("failed to load default model")
	}
	if rt == nil {
		return nil, fmt.Errorf("no active AI model configured by admin")
	}
	return rt, nil
}

func (h *BrainHandler) buildSystemPrompt(contexts []schemaContext, entities []entityContext, pro bool) string {
	skillPrompt := ""
	skill, err := h.DB.GetActiveBrainSkill()
	if err == nil && skill != nil {
		skillPrompt = strings.TrimSpace(skill.Content)
	}

	prompt := baseBrainPrompt
	if pro {
		prompt = proBrainPrompt
	}
	if skillPrompt != "" {
		prompt += "\n\nActive skills:\n" + skillPrompt
	}
	if len(contexts) > 0 {
		prompt += buildMultiSchemaPrompt(contexts)
	}
	if len(entities) > 0 {
		prompt += h.buildEntityContextPrompt(entities)
	}
	return prompt
}

func (h *BrainHandler) buildEntityContextPrompt(entities []entityContext) string {
	var sb strings.Builder
	sb.WriteString("\n\nWorkspace entity context:\n")
	for _, e := range entities {
		switch e.Type {
		case "dashboard":
			dash, err := h.DB.GetDashboardByID(e.ID)
			if err != nil || dash == nil {
				sb.WriteString(fmt.Sprintf("- Dashboard '%s' (id=%s): not found\n", e.Name, e.ID))
				continue
			}
			desc := ""
			if dash.Description != nil && *dash.Description != "" {
				desc = " — " + *dash.Description
			}
			sb.WriteString(fmt.Sprintf("- Dashboard '%s' (id=%s)%s\n", dash.Name, dash.ID, desc))
			panels, err := h.DB.GetPanelsByDashboard(dash.ID)
			if err == nil && len(panels) > 0 {
				sb.WriteString("  Panels:\n")
				for _, p := range panels {
					q := p.Query
					if len(q) > 200 {
						q = q[:200] + "..."
					}
					sb.WriteString(fmt.Sprintf("    - %s (%s): %s\n", p.Name, p.PanelType, q))
				}
			}
		case "saved_query":
			sq, err := h.DB.GetSavedQueryByID(e.ID)
			if err != nil || sq == nil {
				sb.WriteString(fmt.Sprintf("- Saved Query '%s' (id=%s): not found\n", e.Name, e.ID))
				continue
			}
			desc := ""
			if sq.Description != nil && *sq.Description != "" {
				desc = " — " + *sq.Description
			}
			q := sq.Query
			if len(q) > 300 {
				q = q[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("- Saved Query '%s' (id=%s)%s\n  SQL: %s\n", sq.Name, sq.ID, desc, q))
		case "model":
			m, err := h.DB.GetModelByID(e.ID)
			if err != nil || m == nil {
				sb.WriteString(fmt.Sprintf("- Model '%s' (id=%s): not found\n", e.Name, e.ID))
				continue
			}
			sql := m.SQLBody
			if len(sql) > 300 {
				sql = sql[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("- Model '%s' (id=%s): materialization=%s, target=%s.%s, status=%s\n  SQL: %s\n",
				m.Name, m.ID, m.Materialization, m.TargetDatabase, m.Name, m.Status, sql))
		case "pipeline":
			p, err := h.DB.GetPipelineByID(e.ID)
			if err != nil || p == nil {
				sb.WriteString(fmt.Sprintf("- Pipeline '%s' (id=%s): not found\n", e.Name, e.ID))
				continue
			}
			desc := ""
			if p.Description != nil && *p.Description != "" {
				desc = " — " + *p.Description
			}
			sb.WriteString(fmt.Sprintf("- Pipeline '%s' (id=%s): status=%s%s\n", p.Name, p.ID, p.Status, desc))
		default:
			sb.WriteString(fmt.Sprintf("- %s '%s' (id=%s)\n", e.Type, e.Name, e.ID))
		}
	}
	return sb.String()
}

func buildMultiSchemaPrompt(contexts []schemaContext) string {
	var sb strings.Builder
	sb.WriteString("\n\nSchema context:\n")
	for i, sc := range contexts {
		if i > 0 {
			sb.WriteString("\n")
		}
		label := ""
		if sc.Database != "" && sc.Table != "" {
			label = sc.Database + "." + sc.Table
		} else if sc.Database != "" {
			label = sc.Database
		} else if sc.Table != "" {
			label = sc.Table
		}
		sb.WriteString(fmt.Sprintf("Table %d: %s\n", i+1, label))
		if len(sc.Columns) > 0 {
			sb.WriteString("Columns:\n")
			for _, col := range sc.Columns {
				sb.WriteString("- " + col.Name + " (" + col.Type + ")\n")
			}
		}
	}
	return sb.String()
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, payload map[string]interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func autoTitle(prompt string) string {
	t := strings.TrimSpace(prompt)
	if t == "" {
		return "New Chat"
	}
	if len(t) <= 48 {
		return t
	}
	return strings.TrimSpace(t[:48]) + "..."
}

func isBrainReadOnlyQuery(query string) bool {
	re := regexp.MustCompile(`(?is)^\s*(SELECT|WITH|SHOW|DESC|DESCRIBE|EXPLAIN)\b`)
	return re.MatchString(query)
}

