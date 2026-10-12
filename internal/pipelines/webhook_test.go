package pipelines

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleWebhookToken(t *testing.T) {
	const id = "pipe-token-test"
	recv := &webhookReceiver{authToken: "s3cret-token", incoming: make(chan Record, 10)}
	webhookRegistry.Store(id, recv)
	defer webhookRegistry.Delete(id)

	post := func(query, auth string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/pipelines/webhook/"+id+query, strings.NewReader(`{"a":1}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		HandleWebhook(rec, req)
		return rec.Code
	}

	cases := []struct {
		name, query, auth string
		want              int
	}{
		{"header", "", "Bearer s3cret-token", http.StatusOK},
		{"query (legacy)", "?token=s3cret-token", "", http.StatusOK},
		{"wrong header", "", "Bearer s3cret-tokeX", http.StatusUnauthorized},
		{"prefix of token", "", "Bearer s3cret", http.StatusUnauthorized},
		{"wrong query", "?token=nope", "", http.StatusUnauthorized},
		{"missing", "", "", http.StatusUnauthorized},
		{"empty bearer", "", "Bearer ", http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := post(c.query, c.auth); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestWebhookTokenMatchesNeverAcceptsEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	if webhookTokenMatches(req, "") {
		t.Fatal("an empty configured token must not match an empty presented token")
	}
}

func TestHandleWebhookWithoutTokenStaysOpen(t *testing.T) {
	const id = "pipe-open-test"
	webhookRegistry.Store(id, &webhookReceiver{incoming: make(chan Record, 10)})
	defer webhookRegistry.Delete(id)
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/webhook/"+id, strings.NewReader(`{"a":1}`))
	rec := httptest.NewRecorder()
	HandleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for a pipeline without auth", rec.Code)
	}
}
