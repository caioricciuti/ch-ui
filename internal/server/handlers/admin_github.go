package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	ghclient "github.com/caioricciuti/ch-ui/internal/github"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

func (h *AdminHandler) GetGitHubIntegration(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connectionId")
	integration, err := h.DB.GetGitHubIntegration(connID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load integration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "integration": integration})
}

func (h *AdminHandler) SaveGitHubIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}

	connID := chi.URLParam(r, "connectionId")
	session := middleware.GetSession(r)

	var body struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		Path   string `json:"path"`
		PAT    string `json:"pat"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	repo := strings.TrimSpace(body.Repo)
	if repo == "" {
		writeError(w, http.StatusBadRequest, "Repository is required (owner/repo)")
		return
	}
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, http.StatusBadRequest, "Repository must be in owner/repo format")
		return
	}

	branch := strings.TrimSpace(body.Branch)
	if branch == "" {
		branch = "main"
	}
	path := strings.TrimSpace(body.Path)
	if path == "" {
		path = "models/"
	}

	fields := map[string]string{
		"repo":    repo,
		"branch":  branch,
		"path":    path,
		"enabled": "true",
	}

	pat := strings.TrimSpace(body.PAT)
	if pat != "" {
		encrypted, err := crypto.Encrypt(pat, h.Config.AppSecretKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to encrypt token")
			return
		}
		fields["encrypted_pat"] = encrypted
	}

	if err := h.DB.SaveGitHubIntegration(connID, fields); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save integration")
		return
	}

	username := ""
	if session != nil {
		username = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "github.integration.save",
		Username:     strPtr(username),
		ConnectionID: strPtr(connID),
		Details:      strPtr(fmt.Sprintf("repo=%s branch=%s path=%s", repo, branch, path)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) DeleteGitHubIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}

	connID := chi.URLParam(r, "connectionId")
	if err := h.DB.DeleteGitHubIntegration(connID); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete integration")
		return
	}

	session := middleware.GetSession(r)
	username := ""
	if session != nil {
		username = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "github.integration.delete",
		Username:     strPtr(username),
		ConnectionID: strPtr(connID),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) TestGitHubConnection(w http.ResponseWriter, r *http.Request) {
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}

	connID := chi.URLParam(r, "connectionId")
	repo, branch, _, encryptedPAT, _, _ := h.DB.GetGitHubRawSettings(connID)
	if repo == "" || encryptedPAT == "" {
		writeError(w, http.StatusBadRequest, "GitHub integration not configured")
		return
	}

	pat, err := crypto.Decrypt(encryptedPAT, h.Config.AppSecretKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt token")
		return
	}

	parts := strings.SplitN(repo, "/", 2)
	client := ghclient.NewClient(pat)

	if err := client.ValidateAccess(parts[0], parts[1]); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	if _, err := client.GetBranchSHA(parts[0], parts[1], branch); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": fmt.Sprintf("Branch %q not found", branch)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) TriggerGitHubSync(w http.ResponseWriter, r *http.Request) {
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}
	if h.GitHubSyncer == nil {
		writeError(w, http.StatusInternalServerError, "GitHub syncer not initialized")
		return
	}

	connID := chi.URLParam(r, "connectionId")
	session := middleware.GetSession(r)
	triggeredBy := "admin"
	if session != nil {
		triggeredBy = session.ClickhouseUser
	}

	result, err := h.GitHubSyncer.SyncConnection(connID, triggeredBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "github.sync",
		Username:     strPtr(triggeredBy),
		ConnectionID: strPtr(connID),
		Details:      strPtr(fmt.Sprintf("created=%d updated=%d deleted=%d unchanged=%d", result.Created, result.Updated, result.Deleted, result.Unchanged)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"result": map[string]interface{}{
			"created":    result.Created,
			"updated":    result.Updated,
			"deleted":    result.Deleted,
			"unchanged":  result.Unchanged,
			"commit_sha": result.CommitSHA,
		},
	})
}

func (h *AdminHandler) GetGitHubSyncLogs(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connectionId")
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	logs, err := h.DB.GetGitHubSyncLogs(connID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load sync logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "logs": logs})
}

// GitHubWebhookHandler returns an http.HandlerFunc for receiving GitHub push webhooks.
func GitHubWebhookHandler(db *database.DB, cfg *config.Config, syncer *ghclient.Syncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.IsPro() {
			writeError(w, http.StatusPaymentRequired, "Pro license required")
			return
		}

		connID := chi.URLParam(r, "connectionId")
		_, branch, _, _, encryptedSecret, _ := db.GetGitHubRawSettings(connID)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Failed to read body")
			return
		}

		if encryptedSecret != "" {
			secret, err := crypto.Decrypt(encryptedSecret, cfg.AppSecretKey)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to decrypt webhook secret")
				return
			}
			sig := r.Header.Get("X-Hub-Signature-256")
			if !verifyWebhookSignature(body, sig, secret) {
				writeError(w, http.StatusForbidden, "Invalid signature")
				return
			}
		}

		var payload struct {
			Ref string `json:"ref"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		expectedRef := "refs/heads/" + branch
		if payload.Ref != expectedRef {
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "skipped": true, "reason": "push not to configured branch"})
			return
		}

		go func() {
			result, err := syncer.SyncConnection(connID, "github-webhook")
			if err != nil {
				slog.Error("GitHub webhook sync failed", "connectionId", connID, "error", err)
				return
			}
			slog.Info("GitHub webhook sync completed",
				"connectionId", connID,
				"created", result.Created,
				"updated", result.Updated,
				"deleted", result.Deleted,
			)
		}()

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "queued": true})
	}
}

func verifyWebhookSignature(body []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	expected := signature[7:]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	actual := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(actual))
}
