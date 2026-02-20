package queryproc

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TimeRange represents a dashboard time range selection.
type TimeRange struct {
	Type string `json:"type"` // "relative" or "absolute"
	From string `json:"from"`
	To   string `json:"to"`
}

// ProcessorOptions contains all inputs for query variable interpolation.
type ProcessorOptions struct {
	Query         string
	TimeRange     *TimeRange
	TimeField     string
	TimeFieldUnit string // "ns", "us", "ms", "s" - defaults to "ms"
	MaxDataPoints int    // defaults to 1000
	Table         string
}

// ProcessedResult contains the output of query variable interpolation.
type ProcessedResult struct {
	Query            string         `json:"query"`
	HasTimeVariables bool           `json:"has_time_variables"`
	InterpolatedVars map[string]any `json:"interpolated_vars"`
	Errors           []string       `json:"errors"`
}

// Variable patterns for detection and replacement.
var (
	patTimeFilterWithCol = regexp.MustCompile(`(?i)\$__timeFilter\(([^)]+)\)`)
	patTimeFilterSimple  = regexp.MustCompile(`(?i)\$__timeFilter`)
	patTimestampFilter   = regexp.MustCompile(`(?i)\$__timestamp\(([^)]+)\)`)
	patTimeField         = regexp.MustCompile(`(?i)\$__timeField`)
	patTimeFrom          = regexp.MustCompile(`(?i)\$__timeFrom`)
	patTimeTo            = regexp.MustCompile(`(?i)\$__timeTo`)
	patInterval          = regexp.MustCompile(`(?i)\$__interval`)
	patTable             = regexp.MustCompile(`(?i)\$__table`)

	// Pattern for parsing relative time strings like "5m", "1h", "7d", "1M", "1y",
	// and Grafana-style forms like "now-5m" or "now-15min".
	patRelativeTime = regexp.MustCompile(`^(?:now-)?\s*(\d+)\s*([a-zA-Z]+)$`)
)

// HasTimeVariables checks if the query contains any supported time or table variables.
func HasTimeVariables(query string) bool {
	if query == "" {
		return false
	}
	return patTimeFilterWithCol.MatchString(query) ||
		patTimeFilterSimple.MatchString(query) ||
		patTimestampFilter.MatchString(query) ||
		patTimeField.MatchString(query) ||
		patTimeFrom.MatchString(query) ||
		patTimeTo.MatchString(query) ||
		patInterval.MatchString(query) ||
		patTable.MatchString(query)
}

// ProcessQueryVariables interpolates all dashboard variables in the given query.
func ProcessQueryVariables(opts ProcessorOptions) ProcessedResult {
	if opts.Query == "" {
		return ProcessedResult{
			Query:            "",
			HasTimeVariables: false,
			InterpolatedVars: map[string]any{},
			Errors:           []string{"Invalid query provided"},
		}
	}

	// Apply defaults.
	timeFieldUnit := opts.TimeFieldUnit
	if timeFieldUnit == "" {
		timeFieldUnit = "ms"
	}
	maxDataPoints := opts.MaxDataPoints
	if maxDataPoints <= 0 {
		maxDataPoints = 1000
	}

	processedQuery := opts.Query
	interpolatedVars := map[string]any{}
	var errors []string

	// 1. Handle $__table replacement.
	if patTable.MatchString(processedQuery) {
		if opts.Table != "" {
			processedQuery = patTable.ReplaceAllString(processedQuery, opts.Table)
			interpolatedVars["table"] = opts.Table
		} else {
			errors = append(errors, "$__table variable used but no table specified")
		}
	}

	// 2. Handle $__timeField replacement.
	if patTimeField.MatchString(processedQuery) {
		effectiveTimeField := opts.TimeField
		if effectiveTimeField == "" {
			effectiveTimeField = "timestamp"
		}
		processedQuery = patTimeField.ReplaceAllString(processedQuery, effectiveTimeField)
		interpolatedVars["timeField"] = effectiveTimeField
	}

	// 3. Handle time-range-related variables.
	if opts.TimeRange != nil {
		from, to, ok := getTimeBounds(opts.TimeRange)
		if ok {
			// 3a. Handle $__timeFilter(column) with column name in parentheses.
			if matches := patTimeFilterWithCol.FindStringSubmatch(processedQuery); matches != nil {
				columnName := strings.TrimSpace(matches[1])
				timeFilter := generateTimeFilter(from, to, columnName, timeFieldUnit)
				processedQuery = patTimeFilterWithCol.ReplaceAllString(processedQuery, timeFilter)
				interpolatedVars["timeFilter"] = timeFilter
				interpolatedVars["timeFilterColumn"] = columnName
			}

			// 3b. Handle $__timeFilter without column (uses timeField or "timestamp").
			// After the column variant is already replaced, only bare $__timeFilter remain.
			if patTimeFilterSimple.MatchString(processedQuery) {
				effectiveTimeField := opts.TimeField
				if effectiveTimeField == "" {
					effectiveTimeField = "timestamp"
				}
				timeFilter := generateTimeFilter(from, to, effectiveTimeField, timeFieldUnit)
				processedQuery = patTimeFilterSimple.ReplaceAllString(processedQuery, timeFilter)
				interpolatedVars["timeFilter"] = timeFilter
			}

			// 3c. Handle $__timeFrom.
			if patTimeFrom.MatchString(processedQuery) {
				fromValue := convertToEpoch(from, timeFieldUnit)
				processedQuery = patTimeFrom.ReplaceAllString(processedQuery, strconv.FormatInt(fromValue, 10))
				interpolatedVars["timeFrom"] = fromValue
			}

			// 3d. Handle $__timeTo.
			if patTimeTo.MatchString(processedQuery) {
				toValue := convertToEpoch(to, timeFieldUnit)
				processedQuery = patTimeTo.ReplaceAllString(processedQuery, strconv.FormatInt(toValue, 10))
				interpolatedVars["timeTo"] = toValue
			}

			// 3e. Handle $__interval.
			if patInterval.MatchString(processedQuery) {
				intervalSeconds := calculateInterval(from, to, maxDataPoints)
				processedQuery = patInterval.ReplaceAllString(processedQuery, strconv.Itoa(intervalSeconds))
				interpolatedVars["interval"] = intervalSeconds
			}

			// 3f. Handle $__timestamp(column) as a DateTime range predicate.
			if patTimestampFilter.MatchString(processedQuery) {
				fromSec := from.Unix()
				toSec := to.Unix()
				processedQuery = patTimestampFilter.ReplaceAllStringFunc(processedQuery, func(match string) string {
					matches := patTimestampFilter.FindStringSubmatch(match)
					if len(matches) < 2 {
						return match
					}
					columnName := strings.TrimSpace(matches[1])
					return fmt.Sprintf("(%s >= toDateTime(%d) AND %s <= toDateTime(%d))", columnName, fromSec, columnName, toSec)
				})
				interpolatedVars["timestampFrom"] = fromSec
				interpolatedVars["timestampTo"] = toSec
			}
		} else {
			errors = append(errors, "Invalid time range provided")
		}
	} else if patTimeFilterWithCol.MatchString(processedQuery) ||
		patTimeFilterSimple.MatchString(processedQuery) ||
		patTimeFrom.MatchString(processedQuery) ||
		patTimeTo.MatchString(processedQuery) ||
		patInterval.MatchString(processedQuery) ||
		patTimestampFilter.MatchString(processedQuery) {
		errors = append(errors, "Time-range variables found but no time range was provided")
	}

	return ProcessedResult{
		Query:            processedQuery,
		HasTimeVariables: HasTimeVariables(opts.Query),
		InterpolatedVars: interpolatedVars,
		Errors:           errors,
	}
}

// InferTimeUnit infers the time unit from a column name suffix.
// Returns "ns", "us", "ms", or "s". Defaults to "ms".
func InferTimeUnit(columnName string) string {
	lower := strings.ToLower(columnName)

	if strings.HasSuffix(lower, "_ns") || strings.Contains(lower, "_ns_") {
		return "ns"
	}
	if strings.HasSuffix(lower, "_us") || strings.Contains(lower, "_us_") {
		return "us"
	}
	if strings.HasSuffix(lower, "_ms") || strings.Contains(lower, "_ms_") {
		return "ms"
	}
	if strings.HasSuffix(lower, "_s") && !strings.HasSuffix(lower, "_ms") {
		return "s"
	}

	return "ms"
}

// getTimeBounds parses a TimeRange into concrete from/to time.Time values.
func getTimeBounds(tr *TimeRange) (from, to time.Time, ok bool) {
	now := time.Now()

	if tr.Type == "relative" {
		toToken := strings.TrimSpace(tr.To)
		if toToken == "" {
			toToken = "now"
		}
		fromToken := strings.TrimSpace(tr.From)
		if fromToken == "" {
			fromToken = "1h"
		}

		to = parseRelativeTime(toToken, now)
		from = parseRelativeTime(fromToken, now)
	} else {
		from = parseAbsoluteTime(tr.From)
		to = parseAbsoluteTime(tr.To)
	}

	if from.IsZero() || to.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	if from.After(to) {
		from, to = to, from
	}

	return from, to, true
}

// parseRelativeTime parses a relative time string like "5m", "1h", "7d", "1M", "1y"
// as an offset subtracted from the base time. "now" returns the base time unchanged.
func parseRelativeTime(timeStr string, base time.Time) time.Time {
	trimmed := strings.ToLower(strings.TrimSpace(timeStr))
	if trimmed == "" {
		return base.Add(-5 * time.Minute)
	}
	if trimmed == "now" {
		return base
	}

	matches := patRelativeTime.FindStringSubmatch(trimmed)
	if matches == nil {
		// Default to 5 minutes ago on invalid input.
		return base.Add(-5 * time.Minute)
	}

	value, _ := strconv.Atoi(matches[1])
	unit := matches[2]
	switch unit {
	case "s", "sec", "secs", "second", "seconds":
		unit = "s"
	case "m", "min", "mins", "minute", "minutes":
		unit = "m"
	case "h", "hr", "hrs", "hour", "hours":
		unit = "h"
	case "d", "day", "days":
		unit = "d"
	case "w", "week", "weeks":
		unit = "w"
	case "mo", "mon", "month", "months", "mth":
		unit = "M"
	case "y", "yr", "yrs", "year", "years":
		unit = "y"
	}

	switch unit {
	case "s":
		return base.Add(-time.Duration(value) * time.Second)
	case "m":
		return base.Add(-time.Duration(value) * time.Minute)
	case "h":
		return base.Add(-time.Duration(value) * time.Hour)
	case "d":
		return base.Add(-time.Duration(value) * 24 * time.Hour)
	case "w":
		return base.Add(-time.Duration(value) * 7 * 24 * time.Hour)
	case "M":
		// Approximate month as 30 days.
		return base.Add(-time.Duration(value) * 30 * 24 * time.Hour)
	case "y":
		// Approximate year as 365 days.
		return base.Add(-time.Duration(value) * 365 * 24 * time.Hour)
	default:
		return base.Add(-5 * time.Minute)
	}
}

// parseAbsoluteTime attempts to parse a time string in common formats.
func parseAbsoluteTime(s string) time.Time {
	// Try RFC3339 first (most common for API payloads).
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try RFC3339Nano.
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	// Try date-only format.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	// Try datetime without timezone.
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	// Try datetime-local without timezone offset.
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}

// generateTimeFilter builds a SQL time filter condition for the given column.
func generateTimeFilter(from, to time.Time, columnName, timeUnit string) string {
	fromValue := convertToEpoch(from, timeUnit)
	toValue := convertToEpoch(to, timeUnit)
	return fmt.Sprintf("%s >= %d AND %s <= %d", columnName, fromValue, columnName, toValue)
}

// convertToEpoch converts a time.Time to an epoch value in the specified unit.
func convertToEpoch(t time.Time, unit string) int64 {
	switch unit {
	case "ns":
		return t.UnixNano()
	case "us":
		return t.UnixMicro()
	case "ms":
		return t.UnixMilli()
	case "s":
		return t.Unix()
	default:
		return t.UnixMilli()
	}
}

// calculateInterval computes the aggregation interval in seconds for a given
// time span and maximum number of data points.
func calculateInterval(from, to time.Time, maxDataPoints int) int {
	durationMs := to.Sub(from).Milliseconds()
	intervalMs := float64(durationMs) / float64(maxDataPoints)
	if intervalMs < 1000 {
		intervalMs = 1000
	}
	return int(math.Floor(intervalMs / 1000))
}
