// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Report delivery events, raised through the alert rules like the others.
const (
	EventTypeReportFailed    = "report.failed"
	EventTypeReportDisabled  = "report.disabled"
	EventTypeReportDelivered = "report.delivered"
)

// SlackTableLimit caps the code-block table of a Slack message, so the
// whole text stays well under Slack's message limit.
const SlackTableLimit = 3500

// teamsTableLines caps the monospace lines of a Teams card.
const teamsTableLines = 60

// ReportWebhook is one scheduled report as a webhook body. Rows are already
// capped at the subscription's inline row count.
type ReportWebhook struct {
	ReportID  string     `json:"report_id"`
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	Summary   string     `json:"summary"`
	Columns   []string   `json:"columns,omitempty"`
	Rows      [][]string `json:"rows,omitempty"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
	// Table is the monospace text form of the rows, or the text of a
	// dashboard summary; Slack and Teams show it.
	Table     string `json:"-"`
	Link      string `json:"link,omitempty"`
	Timestamp string `json:"timestamp"`
}

// SendReportWebhook POSTs a report to a webhook channel in its configured
// format: generic JSON with columns and rows, a Slack code-block table, or a
// Teams card of monospace lines. Slack and Teams link to CH-UI for the rest;
// nothing is uploaded.
func SendReportWebhook(ctx context.Context, client *http.Client, cfg map[string]interface{}, rep ReportWebhook) (string, error) {
	if err := ValidateWebhookConfig(cfg); err != nil {
		return "", err
	}
	body, err := reportWebhookBody(cfgString(cfg, WebhookCfgFormat), rep)
	if err != nil {
		return "", err
	}
	return postWebhook(ctx, client, cfg, body)
}

func reportWebhookBody(format string, rep ReportWebhook) ([]byte, error) {
	switch format {
	case WebhookFormatSlack:
		var b strings.Builder
		b.WriteString("*" + slackEscape(rep.Title) + "*")
		if rep.Summary != "" {
			b.WriteString("\n" + slackEscape(rep.Summary))
		}
		if strings.TrimSpace(rep.Table) != "" {
			table := strings.ReplaceAll(rep.Table, "`", "'")
			b.WriteString("\n```\n" + CapLines(slackEscape(table), SlackTableLimit) + "\n```")
		}
		if rep.Link != "" {
			b.WriteString("\n<" + rep.Link + "|Open in CH-UI>")
		}
		return json.Marshal(map[string]string{"text": b.String()})
	case WebhookFormatTeams:
		blocks := []map[string]interface{}{
			{"type": "TextBlock", "text": rep.Title, "weight": "Bolder", "size": "Medium", "wrap": true},
		}
		if rep.Summary != "" {
			blocks = append(blocks, map[string]interface{}{"type": "TextBlock", "text": rep.Summary, "wrap": true})
		}
		if strings.TrimSpace(rep.Table) != "" {
			lines := strings.Split(CapLines(rep.Table, SlackTableLimit), "\n")
			if len(lines) > teamsTableLines {
				more := len(lines) - teamsTableLines
				lines = append(lines[:teamsTableLines], fmt.Sprintf("... %d more lines in CH-UI", more))
			}
			for _, line := range lines {
				if line == "" {
					line = " "
				}
				blocks = append(blocks, map[string]interface{}{
					"type": "TextBlock", "text": line, "fontType": "Monospace", "size": "Small", "spacing": "None", "wrap": false,
				})
			}
		}
		card := map[string]interface{}{
			"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
			"type":    "AdaptiveCard",
			"version": "1.4",
			"body":    blocks,
		}
		if rep.Link != "" {
			card["actions"] = []map[string]string{{"type": "Action.OpenUrl", "title": "Open in CH-UI", "url": rep.Link}}
		}
		return json.Marshal(map[string]interface{}{
			"type": "message",
			"attachments": []map[string]interface{}{
				{"contentType": "application/vnd.microsoft.card.adaptive", "content": card},
			},
		})
	default:
		out := struct {
			EventType string `json:"event_type"`
			ReportWebhook
		}{EventType: EventTypeReportDelivered, ReportWebhook: rep}
		if out.Columns == nil {
			out.Columns = []string{}
		}
		if out.Rows == nil {
			out.Rows = [][]string{}
		}
		return json.Marshal(out)
	}
}

// CapLines cuts text at a line boundary so it is at most limit bytes, and
// says how many lines were left out.
func CapLines(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	kept := 0
	for _, line := range lines {
		if b.Len()+len(line)+1 > limit-60 {
			break
		}
		if kept > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		kept++
	}
	if kept == 0 && len(lines) > 0 {
		cut := limit - 60
		if cut < 0 {
			cut = 0
		}
		first := lines[0]
		if len(first) > cut {
			first = strings.ToValidUTF8(first[:cut], "")
		}
		b.WriteString(first)
		kept = 1
	}
	fmt.Fprintf(&b, "\n... %d more lines in CH-UI", len(lines)-kept)
	return b.String()
}

func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
