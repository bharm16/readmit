package cli

import (
	"errors"
	"fmt"
)

// A command stops for one of three reasons, and this file is the one place
// that turns the reason into the process status and the stderr behavior the
// guides document per command:
//
//	misuse    exit 2  the command was invoked wrongly
//	refusal   exit 2  this execution cannot tell what the interface did
//	failure   exit 1  the interface under test did the wrong thing
//
// Whether the command's own output already stated the reason decides whether
// Execute prints it on stderr: a stated refusal or failure is not repeated,
// an unstated one is. A status a retained document already decided — a run's,
// a suite's, a capture's — is carried at its own code rather than decided
// here. Nothing outside this file constructs an ExitError.

// usage refuses a command that was invoked wrongly: a missing or empty flag, a
// format that is not one of the declared choices, a deadline that does not
// parse. It is the one shape for that refusal, so a misuse carries one process
// status everywhere — the same one a command reaches through argument parsing —
// while an execution failure keeps status 1. The message stays the command's
// own; only the decision is shared.
func usage(format string, args ...any) error {
	return &ExitError{Code: 2, Err: fmt.Errorf(format, args...)}
}

// refusal exits 2 because this execution cannot tell what the interface did,
// and nothing has said so yet: Execute prints the reason on stderr.
func refusal(err error) error {
	return &ExitError{Code: 2, Err: err}
}

// statedRefusal exits 2 with a reason the command's own output already
// stated, so Execute does not print it again.
func statedRefusal(err error) error {
	return &ExitError{Code: 2, Err: err, Reported: true}
}

// statedRefusalWhen exits 2 with a reason only the rendering the operator
// selected stated: the machine document carries it where the terminal summary
// says it, so stderr repeats the reason only when it was not already said.
func statedRefusalWhen(stated bool, err error) error {
	return &ExitError{Code: 2, Err: err, Reported: stated}
}

// failure exits 1 because the interface under test did the wrong thing, and
// the command's output already said so.
func failure(err error) error {
	return &ExitError{Code: 1, Err: err, Reported: true}
}

// unstatedFailure exits 1 with a reason stderr must still carry even though
// the command's output detailed what it found.
func unstatedFailure(err error) error {
	return &ExitError{Code: 1, Err: err}
}

// verdict carries the status a retained document already decided for its own
// reasons; the command's output already stated it. The code is the document's
// vocabulary, not this file's.
func verdict(code int, err error) error {
	return &ExitError{Code: code, Err: err, Reported: true}
}

// unstatedVerdict carries a retained document's status whose explanation has
// not been stated yet, so Execute prints it on stderr.
func unstatedVerdict(code int, err error) error {
	return &ExitError{Code: code, Err: err}
}

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
