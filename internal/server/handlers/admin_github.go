// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

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
		// WebhookSecret is optional on save. When set, it is stored encrypted
		// and the push webhook accepts only requests signed with it. The
		// webhook refuses every request until one is set.
		WebhookSecret string `json:"webhook_secret"`
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

	webhookSecret := strings.TrimSpace(body.WebhookSecret)
	if webhookSecret != "" {
		if len(webhookSecret) < minWebhookSecretLength {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Webhook secret must be at least %d characters", minWebhookSecretLength))
			return
		}
		encrypted, err := crypto.Encrypt(webhookSecret, h.Config.AppSecretKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to encrypt webhook secret")
			return
		}
		fields["webhook_secret"] = encrypted
	}

	if err := h.DB.SaveGitHubIntegration(connID, fields); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save integration")
		return
	}

	username, chUser := "", ""
	if session != nil {
		username, chUser = middleware.Actor(session), session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "github.integration.save",
		Username:       strPtr(username),
		ClickhouseUser: strPtr(chUser),
		ConnectionID:   strPtr(connID),
		Details:        strPtr(fmt.Sprintf("repo=%s branch=%s path=%s", repo, branch, path)),
		IPAddress:      strPtr(getClientIP(r)),
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
	username, chUser := "", ""
	if session != nil {
		username, chUser = middleware.Actor(session), session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "github.integration.delete",
		Username:       strPtr(username),
		ClickhouseUser: strPtr(chUser),
		ConnectionID:   strPtr(connID),
		IPAddress:      strPtr(getClientIP(r)),
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
		writeError(w, http.StatusOK, err.Error())
		return
	}

	if _, err := client.GetBranchSHA(parts[0], parts[1], branch); err != nil {
		writeError(w, http.StatusOK, fmt.Sprintf("Branch %q not found", branch))
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
	triggeredBy, chUser := "admin", ""
	if session != nil {
		triggeredBy, chUser = middleware.Actor(session), session.ClickhouseUser
	}

	if !githubSyncs.tryStart(connID) {
		writeError(w, http.StatusConflict, "A GitHub sync is already running for this connection")
		return
	}
	result, err := h.GitHubSyncer.SyncConnection(connID, triggeredBy)
	if githubSyncs.done(connID) {
		// A push arrived while this sync ran; pull once more in the background.
		go runQueuedGitHubSync(h.GitHubSyncer, connID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "github.sync",
		Username:       strPtr(triggeredBy),
		ClickhouseUser: strPtr(chUser),
		ConnectionID:   strPtr(connID),
		Details:        strPtr(fmt.Sprintf("created=%d updated=%d deleted=%d unchanged=%d", result.Created, result.Updated, result.Deleted, result.Unchanged)),
		IPAddress:      strPtr(getClientIP(r)),
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

const (
	// maxGitHubWebhookBody caps a push payload. GitHub caps deliveries at
	// 25 MB, but a push event for a model repo is a few KB; 5 MiB is generous.
	maxGitHubWebhookBody = 5 << 20
	// minWebhookSecretLength rejects trivially guessable webhook secrets.
	minWebhookSecretLength = 16
)

// githubSettingKey mirrors the database package's github.<conn>.<key> layout,
// so the webhook can read the secret with its error instead of the
// error-swallowing GetGitHubRawSettings.
func githubSettingKey(connID, key string) string {
	return "github." + connID + "." + key
}

// githubSyncGuard makes sure at most one sync runs per connection. A push
// that arrives while a sync is running is coalesced into one follow-up sync,
// so a burst of pushes costs at most two syncs instead of one goroutine each.
type githubSyncGuard struct {
	mu      sync.Mutex
	running map[string]bool
	rerun   map[string]bool
}

var githubSyncs = &githubSyncGuard{running: map[string]bool{}, rerun: map[string]bool{}}

// tryStart claims the connection. It returns false if a sync is running.
func (g *githubSyncGuard) tryStart(connID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running[connID] {
		return false
	}
	g.running[connID] = true
	return true
}

// startOrQueue claims the connection, or, if a sync is running, asks it to
// run once more when it finishes. It returns true if the caller must start
// the sync.
func (g *githubSyncGuard) startOrQueue(connID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running[connID] {
		g.rerun[connID] = true
		return false
	}
	g.running[connID] = true
	return true
}

// done is called when a sync finishes. If a rerun was queued it keeps the
// claim and returns true: the caller must run the sync again.
func (g *githubSyncGuard) done(connID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rerun[connID] {
		delete(g.rerun, connID)
		return true
	}
	delete(g.running, connID)
	return false
}

// runQueuedGitHubSync runs webhook syncs for a connection whose claim the
// caller already holds, until no further push is queued.
func runQueuedGitHubSync(syncer *ghclient.Syncer, connID string) {
	for {
		result, err := syncer.SyncConnection(connID, "github-webhook")
		if err != nil {
			slog.Error("GitHub webhook sync failed", "connectionId", connID, "error", err)
		} else {
			slog.Info("GitHub webhook sync completed",
				"connectionId", connID,
				"created", result.Created,
				"updated", result.Updated,
				"deleted", result.Deleted,
			)
		}
		if !githubSyncs.done(connID) {
			return
		}
	}
}

// GitHubWebhookHandler returns an http.HandlerFunc for receiving GitHub push webhooks.
// Every request must carry a valid X-Hub-Signature-256 for the connection's
// stored webhook secret; with no secret stored the webhook is refused.
func GitHubWebhookHandler(db *database.DB, cfg *config.Config, syncer *ghclient.Syncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.IsPro() {
			writeError(w, http.StatusPaymentRequired, "Pro license required")
			return
		}

		connID := chi.URLParam(r, "connectionId")
		encryptedSecret, err := db.GetSetting(githubSettingKey(connID, "webhook_secret"))
		if err != nil {
			slog.Error("GitHub webhook: failed to load webhook secret", "connectionId", connID, "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to load webhook settings")
			return
		}
		if encryptedSecret == "" {
			writeError(w, http.StatusForbidden, "Webhook secret is not configured for this connection. Set one in Admin > GitHub and use the same secret in the GitHub webhook.")
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGitHubWebhookBody))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "Webhook payload too large")
				return
			}
			writeError(w, http.StatusBadRequest, "Failed to read body")
			return
		}

		secret, err := crypto.Decrypt(encryptedSecret, cfg.AppSecretKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to decrypt webhook secret")
			return
		}
		if !verifyWebhookSignature(body, r.Header.Get("X-Hub-Signature-256"), secret) {
			writeError(w, http.StatusForbidden, "Invalid signature")
			return
		}

		var payload struct {
			Ref string `json:"ref"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		_, branch, _, _, _, _ := db.GetGitHubRawSettings(connID)
		expectedRef := "refs/heads/" + branch
		if payload.Ref != expectedRef {
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "skipped": true, "reason": "push not to configured branch"})
			return
		}

		if syncer == nil {
			writeError(w, http.StatusInternalServerError, "GitHub syncer not initialized")
			return
		}
		started := githubSyncs.startOrQueue(connID)
		if started {
			go runQueuedGitHubSync(syncer, connID)
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "queued": true, "coalesced": !started})
	}
}

// verifyWebhookSignature checks a GitHub "sha256=<hex>" signature in
// constant time.
func verifyWebhookSignature(body []byte, signature, secret string) bool {
	if secret == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(signature[len("sha256="):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
