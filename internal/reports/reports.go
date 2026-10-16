// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package reports delivers a saved query result or a dashboard summary on a
// cron to the existing alert channels (email, Slack, Teams, webhook). Every
// run uses the connection's dedicated "report" background account with
// readonly=1 and fixed limits. A run renders one snapshot; sends to each
// target retry from that snapshot, never by running the query again.
package reports

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const (
	// Worker is the background account reports run with. It must be a
	// dedicated account: reports never borrow a signed-in session.
	Worker = "report"
	// GuardrailEndpoint names report queries to the governance guardrails.
	GuardrailEndpoint = "report"

	SourceSavedQuery = "saved_query"
	SourceDashboard  = "dashboard"

	FormatCSV    = "csv"
	FormatInline = "inline"
	FormatBoth   = "both"

	DefaultRowLimit   = 1000
	MaxRowLimit       = 50000
	DefaultInlineRows = 50
	MaxInlineRows     = 200
	DefaultMaxBytes   = 1 << 20
	MinMaxBytes       = 1 << 10
	MaxMaxBytes       = 5 << 20
	DefaultTimeout    = 60
	MinTimeout        = 5
	MaxTimeout        = 300
	MaxTargets        = 5
	MaxRecipients     = 20
	// MaxConcurrent bounds the runs in flight: scheduled, manual and
	// previews together.
	MaxConcurrent = 3
	// MaxConsecutiveFailures turns a subscription off.
	MaxConsecutiveFailures = 10
	// SendAttempts is how often one target is tried before it fails.
	SendAttempts = 5
	// DefaultTimeRange is the dashboard range when none is set.
	DefaultTimeRange = "24h"
	// MaxDashboardPanels bounds the panels in a dashboard summary.
	MaxDashboardPanels = 25
	// SnapshotRetention and HistoryRetention bound what is kept.
	SnapshotRetention = 7 * 24 * time.Hour
	HistoryRetention  = 90 * 24 * time.Hour

	// SettingRecipientDomains holds the comma separated email domains
	// report recipients must use. Empty means any address.
	SettingRecipientDomains = "report_recipient_domains"
)

var timeRangeRE = regexp.MustCompile(`^[1-9][0-9]{0,3}(m|h|d|w|M)$`)

// Normalize checks a subscription and fills its defaults. The caller checks
// that the saved query, dashboard, connection and channels exist.
func Normalize(s *database.ReportSubscription) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(s.Name) > 200 {
		return fmt.Errorf("name is limited to 200 characters")
	}
	if strings.TrimSpace(s.ConnectionID) == "" {
		return fmt.Errorf("connection is required")
	}
	switch s.SourceType {
	case SourceSavedQuery:
		if s.SavedQueryID == nil || strings.TrimSpace(*s.SavedQueryID) == "" {
			return fmt.Errorf("choose a saved query")
		}
		s.DashboardID = nil
		s.TimeRange = ""
		s.Variables = nil
	case SourceDashboard:
		if s.DashboardID == nil || strings.TrimSpace(*s.DashboardID) == "" {
			return fmt.Errorf("choose a dashboard")
		}
		s.SavedQueryID = nil
		// A dashboard summary is text: there is no table to attach.
		s.Format = FormatInline
		s.TimeRange = strings.TrimSpace(s.TimeRange)
		if s.TimeRange == "" {
			s.TimeRange = DefaultTimeRange
		}
		if !timeRangeRE.MatchString(s.TimeRange) {
			return fmt.Errorf("time range must look like 1h, 24h, 7d or 30d")
		}
	default:
		return fmt.Errorf("source must be a saved query or a dashboard")
	}
	s.Cron = strings.Join(strings.Fields(s.Cron), " ")
	if !cronexpr.ValidateCron(s.Cron) {
		return fmt.Errorf("cron expression is not valid (five fields: minute hour day month weekday)")
	}
	s.Timezone = strings.TrimSpace(s.Timezone)
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("unknown time zone %q", s.Timezone)
	}
	switch s.Format {
	case "":
		s.Format = FormatCSV
	case FormatCSV, FormatInline, FormatBoth:
	default:
		return fmt.Errorf("format must be csv, inline or both")
	}
	if err := clamp(&s.RowLimit, DefaultRowLimit, 1, MaxRowLimit, "row limit"); err != nil {
		return err
	}
	if err := clamp(&s.InlineRows, DefaultInlineRows, 1, MaxInlineRows, "inline rows"); err != nil {
		return err
	}
	if err := clamp(&s.MaxBytes, DefaultMaxBytes, MinMaxBytes, MaxMaxBytes, "attachment size"); err != nil {
		return err
	}
	if err := clamp(&s.TimeoutSeconds, DefaultTimeout, MinTimeout, MaxTimeout, "timeout"); err != nil {
		return err
	}
	s.SubjectTemplate = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s.SubjectTemplate))
	if len(s.SubjectTemplate) > 300 {
		return fmt.Errorf("subject is limited to 300 characters")
	}
	if len(s.Targets) == 0 {
		return fmt.Errorf("choose at least one channel")
	}
	if len(s.Targets) > MaxTargets {
		return fmt.Errorf("at most %d channels per report", MaxTargets)
	}
	seen := map[string]bool{}
	for i := range s.Targets {
		t := &s.Targets[i]
		t.ChannelID = strings.TrimSpace(t.ChannelID)
		if t.ChannelID == "" || seen[t.ChannelID] {
			return fmt.Errorf("each channel may be chosen once")
		}
		seen[t.ChannelID] = true
		clean, err := CleanRecipients(t.Recipients)
		if err != nil {
			return err
		}
		t.Recipients = clean
	}
	return nil
}

func clamp(v *int, def, min, max int, label string) error {
	if *v == 0 {
		*v = def
	}
	if *v < min || *v > max {
		return fmt.Errorf("%s must be between %d and %d", label, min, max)
	}
	return nil
}

// CleanRecipients trims, de-duplicates and checks email addresses.
func CleanRecipients(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		a := strings.TrimSpace(raw)
		if a == "" {
			continue
		}
		parsed, err := mail.ParseAddress(a)
		if err != nil || parsed.Address != a || strings.ContainsAny(a, "\r\n,;") || len(a) > 254 {
			return nil, fmt.Errorf("%q is not a plain email address", a)
		}
		key := strings.ToLower(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	if len(out) > MaxRecipients {
		return nil, fmt.Errorf("at most %d recipients per channel", MaxRecipients)
	}
	return out, nil
}

// ParseDomains reads the recipient domain setting.
func ParseDomains(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' }) {
		d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(part), "@"))
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// CheckDomains refuses an address outside the allowed domains. No domains
// means any address.
func CheckDomains(recipients, domains []string) error {
	if len(domains) == 0 {
		return nil
	}
	for _, a := range recipients {
		at := strings.LastIndex(a, "@")
		domain := strings.ToLower(a[at+1:])
		ok := false
		for _, d := range domains {
			if domain == d {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s is outside the allowed report domains (%s)", a, strings.Join(domains, ", "))
		}
	}
	return nil
}

// NextRun is the subscription's next slot strictly after from, in its time
// zone, as UTC.
func NextRun(s *database.ReportSubscription, from time.Time) *time.Time {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return cronexpr.ComputeNextRunIn(s.Cron, from, loc)
}

var (
	literalOrComment = regexp.MustCompile(`(?s)'(?:[^'\\]|\\.|'')*'|"(?:[^"\\]|\\.)*"|` + "`(?:[^`\\\\]|\\\\.)*`" + `|--[^\n]*|/\*.*?\*/`)
	statementRE      = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)
	settingsRE       = regexp.MustCompile(`(?i)\bSETTINGS\s+[A-Za-z_][A-Za-z0-9_]*\s*=`)
	formatRE         = regexp.MustCompile(`(?is)\bFORMAT\s+[A-Za-z][A-Za-z0-9_]*\s*$`)
	intoOutfileRE    = regexp.MustCompile(`(?i)\bINTO\s+OUTFILE\b`)
	paramRE          = regexp.MustCompile(`\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*[^{}]+\}`)
)

// ValidateSQL checks a report query: one SELECT or WITH statement without
// SETTINGS, FORMAT or INTO OUTFILE. It returns it without a trailing
// semicolon.
func ValidateSQL(sql string) (string, error) {
	trimmed := strings.TrimSpace(sql)
	for strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	}
	if trimmed == "" {
		return "", fmt.Errorf("the saved query is empty")
	}
	bare := literalOrComment.ReplaceAllString(trimmed, " ")
	if strings.Contains(bare, ";") {
		return "", fmt.Errorf("a report runs one statement; the saved query has several")
	}
	if !statementRE.MatchString(bare) {
		return "", fmt.Errorf("a report runs SELECT or WITH queries only")
	}
	if settingsRE.MatchString(bare) {
		return "", fmt.Errorf("remove the SETTINGS clause; reports run with fixed limits")
	}
	if formatRE.MatchString(bare) || intoOutfileRE.MatchString(bare) {
		return "", fmt.Errorf("remove the FORMAT or INTO OUTFILE clause")
	}
	return trimmed, nil
}

// QueryParams returns the names of the {name:Type} bind parameters of a
// query, sorted, outside string literals and comments.
func QueryParams(sql string) []string {
	bare := literalOrComment.ReplaceAllString(sql, " ")
	seen := map[string]bool{}
	var out []string
	for _, m := range paramRE.FindAllStringSubmatch(bare, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// StoredParams decodes a saved query's default parameter values.
func StoredParams(stored *string) map[string]string {
	out := map[string]string{}
	if stored == nil || strings.TrimSpace(*stored) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(*stored), &out); err == nil {
		return out
	}
	var loose map[string]interface{}
	if err := json.Unmarshal([]byte(*stored), &loose); err == nil {
		for k, v := range loose {
			if v != nil {
				out[k] = fmt.Sprint(v)
			}
		}
	}
	return out
}

// SavedQueryParams returns the bind parameters a report sends for a saved
// query: its stored defaults. A parameter without a default is an error,
// since nobody is there to fill it in.
func SavedQueryParams(q *database.SavedQuery) (map[string]string, error) {
	defaults := StoredParams(q.Parameters)
	out := map[string]string{}
	var missing []string
	for _, name := range QueryParams(q.Query) {
		v, ok := defaults[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		out[name] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("saved query parameter %s has no default value; set one in Saved Queries first", strings.Join(missing, ", "))
	}
	return out, nil
}
