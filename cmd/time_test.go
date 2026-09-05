package cmd

import (
	"testing"
	"time"
)

func TestFirstArg(t *testing.T) {
	if got := firstArg([]string{"first", "second"}); got != "first" {
		t.Errorf("expected 'first', got %q", got)
	}
	if got := firstArg(nil); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
	if got := firstArg([]string{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

func TestParseRelativeDuration(t *testing.T) {
	cases := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"15m", 15 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"invalid", 0, true},
		{"abc d", 0, true},
	}

	for _, tc := range cases {
		dur, err := parseRelativeDuration(tc.input)
		if tc.wantErr && err == nil {
			t.Errorf("parseRelativeDuration(%q) expected error, got nil", tc.input)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("parseRelativeDuration(%q) unexpected error: %v", tc.input, err)
		}
		if !tc.wantErr && dur != tc.want {
			t.Errorf("parseRelativeDuration(%q) = %v, want %v", tc.input, dur, tc.want)
		}
	}
}

func TestParseDurationWindow(t *testing.T) {
	// Default empty input
	start, end, err := parseDurationWindow("")
	if err != nil {
		t.Fatalf("parseDurationWindow(\"\") error: %v", err)
	}
	if end.Sub(start) <= 0 {
		t.Errorf("expected positive window duration, got %v", end.Sub(start))
	}

	// Valid 45m input
	start, end, err = parseDurationWindow("45m")
	if err != nil {
		t.Fatalf("parseDurationWindow(\"45m\") error: %v", err)
	}
	dur := end.Sub(start)
	if dur < 44*time.Minute || dur > 46*time.Minute {
		t.Errorf("expected ~45m window, got %v", dur)
	}

	// Non-positive input rejected
	if _, _, err := parseDurationWindow("0s"); err == nil {
		t.Errorf("expected error for 0s window, got nil")
	}
	if _, _, err := parseDurationWindow("-10m"); err == nil {
		t.Errorf("expected error for negative window, got nil")
	}
	if _, _, err := parseDurationWindow("not-a-duration"); err == nil {
		t.Errorf("expected error for malformed duration, got nil")
	}
}
