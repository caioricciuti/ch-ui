// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package reports

import (
	"bytes"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/mail"
)

// Snapshot is everything one run sends, rendered once and stored with the
// delivery so retries resend exactly the same content.
type Snapshot struct {
	ReportID  string     `json:"report_id"`
	Name      string     `json:"name"`
	Subject   string     `json:"subject"`
	Text      string     `json:"text"`
	HTML      string     `json:"html"`
	CSV       []byte     `json:"csv,omitempty"`
	CSVName   string     `json:"csv_name,omitempty"`
	Summary   string     `json:"summary"`
	Columns   []string   `json:"columns,omitempty"`
	Rows      [][]string `json:"rows,omitempty"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
	Empty     bool       `json:"empty"`
	Table     string     `json:"table"`
	Link      string     `json:"link,omitempty"`
	Timestamp string     `json:"timestamp"`
}

// Bytes is the size a delivery records: the CSV when there is one, else
// the text body.
func (s *Snapshot) Bytes() int {
	if len(s.CSV) > 0 {
		return len(s.CSV)
	}
	return len(s.Text)
}

// Message is the email form of the snapshot.
func (s *Snapshot) Message() mail.Message {
	m := mail.Message{Subject: s.Subject, Text: s.Text, HTML: s.HTML}
	if len(s.CSV) > 0 {
		m.Attachments = []mail.Attachment{{Filename: s.CSVName, ContentType: "text/csv; charset=utf-8", Data: s.CSV}}
	}
	return m
}

// Webhook is the webhook form of the snapshot.
func (s *Snapshot) Webhook() alerts.ReportWebhook {
	return alerts.ReportWebhook{
		ReportID: s.ReportID, Name: s.Name, Title: s.Subject, Summary: s.Summary,
		Columns: s.Columns, Rows: s.Rows, RowCount: s.RowCount, Truncated: s.Truncated,
		Table: s.Table, Link: s.Link, Timestamp: s.Timestamp,
	}
}

// Subject renders the subject template: {{name}}, {{date}} (in the
// subscription's time zone) and {{rows}}. Without a template it is
// "[CH-UI] Report: <name>".
func Subject(s *database.ReportSubscription, rows int, at time.Time) string {
	tpl := strings.TrimSpace(s.SubjectTemplate)
	if tpl == "" {
		tpl = "[CH-UI] Report: {{name}}"
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		loc = time.UTC
	}
	out := strings.NewReplacer(
		"{{name}}", s.Name,
		"{{date}}", at.In(loc).Format("2006-01-02"),
		"{{rows}}", strconv.Itoa(rows),
	).Replace(tpl)
	out = strings.NewReplacer("\r", " ", "\n", " ").Replace(out)
	if len(out) > 300 {
		out = strings.ToValidUTF8(out[:300], "")
	}
	return out
}

func csvFilename(name string, at time.Time) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "report"
	}
	if len(base) > 60 {
		base = base[:60]
	}
	return base + "-" + at.UTC().Format("20060102-1504") + ".csv"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// RenderQuery renders a saved query result.
func RenderQuery(s *database.ReportSubscription, t Table, link string, at time.Time) *Snapshot {
	rows := len(t.Rows)
	snap := &Snapshot{
		ReportID: s.ID, Name: s.Name, Subject: Subject(s, rows, at), RowCount: rows, Truncated: t.Truncated,
		Empty: rows == 0, Link: link, Timestamp: at.UTC().Format(time.RFC3339), Columns: t.Columns,
	}
	inline := t.Rows
	if len(inline) > s.InlineRows {
		inline = inline[:s.InlineRows]
	}
	snap.Rows = inline
	snap.Table = TextTable(t.Columns, inline)

	withCSV := s.Format == FormatCSV || s.Format == FormatBoth
	withInline := s.Format == FormatInline || s.Format == FormatBoth

	var notes []string
	if rows == 0 {
		notes = append(notes, "The query returned no rows.")
	} else {
		notes = append(notes, fmt.Sprintf("The query returned %s.", plural(rows, "row", "rows")))
	}
	if t.Truncated {
		notes = append(notes, fmt.Sprintf("The result was cut at the row limit of %d; the query has more rows.", s.RowLimit))
	}
	var csvNote string
	if withCSV && len(t.Columns) > 0 {
		data, written, cut := BuildCSV(t, s.MaxBytes)
		snap.CSV = data
		snap.CSVName = csvFilename(s.Name, at)
		if cut {
			snap.Truncated = true
			csvNote = fmt.Sprintf("The attached CSV (%s) stops at row %d of %d to stay under %s.", snap.CSVName, written, rows, formatBytes(s.MaxBytes))
		} else {
			csvNote = fmt.Sprintf("The full result is attached as %s (%s).", snap.CSVName, plural(written, "row", "rows"))
		}
		notes = append(notes, csvNote)
	}
	shown := len(inline) < rows
	if withInline && shown {
		notes = append(notes, fmt.Sprintf("The table shows the first %d of %d rows.", len(inline), rows))
	}

	// Webhooks cannot carry the CSV: they always get the inline table.
	var hook []string
	hook = append(hook, notes[0])
	if t.Truncated {
		hook = append(hook, notes[1])
	}
	if shown {
		hook = append(hook, fmt.Sprintf("Showing the first %d of %d rows.", len(inline), rows))
	}
	snap.Summary = strings.Join(hook, " ")

	var text strings.Builder
	text.WriteString(s.Name + "\n")
	text.WriteString("Generated " + at.UTC().Format("2006-01-02 15:04 UTC") + "\n\n")
	for _, n := range notes {
		text.WriteString(n + "\n")
	}
	if withInline && rows > 0 {
		text.WriteString("\n" + snap.Table + "\n")
	}
	if link != "" {
		text.WriteString("\nOpen in CH-UI: " + link + "\n")
	}
	snap.Text = text.String()

	var tableRows [][]string
	if withInline {
		tableRows = inline
	}
	snap.HTML = renderHTML(htmlData{
		Title: s.Name, Generated: at.UTC().Format("2006-01-02 15:04 UTC"), Notes: notes,
		Columns: t.Columns, Rows: tableRows, Link: link,
	})
	return snap
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

type htmlData struct {
	Title     string
	Generated string
	Notes     []string
	Columns   []string
	Rows      [][]string
	Pre       string
	Link      string
}

var htmlTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html><body style="margin:0;padding:16px;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;font-size:13px;color:#1f2328;background:#ffffff">
<h2 style="margin:0 0 4px;font-size:16px">{{.Title}}</h2>
<p style="margin:0 0 12px;color:#57606a">Generated {{.Generated}}</p>
{{range .Notes}}<p style="margin:0 0 6px">{{.}}</p>
{{end}}{{if .Rows}}<table cellpadding="0" cellspacing="0" style="border-collapse:collapse;margin-top:12px;font-size:12px">
<thead><tr>{{range .Columns}}<th style="text-align:left;padding:4px 8px;border-bottom:1px solid #d0d7de;background:#f6f8fa;white-space:nowrap">{{.}}</th>{{end}}</tr></thead>
<tbody>{{range .Rows}}<tr>{{range .}}<td style="padding:4px 8px;border-bottom:1px solid #eaeef2;white-space:nowrap">{{.}}</td>{{end}}</tr>
{{end}}</tbody></table>{{end}}{{if .Pre}}<pre style="margin-top:12px;font-family:SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;white-space:pre">{{.Pre}}</pre>{{end}}
{{if .Link}}<p style="margin-top:16px"><a href="{{.Link}}" style="color:#0969da">Open in CH-UI</a></p>{{end}}
</body></html>`))

func renderHTML(d htmlData) string {
	var b bytes.Buffer
	if err := htmlTemplate.Execute(&b, d); err != nil {
		return ""
	}
	return b.String()
}
