package scheduler

import (
	"strconv"
	"strings"
	"time"
)

// parseField parses a single cron field (e.g. "*/5", "1-15", "1,5,10", "*")
// and returns a set of matching integer values within [min, max].
func parseField(field string, min, max int) map[int]bool {
	values := make(map[int]bool)
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
					values[v] = true
				}
			}

		default:
			num, err := strconv.Atoi(rangePart)
			if err == nil {
				values[num] = true
			}
		}
	}

	return values
}

// ComputeNextRun parses a standard 5-field cron expression (minute hour dom month dow)
// and returns the next matching UTC time after `from`, iterating minute by minute
// up to 1 year ahead. Returns nil if no match is found.
func ComputeNextRun(cron string, from time.Time) *time.Time {
	fields := strings.Fields(strings.TrimSpace(cron))
	if len(fields) != 5 {
		return nil
	}

	minutes := parseField(fields[0], 0, 59)
	hours := parseField(fields[1], 0, 23)
	dom := parseField(fields[2], 1, 31)
	months := parseField(fields[3], 1, 12)
	dow := parseField(fields[4], 0, 6)

	if len(minutes) == 0 || len(hours) == 0 || len(dom) == 0 || len(months) == 0 || len(dow) == 0 {
		return nil
	}

	// Start from the next minute after `from`, truncated to the minute boundary.
	next := from.UTC().Truncate(time.Minute).Add(time.Minute)

	// 525600 minutes = 1 year
	for i := 0; i < 525600; i++ {
		m := next.Minute()
		h := next.Hour()
		d := next.Day()
		mo := int(next.Month())
		dw := int(next.Weekday()) // Sunday=0

		if minutes[m] && hours[h] && dom[d] && months[mo] && dow[dw] {
			result := next
			return &result
		}

		next = next.Add(time.Minute)
	}

	return nil
}

// ValidateCron returns true if the cron expression is syntactically valid
// and can produce at least one future run time.
func ValidateCron(cron string) bool {
	return ComputeNextRun(cron, time.Now()) != nil
}
