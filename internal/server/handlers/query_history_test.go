package handlers

import (
	"strings"
	"testing"
)

func TestSanitizeQueryForHistory_RedactsIdentifiedBy(t *testing.T) {
	cases := []struct {
		in       string
		mustHide string
	}{
		{`CREATE USER bob IDENTIFIED BY 'sup3rsecret'`, "sup3rsecret"},
		{`ALTER USER bob IDENTIFIED WITH sha256_password BY 'p@ss'`, "p@ss"},
		{`create user x identified by 'lower-case'`, "lower-case"},
	}
	for _, c := range cases {
		out := sanitizeQueryForHistory(c.in)
		if strings.Contains(out, c.mustHide) {
			t.Fatalf("secret %q not redacted in %q", c.mustHide, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Fatalf("expected redaction marker in %q", out)
		}
	}
}

func TestSanitizeQueryForHistory_RedactsCredentialFunctions(t *testing.T) {
	out := sanitizeQueryForHistory(`SELECT * FROM s3('https://bucket/x.csv', 'ACCESS_KEY', 'SECRET_KEY', 'CSV')`)
	if strings.Contains(out, "ACCESS_KEY") || strings.Contains(out, "SECRET_KEY") {
		t.Fatalf("s3 credentials not redacted: %q", out)
	}

	out = sanitizeQueryForHistory(`SELECT * FROM mysql('host:3306', 'db', 'table', 'user', 'password123')`)
	if strings.Contains(out, "password123") {
		t.Fatalf("mysql credentials not redacted: %q", out)
	}
}

func TestSanitizeQueryForHistory_LeavesPlainQueriesAlone(t *testing.T) {
	q := `SELECT name, count() FROM events WHERE type = 'click' GROUP BY name`
	if out := sanitizeQueryForHistory(q); out != q {
		t.Fatalf("plain query modified: %q", out)
	}
}

func TestTruncateUTF8_DoesNotSplitRunes(t *testing.T) {
	s := strings.Repeat("a", 10) + "é"
	out := truncateUTF8(s, 11) // would split the 2-byte é
	if out != strings.Repeat("a", 10) {
		t.Fatalf("expected rune-safe truncation, got %q", out)
	}
	if got := truncateUTF8("short", 100); got != "short" {
		t.Fatalf("short strings must pass through, got %q", got)
	}
}
