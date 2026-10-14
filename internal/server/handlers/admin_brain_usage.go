package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// registerBrainUsageRoutes mounts the Brain settings, usage ledger and
// custom model routes. Admin only, like the rest of AdminHandler.
func (h *AdminHandler) registerBrainUsageRoutes(r chi.Router) {
	r.Get("/brain/settings", h.GetBrainSettings)
	r.Put("/brain/settings", h.UpdateBrainSettings)
	r.Get("/brain/usage", h.GetBrainUsage)
	r.Get("/brain/budgets", h.ListBrainBudgets)
	r.Post("/brain/budgets", h.CreateBrainBudget)
	r.Put("/brain/budgets/{id}", h.UpdateBrainBudget)
	r.Delete("/brain/budgets/{id}", h.DeleteBrainBudget)
	r.Post("/brain/providers/{id}/models", h.AddBrainProviderModel)
}

// GetBrainSettings returns the model roles, budget, output cap and prices.
func (h *AdminHandler) GetBrainSettings(w http.ResponseWriter, r *http.Request) {
	s := loadBrainSettings(h.DB)
	// quickModelSet tells the UI whether an admin ever saved the quick
	// model, so it can suggest one only when nobody chose, "Automatic"
	// included.
	quickSet := false
	if all, err := h.DB.GetAllSettings(); err == nil {
		_, quickSet = all[settingBrainQuickModel]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"settings":      s,
		"quickModelSet": quickSet,
		"defaultPrices": braincore.DefaultPrices(),
	})
}

// UpdateBrainSettings saves the settings. Every field is optional.
func (h *AdminHandler) UpdateBrainSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentModelID     *string               `json:"agentModelId"`
		QuickModelID     *string               `json:"quickModelId"`
		DailyTokenBudget *int                  `json:"dailyTokenBudget"`
		MaxOutputTokens  *int                  `json:"maxOutputTokens"`
		Prices           *braincore.PriceTable `json:"prices"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	for _, id := range []*string{body.AgentModelID, body.QuickModelID} {
		if id == nil || strings.TrimSpace(*id) == "" {
			continue
		}
		m, err := h.DB.GetBrainModelByID(strings.TrimSpace(*id))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to load model")
			return
		}
		if m == nil {
			writeError(w, http.StatusBadRequest, "Model not found")
			return
		}
	}
	if body.DailyTokenBudget != nil && *body.DailyTokenBudget < 0 {
		writeError(w, http.StatusBadRequest, "The daily token budget cannot be negative")
		return
	}
	if body.MaxOutputTokens != nil && (*body.MaxOutputTokens < 0 || *body.MaxOutputTokens > 128000) {
		writeError(w, http.StatusBadRequest, "The output token limit must be between 0 (model default) and 128000")
		return
	}
	if body.Prices != nil {
		for model, p := range *body.Prices {
			if strings.TrimSpace(model) == "" {
				writeError(w, http.StatusBadRequest, "Every price needs a model id")
				return
			}
			for _, v := range []*float64{p.Input, p.Output, p.CacheRead, p.CacheWrite} {
				if v != nil && *v < 0 {
					writeError(w, http.StatusBadRequest, "Prices cannot be negative")
					return
				}
			}
		}
	}

	set := func(key, value string) error { return h.DB.SetSetting(key, value) }
	var err error
	if body.AgentModelID != nil {
		err = firstErr(err, set(settingBrainAgentModel, strings.TrimSpace(*body.AgentModelID)))
	}
	if body.QuickModelID != nil {
		err = firstErr(err, set(settingBrainQuickModel, strings.TrimSpace(*body.QuickModelID)))
	}
	if body.DailyTokenBudget != nil {
		// Kept for older clients: it is the global daily rule's token limit.
		err = firstErr(err, h.DB.SetGlobalDailyTokenBudget(*body.DailyTokenBudget))
	}
	if body.MaxOutputTokens != nil {
		err = firstErr(err, set(settingBrainMaxOutput, strconv.Itoa(*body.MaxOutputTokens)))
	}
	if body.Prices != nil {
		raw, mErr := marshalPrices(*body.Prices)
		err = firstErr(err, mErr)
		if mErr == nil {
			err = firstErr(err, set(settingBrainPrices, raw))
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save Brain settings")
		return
	}

	h.auditBrain(r, "brain.settings.updated", "")
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "settings": loadBrainSettings(h.DB)})
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// GetBrainUsage returns the token ledger for the last ?days= days (1 to
// 366, default 30), per person (?group_by=person, the default) or per role
// (?group_by=role), provider and model, with the cost stored when each
// request ran, plus totals and the models that had no price.
func (h *AdminHandler) GetBrainUsage(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 366 {
			writeError(w, http.StatusBadRequest, "days must be between 1 and 366")
			return
		}
		days = n
	}
	groupBy := strings.TrimSpace(r.URL.Query().Get("group_by"))
	switch groupBy {
	case "":
		groupBy = "person"
	case "person", "role":
	default:
		writeError(w, http.StatusBadRequest, "group_by must be person or role")
		return
	}
	until := time.Now().UTC()
	since := periodStart("day", until).AddDate(0, 0, -(days - 1))

	rows, err := h.DB.BrainUsageSummary(since, until.Add(time.Second), groupBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load Brain usage")
		return
	}

	type unpricedModel struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Requests int    `json:"requests"`
	}
	var total braincore.ChatResult
	totalCost := 0.0
	unpriced, requests := 0, 0
	estimated := false
	groups := map[string]bool{}
	unpricedByModel := map[string]*unpricedModel{}
	unpricedModels := make([]*unpricedModel, 0)
	for _, row := range rows {
		total.Add(&braincore.ChatResult{
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
		})
		requests += row.Requests
		groups[row.Group] = true
		if row.CostUSD != nil {
			totalCost += *row.CostUSD
		}
		estimated = estimated || row.Estimated
		unpriced += row.UnpricedRequests
		if row.UnpricedRequests > 0 {
			key := row.Provider + "\x00" + row.Model
			m, ok := unpricedByModel[key]
			if !ok {
				m = &unpricedModel{Provider: row.Provider, Model: row.Model}
				unpricedByModel[key] = m
				unpricedModels = append(unpricedModels, m)
			}
			m.Requests += row.UnpricedRequests
		}
	}
	for i := range rows {
		if rows[i].CostUSD != nil {
			c := roundCost(*rows[i].CostUSD)
			rows[i].CostUSD = &c
		}
	}

	totals := map[string]any{
		"requests":           requests,
		"groups":             len(groups),
		"input_tokens":       total.InputTokens,
		"output_tokens":      total.OutputTokens,
		"cache_read_tokens":  total.CacheReadTokens,
		"cache_write_tokens": total.CacheWriteTokens,
		"cost_usd":           roundCost(totalCost),
		"cost_estimated":     estimated,
		"unpriced_requests":  unpriced,
	}
	if groupBy == "person" {
		totals["users"] = len(groups)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":         true,
		"since":           since.Format(time.RFC3339),
		"until":           until.Format(time.RFC3339),
		"days":            days,
		"group_by":        groupBy,
		"rows":            rows,
		"unpriced_models": unpricedModels,
		"totals":          totals,
	})
}

// AddBrainProviderModel adds a model id by hand, for models the provider's
// list does not return (a new release, a proxy alias).
func (h *AdminHandler) AddBrainProviderModel(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	provider, err := h.DB.GetBrainProviderByID(providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load provider")
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "Provider not found")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 200 || strings.ContainsAny(name, " \t\n") {
		writeError(w, http.StatusBadRequest, "Enter a model id without spaces, such as claude-sonnet-5-5")
		return
	}
	id, err := h.DB.EnsureBrainModel(providerID, name, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to add model")
		return
	}
	if err := h.DB.SetBrainModelActive(id, true); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to activate model")
		return
	}
	h.auditBrain(r, "brain.model.added", fmt.Sprintf("provider_id=%s model=%s", providerID, name))
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "id": id})
}

func (h *AdminHandler) auditBrain(r *http.Request, action, details string) {
	session := middleware.GetSession(r)
	actor, chUser := "", ""
	if session != nil {
		actor, chUser = middleware.Actor(session), session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(actor),
		ClickhouseUser: strPtr(chUser),
		Details:        strPtr(details),
		IPAddress:      strPtr(getClientIP(r)),
	})
}
