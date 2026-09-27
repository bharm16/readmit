package replay_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
)

func TestExplicitSequencePreservesOrderWhileLegacySelectionStaysFrozen(t *testing.T) {
	source := caseAt(t, append(mllp.Frame(request("A")), mllp.Frame(request("B"))...))
	receiver := target("127.0.0.1:12345")
	sequence := []string{"s0001-e000002", "s0001-e000001", "s0001-e000002"}
	p, err := replay.PrepareScopedSequence(source, receiver, replay.Options{Occurrences: sequence})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range p.Mappings() {
		got = append(got, m.SourceOccurrence)
	}
	if !reflect.DeepEqual(got, sequence) {
		t.Fatal("sequence reordered or deduplicated", got)
	}
	if _, err = replay.PrepareScoped(source, receiver, replay.Options{Occurrences: sequence}); err == nil {
		t.Fatal("frozen preparation acquired duplicate semantics")
	}
	legacy, err := replay.PrepareScoped(source, receiver, replay.Options{Occurrences: sequence[:2]})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Mappings()[0].SourceOccurrence != "s0001-e000001" || legacy.Mappings()[1].SourceOccurrence != "s0001-e000002" {
		t.Fatal("legacy source ordering changed")
	}
	for _, ids := range [][]string{nil, {"s0001-e999999"}} {
		if _, err = replay.PrepareScopedSequence(source, receiver, replay.Options{Occurrences: ids}); err == nil {
			t.Fatal("unbounded or unknown sequence accepted")
		}
	}
}

func TestSequenceFamilyDoesNotConsumeFrozenRunFutureVersion(t *testing.T) {
	path := t.TempDir()
	for name, raw := range map[string]string{"manifest.json": `{"schema":"readmit-run/v2"}`, "events.jsonl": "\n", "identity.sha256": "not-a-valid-identity\n"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := replay.Open(path); err == nil || err.Error() != "unsupported run bundle schema version" {
		t.Fatalf("future old-family version was reinterpreted: %v", err)
	}
	if replay.SequenceSchema != "readmit-sequence-run/v1" {
		t.Fatal("sequence did not declare its own family")
	}
}
