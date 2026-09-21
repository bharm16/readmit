package cli

import (
	"errors"
	"testing"
)

// Every stop reason decides its process status and its stderr behavior in one
// place, so the status tables the guides document per command are pinned here
// once instead of asserted per command through a spawned process.
func TestEveryStopReasonDecidesItsStatusAndStderrBehavior(t *testing.T) {
	reason := errors.New("reason")
	for name, stop := range map[string]struct {
		err      error
		code     int
		reported bool
	}{
		"misuse":                        {usage("reason"), 2, false},
		"refusal":                       {refusal(reason), 2, false},
		"stated refusal":                {statedRefusal(reason), 2, true},
		"refusal stated by a rendering": {statedRefusalWhen(true, reason), 2, true},
		"refusal stated by none":        {statedRefusalWhen(false, reason), 2, false},
		"failure":                       {failure(reason), 1, true},
		"unstated failure":              {unstatedFailure(reason), 1, false},
		"carried verdict":               {verdict(3, reason), 3, true},
		"unstated carried verdict":      {unstatedVerdict(3, reason), 3, false},
	} {
		var status *ExitError
		if !errors.As(stop.err, &status) {
			t.Fatalf("%s did not stop through the status table: %v", name, stop.err)
		}
		if status.Code != stop.code || status.Reported != stop.reported {
			t.Fatalf("%s exits %d reported %t, want %d reported %t", name, status.Code, status.Reported, stop.code, stop.reported)
		}
		if ExitCode(stop.err) != stop.code {
			t.Fatalf("%s exits %d through ExitCode, want %d", name, ExitCode(stop.err), stop.code)
		}
	}
}

// An error that never crossed the status table still exits 1, and success
// exits 0.
func TestStatusesOutsideTheTable(t *testing.T) {
	if ExitCode(nil) != 0 {
		t.Fatal("success exits nonzero")
	}
	if ExitCode(errors.New("plain")) != 1 {
		t.Fatal("a plain error does not exit 1")
	}
}
