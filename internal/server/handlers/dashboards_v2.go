package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/dashboards"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/queryproc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// statusError carries the HTTP status an error should be answered with.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

func asStatusError(err error) *statusError {
	var se *statusError
	if errors.As(err, &se) {
		return se
	}
	return &statusError{status: http.StatusBadRequest, msg: err.Error()}
}

// optionsQuerySettings bound a variable options query: read only, at most
// MaxOptions rows, and short.
func optionsQuerySettings() map[string]string {
	return map[string]string{
		"readonly":             "1",
		"max_result_rows":      strconv.Itoa(dashboards.MaxOptions),
		"result_overflow_mode": "break",
		"max_execution_time":   "30",
		"log_comment":          "ch-ui:dashboard-variable",
	}
}

// optionsCacheTTL is how long a share link reuses the options of a variable.
const optionsCacheTTL = 60 * time.Second

func (h *DashboardsHandler) optionsCacheState() *publicQueryCache {
	h.optionsOnce.Do(func() {
		h.optionsCache = newPublicQueryCache(optionsCacheTTL, 512, 32<<20, 2<<20)
	})
	return h.optionsCache
}

// loadVariables returns a dashboard's variable definitions (none on error).
func (h *DashboardsHandler) loadVariables(id string) []dashboards.Variable {
	raw, err := h.DB.GetDashboardVariables(id)
	if err != nil {
		slog.Warn("Failed to load dashboard variables", "error", err, "id", id)
		return []dashboards.Variable{}
	}
	return dashboards.DecodeStored(raw)
}

// ---------- Versions ----------

// ensureBaselineVersion records the current state of a dashboard before its
// first change, so the state from before history existed can be restored.
func (h *DashboardsHandler) ensureBaselineVersion(id string) {
	has, err := h.DB.HasDashboardVersions(id)
	if err != nil || has {
		return
	}
	if _, err := h.DB.RecordDashboardVersion(id, "", "Before first recorded change"); err != nil {
		slog.Warn("Failed to record baseline dashboard version", "error", err, "id", id)
	}
}

// recordVersion saves the state of a dashboard after a change. A failure is
// logged, never returned: the change itself has been saved.
func (h *DashboardsHandler) recordVersion(r *http.Request, id, note string) {
	actor := ""
	if session := middleware.GetSession(r); session != nil {
		actor = middleware.Actor(session)
	}
	if _, err := h.DB.RecordDashboardVersion(id, actor, note); err != nil {
		slog.Warn("Failed to record dashboard version", "error", err, "id", id)
	}
}

// ListVersions returns a dashboard's saved versions, newest first.
func (h *DashboardsHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if d, err := h.DB.GetDashboardByID(id); err != nil || d == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	versions, err := h.DB.ListDashboardVersions(id)
	if err != nil {
		slog.Error("Failed to list dashboard versions", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to list versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": versions})
}

func (h *DashboardsHandler) versionFromURL(w http.ResponseWriter, r *http.Request) *database.DashboardVersion {
	id := chi.URLParam(r, "id")
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "Version must be a positive number")
		return nil
	}
	v, err := h.DB.GetDashboardVersion(id, n)
	if err != nil {
		slog.Error("Failed to get dashboard version", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to get version")
		return nil
	}
	if v == nil {
		writeError(w, http.StatusNotFound, "Version not found")
		return nil
	}
	return v
}

// GetVersion returns one version with its snapshot (the export JSON).
func (h *DashboardsHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	if v := h.versionFromURL(w, r); v != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"version": v})
	}
}

// RestoreVersion puts a dashboard back to a saved version. It goes through
// the same validation and transaction as an import, and the restored state
// is recorded as a new version.
func (h *DashboardsHandler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	existing, err := h.DB.GetDashboardByID(chi.URLParam(r, "id"))
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	v := h.versionFromURL(w, r)
	if v == nil {
		return
	}
	var body importBody
	if err := json.Unmarshal(v.Snapshot, &body); err != nil {
		writeError(w, http.StatusInternalServerError, "Saved version is not readable")
		return
	}
	if body.Variables == nil {
		body.Variables = json.RawMessage("[]")
	}
	h.applyContent(w, r, existing, body, "dashboard.version_restored", fmt.Sprintf("Restored version %d", v.Version))
}

// ---------- Variables ----------

// SetVariables replaces a dashboard's variable definitions.
func (h *DashboardsHandler) SetVariables(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetDashboardByID(id)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	var body struct {
		Variables json.RawMessage `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	vars, err := dashboards.ParseVariables(body.Variables)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ensureBaselineVersion(id)
	if err := h.DB.SetDashboardVariables(id, dashboards.Encode(vars)); err != nil {
		slog.Error("Failed to save dashboard variables", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to save variables")
		return
	}
	h.recordVersion(r, id, "")
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "dashboard.variables_updated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(fmt.Sprintf("%s (%d variables)", existing.Name, len(vars))),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"variables": vars})
}

// chCredentials is who an options query runs as.
type chCredentials struct {
	connectionID, user, encryptedPassword string
}

// runOptionsQuery expands the macros and the parent variables in an options
// query, applies the governance guardrails and runs it read only, capped at
// MaxOptions rows. The first column is the value, the second (if any) the
// label. When the cap is reached, all is the SELECT "All" expands to (see
// dashboards.AllSubquery).
func (h *DashboardsHandler) runOptionsQuery(ctx context.Context, creds chCredentials, endpoint string, v dashboards.Variable, parents map[string][]string, parentTypes map[string]string, timeRange *queryproc.TimeRange) ([]dashboards.Option, string, error) {
	if parents == nil {
		parents = map[string][]string{}
	}
	processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{
		Query:         v.Query,
		TimeRange:     timeRange,
		MaxDataPoints: 1000,
		Vars:          parents,
		VarTypes:      parentTypes,
	})
	if len(processed.Errors) > 0 {
		return nil, "", &statusError{http.StatusBadRequest, fmt.Sprintf("variable %s: %s", v.Name, strings.Join(processed.Errors, "; "))}
	}
	query := strings.TrimSpace(processed.Query)
	if h.guardrailsEnabled() {
		decision, err := h.Guardrails.EvaluateQuery(creds.connectionID, creds.user, query, endpoint)
		if err != nil {
			slog.Error("Guardrail evaluation failed for variable options", "error", err)
			return nil, "", &statusError{http.StatusInternalServerError, "Failed to evaluate governance guardrails"}
		}
		if !decision.Allowed {
			return nil, "", &statusError{http.StatusForbidden, fmt.Sprintf("variable %s: query blocked by governance policy", v.Name)}
		}
	}
	if h.Gateway == nil || h.Config == nil {
		return nil, "", &statusError{http.StatusServiceUnavailable, "No ClickHouse connection available"}
	}
	password, err := crypto.Decrypt(creds.encryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt credentials for variable options", "error", err)
		return nil, "", &statusError{http.StatusInternalServerError, "Failed to decrypt credentials"}
	}
	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(ctx, creds.connectionID, query, creds.user, password, optionsQuerySettings(), 35*time.Second)
	if err != nil {
		return nil, "", &statusError{http.StatusBadGateway, fmt.Sprintf("variable %s: %v", v.Name, err)}
	}
	opts, valueColumn, err := parseOptions(result.Meta, result.Data)
	if err != nil {
		return nil, "", err
	}
	all := ""
	if len(opts) >= dashboards.MaxOptions && valueColumn != "" {
		all = dashboards.AllSubquery(query, valueColumn)
	}
	return opts, all, nil
}

// parseOptions turns a FORMAT JSON result into options: the first column is
// the value, an optional second column the label. Duplicates are dropped. It
// also returns the name of the value column.
func parseOptions(metaRaw, dataRaw json.RawMessage) ([]dashboards.Option, string, error) {
	var meta []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil || len(meta) == 0 {
		return []dashboards.Option{}, "", nil
	}
	var rows []map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(dataRaw))
	dec.UseNumber()
	if err := dec.Decode(&rows); err != nil {
		return nil, "", &statusError{http.StatusBadGateway, "Unexpected options query result"}
	}
	out := make([]dashboards.Option, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		value := jsonText(row[meta[0].Name])
		if seen[value] || len(out) >= dashboards.MaxOptions {
			continue
		}
		seen[value] = true
		label := value
		if len(meta) > 1 {
			if l := jsonText(row[meta[1].Name]); l != "" {
				label = l
			}
		}
		out = append(out, dashboards.Option{Value: value, Label: label})
	}
	return out, meta[0].Name, nil
}

// jsonText renders one JSON value as text: strings unquoted, null empty,
// anything else as written.
func jsonText(raw json.RawMessage) string {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return ""
	}
	if strings.HasPrefix(t, `"`) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return t
}

func sessionCredentials(s *middleware.SessionInfo) chCredentials {
	return chCredentials{connectionID: s.ConnectionID, user: s.ClickhouseUser, encryptedPassword: s.EncryptedPassword}
}

func shareCredentials(s *database.DashboardShare) chCredentials {
	return chCredentials{connectionID: s.ConnectionID, user: s.ClickhouseUser, encryptedPassword: s.EncryptedPassword}
}

// sessionVariableValues resolves the variable values for an authenticated
// panel query. Without a dashboard the values are used as given, always
// quoted. With one, defaults fill what is missing and "All" expands.
func (h *DashboardsHandler) sessionVariableValues(r *http.Request, session *middleware.SessionInfo, dashboardID, query string, requested map[string][]string, timeRange *queryproc.TimeRange) (map[string][]string, map[string]string, *statusError) {
	if dashboardID == "" {
		return requested, nil, nil
	}
	all := h.loadVariables(dashboardID)
	if len(all) == 0 {
		return requested, nil, nil
	}
	// Only what this query uses is resolved, so options queries run only
	// when a panel needs them.
	defs := dashboards.Needed(all, query)
	run := func(v dashboards.Variable, parents map[string][]string, parentTypes map[string]string) ([]dashboards.Option, string, error) {
		return h.runOptionsQuery(r.Context(), sessionCredentials(session), r.URL.Path, v, parents, parentTypes, timeRange)
	}
	values, types, err := dashboards.Resolve(defs, requested, false, run)
	if err != nil {
		return nil, nil, asStatusError(err)
	}
	return values, types, nil
}

// publicOptions returns the options of a variable for a share link, from
// cache when possible. A miss counts against the viewer's query budget.
//
// The cached payload never leaves the server whole: it holds the "All"
// subquery, which carries the options query a share link must not see.
func (h *DashboardsHandler) publicOptions(r *http.Request, share *database.DashboardShare, v dashboards.Variable, parents map[string][]string, parentTypes map[string]string, timeRange *queryproc.TimeRange, rangeKey string) ([]dashboards.Option, string, error) {
	if v.Type != dashboards.TypeQuery {
		return v.StaticOptions(), "", nil
	}
	// Only the parents the query references can change its result.
	used := map[string][]string{}
	usedTypes := map[string]string{}
	for _, name := range queryproc.ReferencedVars(v.Query, keys(parents)) {
		used[name] = parents[name]
		if t := parentTypes[name]; t != "" {
			usedTypes[name] = t
		}
	}
	if !queryproc.HasTimeVariables(v.Query) {
		rangeKey = "none"
	}
	sum := hashToken(v.Query)
	key := strings.Join([]string{share.Token, v.Name, sum, rangeKey, dashboards.CanonicalKey(used)}, "|")
	cache := h.optionsCacheState()
	if status, payload, ok := cache.get(key); ok {
		return decodeOptionsPayload(status, payload)
	}
	_, limiter := h.publicState()
	if !limiter.allow(share.Token + "|" + middleware.ClientIP(r)) {
		return nil, "", &statusError{http.StatusTooManyRequests, "Rate limit exceeded. Please slow down."}
	}
	status, payload := cache.do(key, func() (int, []byte) {
		opts, all, err := h.runOptionsQuery(context.Background(), shareCredentials(share), r.URL.Path, v, used, usedTypes, timeRange)
		if err != nil {
			se := asStatusError(err)
			if se.status == http.StatusBadGateway {
				// A ClickHouse error can quote the options query.
				slog.Warn("Public variable options query failed", "error", se.msg, "dashboard", share.DashboardID)
				return errorPayload(se.status, fmt.Sprintf("The options of %s failed to load", v.Name))
			}
			return errorPayload(se.status, se.msg)
		}
		b, _ := json.Marshal(map[string]interface{}{"success": true, "options": opts, "all": all})
		return http.StatusOK, b
	})
	return decodeOptionsPayload(status, payload)
}

func decodeOptionsPayload(status int, payload []byte) ([]dashboards.Option, string, error) {
	var body struct {
		Options []dashboards.Option `json:"options"`
		All     string              `json:"all"`
		Error   string              `json:"error"`
	}
	_ = json.Unmarshal(payload, &body)
	if status != http.StatusOK {
		return nil, "", &statusError{status, body.Error}
	}
	if body.Options == nil {
		body.Options = []dashboards.Option{}
	}
	return body.Options, body.All, nil
}

// publicVariableValues resolves variable values for a share link: every
// value must be an option of its variable, a text or constant variable is
// its default.
func (h *DashboardsHandler) publicVariableValues(r *http.Request, share *database.DashboardShare, query string, requested map[string][]string, timeRange *queryproc.TimeRange) (map[string][]string, map[string]string, *statusError) {
	defs := dashboards.Needed(h.loadVariables(share.DashboardID), query)
	if len(defs) == 0 {
		return nil, nil, nil
	}
	rangeKey := "none"
	if timeRange != nil {
		rangeKey = timeRange.Type + "|" + timeRange.From + "|" + timeRange.To
	}
	run := func(v dashboards.Variable, parents map[string][]string, parentTypes map[string]string) ([]dashboards.Option, string, error) {
		return h.publicOptions(r, share, v, parents, parentTypes, timeRange, rangeKey)
	}
	values, types, err := dashboards.Resolve(defs, requested, true, run)
	if err != nil {
		return nil, nil, asStatusError(err)
	}
	return values, types, nil
}

type optionsRequest struct {
	Variables map[string][]string  `json:"variables"`
	TimeRange *queryproc.TimeRange `json:"time_range"`
	// Definition previews a variable that is not saved yet (editor only).
	Definition *dashboards.Variable `json:"definition"`
}

// passesWriterCheck applies middleware.RequireWriter, the check on the
// dashboard mutation routes, inside a handler, for a request that needs write
// access only for some bodies. On refusal the 403 is already written.
func passesWriterCheck(w http.ResponseWriter, r *http.Request) bool {
	allowed := false
	middleware.RequireWriter()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		allowed = true
	})).ServeHTTP(w, r)
	return allowed
}

// VariableOptions lists the options of one variable, with the values of the
// variables before it interpolated (cascading variables). Previewing an
// unsaved definition needs write access.
func (h *DashboardsHandler) VariableOptions(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	if d, err := h.DB.GetDashboardByID(id); err != nil || d == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	var body optionsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	name := chi.URLParam(r, "name")
	defs := h.loadVariables(id)
	v, found := dashboards.Find(defs, name)
	if body.Definition != nil {
		// A preview runs SQL that is not saved anywhere: editors only.
		if !passesWriterCheck(w, r) {
			return
		}
		def := *body.Definition
		def.Name = name
		check := []dashboards.Variable{def}
		if err := dashboards.Validate(check); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		v, found = check[0], true
	}
	if !found {
		writeError(w, http.StatusNotFound, "Variable not found")
		return
	}

	parentDefs := dashboards.Before(defs, name)
	run := func(pv dashboards.Variable, parents map[string][]string, parentTypes map[string]string) ([]dashboards.Option, string, error) {
		return h.runOptionsQuery(r.Context(), sessionCredentials(session), r.URL.Path, pv, parents, parentTypes, body.TimeRange)
	}
	parents, parentTypes, err := dashboards.Resolve(parentDefs, body.Variables, false, run)
	if err != nil {
		se := asStatusError(err)
		writeError(w, se.status, se.msg)
		return
	}
	opts, err := dashboards.Options(v, parents, parentTypes, run)
	if err != nil {
		se := asStatusError(err)
		writeError(w, se.status, se.msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"options": opts, "truncated": len(opts) >= dashboards.MaxOptions})
}

// PublicVariableOptions lists the options of one variable for a share link,
// run with the sharer's credentials and cached.
func (h *DashboardsHandler) PublicVariableOptions(w http.ResponseWriter, r *http.Request) {
	share := h.resolvePublicShare(w, r)
	if share == nil {
		return
	}
	var body optionsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	name := chi.URLParam(r, "name")
	defs := h.loadVariables(share.DashboardID)
	v, found := dashboards.Find(defs, name)
	// A share link does not list constants, so it has no options to ask for.
	if !found || v.Type == dashboards.TypeConstant {
		writeError(w, http.StatusNotFound, "Variable not found")
		return
	}
	timeRange, rangeKey := snapPublicTimeRange(body.TimeRange, time.Now())
	run := func(pv dashboards.Variable, parents map[string][]string, parentTypes map[string]string) ([]dashboards.Option, string, error) {
		return h.publicOptions(r, share, pv, parents, parentTypes, timeRange, rangeKey)
	}
	parents, parentTypes, err := dashboards.Resolve(dashboards.Before(defs, name), body.Variables, true, run)
	if err != nil {
		se := asStatusError(err)
		writeError(w, se.status, se.msg)
		return
	}
	opts, err := dashboards.Options(v, parents, parentTypes, run)
	if err != nil {
		se := asStatusError(err)
		writeError(w, se.status, se.msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"options": opts, "truncated": len(opts) >= dashboards.MaxOptions})
}

// ---------- Templates and duplicate ----------

// ListTemplates lists the built-in dashboard templates.
func (h *DashboardsHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := dashboards.Templates()
	if err != nil {
		slog.Error("Failed to load dashboard templates", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load templates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": list})
}

// CreateFromTemplate creates a dashboard from a built-in template, with all
// its panels and variables, in one transaction.
func (h *DashboardsHandler) CreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		TemplateID string `json:"template_id"`
		Name       string `json:"name"`
		FolderID   string `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	raw, tpl, err := dashboards.TemplateContent(strings.TrimSpace(body.TemplateID))
	if err != nil {
		writeError(w, http.StatusNotFound, "Template not found")
		return
	}
	var content importBody
	if err := json.Unmarshal(raw, &content); err != nil {
		slog.Error("Template is not valid", "error", err, "template", tpl.ID)
		writeError(w, http.StatusInternalServerError, "Template is not valid")
		return
	}
	if name := strings.TrimSpace(body.Name); name != "" && content.Dashboard != nil {
		content.Dashboard.Name = &name
	}
	h.createWithContent(w, r, content, strings.TrimSpace(body.FolderID), tpl.Tags, "dashboard.created_from_template", "Created from template "+tpl.Name)
}

// DuplicateDashboard copies a dashboard, its panels and variables, into a
// new dashboard in the same folder, in one transaction.
func (h *DashboardsHandler) DuplicateDashboard(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetDashboardByID(id)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	snap, err := h.DB.DashboardSnapshot(id)
	if err != nil || snap == nil {
		writeError(w, http.StatusInternalServerError, "Failed to read dashboard")
		return
	}
	raw, _ := json.Marshal(snap)
	var content importBody
	if err := json.Unmarshal(raw, &content); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read dashboard")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = existing.Name + " (copy)"
	}
	content.Dashboard.Name = &name
	folder := ""
	if existing.FolderID != nil {
		folder = *existing.FolderID
	}
	h.createWithContent(w, r, content, folder, existing.Tags, "dashboard.duplicated", "Duplicated from "+existing.Name)
}

func (h *DashboardsHandler) createWithContent(w http.ResponseWriter, r *http.Request, body importBody, folderID string, tags []string, action, note string) {
	session := middleware.GetSession(r)
	if folderID != "" {
		if folder, err := h.DB.GetDashboardFolder(folderID); err != nil || folder == nil {
			writeError(w, http.StatusBadRequest, "Folder not found")
			return
		}
	}
	content, err := h.buildContent(body, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.DB.CreateDashboardWithContent(content, middleware.Actor(session), folderID, tags)
	if err != nil {
		slog.Error("Failed to create dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create dashboard")
		return
	}
	h.recordVersion(r, id, note)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(content.Name),
	})
	h.writeDashboardDetail(w, r, id, http.StatusCreated)
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
