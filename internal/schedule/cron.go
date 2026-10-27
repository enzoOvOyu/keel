// Package schedule parses bounded UTC scheduling expressions. It does not own
// device work or an execution queue; callers admit work into the shared kernel.
package schedule

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type field struct {
	values   map[int]bool
	wildcard bool
}
type Cron struct{ minute, hour, day, month, weekday field }

func cronNumber(text string) (int, error) {
	if text == "" {
		return 0, errors.New("empty cron number")
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid cron number")
		}
	}
	return strconv.Atoi(text)
}
func parseField(text string, min, max int) (field, error) {
	out := field{values: map[int]bool{}, wildcard: strings.HasPrefix(text, "*")}
	if text == "" || len(text) > 120 {
		return out, errors.New("invalid cron field")
	}
	for _, part := range strings.Split(text, ",") {
		base, stepText, stepped := strings.Cut(part, "/")
		step := 1
		if stepped {
			var e error
			step, e = cronNumber(stepText)
			if e != nil || step < 1 || step > max-min+1 {
				return out, errors.New("invalid cron step")
			}
		}
		start, end := min, max
		if base != "*" {
			left, right, ranged := strings.Cut(base, "-")
			var e error
			start, e = cronNumber(left)
			if e != nil {
				return out, errors.New("invalid cron number")
			}
			end = start
			if ranged {
				end, e = cronNumber(right)
				if e != nil {
					return out, errors.New("invalid cron range")
				}
			} else if stepped {
				end = max
			}
		}
		if start < min || end > max || start > end {
			return out, errors.New("cron field out of range")
		}
		for i := start; i <= end; i += step {
			out.values[i] = true
		}
	}
	if len(out.values) == 0 {
		return out, errors.New("empty cron field")
	}
	return out, nil
}
func Parse(expression string) (*Cron, error) {
	if len(expression) > 160 {
		return nil, errors.New("cron expression too long")
	}
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return nil, errors.New("five UTC cron fields required")
	}
	bounds := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	fields := make([]field, 5)
	for i, p := range parts {
		v, e := parseField(p, bounds[i][0], bounds[i][1])
		if e != nil {
			return nil, e
		}
		fields[i] = v
	}
	if fields[4].values[7] {
		fields[4].values[0] = true
		delete(fields[4].values, 7)
	}
	return &Cron{minute: fields[0], hour: fields[1], day: fields[2], month: fields[3], weekday: fields[4]}, nil
}
func (c *Cron) matchesDay(t time.Time) bool {
	dom, dow := c.day.values[t.Day()], c.weekday.values[int(t.Weekday())]
	if c.day.wildcard {
		return dow
	}
	if c.weekday.wildcard {
		return dom
	}
	return dom || dow
}

// Next returns the next minute strictly after the supplied instant, in UTC.
// Missing intervals coalesce at the caller; there is no implicit catch-up storm.
func (c *Cron) Next(after time.Time) (time.Time, error) {
	if c == nil {
		return time.Time{}, errors.New("cron unavailable")
	}
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	lastYear := candidate.Year() + 5
	for candidate.Year() <= lastYear {
		year, month, day := candidate.Date()
		if !c.month.values[int(month)] {
			candidate = time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !c.matchesDay(candidate) {
			candidate = time.Date(year, month, day+1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !c.hour.values[candidate.Hour()] {
			candidate = candidate.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		if !c.minute.values[candidate.Minute()] {
			candidate = candidate.Add(time.Minute)
			continue
		}
		return candidate, nil
	}
	return time.Time{}, errors.New("cron has no occurrence within five years")
}
