package desktop_test

// The assertion set and the retained run the window's explanations and
// linked check groups are decided against, and `readmit explain`'s reading of
// one set against that run. The run is the one the installed native window
// retained in September (testdata/acceptance/native-109), copied and never
// rewritten.

import (
	"path/filepath"
	"testing"
)

// reviewedSet is an assertion set about the retained run, laid out as a
// person wrote it rather than as the structured draft generates one, so an
// export that rewrote it would show.
const reviewedSet = `{"schema": "readmit-assertion-set/v1",
 "name": "Retained reschedule accepted",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "reschedule-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000002", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "booking-control-echoed", "operator": "values_equal",
   "subject": {"pair": {"left": {"scope": "input", "message": "s0001-e000001", "selector": "MSH-10"},
                        "right": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-2"}}},
   "when": null, "expected": {"holds": true}}
 ]}
`

// explainedWorkspace is a workspace holding a copy of the run the native
// window retained, which `readmit explain` re-decides a set against.
func explainedWorkspace(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	run := filepath.Join(root, "retained.run")
	copyEntry(t, filepath.Join(nativeAcceptance, "post-fix", "run"), run)
	return root, run
}

// explained is `readmit explain`'s reading of one set against the retained run.
func explained(t *testing.T, run, set string) (string, string, error) {
	t.Helper()
	return commandLine(t, "explain", run, "--assertions", set)
}
