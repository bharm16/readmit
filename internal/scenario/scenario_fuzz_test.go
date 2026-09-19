package scenario_test

import (
	"encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/scenario"
)

// FuzzScenarioDocument exercises the scenario reader and the preview together.
//
// No input may panic, and no bytes at all may produce a scenario that reads
// back as a different scenario, a preview whose steps are not the steps that
// were designed, a refused step that moved the subject it was refused for, or
// an accepted step whose author declared it refused. That is the whole claim of
// this delivery — a designed workflow means what its author declared — stated
// as a property, so it cannot be lost by widening the reader.
func FuzzScenarioDocument(f *testing.F) {
	for _, shipped := range []string{"scenario-adt.json", "scenario-siu.json", "scenario-refused.json"} {
		data, err := os.ReadFile("../../testdata/fixtures/" + shipped)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(data))
	}
	f.Add(`{"schema":"readmit-scenario/v2"}`)
	f.Add(`{"schema":"readmit-scenario/v1","scenario":{"id":"s","version":"1"},"profile":"readmit-adt-lifecycle-v1",` +
		`"base_time":"2026-01-01T12:00:00Z","subjects":[],"steps":[]}`)
	f.Add(`{`)

	f.Fuzz(func(t *testing.T, document string) {
		designed, err := scenario.Decode([]byte(document))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(designed)
		if err != nil {
			t.Fatalf("an accepted scenario could not be written back: %v", err)
		}
		again, err := scenario.Decode(encoded)
		if err != nil {
			t.Fatalf("a scenario did not read back at all: %v", err)
		}
		if again.Schema != designed.Schema || again.Scenario != designed.Scenario || again.Profile != designed.Profile ||
			!again.BaseTime.Equal(designed.BaseTime) ||
			!reflect.DeepEqual(again.Subjects, designed.Subjects) || !reflect.DeepEqual(again.Steps, designed.Steps) {
			t.Fatal("a scenario did not read back as itself")
		}
		timeline, err := scenario.Preview(designed)
		if err != nil {
			return
		}
		if len(timeline.Steps) != len(designed.Steps) || timeline.Accepted+timeline.Refused != len(designed.Steps) {
			t.Fatalf("%d designed steps previewed as %d (%d accepted, %d refused)",
				len(designed.Steps), len(timeline.Steps), timeline.Accepted, timeline.Refused)
		}
		for i, step := range timeline.Steps {
			if step.ID != designed.Steps[i].ID || step.Ordinal != i+1 {
				t.Fatalf("step %d previewed as %q at %d", i+1, step.ID, step.Ordinal)
			}
			if step.Expect != designed.Steps[i].Expect {
				t.Fatalf("step %q previewed a different expectation than it declared", step.ID)
			}
			if (step.Reason == "") != (step.Expect == scenario.Accepted) {
				t.Fatalf("step %q was previewed against its declared outcome", step.ID)
			}
			if step.Reason != "" && step.From != step.To {
				t.Fatalf("refused step %q moved its subject from %q to %q", step.ID, step.From, step.To)
			}
		}
	})
}
