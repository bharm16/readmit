package evidencesource_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/importer"
)

// collectedReceipt collects a directory source holding the named entries and
// returns the receipt's bytes, the staged directory and the options it ran
// under.
func collectedReceipt(t *testing.T, entries map[string]string) ([]byte, string, evidencesource.Options) {
	t.Helper()
	source, _ := directorySource(t, entries)
	output := destination(t)
	chosen := options(t, ".hl7")
	collection, err := evidencesource.Collect(context.Background(), source, output, chosen)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := evidencesource.EncodeCollection(collection)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, output, chosen
}

// The reader reads exactly what a collection writes: decoding a receipt and
// encoding it again reproduces it byte for byte.
func TestDecodeCollectionReadsTheReceiptACollectionWrote(t *testing.T) {
	encoded, _, _ := collectedReceipt(t, map[string]string{"a.hl7": message, "b.hl7": message, "notes.md": "not evidence"})
	collection, err := evidencesource.DecodeCollection(encoded)
	if err != nil {
		t.Fatal(err)
	}
	again, err := evidencesource.EncodeCollection(collection)
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("a decoded receipt re-encodes differently (%v):\n%s\nwant\n%s", err, again, encoded)
	}
	if collection.Totals.Collected != 1 || collection.Totals.Duplicates != 1 || collection.Totals.Excluded != 1 {
		t.Fatalf("totals = %+v", collection.Totals)
	}
}

// A receipt is read under its own contract and nothing else: every member is
// required at every level, no member the contract does not declare is read,
// a later version is unsupported rather than invalid, and a receipt whose
// identity is not the digest of the entries it records is refused.
func TestDecodeCollectionRefusesWhatTheContractDoesNotDeclare(t *testing.T) {
	encoded, _, _ := collectedReceipt(t, map[string]string{"a.hl7": message, "notes.md": "not evidence"})
	receipt := string(encoded)
	edit := func(old, replacement string) []byte {
		t.Helper()
		if !strings.Contains(receipt, old) {
			t.Fatalf("the receipt holds no %q to edit:\n%s", old, receipt)
		}
		return []byte(strings.Replace(receipt, old, replacement, 1))
	}
	const (
		invalid  = "invalid source collection receipt"
		requires = "a source collection receipt declares its source, collection time, plan, status, run state, reason, quota, entries, totals and identity"
		identity = "the collection receipt's identity is not the digest of the entries it records"
	)
	cases := map[string]struct {
		data []byte
		want string
	}{
		"an unknown member":               {edit(`"schema":`, `"staged_at":"/evidence","schema":`), invalid},
		"an unknown source member":        {edit(`"kind":"directory"`, `"kind":"directory","root":"/evidence"`), invalid},
		"an unknown entry member":         {edit(`"attempts":1`, `"attempts":1,"path":"/evidence/a.hl7"`), invalid},
		"an unknown totals member":        {edit(`"not_read":0`, `"not_read":0,"skipped":0`), invalid},
		"an unknown plan member":          {edit(`"framing":"raw"`, `"framing":"raw","detect":true`), invalid},
		"a missing member":                {edit(`"run_state":"",`, ``), requires},
		"a null member":                   {edit(`"identity":"`, `"identity":null,"_":"`), requires},
		"a missing source member":         {edit(`"scope":"appointments"`, `"scope_":"appointments"`), invalid},
		"a missing entry member":          {edit(`"duplicate_of":"",`, ``), invalid},
		"a null entry member":             {edit(`"duplicate_of":"","reason":""`, `"duplicate_of":"","reason":null`), invalid},
		"a missing totals member":         {edit(`"bytes":`, `"bytes_":`), invalid},
		"a missing quota member":          {edit(`"max_entries":`, `"max_entries_":`), invalid},
		"an entry digest that was edited": {edit(`"sha256":"`, `"sha256":"0`), identity},
		"an identity that was edited":     {edit(`"identity":"`, `"identity":"0`), identity},
		"a status this release never writes": {
			edit(`"status":"complete"`, `"status":"observed"`), "a source collection receipt records a status this release does not write",
		},
		"a run state that is not its status's": {
			edit(`"run_state":""`, `"run_state":"execution_error"`), "a source collection receipt records a status this release does not write",
		},
		"an entry state this release never writes": {
			edit(`"state":"excluded"`, `"state":"skipped"`), "a source collection receipt records an entry state this release does not write",
		},
		"a plan that is not a usable plan": {
			edit(`"framing":"raw"`, `"framing":"auto"`), "framing must be declared as raw, mllp, or batch",
		},
		"too many bytes": {append(encoded, strings.Repeat(" ", evidencesource.MaxCollectionBytes)...), "the source collection receipt exceeds its size limit"},
	}
	for name, c := range cases {
		if _, err := evidencesource.DecodeCollection(c.data); err == nil || err.Error() != c.want {
			t.Errorf("%s: DecodeCollection = %v, want %q", name, err, c.want)
		}
	}
	if _, err := evidencesource.DecodeCollection(edit(`"schema":"readmit-source-collection/v1"`, `"schema":"readmit-source-collection/v2"`)); !errors.Is(err, evidencesource.ErrUnsupportedVersion) {
		t.Errorf("a later version: %v, want %v", err, evidencesource.ErrUnsupportedVersion)
	}
}

// staged reads a staged directory as the one folder an import of it under the
// collection's own plan reads.
func staged(t *testing.T, folder string, plan importer.Plan) importer.Container {
	t.Helper()
	extraction, err := importer.Extract(context.Background(), plan, nil, []string{folder}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return extraction.Containers[0]
}

// The staged folder an import reads holds exactly the evidence the receipt
// names: every collected entry, under its own name, with its recorded bytes,
// and no other member the plan selects. A member the plan does not select is
// no evidence of the collection's and is left to the import to exclude.
func TestVerifyStagedHoldsTheFolderToTheEntriesTheCollectionStaged(t *testing.T) {
	encoded, output, chosen := collectedReceipt(t, map[string]string{"a.hl7": message, "b.hl7": message + "NTE|1\r"})
	collection, err := evidencesource.DecodeCollection(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := collection.VerifyStaged(staged(t, output, chosen.Plan)); err != nil {
		t.Fatalf("the folder the collection staged: %v", err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(output, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("notes.md", "a person's note beside the evidence")
	if err := collection.VerifyStaged(staged(t, output, chosen.Plan)); err != nil {
		t.Fatalf("a member the plan does not select: %v", err)
	}

	write("c.hl7", message+"NTE|2\r")
	if err := collection.VerifyStaged(staged(t, output, chosen.Plan)); !errors.Is(err, evidencesource.ErrUnrecordedMember) {
		t.Fatalf("a member the collection did not collect: %v", err)
	}
	if err := os.Remove(filepath.Join(output, "c.hl7")); err != nil {
		t.Fatal(err)
	}

	write("b.hl7", message+"NTE|9\r")
	if err := collection.VerifyStaged(staged(t, output, chosen.Plan)); !errors.Is(err, evidencesource.ErrChangedEntry) {
		t.Fatalf("an entry whose bytes changed: %v", err)
	}

	if err := os.Remove(filepath.Join(output, "b.hl7")); err != nil {
		t.Fatal(err)
	}
	if err := collection.VerifyStaged(staged(t, output, chosen.Plan)); !errors.Is(err, evidencesource.ErrMissingEntry) {
		t.Fatalf("an entry that is no longer staged: %v", err)
	}
}

// Only a completed collection staged the whole of its scope. A failed one
// reads back as the attempt it was, and is not importable.
func TestOnlyACompletedCollectionIsImportable(t *testing.T) {
	encoded, _, _ := collectedReceipt(t, map[string]string{"a.hl7": message})
	complete, err := evidencesource.DecodeCollection(encoded)
	if err != nil || complete.Importable() != nil {
		t.Fatalf("a completed collection: %v, importable %v", err, complete.Importable())
	}
	source, _ := directorySource(t, map[string]string{"a.hl7": message, "b.hl7": "\x0b" + message + "\x1c\r"})
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("a collection whose entry contradicts its framing: %v", err)
	}
	encoded, err = evidencesource.EncodeCollection(collection)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := evidencesource.DecodeCollection(encoded)
	if err != nil || !errors.Is(failed.Importable(), evidencesource.ErrNotImportable) {
		t.Fatalf("a failed collection: %v, importable %v", err, failed.Importable())
	}
}
