package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
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

func (h *BrainHandler) Routes(r chi.Router) {
	r.Get("/models", h.ListModels)
	r.Post("/generate-sql", h.GenerateSQL) // Pro: text-to-SQL grounded in live schema
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

	h.registerAgentRoutes(r)
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
	chats, err := h.DB.GetBrainChatsByUser(middleware.Actor(session), session.ConnectionID, includeArchived)
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

	chatID, err := h.DB.CreateBrainChat(middleware.Actor(session), session.ConnectionID, title, providerID, modelID, "", "", "")
	if err != nil {
		slog.Error("Failed to create Brain chat", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create chat")
		return
	}

	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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

	updated, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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

	artifactID, err := h.DB.CreateBrainArtifact(chatID, body.MessageID, "query_result", title, string(artifactPayload), middleware.Actor(session))
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
		Action:         "brain.query.run",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(title),
		IPAddress:      strPtr(r.RemoteAddr),
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
	chat, err := h.DB.GetBrainChatByIDForUser(chatID, middleware.Actor(session))
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
		Action:         "brain.chat",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(fmt.Sprintf("chat=%s user_msg=%s", chatID, userMessageID)),
		IPAddress:      strPtr(r.RemoteAddr),
	})

	_ = writeSSE(w, flusher, map[string]interface{}{"type": "done", "messageId": assistantMessageID, "chatId": chatID})
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
