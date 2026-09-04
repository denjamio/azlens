package cmd

import (
	"testing"
	"time"
)

func TestParseDeployTime(t *testing.T) {
	refTime := time.Date(2026, 9, 3, 14, 30, 0, 0, time.Local)

	// 1. Relative offset "-20m"
	t1, err := parseDeployTime("-20m", refTime)
	if err != nil {
		t.Fatalf("failed parsing relative offset '-20m': %v", err)
	}
	if want := refTime.Add(-20 * time.Minute); !t1.Equal(want) {
		t.Errorf("expected %v, got %v", want, t1)
	}

	// 2. Relative offset "20m" (implies 20m ago)
	t2, err := parseDeployTime("20m", refTime)
	if err != nil {
		t.Fatalf("failed parsing relative offset '20m': %v", err)
	}
	if want := refTime.Add(-20 * time.Minute); !t2.Equal(want) {
		t.Errorf("expected %v, got %v", want, t2)
	}

	// 3. RFC3339 full timestamp
	rfc := "2026-09-03T12:00:00Z"
	t3, err := parseDeployTime(rfc, refTime)
	if err != nil {
		t.Fatalf("failed parsing RFC3339: %v", err)
	}
	if t3.UTC().Hour() != 12 {
		t.Errorf("expected hour 12, got %d", t3.UTC().Hour())
	}

	// 4. Time only "14:15"
	t4, err := parseDeployTime("14:15", refTime)
	if err != nil {
		t.Fatalf("failed parsing time-only '14:15': %v", err)
	}
	if t4.Hour() != 14 || t4.Minute() != 15 {
		t.Errorf("expected 14:15, got %02d:%02d", t4.Hour(), t4.Minute())
	}

	// 5. Future time today rolls back to yesterday
	t5, err := parseDeployTime("16:00", refTime)
	if err != nil {
		t.Fatalf("failed parsing future time '16:00': %v", err)
	}
	if t5.Day() != refTime.Day()-1 {
		t.Errorf("expected previous day for future time, got day %d (ref day: %d)", t5.Day(), refTime.Day())
	}

	// 6. Invalid input returns error
	if _, err := parseDeployTime("invalid-time-format", refTime); err == nil {
		t.Errorf("expected error for invalid time format")
	}
}
