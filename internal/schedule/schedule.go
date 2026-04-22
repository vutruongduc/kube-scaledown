package schedule

import (
	"fmt"
	"strings"
	"time"
)

var dayMap = map[string]time.Weekday{
	"Mon": time.Monday,
	"Tue": time.Tuesday,
	"Wed": time.Wednesday,
	"Thu": time.Thursday,
	"Fri": time.Friday,
	"Sat": time.Saturday,
	"Sun": time.Sunday,
}

// Rule represents a single schedule rule like "Mon-Fri 08:00-20:00 Asia/Ho_Chi_Minh".
type Rule struct {
	DayStart  time.Weekday
	DayEnd    time.Weekday
	TimeStart int // minutes from midnight
	TimeEnd   int // minutes from midnight
	Location  *time.Location
}

// Parse parses a comma-separated schedule string into a slice of Rules.
func Parse(schedule string) ([]Rule, error) {
	parts := strings.Split(schedule, ",")
	var rules []Rule
	for _, part := range parts {
		rule, err := ParseRule(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parsing rule %q: %w", strings.TrimSpace(part), err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ParseRule parses a single schedule rule.
// Format: "<DayStart>-<DayEnd> <HH:MM>-<HH:MM> <Timezone>" or "<Day> <HH:MM>-<HH:MM> <Timezone>"
func ParseRule(s string) (Rule, error) {
	fields := strings.Fields(s)
	if len(fields) != 3 {
		return Rule{}, fmt.Errorf("expected 3 fields (days time timezone), got %d", len(fields))
	}

	dayStart, dayEnd, err := parseDayRange(fields[0])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid day range: %w", err)
	}

	timeStart, timeEnd, err := parseTimeRange(fields[1])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid time range: %w", err)
	}

	loc, err := time.LoadLocation(fields[2])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid timezone %q: %w", fields[2], err)
	}

	return Rule{
		DayStart:  dayStart,
		DayEnd:    dayEnd,
		TimeStart: timeStart,
		TimeEnd:   timeEnd,
		Location:  loc,
	}, nil
}

// IsActive returns true if the given time falls within any of the rules.
func IsActive(rules []Rule, t time.Time) bool {
	for _, rule := range rules {
		if rule.Contains(t) {
			return true
		}
	}
	return false
}

// Contains returns true if the given time falls within this rule.
func (r Rule) Contains(t time.Time) bool {
	t = t.In(r.Location)
	weekday := t.Weekday()
	minuteOfDay := t.Hour()*60 + t.Minute()

	if !dayInRange(weekday, r.DayStart, r.DayEnd) {
		return false
	}

	return minuteOfDay >= r.TimeStart && minuteOfDay < r.TimeEnd
}

func parseDayRange(s string) (time.Weekday, time.Weekday, error) {
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		start, ok := dayMap[parts[0]]
		if !ok {
			return 0, 0, fmt.Errorf("unknown day %q", parts[0])
		}
		end, ok := dayMap[parts[1]]
		if !ok {
			return 0, 0, fmt.Errorf("unknown day %q", parts[1])
		}
		return start, end, nil
	}
	day, ok := dayMap[s]
	if !ok {
		return 0, 0, fmt.Errorf("unknown day %q", s)
	}
	return day, day, nil
}

func parseTimeRange(s string) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM-HH:MM, got %q", s)
	}

	start, err := parseTime(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := parseTime(parts[1])
	if err != nil {
		return 0, 0, err
	}

	return start, end, nil
}

func parseTime(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}

	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return 0, fmt.Errorf("parsing time %q: %w", s, err)
	}

	if h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time out of range: %q", s)
	}

	return h*60 + m, nil
}

func dayInRange(day, start, end time.Weekday) bool {
	if start <= end {
		return day >= start && day <= end
	}
	// Wraps around (e.g., Fri-Mon)
	return day >= start || day <= end
}
