// Package duration reads and writes the durations of the CLI. A flag that
// takes a duration accepts a day unit, as in "30d", because policy durations
// such as a trust expiry are in days. The time package has no day unit.
package duration

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	day = 24 * time.Hour

	// maxDays keeps the result inside time.Duration, which holds about
	// 106,751 days.
	maxDays = 100000
)

var dayPattern = regexp.MustCompile(`^([0-9]+)d(.*)$`)

// Parse reads a duration as time.ParseDuration does, and also accepts a
// leading day unit: "30d", "1d12h". A day is 24 hours.
func Parse(s string) (time.Duration, error) {
	text := strings.TrimSpace(s)
	match := dayPattern.FindStringSubmatch(text)
	if match == nil {
		d, err := time.ParseDuration(text)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: use a number and a unit, as in 30d, 12h or 90m", s)
		}
		return d, nil
	}

	days, err := strconv.Atoi(match[1])
	if err != nil || days > maxDays {
		return 0, fmt.Errorf("invalid duration %q: use at most %d days", s, maxDays)
	}
	total := time.Duration(days) * day
	if match[2] == "" {
		return total, nil
	}
	rest, err := time.ParseDuration(match[2])
	if err != nil || rest < 0 {
		return 0, fmt.Errorf("invalid duration %q: use a number and a unit, as in 30d, 12h or 90m", s)
	}
	return total + rest, nil
}

// Format writes a duration that Parse reads back to the same value. It drops
// zero minutes and seconds, so 48 hours is "48h", not "48h0m0s". Tables use
// dry/tui/humanize instead.
func Format(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Value is a pflag value for a duration that accepts the day unit.
type Value struct {
	target *time.Duration
}

// NewValue sets *target to def and returns a flag value that writes to it.
func NewValue(target *time.Duration, def time.Duration) *Value {
	*target = def
	return &Value{target: target}
}

func (v *Value) String() string {
	if v == nil || v.target == nil {
		return ""
	}
	if *v.target == 0 {
		return ""
	}
	return Format(*v.target)
}

func (v *Value) Set(s string) error {
	d, err := Parse(s)
	if err != nil {
		return err
	}
	*v.target = d
	return nil
}

func (v *Value) Type() string { return "duration" }
