// SPDX-License-Identifier: BUSL-1.1
package incidenttimeline

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWindowValidation(t *testing.T) {
	for _, pair := range [][2]string{
		{"", ""}, {"2026-09-21T00:00:00Z", "2026-09-21T00:00:00Z"},
		{"2026-09-22T00:00:00Z", "2026-09-21T00:00:00Z"},
		{"2026-09-01T00:00:00Z", "2026-09-21T00:00:00Z"},
		{"2026-09-21T00:00:00Z' OR 1=1", "2026-09-21T01:00:00Z"},
	} {
		if _, err := ParseWindow(pair[0], pair[1]); err == nil {
			t.Errorf("accepted invalid window %v", pair)
		}
	}
	w, err := ParseWindow("2026-09-21T10:00:00+02:00", "2026-09-28T10:00:00+02:00")
	if err != nil || w.From.Hour() != 8 {
		t.Fatalf("valid timezone window: %+v %v", w, err)
	}
	if w.BucketSeconds() != 1800 {
		t.Fatal("week should use bounded half-hour buckets")
	}
}

func TestEventOrderingUsesTimeNotStringOrder(t *testing.T) {
	events := []Event{{ID: "later", At: "2026-09-21T10:00:00.1Z"}, {ID: "early", At: "2026-09-21T10:00:00Z"}, {ID: "earliest", At: "2026-09-21T11:59:59+02:00"}}
	Sort(events)
	if events[0].ID != "earliest" || events[1].ID != "early" || events[2].ID != "later" {
		t.Fatalf("wrong event order: %+v", events)
	}
}

func TestIncidentTimelineQueriesLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to test generated SQL against ClickHouse")
	}
	w := Window{From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC().Add(time.Minute)}
	client := &http.Client{Timeout: 20 * time.Second}
	for source, query := range map[string]string{"queries": w.QuerySQL(), "parts": w.PartsSQL()} {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(query))
		if err != nil {
			t.Fatal(err)
		}
		user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
		if user == "" {
			user = "default"
		}
		req.SetBasicAuth(user, os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"))
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if source == "parts" && res.StatusCode != 200 && strings.Contains(string(body), "UNKNOWN_TABLE") {
			t.Log("part_log absent: expected optional source degradation")
			continue
		}
		if res.StatusCode != 200 {
			t.Fatalf("%s query HTTP %d: %s", source, res.StatusCode, body)
		}
		if !strings.Contains(string(body), `"data"`) {
			t.Fatalf("%s did not return JSON rows: %s", source, body)
		}
	}
}
