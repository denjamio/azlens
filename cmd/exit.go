package cmd

import (
	"errors"
	"fmt"
)

// Process exit codes used by azlens (Section 19):
//
//	0 - observed and no actionable problem
//	1 - command/config/query execution failed
//	2 - actionable production problem / regression
//	3 - insufficient visibility to determine health
const (
	ExitCodeOK           = 0
	ExitCodeFailure      = 1
	ExitCodeActionable   = 2
	ExitCodeInsufficient = 3
	ExitCodeUnknown      = 3
)

// ExitCodeError wraps an error with a specific process exit code, so quality
// gates and health status can fail a CI pipeline with a meaningful status.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string { return e.Err.Error() }

func (e *ExitCodeError) Unwrap() error { return e.Err }

func NewExitError(code int, format string, args ...any) *ExitCodeError {
	return &ExitCodeError{Code: code, Err: fmt.Errorf(format, args...)}
}

// GetExitCode returns the process exit code for an error.
func GetExitCode(err error) int {
	if err == nil {
		return ExitCodeOK
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code
	}
	return ExitCodeFailure
}

func newActionableProblemError(format string, args ...any) *ExitCodeError {
	return NewExitError(ExitCodeActionable, format, args...)
}

func newUnknownHealthError(format string, args ...any) *ExitCodeError {
	return NewExitError(ExitCodeUnknown, format, args...)
}
