package cmd

import "fmt"

// Process exit codes used by azlens:
//
//	0 - success (no regressions, or none above the configured thresholds)
//	1 - runtime or configuration error
//	2 - deploy-check quality gate failed (critical regressions detected)
const (
	ExitCodeOK      = 0
	ExitCodeFailure = 1
	ExitCodeQuality = 2
)

// ExitCodeError wraps an error with a specific process exit code, so quality
// gates like 'deploy-check' can fail a CI pipeline with a meaningful status.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string { return e.Err.Error() }

func (e *ExitCodeError) Unwrap() error { return e.Err }

func newQualityGateError(format string, args ...any) *ExitCodeError {
	return &ExitCodeError{Code: ExitCodeQuality, Err: fmt.Errorf(format, args...)}
}
