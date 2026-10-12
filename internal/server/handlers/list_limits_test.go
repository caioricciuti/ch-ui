package handlers

import (
	"net/url"
	"testing"
)

func TestModelRunsPageCap(t *testing.T) {
	cases := []struct {
		query             string
		wantLim, wantOffs int
	}{
		{"", 20, 0},
		{"limit=50&offset=10", 50, 10},
		{"limit=1000000", maxModelRunsLimit, 0},
		{"limit=-5&offset=-3", 20, 0},
		{"limit=abc", 20, 0},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		lim, off := modelRunsPage(q)
		if lim != c.wantLim || off != c.wantOffs {
			t.Errorf("%q: got limit=%d offset=%d, want %d %d", c.query, lim, off, c.wantLim, c.wantOffs)
		}
	}
	if maxModelRunsLimit != 500 {
		t.Fatalf("maxModelRunsLimit = %d, want 500", maxModelRunsLimit)
	}
}

func TestAuditLogLimitCap(t *testing.T) {
	cases := []struct {
		raw            string
		def, max, want int
	}{
		{"", 100, maxAuditLogPage, 100},
		{"250", 100, maxAuditLogPage, 250},
		{"5000", 100, maxAuditLogPage, 1000},
		{"0", 100, maxAuditLogPage, 100},
		{"", maxAuditLogExport, maxAuditLogExport, 100000},
		{"1000000", maxAuditLogExport, maxAuditLogExport, 100000},
		{"500", maxAuditLogExport, maxAuditLogExport, 500},
	}
	for _, c := range cases {
		if got := auditLogLimit(c.raw, c.def, c.max); got != c.want {
			t.Errorf("auditLogLimit(%q, %d, %d) = %d, want %d", c.raw, c.def, c.max, got, c.want)
		}
	}
}
