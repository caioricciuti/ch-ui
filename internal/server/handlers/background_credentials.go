package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// BackgroundCredentialRoutes is mounted under the authenticated connection API.
func (h *ConnectionsHandler) BackgroundCredentialRoutes(r chi.Router) {
	r.Use(middleware.RequireAdmin(h.DB))
	r.Get("/", h.ListBackgroundCredentials)
	r.Put("/{worker}", h.SetBackgroundCredential)
}

func (h *ConnectionsHandler) backgroundConnection(w http.ResponseWriter, r *http.Request) string {
	id := chi.URLParam(r, "id")
	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load connection")
		return ""
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return ""
	}
	return id
}

func (h *ConnectionsHandler) ListBackgroundCredentials(w http.ResponseWriter, r *http.Request) {
	id := h.backgroundConnection(w, r)
	if id == "" {
		return
	}
	items := make([]database.BackgroundCredential, 0, len(database.BackgroundWorkers()))
	for _, worker := range database.BackgroundWorkers() {
		c, err := h.DB.GetBackgroundCredential(id, worker)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to load background accounts")
			return
		}
		items = append(items, c)
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *ConnectionsHandler) SetBackgroundCredential(w http.ResponseWriter, r *http.Request) {
	id := h.backgroundConnection(w, r)
	if id == "" {
		return
	}
	worker := chi.URLParam(r, "worker")
	if !database.ValidBackgroundWorker(worker) {
		writeError(w, http.StatusBadRequest, "Unknown background worker")
		return
	}
	var in struct {
		Mode     string  `json:"mode"`
		Username string  `json:"username"`
		Password *string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	c := database.BackgroundCredential{Worker: worker, Mode: in.Mode, Username: strings.TrimSpace(in.Username)}
	switch in.Mode {
	case "session", "disabled":
		c.Username = ""
	case "service_account":
		if c.Username == "" || len(c.Username) > 256 || in.Password == nil {
			writeError(w, http.StatusBadRequest, "username and password are required; send an empty password explicitly if needed")
			return
		}
		if h.Gateway == nil || !h.Gateway.IsTunnelOnline(id) {
			writeError(w, http.StatusServiceUnavailable, "Connection must be online to verify this account")
			return
		}
		// Validate only authentication. The worker's actual queries are still
		// authorized by ClickHouse, and may need additional grants.
		_, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), id, "SELECT 1", c.Username, *in.Password, nil, 10*time.Second)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Account verification failed; check the credentials and connection")
			return
		}
		c.EncryptedPassword, err = crypto.Encrypt(*in.Password, h.Config.AppSecretKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to encrypt credentials")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "mode must be session, service_account or disabled")
		return
	}
	if err := h.DB.SetBackgroundCredential(id, c); err != nil {
		slog.Error("Failed to save background account", "connection", id, "worker", worker, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to save background account")
		return
	}
	actor := middleware.GetSession(r).ClickhouseUser
	if subject := middleware.GetSession(r).AuthSubject; subject != "" {
		actor = subject
	}
	details, _ := json.Marshal(map[string]string{"worker": worker, "mode": c.Mode, "account": c.Username})
	if err := h.DB.CreateAuditLog(database.AuditLogParams{
		Action: "connection.background_account.updated", Username: &actor, ConnectionID: &id,
		Details: strPtr(string(details)), IPAddress: strPtr(getClientIP(r)),
	}); err != nil {
		slog.Error("Failed to audit background account update", "connection", id, "worker", worker, "error", err)
	}
	saved, err := h.DB.GetBackgroundCredential(id, worker)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Account saved but could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
