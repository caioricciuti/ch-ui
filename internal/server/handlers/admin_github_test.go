package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const ghTestConn = "conn-gh"

func githubWebhookRig(t *testing.T) (*chi.Mux, *database.DB, *config.Config) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "gh.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{AppSecretKey: "gh-webhook-test-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	if err := db.SaveGitHubIntegration(ghTestConn, map[string]string{"repo": "acme/models", "branch": "main", "enabled": "true"}); err != nil {
		t.Fatalf("save integration: %v", err)
	}
	r := chi.NewRouter()
	r.Post("/api/github/webhook/{connectionId}", GitHubWebhookHandler(db, cfg, nil))
	return r, db, cfg
}

func storeWebhookSecret(t *testing.T, db *database.DB, cfg *config.Config, secret string) {
	t.Helper()
	enc, err := crypto.Encrypt(secret, cfg.AppSecretKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := db.SaveGitHubIntegration(ghTestConn, map[string]string{"webhook_secret": enc}); err != nil {
		t.Fatalf("save secret: %v", err)
	}
}

func signGitHub(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postGitHubWebhook(r http.Handler, body []byte, sig string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/github/webhook/"+ghTestConn, bytes.NewReader(body))
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestGitHubWebhookWithoutSecretIsRejected(t *testing.T) {
	r, _, _ := githubWebhookRig(t)
	body := []byte(`{"ref":"refs/heads/other"}`)
	for _, sig := range []string{"", signGitHub(body, "")} {
		rec := postGitHubWebhook(r, body, sig)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Webhook secret is not configured") {
			t.Fatalf("sig %q: got %d %s, want 403 secret not configured", sig, rec.Code, rec.Body.String())
		}
	}
}

func TestGitHubWebhookBadSignatureIsRejected(t *testing.T) {
	r, db, cfg := githubWebhookRig(t)
	storeWebhookSecret(t, db, cfg, "correct-horse-battery-staple")
	body := []byte(`{"ref":"refs/heads/other"}`)
	cases := map[string]string{
		"missing":      "",
		"wrong secret": signGitHub(body, "not-the-secret-at-all"),
		"no prefix":    strings.TrimPrefix(signGitHub(body, "correct-horse-battery-staple"), "sha256="),
		"not hex":      "sha256=zz",
		"other body":   signGitHub([]byte(`{"ref":"refs/heads/main"}`), "correct-horse-battery-staple"),
	}
	for name, sig := range cases {
		rec := postGitHubWebhook(r, body, sig)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Invalid signature") {
			t.Fatalf("%s: got %d %s, want 403 Invalid signature", name, rec.Code, rec.Body.String())
		}
	}
}

func TestGitHubWebhookGoodSignatureIsAccepted(t *testing.T) {
	r, db, cfg := githubWebhookRig(t)
	storeWebhookSecret(t, db, cfg, "correct-horse-battery-staple")
	// A push to another branch is accepted and skipped, so no sync runs.
	body := []byte(`{"ref":"refs/heads/other"}`)
	rec := postGitHubWebhook(r, body, signGitHub(body, "correct-horse-battery-staple"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["skipped"] != true {
		t.Fatalf("want skipped push, got %v", out)
	}
}

func TestGitHubWebhookBodyIsCapped(t *testing.T) {
	r, db, cfg := githubWebhookRig(t)
	storeWebhookSecret(t, db, cfg, "correct-horse-battery-staple")
	body := bytes.Repeat([]byte("a"), maxGitHubWebhookBody+1)
	rec := postGitHubWebhook(r, body, signGitHub(body, "correct-horse-battery-staple"))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

func TestSaveGitHubIntegrationStoresWebhookSecret(t *testing.T) {
	_, db, cfg := githubWebhookRig(t)
	h := &AdminHandler{DB: db, Config: cfg}
	r := chi.NewRouter()
	r.Put("/api/admin/github/{connectionId}", h.SaveGitHubIntegration)
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/github/"+ghTestConn, strings.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := put(`{"repo":"acme/models","webhook_secret":"short"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("short secret: got %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec := put(`{"repo":"acme/models","webhook_secret":"correct-horse-battery-staple"}`); rec.Code != http.StatusOK {
		t.Fatalf("save: got %d %s, want 200", rec.Code, rec.Body.String())
	}
	stored, err := db.GetSetting(githubSettingKey(ghTestConn, "webhook_secret"))
	if err != nil || stored == "" || strings.Contains(stored, "correct-horse") {
		t.Fatalf("secret must be stored encrypted: %q %v", stored, err)
	}
	// Saving without a secret keeps the stored one.
	if rec := put(`{"repo":"acme/models"}`); rec.Code != http.StatusOK {
		t.Fatalf("resave: got %d", rec.Code)
	}
	again, _ := db.GetSetting(githubSettingKey(ghTestConn, "webhook_secret"))
	if again != stored {
		t.Fatal("a save without webhook_secret must keep the stored secret")
	}
	integ, _ := db.GetGitHubIntegration(ghTestConn)
	if integ == nil || !integ.HasWebhookSecret {
		t.Fatalf("integration should report a webhook secret: %+v", integ)
	}
}

func TestGitHubSyncGuardCoalesces(t *testing.T) {
	g := &githubSyncGuard{running: map[string]bool{}, rerun: map[string]bool{}}
	if !g.startOrQueue("c1") {
		t.Fatal("first push must start a sync")
	}
	for i := 0; i < 5; i++ {
		if g.startOrQueue("c1") {
			t.Fatal("a push during a running sync must not start a second one")
		}
	}
	if g.tryStart("c1") {
		t.Fatal("manual sync must not start while one is running")
	}
	if !g.startOrQueue("c2") {
		t.Fatal("other connections are independent")
	}
	if !g.done("c1") {
		t.Fatal("queued pushes must cause exactly one rerun")
	}
	if g.done("c1") {
		t.Fatal("after the rerun nothing is queued")
	}
	if !g.tryStart("c1") {
		t.Fatal("connection must be free after the last sync")
	}
}
