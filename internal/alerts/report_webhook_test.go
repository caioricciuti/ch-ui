// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sampleReport() ReportWebhook {
	return ReportWebhook{
		ReportID: "r1", Name: "Daily", Title: "[CH-UI] Report: Daily", Summary: "The query returned 2 rows.",
		Columns: []string{"a", "b"}, Rows: [][]string{{"1", "x"}, {"2", "y"}}, RowCount: 2,
		Table: "a | b\n--+--\n1 | x\n2 | y", Link: "https://chui.example.com/schedules?section=reports", Timestamp: "2026-10-13T08:00:00Z",
	}
}

func postReport(t *testing.T, format string, rep ReportWebhook) []byte {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	cfg := map[string]interface{}{WebhookCfgURL: srv.URL, WebhookCfgFormat: format, WebhookCfgAllowHTTP: true, WebhookCfgAllowPrivate: true}
	if _, err := SendReportWebhook(context.Background(), srv.Client(), cfg, rep); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestReportWebhookGeneric(t *testing.T) {
	var got struct {
		EventType string     `json:"event_type"`
		Columns   []string   `json:"columns"`
		Rows      [][]string `json:"rows"`
		RowCount  int        `json:"row_count"`
		Link      string     `json:"link"`
	}
	if err := json.Unmarshal(postReport(t, WebhookFormatGeneric, sampleReport()), &got); err != nil {
		t.Fatal(err)
	}
	if got.EventType != EventTypeReportDelivered || len(got.Columns) != 2 || len(got.Rows) != 2 || got.RowCount != 2 || got.Link == "" {
		t.Fatalf("generic body %+v", got)
	}
}

func TestReportWebhookSlackCapsTable(t *testing.T) {
	rep := sampleReport()
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, strings.Repeat("x", 30)+" | `tick` <b>")
	}
	rep.Table = strings.Join(lines, "\n")
	var got map[string]string
	if err := json.Unmarshal(postReport(t, WebhookFormatSlack, rep), &got); err != nil {
		t.Fatal(err)
	}
	text := got["text"]
	start := strings.Index(text, "```\n")
	end := strings.LastIndex(text, "\n```")
	if start < 0 || end <= start {
		t.Fatalf("no code block: %q", text)
	}
	table := text[start+4 : end]
	if len(table) > SlackTableLimit+200 {
		t.Fatalf("table not capped: %d bytes", len(table))
	}
	if !strings.Contains(table, "more lines in CH-UI") {
		t.Fatal("truncation not stated")
	}
	if strings.Contains(table, "`") || strings.Contains(table, "<b>") {
		t.Fatal("backticks or markup leaked into the code block")
	}
	if !strings.Contains(text, "|Open in CH-UI>") {
		t.Fatal("missing link")
	}
}

func TestReportWebhookTeamsMonospace(t *testing.T) {
	var got struct {
		Attachments []struct {
			Content struct {
				Body []map[string]interface{} `json:"body"`
			} `json:"content"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(postReport(t, WebhookFormatTeams, sampleReport()), &got); err != nil {
		t.Fatal(err)
	}
	mono := 0
	for _, b := range got.Attachments[0].Content.Body {
		if b["fontType"] == "Monospace" {
			mono++
		}
	}
	if mono != 4 {
		t.Fatalf("want 4 monospace lines, got %d", mono)
	}
}
