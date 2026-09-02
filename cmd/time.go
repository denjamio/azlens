package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// firstArg safely returns the first CLI argument or an empty string
func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// parseRelativeDuration parses Go duration strings as well as day suffixes like "7d"
func parseRelativeDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// parseDurationWindow converts a window string into start and end timestamps.
// If input is empty, it falls back to the system default window.
func parseDurationWindow(input string) (time.Time, time.Time, error) {
	now := time.Now()
	if input == "" {
		input = config.DefaultWindow
	}

	dur, err := parseRelativeDuration(input)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return now.Add(-dur), now, nil
}

// parseDeployTime parses deployment time arguments formatted as relative offsets, RFC3339, or local times
func parseDeployTime(atStr string, now time.Time) (time.Time, error) {
	atStr = strings.TrimSpace(atStr)
	if atStr == "" {
		return time.Time{}, errors.New("empty deploy time")
	}

	// 1. Relative offset: e.g. "-20m", "-1h", or "20m" (implies 20m ago)
	if strings.HasPrefix(atStr, "-") {
		d, err := parseRelativeDuration(strings.TrimPrefix(atStr, "-"))
		if err == nil {
			return now.Add(-d), nil
		}
	} else if d, err := parseRelativeDuration(atStr); err == nil {
		return now.Add(-d), nil
	}

	// 2. RFC3339 / ISO
	if t, err := time.Parse(time.RFC3339, atStr); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05", atStr); err == nil {
		return t, nil
	}

	// 3. Date + time (local)
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, atStr, time.Local); err == nil {
			return t, nil
		}
	}

	// 4. Time only (assumes today in local time)
	timeOnlyFormats := []string{"15:04:05", "15:04"}
	for _, f := range timeOnlyFormats {
		if t, err := time.ParseInLocation(f, atStr, time.Local); err == nil {
			combined := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
			if combined.After(now) {
				combined = combined.Add(-24 * time.Hour)
			}
			return combined, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse deploy time '%s'. Expected format: '14:30', '2026-09-03T14:30:00Z', or '-20m'", atStr)
}
