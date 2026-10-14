// Package cronexpr parses standard 5-field cron expressions. It is core
// (Apache-2.0): free model and pipeline schedules use it, and Pro scheduled
// query jobs build on it.
package cronexpr

import (
	"strconv"
	"strings"
	"time"
)

// parseField parses a single cron field (e.g. "*/5", "1-15", "1,5,10", "*")
// and returns a set of matching integer values within [min, max]. Values
// outside [min, max] are dropped, so a field holding only those is empty.
func parseField(field string, min, max int) map[int]bool {
	values := make(map[int]bool)
	add := func(v int) {
		if v >= min && v <= max {
			values[v] = true
		}
	}
	parts := strings.Split(field, ",")

	for _, part := range parts {
		rangePart := part
		step := 1

		if idx := strings.Index(part, "/"); idx >= 0 {
			rangePart = part[:idx]
			s, err := strconv.Atoi(part[idx+1:])
			if err != nil || s <= 0 {
				continue
			}
			step = s
		}

		switch {
		case rangePart == "*":
			// Every value from min to max, filtered by step.
			for v := min; v <= max; v++ {
				if (v-min)%step == 0 {
					values[v] = true
				}
			}

		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			s, err1 := strconv.Atoi(bounds[0])
			e, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil {
				continue
			}
			for v := s; v <= e; v++ {
				if (v-s)%step == 0 {
					add(v)
				}
			}

		default:
			num, err := strconv.Atoi(rangePart)
			if err == nil {
				add(num)
			}
		}
	}

	return values
}

// ComputeNextRun parses a standard 5-field cron expression (minute hour dom month dow)
// and returns the next matching UTC time after `from`, iterating minute by minute
// up to 1 year ahead. Returns nil if no match is found.
func ComputeNextRun(cron string, from time.Time) *time.Time {
	return ComputeNextRunIn(cron, from, time.UTC)
}

// ComputeNextRunIn is ComputeNextRun with the fields read as wall-clock time
// in loc (so "0 9 * * *" in Europe/Lisbon is 09:00 Lisbon time). The result is
// in UTC.
func ComputeNextRunIn(cron string, from time.Time, loc *time.Location) *time.Time {
	if loc == nil {
		loc = time.UTC
	}
	fields := strings.Fields(strings.TrimSpace(cron))
	if len(fields) != 5 {
		return nil
	}

	minutes := parseField(fields[0], 0, 59)
	hours := parseField(fields[1], 0, 23)
	dom := parseField(fields[2], 1, 31)
	months := parseField(fields[3], 1, 12)
	// Day of week accepts 0-7 like standard cron, with 7 = Sunday. Over 0-7 a
	// "*" step only adds 7 when it also adds 0, so "*/2" stays 0,2,4,6.
	dow := parseField(fields[4], 0, 7)
	if dow[7] {
		dow[0] = true
		delete(dow, 7)
	}

	if len(minutes) == 0 || len(hours) == 0 || len(dom) == 0 || len(months) == 0 || len(dow) == 0 {
		return nil
	}

	// Vixie cron: when both day fields are restricted (neither starts with
	// "*"), a day matches if either field does. Otherwise both must match.
	dayOr := !strings.HasPrefix(fields[2], "*") && !strings.HasPrefix(fields[4], "*")

	// Start from the next minute after `from`, truncated to the minute boundary.
	next := from.In(loc).Truncate(time.Minute).Add(time.Minute)

	// 525600 minutes = 1 year
	for i := 0; i < 525600; i++ {
		m := next.Minute()
		h := next.Hour()
		d := next.Day()
		mo := int(next.Month())
		dw := int(next.Weekday()) // Sunday=0

		dayOK := dom[d] && dow[dw]
		if dayOr {
			dayOK = dom[d] || dow[dw]
		}

		if minutes[m] && hours[h] && months[mo] && dayOK {
			result := next.UTC()
			return &result
		}

		next = next.Add(time.Minute)
	}

	return nil
}

// ComputeNextRunInZone is ComputeNextRunIn with loc named by an IANA
// timezone. An empty or unknown name reads the cron in UTC.
func ComputeNextRunInZone(cron, tz string, from time.Time) *time.Time {
	loc := time.UTC
	if tz = strings.TrimSpace(tz); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return ComputeNextRunIn(cron, from, loc)
}

// ValidateCron returns true if the cron expression is syntactically valid
// and can produce at least one future run time.
func ValidateCron(cron string) bool {
	return ComputeNextRun(cron, time.Now()) != nil
}
