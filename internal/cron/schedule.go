// Package cron provides a small standard-5-field cron scheduler that fires
// persisted jobs (shell commands) on a schedule. Jobs are stored in the shared
// session database so they survive a daemon restart.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed 5-field cron expression: minute, hour, day-of-month,
// month, day-of-week (0 = Sunday). Each field is a set of permitted values.
type Schedule struct {
	minute map[int]bool
	hour   map[int]bool
	dom    map[int]bool
	month  map[int]bool
	dow    map[int]bool
	// domRestricted/dowRestricted track whether the field is narrower than "*",
	// so Matches can apply cron's "OR when both are restricted" rule.
	domRestricted bool
	dowRestricted bool
}

type fieldRange struct{ min, max int }

var (
	minuteRange = fieldRange{0, 59}
	hourRange   = fieldRange{0, 23}
	domRange    = fieldRange{1, 31}
	monthRange  = fieldRange{1, 12}
	dowRange    = fieldRange{0, 6}
)

// Parse parses a 5-field cron expression. It also accepts a few common macros
// (@hourly, @daily, @weekly, @monthly).
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if m, ok := macros[expr]; ok {
		expr = m
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields, got %d in %q", len(fields), expr)
	}
	minute, _, err := parseField(fields[0], minuteRange)
	if err != nil {
		return nil, err
	}
	hour, _, err := parseField(fields[1], hourRange)
	if err != nil {
		return nil, err
	}
	dom, domR, err := parseField(fields[2], domRange)
	if err != nil {
		return nil, err
	}
	month, _, err := parseField(fields[3], monthRange)
	if err != nil {
		return nil, err
	}
	dow, dowR, err := parseField(fields[4], dowRange)
	if err != nil {
		return nil, err
	}
	return &Schedule{
		minute: minute, hour: hour, dom: dom, month: month, dow: dow,
		domRestricted: domR, dowRestricted: dowR,
	}, nil
}

var macros = map[string]string{
	"@hourly":  "0 * * * *",
	"@daily":   "0 0 * * *",
	"@weekly":  "0 0 * * 0",
	"@monthly": "0 0 1 * *",
}

// parseField parses one cron field into the set of permitted values. The second
// return reports whether the field is restricted (not "*").
func parseField(f string, r fieldRange) (map[int]bool, bool, error) {
	set := map[int]bool{}
	restricted := true
	for _, part := range strings.Split(f, ",") {
		step := 1
		rangePart := part
		if slash := strings.Index(part, "/"); slash >= 0 {
			s, err := strconv.Atoi(part[slash+1:])
			if err != nil || s <= 0 {
				return nil, false, fmt.Errorf("cron: bad step in %q", part)
			}
			step = s
			rangePart = part[:slash]
		}

		var lo, hi int
		switch {
		case rangePart == "*":
			lo, hi = r.min, r.max
			if step == 1 {
				restricted = false
			}
		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			a, err1 := strconv.Atoi(bounds[0])
			b, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil {
				return nil, false, fmt.Errorf("cron: bad range %q", rangePart)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(rangePart)
			if err != nil {
				return nil, false, fmt.Errorf("cron: bad value %q", rangePart)
			}
			lo, hi = v, v
		}
		if lo < r.min || hi > r.max || lo > hi {
			return nil, false, fmt.Errorf("cron: value out of range [%d,%d] in %q", r.min, r.max, part)
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return set, restricted, nil
}

// Matches reports whether t (resolved to its minute) satisfies the schedule.
func (s *Schedule) Matches(t time.Time) bool {
	if !s.minute[t.Minute()] || !s.hour[t.Hour()] || !s.month[int(t.Month())] {
		return false
	}
	domOK := s.dom[t.Day()]
	dowOK := s.dow[int(t.Weekday())]
	// Standard cron: when both day fields are restricted, either may match.
	if s.domRestricted && s.dowRestricted {
		return domOK || dowOK
	}
	return domOK && dowOK
}
