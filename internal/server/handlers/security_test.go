package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/caioricciuti/ch-ui/internal/config"
)

func TestSecurityLoginRejectsBrowserForgery(t *testing.T) {
	h := &AuthHandler{Config: &config.Config{AppURL: "https://app.example"}}
	for _, tc := range []struct {
		contentType, origin string
		want                int
	}{
		{"text/plain", "https://evil.example", 415},
		{"application/x-www-form-urlencoded", "https://evil.example", 415},
		{"application/json", "https://evil.example", 403},
		{"application/json", "https://sibling.example", 403},
		{"application/json; charset=utf-8", "https://app.example", 400},
	} {
		r := httptest.NewRequest(http.MethodPost, "https://app.example/api/auth/login", strings.NewReader(`{"username":""}`))
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.Login(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: status %d want %d", tc.contentType, tc.origin, w.Code, tc.want)
		}
	}
}

func TestSecurityAuditPreviewRedactsBeforeTruncating(t *testing.T) {
	for _, query := range []string{
		`CREATE USER bob IDENTIFIED BY 'secret'`,
		strings.Repeat(" ", 55) + `CREATE USER bob IDENTIFIED BY 'secret` + strings.Repeat("x", 150) + `'`,
		`SELECT * FROM s3('https://bucket/data','access','secret','CSV')`,
		`SELECT * FROM mysql('db:3306','db','table','user','secret')`,
	} {
		got := auditQueryPreview(query)
		if strings.Contains(got, "secret") || !utf8.ValidString(got) {
			t.Fatalf("unsafe audit: %q", got)
		}
	}
	if got := auditQueryPreview("SELECT 1"); got != "SELECT 1" {
		t.Fatal(got)
	}
}
