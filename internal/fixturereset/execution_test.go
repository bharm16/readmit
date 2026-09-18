package fixturereset

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/testrunner"
)

// TestNoImportedDocumentCanReachAnExecutionFacility is the ticket's first hard
// rule proved at the package boundary rather than one input at a time. A reset
// action is a typed Go operator this release reviewed; if the package that runs
// them cannot reach a process, an interpreter or a loadable object at all, then
// no document, however it is spelled, can make it start one.
//
// ADR-0003 rejected an embedded expression language and shell hooks in specs
// exactly so that a regression packet a customer keeps and reruns in CI never
// acquires general code execution. This test fails the moment that changes.
func TestNoImportedDocumentCanReachAnExecutionFacility(t *testing.T) {
	forbidden := map[string]string{
		`"os/exec"`:       "starting a process",
		`"plugin"`:        "loading code at run time",
		`"syscall"`:       "reaching the operating system directly",
		`"unsafe"`:        "leaving the type system",
		`"text/template"`: "evaluating an expression from a document",
		`"html/template"`: "evaluating an expression from a document",
		`"os/user"`:       "widening ambient authority",
		`"net/http"`:      "fetching something a document named",
		`"encoding/json"`: "reading an artifact outside the strict v2 contract",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checked++
		for _, imported := range file.Imports {
			if why, found := forbidden[imported.Path.Value]; found {
				t.Errorf("%s imports %s, which is %s; a reset action is a reviewed typed operator and nothing a document names may reach that", name, imported.Path.Value, why)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no package source was inspected; the proof would pass vacuously")
	}
}

// TestAnImportedSpecCannotNameAResetAction keeps the two documents apart. A
// spec carries prose for a person and readmit executes none of it; a reviewed
// action comes from a plan somebody selected on the command line. A spec that
// tries to carry a plan, an action, a command or a hook is refused by the spec
// reader rather than read as though readmit had always allowed it.
func TestAnImportedSpecCannotNameAResetAction(t *testing.T) {
	const assertion = `{"id":"accepted","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}`
	spec := func(extra string) string {
		return `{"schema":"readmit-test/v1","name":"reschedule","input":{"case":"case","messages":["s0001-e000001"]},` +
			`"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Restart the listener."}` + extra +
			`,"observation":{"boundary":"ack-contract"},"assertions":[` + assertion + `]}`
	}
	if _, err := testrunner.DecodeSpec([]byte(spec(""))); err != nil {
		t.Fatalf("the control spec must be valid, otherwise the refusals below prove nothing: %v", err)
	}
	for name, extra := range map[string]string{
		"a reset plan":         `,"reset_plan":"reset.json"`,
		"reset actions":        `,"reset_actions":[{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x"}]`,
		"a reset command":      `,"reset_command":"psql -c 'truncate appointments'"`,
		"a setup hook":         `,"setup_hook":{"shell":"/bin/sh","argv":["-c","true"]}`,
		"a cleanup script":     `,"cleanup":"rm -rf /var/lib/fixture"`,
		"an authority to hold": `,"authority":"connect_approved_target"`,
	} {
		if _, err := testrunner.DecodeSpec([]byte(spec(extra))); err == nil {
			t.Errorf("%s: a test spec accepted a member that would let an imported document direct a reset", name)
		}
	}
}

// TestResetProseIsNeverAnAction completes the separation from the other side: a
// plan's own instructions are prose for a person even when somebody wrote a
// shell command in them. readmit records the action as awaiting that person and
// executes nothing.
func TestResetProseIsNeverAnAction(t *testing.T) {
	document := planWith(`{"id":"stop-listener","operator":"operator_confirms","authority":"none","instructions":"psql -c 'truncate appointments'; rm -rf /var/lib/fixture"}`)
	plan, err := DecodePlan([]byte(document))
	if err != nil {
		t.Fatalf("prose is prose, whatever it says: %v", err)
	}
	result := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(document), PlanDirectory: planDirectory(t, nil),
	}, noResolution)
	if result.Outcome != Unconfirmed || result.Reason != AwaitingOperator {
		t.Fatalf("a step nobody performed is unconfirmed, never run: %+v", result)
	}
	if plan.Actions[0].Operator != OperatorConfirms || reviewed[plan.Actions[0].Operator] != NoAuthority {
		t.Fatalf("an action holding shell prose still runs under no authority at all: %+v", plan.Actions[0])
	}
	confirmed := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(document), PlanDirectory: planDirectory(t, nil),
		Confirmed: []string{"stop-listener"},
	}, noResolution)
	if confirmed.Outcome != Confirmed || confirmed.Reason != EveryActionConfirmed {
		t.Fatalf("confirming the step records that a person did it: %+v", confirmed)
	}
}
