package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

// One rule names every execution's new output entry. A proposed name is the
// first prefix-NNN free for every entry the flow writes, a name that is not one
// entry is refused, and a name already taken is refused by a run and reported
// by a replay, whose preview still shows what would be sent.
func TestEveryFlowNamesItsNewOutputThroughOneRule(t *testing.T) {
	root := t.TempDir()
	for _, taken := range []string{"job-001", "replay-001.decision.json", "taken"} {
		if err := os.Mkdir(filepath.Join(root, taken), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(requested string) (RunDestination, refusal) { return destinationFor(root, requested, "job") }
	replayed := func(requested string) (RunDestination, refusal) { return replayOutput.destination(root, requested) }
	for name, check := range map[string]struct {
		destination func(string) (RunDestination, refusal)
		requested   string
		want        RunDestination
		refused     refusal
	}{
		"a run proposes the next free name":           {run, "", RunDestination{Name: "job-002", Generated: true, Fresh: true}, refusal{}},
		"a replay skips a name its decision holds":    {replayed, "", RunDestination{Name: "replay-002", Generated: true, Fresh: true}, refusal{}},
		"a run accepts a new entry":                   {run, "new", RunDestination{Name: "new", Fresh: true}, refusal{}},
		"a replay accepts a new entry":                {replayed, "new", RunDestination{Name: "new", Fresh: true}, refusal{}},
		"a run refuses a name already taken":          {run, "taken", RunDestination{Name: "taken"}, refusal{Failed, "the run folder already exists; execution requires a fresh destination"}},
		"a replay reports a name already taken":       {replayed, "taken", RunDestination{Name: "taken", Reason: "that run folder or the decision file beside it already exists; a send writes both as new entries"}, refusal{}},
		"a replay reports a decision already written": {replayed, "replay-001", RunDestination{Name: "replay-001", Reason: "that run folder or the decision file beside it already exists; a send writes both as new entries"}, refusal{}},
		"a run refuses a path that is not one entry":  {run, filepath.Join("..", "outside"), RunDestination{Name: filepath.Join("..", "outside")}, refusal{Failed, "the run folder must be one new entry of the open workspace"}},
		"a replay refuses a path that is not one entry": {replayed, filepath.Join("..", "outside"), RunDestination{Name: filepath.Join("..", "outside")},
			refusal{Failed, "a replay's run folder must be one new entry of the open workspace"}},
	} {
		got, refused := check.destination(check.requested)
		if got != check.want || refused != check.refused {
			t.Errorf("%s: %+v %+v, want %+v %+v", name, got, refused, check.want, check.refused)
		}
	}
}
