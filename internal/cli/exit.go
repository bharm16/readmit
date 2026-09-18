package cli

import "errors"

// ExitError carries a command-specific process status without changing the
// private error message. Existing commands continue to use status 1 on error.
type ExitError struct {
	Code     int
	Err      error
	Reported bool
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var status *ExitError
	if errors.As(err, &status) && status.Code > 0 {
		return status.Code
	}
	return 1
}
