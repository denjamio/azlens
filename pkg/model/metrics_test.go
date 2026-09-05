package model_test

import (
	"testing"

	"github.com/denjamio/azlens/pkg/model"
)

func TestErrorSummaryInstrumentationTarget(t *testing.T) {
	cases := []struct {
		message string
		target  string
		isNoise bool
	}{
		{
			message: "Exception occurred when instrumenting: fastapi",
			target:  "fastapi",
			isNoise: true,
		},
		{
			message: "exception occurred when instrumenting flask.",
			target:  "flask",
			isNoise: true,
		},
		{
			message: "Database connection failed",
			target:  "",
			isNoise: false,
		},
		{
			message: "",
			target:  "",
			isNoise: false,
		},
	}

	for _, tc := range cases {
		errSummary := model.ErrorSummary{
			Message: tc.message,
		}
		target, isNoise := errSummary.InstrumentationTarget()
		if isNoise != tc.isNoise {
			t.Errorf("expected isNoise=%v for message %q, got %v", tc.isNoise, tc.message, isNoise)
		}
		if target != tc.target {
			t.Errorf("expected target=%q for message %q, got %q", tc.target, tc.message, target)
		}
	}
}
