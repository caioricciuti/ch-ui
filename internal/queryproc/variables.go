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
	Query     string
	TimeRange *TimeRange
	TimeField string
	// TimeFieldUnit is the epoch unit ("ns", "us", "ms", "s") of the time
	// column, or "datetime" for Date/DateTime columns. It applies to
	// $__timeFrom and $__timeTo (default "ms") and to $__timeFilter when the
	// macro does not name a unit itself. Empty means not known.
	TimeFieldUnit string
	MaxDataPoints int // defaults to 1000
	Table         string
	// Vars holds the current values of the dashboard variables, by name.
	// They are substituted last, after the built-in macros, and always as
	// quoted literals (see SubstituteVars).
	Vars map[string][]string
	// VarTypes holds the type of each variable that needs one ("interval").
	VarTypes map[string]string
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
	// $__timeFilter(col) or $__timeFilter(col, unit), used for detection;
	// replaceTimeFilterCalls does the parsing.
	patTimeFilterWithCol = regexp.MustCompile(`(?i)\$__timeFilter\s*\(`)
	patTimeFilterSimple  = regexp.MustCompile(`(?i)\$__timeFilter\b`)
	patTimestampFilter   = regexp.MustCompile(`(?i)\$__timestamp\(([^)]+)\)`)
	patTimeField         = regexp.MustCompile(`(?i)\$__timeField\b`)
	patTimeFrom          = regexp.MustCompile(`(?i)\$__timeFrom\b`)
	patTimeTo            = regexp.MustCompile(`(?i)\$__timeTo\b`)
	patIntervalMs        = regexp.MustCompile(`(?i)\$__interval_ms\b`)
	patInterval          = regexp.MustCompile(`(?i)\$__interval\b`)
	patTable             = regexp.MustCompile(`(?i)\$__table\b`)

	// Pattern for parsing relative time strings like "5m", "1h", "7d", "1M", "1y",
	// and Grafana-style forms like "now-5m" or "now-15min". The unit keeps its
	// case: "M" is months, "m" is minutes.
	patRelativeTime = regexp.MustCompile(`^(?i:now-)?\s*(\d+)\s*([a-zA-Z]+)$`)
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
		patIntervalMs.MatchString(query) ||
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

	// Apply defaults. $__timeFrom and $__timeTo stay in milliseconds unless
	// the caller names a unit; $__timeFilter resolves its own unit per column.
	explicitUnit := strings.ToLower(strings.TrimSpace(opts.TimeFieldUnit))
	timeFieldUnit := explicitUnit
	if timeFieldUnit == "" || timeFieldUnit == unitDateTime {
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
			// 3a. Handle $__timeFilter(column) and $__timeFilter(column, unit).
			// Each occurrence is replaced on its own, so two different columns
			// in one query each get their own predicate.
			var filterErr error
			processedQuery = replaceTimeFilterCalls(processedQuery, func(columnName, macroUnit string) string {
				unit, err := resolveFilterUnit(columnName, macroUnit, explicitUnit)
				if err != nil {
					filterErr = err
					return ""
				}
				timeFilter := generateTimeFilter(from, to, columnName, unit)
				interpolatedVars["timeFilter"] = timeFilter
				interpolatedVars["timeFilterColumn"] = columnName
				return timeFilter
			})
			if filterErr != nil {
				errors = append(errors, filterErr.Error())
			}

			// 3b. Handle $__timeFilter without column (uses timeField or "timestamp").
			// After the column variant is already replaced, only bare $__timeFilter remain.
			if patTimeFilterSimple.MatchString(processedQuery) {
				effectiveTimeField := opts.TimeField
				if effectiveTimeField == "" {
					effectiveTimeField = "timestamp"
				}
				unit, err := resolveFilterUnit(effectiveTimeField, "", explicitUnit)
				if err != nil {
					errors = append(errors, err.Error())
				} else {
					timeFilter := generateTimeFilter(from, to, effectiveTimeField, unit)
					processedQuery = patTimeFilterSimple.ReplaceAllString(processedQuery, timeFilter)
					interpolatedVars["timeFilter"] = timeFilter
				}
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

			// 3e. Handle $__interval_ms, then $__interval. The word boundary on
			// both keeps one from eating the other.
			if patIntervalMs.MatchString(processedQuery) {
				intervalMs := calculateInterval(from, to, maxDataPoints) * 1000
				processedQuery = patIntervalMs.ReplaceAllString(processedQuery, strconv.Itoa(intervalMs))
				interpolatedVars["interval_ms"] = intervalMs
			}
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
		patIntervalMs.MatchString(processedQuery) ||
		patInterval.MatchString(processedQuery) ||
		patTimestampFilter.MatchString(processedQuery) {
		errors = append(errors, "Time-range variables found but no time range was provided")
	}

	// 4. Dashboard variables, last, so a macro can never expand inside a
	// substituted value.
	if len(errors) == 0 && len(opts.Vars) > 0 {
		substituted, err := SubstituteVars(processedQuery, opts.Vars, opts.VarTypes)
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			processedQuery = substituted
		}
	}

	return ProcessedResult{
		Query:            processedQuery,
		HasTimeVariables: HasTimeVariables(opts.Query),
		InterpolatedVars: interpolatedVars,
		Errors:           errors,
	}
}

var patTimeFilterCall = regexp.MustCompile(`(?i)\$__timeFilter\s*\(`)

// replaceTimeFilterCalls replaces every $__timeFilter(expr) and
// $__timeFilter(expr, unit) with what fn returns. The argument may hold
// nested parentheses, such as $__timeFilter(toDateTime(ts)); a trailing
// ", word" is the unit. A call without a closing parenthesis is left alone.
func replaceTimeFilterCalls(query string, fn func(column, unit string) string) string {
	var b strings.Builder
	rest := query
	for {
		loc := patTimeFilterCall.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(rest)
			return b.String()
		}
		depth, end, lastComma := 1, -1, -1
		for i := loc[1]; i < len(rest) && end < 0; i++ {
			switch rest[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			case ',':
				if depth == 1 {
					lastComma = i
				}
			}
		}
		if end < 0 {
			b.WriteString(rest)
			return b.String()
		}
		arg := rest[loc[1]:end]
		column, unit := arg, ""
		if lastComma >= 0 {
			if u := strings.TrimSpace(rest[lastComma+1 : end]); patUnitWord.MatchString(u) {
				column, unit = rest[loc[1]:lastComma], u
			}
		}
		b.WriteString(rest[:loc[0]])
		if out := fn(strings.TrimSpace(column), unit); out != "" {
			b.WriteString(out)
		} else {
			b.WriteString(rest[loc[0] : end+1])
		}
		rest = rest[end+1:]
	}
}

var patUnitWord = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// InferTimeUnit infers an epoch unit from a column name suffix such as
// "created_ms" or "ts_s". Returns "ns", "us", "ms", "s", or "" when the name
// carries no unit.
func InferTimeUnit(columnName string) string {
	lower := strings.ToLower(strings.TrimSpace(columnName))
	// Ignore a table or alias qualifier.
	if i := strings.LastIndex(lower, "."); i >= 0 {
		lower = lower[i+1:]
	}
	lower = strings.Trim(lower, "`\"")

	switch {
	case strings.HasSuffix(lower, "_ns") || strings.Contains(lower, "_ns_"):
		return "ns"
	case strings.HasSuffix(lower, "_us") || strings.Contains(lower, "_us_"):
		return "us"
	case strings.HasSuffix(lower, "_ms") || strings.Contains(lower, "_ms_"):
		return "ms"
	case strings.HasSuffix(lower, "_s") || strings.HasSuffix(lower, "_sec") || strings.HasSuffix(lower, "_secs"):
		return "s"
	}
	return ""
}

const (
	unitDateTime = "datetime"
	// unitAuto means the column type is not known.
	unitAuto = "auto"
)

// resolveFilterUnit picks the unit for one $__timeFilter column: the unit
// written in the macro wins, then the unit the caller passed, then a unit
// suffix on the column name, else auto.
func resolveFilterUnit(columnName, macroUnit, explicitUnit string) (string, error) {
	if u := strings.ToLower(strings.TrimSpace(macroUnit)); u != "" {
		return normalizeFilterUnit(u)
	}
	if explicitUnit != "" {
		return normalizeFilterUnit(explicitUnit)
	}
	if u := InferTimeUnit(columnName); u != "" {
		return u, nil
	}
	return unitAuto, nil
}

func normalizeFilterUnit(u string) (string, error) {
	switch u {
	case "s", "sec", "secs", "second", "seconds":
		return "s", nil
	case "ms", "millis", "milliseconds":
		return "ms", nil
	case "us", "micros", "microseconds":
		return "us", nil
	case "ns", "nanos", "nanoseconds":
		return "ns", nil
	case "datetime", "datetime64", "date", "dt":
		return unitDateTime, nil
	}
	return "", fmt.Errorf("$__timeFilter: unknown unit %q (use s, ms, us, ns or datetime)", u)
}

// BoundsAt resolves a TimeRange into concrete instants, with relative
// offsets taken from now.
func BoundsAt(tr *TimeRange, now time.Time) (from, to time.Time, ok bool) {
	if tr == nil {
		return time.Time{}, time.Time{}, false
	}
	return getTimeBoundsAt(tr, now)
}

// getTimeBounds parses a TimeRange into concrete from/to time.Time values.
func getTimeBounds(tr *TimeRange) (from, to time.Time, ok bool) {
	return getTimeBoundsAt(tr, time.Now())
}

func getTimeBoundsAt(tr *TimeRange, now time.Time) (from, to time.Time, ok bool) {

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
	trimmed := strings.TrimSpace(timeStr)
	if trimmed == "" {
		return base.Add(-5 * time.Minute)
	}
	if strings.EqualFold(trimmed, "now") {
		return base
	}

	matches := patRelativeTime.FindStringSubmatch(trimmed)
	if matches == nil {
		// Default to 5 minutes ago on invalid input.
		return base.Add(-5 * time.Minute)
	}

	value, _ := strconv.Atoi(matches[1])
	unit := matches[2]
	// "M" alone is months and "m" alone is minutes, as in Grafana. Every
	// longer unit is matched without regard to case.
	if unit != "M" {
		switch strings.ToLower(unit) {
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
	}

	switch unit {
	case "s":
		return base.Add(-time.Duration(value) * time.Second)
	case "m":
		return base.Add(-time.Duration(value) * time.Minute)
	case "h":
		return base.Add(-time.Duration(value) * time.Hour)
	case "d":
		return base.AddDate(0, 0, -value)
	case "w":
		return base.AddDate(0, 0, -7*value)
	case "M":
		return base.AddDate(0, -value, 0)
	case "y":
		return base.AddDate(-value, 0, 0)
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

// generateTimeFilter builds a SQL time range predicate for the given column,
// wrapped in parentheses so it composes with OR.
//
//   - datetime: compares with DateTime64 values, right for Date, Date32,
//     DateTime and DateTime64 columns in any time zone.
//   - s, ms, us, ns: compares with epoch integers in that unit.
//   - auto (type unknown): matches the range in seconds OR in milliseconds.
//     ClickHouse compares a DateTime or DateTime64 with an integer as epoch
//     seconds, so this is right for DateTime, DateTime64 and integer epoch
//     columns in seconds or milliseconds; the two ranges are far apart
//     (about 1.7e9 against 1.7e12), so neither can match the other's values
//     for any realistic date, and both use the primary key. A Date column is
//     refused by ClickHouse with "Illegal types": use the datetime unit.
func generateTimeFilter(from, to time.Time, columnName, timeUnit string) string {
	switch timeUnit {
	case unitDateTime:
		return fmt.Sprintf("(%s >= fromUnixTimestamp64Milli(%d) AND %s <= fromUnixTimestamp64Milli(%d))",
			columnName, from.UnixMilli(), columnName, to.UnixMilli())
	case unitAuto:
		return fmt.Sprintf("((%s >= %d AND %s <= %d) OR (%s >= %d AND %s <= %d))",
			columnName, from.Unix(), columnName, to.Unix(),
			columnName, from.UnixMilli(), columnName, to.UnixMilli())
	}
	fromValue := convertToEpoch(from, timeUnit)
	toValue := convertToEpoch(to, timeUnit)
	return fmt.Sprintf("(%s >= %d AND %s <= %d)", columnName, fromValue, columnName, toValue)
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
