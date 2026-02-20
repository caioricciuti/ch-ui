package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

func normalizeProviderKind(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai":
		return "openai", true
	case "openai_compatible", "openai-compatible":
		return "openai_compatible", true
	case "ollama":
		return "ollama", true
	default:
		return "", false
	}
}

func modelDisplayName(m database.BrainModel) string {
	if m.DisplayName != nil && strings.TrimSpace(*m.DisplayName) != "" {
		return strings.TrimSpace(*m.DisplayName)
	}
	return m.Name
}

func scoreRecommendedModel(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(n, "gpt-5"):
		return 100
	case strings.Contains(n, "gpt-4.1"):
		return 95
	case strings.Contains(n, "gpt-4o"):
		return 90
	case strings.Contains(n, "gpt-4"):
		return 80
	case strings.Contains(n, "o3"), strings.Contains(n, "o1"):
		return 70
	case strings.Contains(n, "claude"):
		return 60
	case strings.Contains(n, "llama"), strings.Contains(n, "qwen"), strings.Contains(n, "mistral"), strings.Contains(n, "gemma"):
		return 50
	default:
		return 10
	}
}

func pickRecommendedModel(models []database.BrainModel) *database.BrainModel {
	if len(models) == 0 {
		return nil
	}
	ordered := make([]database.BrainModel, 0, len(models))
	ordered = append(ordered, models...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a := ordered[i]
		b := ordered[j]
		sa := scoreRecommendedModel(a.Name)
		sb := scoreRecommendedModel(b.Name)
		if sa != sb {
			return sa > sb
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return &ordered[0]
}

func applyModelBulkAction(db *database.DB, providerID, action string) (int, error) {
	models, err := db.GetBrainModels(providerID)
	if err != nil {
		return 0, err
	}
	if len(models) == 0 {
		return 0, nil
	}

	switch action {
	case "deactivate_all":
		if err := db.ClearDefaultBrainModelsByProvider(providerID); err != nil {
			return 0, err
		}
		for _, m := range models {
			if err := db.SetBrainModelActive(m.ID, false); err != nil {
				return 0, err
			}
		}
		return len(models), nil
	case "activate_all":
		var defaultModelID string
		for _, m := range models {
			if m.IsDefault {
				defaultModelID = m.ID
				break
			}
		}
		if defaultModelID == "" {
			rec := pickRecommendedModel(models)
			if rec != nil {
				defaultModelID = rec.ID
			}
		}
		for _, m := range models {
			display := modelDisplayName(m)
			isDefault := m.ID == defaultModelID
			if err := db.UpdateBrainModel(m.ID, display, true, isDefault); err != nil {
				return 0, err
			}
		}
		return len(models), nil
	case "activate_recommended":
		rec := pickRecommendedModel(models)
		if rec == nil {
			return 0, nil
		}
		if err := db.ClearDefaultBrainModelsByProvider(providerID); err != nil {
			return 0, err
		}
		for _, m := range models {
			display := modelDisplayName(m)
			isRec := m.ID == rec.ID
			if err := db.UpdateBrainModel(m.ID, display, isRec, isRec); err != nil {
				return 0, err
			}
		}
		return len(models), nil
	default:
		return 0, fmt.Errorf("unsupported action: %s", action)
	}
}

func (h *AdminHandler) ListBrainProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.DB.GetBrainProviders()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list providers"})
		return
	}
	if providers == nil {
		providers = []database.BrainProvider{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "providers": providers})
}

func (h *AdminHandler) CreateBrainProvider(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	var body struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
		IsActive  *bool  `json:"isActive"`
		IsDefault *bool  `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provider name is required"})
		return
	}
	kind, ok := normalizeProviderKind(body.Kind)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provider kind must be openai, openai_compatible, or ollama"})
		return
	}

	var encryptedKey *string
	if strings.TrimSpace(body.APIKey) != "" {
		encrypted, err := crypto.Encrypt(strings.TrimSpace(body.APIKey), h.Config.AppSecretKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to encrypt API key"})
			return
		}
		encryptedKey = &encrypted
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	isDefault := false
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	providerID, err := h.DB.CreateBrainProvider(name, kind, body.BaseURL, encryptedKey, isActive, isDefault, actor)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create provider"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.provider.created",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("provider=%s kind=%s", name, kind)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": providerID})
}

func (h *AdminHandler) UpdateBrainProvider(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	providerID := chi.URLParam(r, "id")
	if strings.TrimSpace(providerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provider ID is required"})
		return
	}

	existing, err := h.DB.GetBrainProviderByID(providerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load provider"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Provider not found"})
		return
	}

	var body struct {
		Name      *string `json:"name"`
		Kind      *string `json:"kind"`
		BaseURL   *string `json:"baseUrl"`
		APIKey    *string `json:"apiKey"`
		IsActive  *bool   `json:"isActive"`
		IsDefault *bool   `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := existing.Name
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		name = strings.TrimSpace(*body.Name)
	}
	kind := existing.Kind
	if body.Kind != nil {
		n, ok := normalizeProviderKind(*body.Kind)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provider kind must be openai, openai_compatible, or ollama"})
			return
		}
		kind = n
	}
	baseURL := ""
	if existing.BaseURL != nil {
		baseURL = *existing.BaseURL
	}
	if body.BaseURL != nil {
		baseURL = strings.TrimSpace(*body.BaseURL)
	}
	isActive := existing.IsActive
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	isDefault := existing.IsDefault
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	updateAPIKey := false
	var encryptedKey *string
	if body.APIKey != nil {
		updateAPIKey = true
		if strings.TrimSpace(*body.APIKey) != "" {
			encrypted, encErr := crypto.Encrypt(strings.TrimSpace(*body.APIKey), h.Config.AppSecretKey)
			if encErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to encrypt API key"})
				return
			}
			encryptedKey = &encrypted
		}
	}

	if err := h.DB.UpdateBrainProvider(providerID, name, kind, baseURL, encryptedKey, updateAPIKey, isActive, isDefault); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update provider"})
		return
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.provider.updated",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("provider_id=%s", providerID)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) DeleteBrainProvider(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	providerID := chi.URLParam(r, "id")
	if strings.TrimSpace(providerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provider ID is required"})
		return
	}

	if err := h.DB.DeleteBrainProvider(providerID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete provider"})
		return
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.provider.deleted",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("provider_id=%s", providerID)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) SyncBrainProviderModels(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	providerID := chi.URLParam(r, "id")
	if strings.TrimSpace(providerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Provider ID is required"})
		return
	}

	provider, err := h.DB.GetBrainProviderByID(providerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load provider"})
		return
	}
	if provider == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Provider not found"})
		return
	}

	adapter, err := braincore.NewProvider(provider.Kind)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	cfg := braincore.ProviderConfig{Kind: provider.Kind}
	if provider.BaseURL != nil {
		cfg.BaseURL = *provider.BaseURL
	}
	if provider.EncryptedAPIKey != nil {
		decrypted, decErr := crypto.Decrypt(*provider.EncryptedAPIKey, h.Config.AppSecretKey)
		if decErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt provider API key"})
			return
		}
		cfg.APIKey = decrypted
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	modelNames, err := adapter.ListModels(ctx, cfg)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if len(modelNames) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "models": []database.BrainModel{}})
		return
	}

	var firstID string
	for _, name := range modelNames {
		id, ensureErr := h.DB.EnsureBrainModel(providerID, name, name)
		if ensureErr != nil {
			slog.Warn("Failed to sync model", "provider", providerID, "model", name, "error", ensureErr)
			continue
		}
		if firstID == "" {
			firstID = id
		}
	}

	models, _ := h.DB.GetBrainModels(providerID)
	hasDefault := false
	hasActive := false
	for _, m := range models {
		if m.IsDefault {
			hasDefault = true
		}
		if m.IsActive {
			hasActive = true
		}
		if hasDefault && hasActive {
			break
		}
	}
	if !hasDefault || !hasActive {
		rec := pickRecommendedModel(models)
		if rec != nil {
			_ = h.DB.ClearDefaultBrainModelsByProvider(providerID)
			_ = h.DB.UpdateBrainModel(rec.ID, modelDisplayName(*rec), true, true)
		} else if firstID != "" {
			_ = h.DB.UpdateBrainModel(firstID, modelNames[0], true, true)
		}
		models, _ = h.DB.GetBrainModels(providerID)
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.provider.models_synced",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("provider_id=%s models=%d", providerID, len(modelNames))),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "models": models})
}

func (h *AdminHandler) ListBrainModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.DB.GetBrainModelsWithProvider(false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list models"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "models": models})
}

func (h *AdminHandler) UpdateBrainModel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	modelID := chi.URLParam(r, "id")
	if strings.TrimSpace(modelID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Model ID is required"})
		return
	}

	existing, err := h.DB.GetBrainModelByID(modelID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load model"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Model not found"})
		return
	}

	var body struct {
		DisplayName *string `json:"displayName"`
		IsActive    *bool   `json:"isActive"`
		IsDefault   *bool   `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	displayName := existing.Name
	if existing.DisplayName != nil && strings.TrimSpace(*existing.DisplayName) != "" {
		displayName = *existing.DisplayName
	}
	if body.DisplayName != nil {
		displayName = strings.TrimSpace(*body.DisplayName)
	}
	isActive := existing.IsActive
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	isDefault := existing.IsDefault
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}
	if isDefault {
		isActive = true
	}
	if !isActive {
		isDefault = false
	}

	if err := h.DB.UpdateBrainModel(modelID, displayName, isActive, isDefault); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update model"})
		return
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.model.updated",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("model_id=%s", modelID)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) BulkUpdateBrainModels(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	var body struct {
		ProviderID string `json:"providerId"`
		Action     string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	providerID := strings.TrimSpace(body.ProviderID)
	if providerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "providerId is required"})
		return
	}
	action := strings.TrimSpace(body.Action)
	if action != "deactivate_all" && action != "activate_all" && action != "activate_recommended" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be one of: deactivate_all, activate_all, activate_recommended"})
		return
	}

	updated, err := applyModelBulkAction(h.DB, providerID, action)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to apply bulk model action"})
		return
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.model.bulk_updated",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("provider_id=%s action=%s updated=%d", providerID, action, updated)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"updated": updated,
	})
}

func (h *AdminHandler) ListBrainSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := h.DB.GetBrainSkills()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list skills"})
		return
	}
	if skills == nil {
		skills = []database.BrainSkill{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "skills": skills})
}

func (h *AdminHandler) CreateBrainSkill(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	var body struct {
		Name      string `json:"name"`
		Content   string `json:"content"`
		IsActive  *bool  `json:"isActive"`
		IsDefault *bool  `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	content := strings.TrimSpace(body.Content)
	if name == "" || content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name and content are required"})
		return
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	isDefault := false
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	id, err := h.DB.CreateBrainSkill(name, content, actor, isActive, isDefault)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create skill"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.skill.created",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("skill=%s", name)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
}

func (h *AdminHandler) UpdateBrainSkill(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	skillID := chi.URLParam(r, "id")
	if strings.TrimSpace(skillID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Skill ID is required"})
		return
	}

	existing, err := h.DB.GetBrainSkillByID(skillID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load skill"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Skill not found"})
		return
	}

	var body struct {
		Name      *string `json:"name"`
		Content   *string `json:"content"`
		IsActive  *bool   `json:"isActive"`
		IsDefault *bool   `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := existing.Name
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		name = strings.TrimSpace(*body.Name)
	}
	content := existing.Content
	if body.Content != nil {
		content = strings.TrimSpace(*body.Content)
	}
	isActive := existing.IsActive
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	isDefault := existing.IsDefault
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	if strings.TrimSpace(name) == "" || strings.TrimSpace(content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name and content are required"})
		return
	}

	if err := h.DB.UpdateBrainSkill(skillID, name, content, isActive, isDefault); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update skill"})
		return
	}

	actor := ""
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "brain.skill.updated",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("skill_id=%s", skillID)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
