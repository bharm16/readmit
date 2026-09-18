package index_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

// PID-2 is empty, PID-3 present, PID-7 an explicit HL7 null, PID-8 empty after
// the trailing separator and PID-9 absent, so one fixture exercises all four
// decoded states a search has to keep apart.
const booking = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE||\"\"|\rSCH|1||||||||||20260101130000\r"

const acknowledgement = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-2|P|2.5.1\rMSA|AA|CTL-1\r"

func frame(message string) string { return "\x0b" + message + "\x1c\r" }

func importedAt() *time.Time {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &at
}

func builtAt() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) }

// caseOf writes one imported case bundle of the given sources and returns the
// directory and the opened evidence.
func caseOf(t testing.TB, sources ...string) (string, *bundle.Bundle) {
	t.Helper()
	inputs := make([]bundle.Input, 0, len(sources))
	for i, data := range sources {
		inputs = append(inputs, bundle.Input{
			Path:    "fixture-" + string(rune('a'+i)),
			Data:    []byte(data),
			Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
		})
	}
	path := filepath.Join(t.TempDir(), "case")
	written, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: importedAt()})
	if err != nil {
		t.Fatalf("case bundle: %v", err)
	}
	return path, written
}

func policyOf(retention index.Retention, fields ...string) index.Policy {
	return index.Policy{Fields: fields, Retention: retention}
}

func build(t testing.TB, opened *bundle.Bundle, policy index.Policy) index.Document {
	t.Helper()
	document, err := index.Build(context.Background(), opened, policy, builtAt())
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return document
}

// evidenceFingerprint is every file of a case directory and its exact bytes. It
// is taken before and after each index operation, so "index corruption must not
// change evidence" is checked against the bytes rather than asserted.
func evidenceFingerprint(t testing.TB, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
		return nil
	}); err != nil {
		t.Fatalf("read case evidence: %v", err)
	}
	return files
}

func TestIndexRetainsDeclaredFieldsSourceMetadataAndEveryDecodedState(t *testing.T) {
	_, opened := caseOf(t, frame(booking)+frame(acknowledgement))
	document := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]", "PID[1]-2[1]", "PID[1]-7[1]", "PID[1]-9[1]"))

	if document.Schema != index.Schema || document.Case.Identity != opened.Identity ||
		document.Case.Schema != bundle.Schema || document.Case.Provenance != string(bundle.Imported) {
		t.Fatalf("index does not name the evidence it describes: %+v", document.Case)
	}
	if len(document.Case.Sources) != 1 {
		t.Fatalf("source metadata: %+v", document.Case.Sources)
	}
	source := document.Case.Sources[0]
	manifest := opened.Manifest.Sources[0]
	if source.ID != manifest.ID || source.Path != manifest.Path || source.Format != manifest.Format ||
		source.Terminator != manifest.Terminator || source.Size != manifest.Size ||
		source.SHA256 != manifest.SHA256 || source.Occurrences != manifest.Occurrences {
		t.Fatalf("index source metadata is not the metadata the case declared: %+v", source)
	}
	if len(document.Records) != 2 {
		t.Fatalf("records: %d", len(document.Records))
	}
	record := document.Records[0]
	if record.ID != "s0001-e000001" || record.SourceID != "s0001" || record.Sequence != 1 ||
		record.Kind != bundle.Message || record.Direction != bundle.Unknown || record.Size != len(frame(booking)) {
		t.Fatalf("record metadata: %+v", record)
	}
	if document.Records[1].Kind != bundle.Acknowledgement {
		t.Fatalf("the acknowledgement was indexed as %q", document.Records[1].Kind)
	}
	states := map[string]hl7.State{}
	for _, value := range record.Values {
		states[value.Selector] = value.State
	}
	want := map[string]hl7.State{
		"PID[1]-3[1]": hl7.Present, "PID[1]-2[1]": hl7.Empty,
		"PID[1]-7[1]": hl7.Null, "PID[1]-9[1]": hl7.Omitted,
	}
	for selector, state := range want {
		if states[selector] != state {
			t.Errorf("%s indexed as %q, want %q", selector, states[selector], state)
		}
	}
	// The retained span must reach the exact original bytes of the occurrence,
	// not the copy the index holds.
	raw, err := opened.Raw(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	value := record.Values[0]
	if got := raw[value.Offset : value.Offset+value.Length]; !bytes.Equal(got, value.Bytes) {
		t.Fatalf("the indexed span does not name the original bytes: %q vs %q", got, value.Bytes)
	}
	if string(value.Bytes) != "MRN-1^^^READMIT^MR" || value.Truncated {
		t.Fatalf("retained value: %q truncated=%t", value.Bytes, value.Truncated)
	}
	for _, empty := range record.Values[1:] {
		if len(empty.Bytes) != 0 || empty.Digest != "" {
			t.Errorf("%s retained bytes for a value that is not present", empty.Selector)
		}
	}
}

// The golden below is written out by hand from the contract description, not
// produced by the encoder, so a change of member name, order, or encoding is a
// test failure rather than a silently regenerated expectation. Only the case
// identity is taken from the bundle reader, because only a case can state it.
func TestEncodedIndexMatchesTheIndependentlyAuthoredContract(t *testing.T) {
	data := frame(booking)
	_, opened := caseOf(t, data)
	document := build(t, opened, policyOf(index.RetainStates, "PID[1]-3[1]"))
	sum := sha256.Sum256([]byte(data))

	contents := `{"schema":"readmit-index/v1","case":{"identity":"` + opened.Identity +
		`","schema":"readmit-case/v1","provenance":"imported","sources":[{"id":"s0001","path":"fixture-a","format":"mllp","terminator":"cr","size":` +
		strconv.Itoa(len(data)) + `,"sha256":"` + hex.EncodeToString(sum[:]) +
		`","occurrences":1}]},"policy":{"fields":["PID[1]-3[1]"],"retention":"states","retain_until":null},` +
		`"built_at":"2026-09-18T12:00:00Z","records":[{"id":"s0001-e000001","source_id":"s0001","sequence":1,` +
		`"offset":0,"size":` + strconv.Itoa(len(data)) + `,"kind":"message","direction":"unknown","observed_at":null,` +
		`"imported_at":"2026-01-02T03:04:05Z","values":[{"selector":"PID[1]-3[1]","state":"present","offset":77,"length":18,"truncated":false}]}],"digest":`
	sealed := sha256.Sum256([]byte(contents + `""}`))
	want := contents + `"` + hex.EncodeToString(sealed[:]) + `"}` + "\n"

	encoded, err := index.Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != want {
		t.Fatalf("index document:\n got %s\nwant %s", encoded, want)
	}
	// A built document carries no digest; Encode records one and Decode verifies
	// it, so the value that reads back is the value the golden pins.
	if document.Digest != "" {
		t.Fatal("a document that was never written carried a recorded digest")
	}
	decoded, err := index.Decode(encoded)
	if err != nil || decoded.Digest != hex.EncodeToString(sealed[:]) || decoded.Case.Identity != opened.Identity {
		t.Fatalf("the written contract did not read back: %v", err)
	}
}

func TestRebuildFromCanonicalEvidenceProducesTheSameIndex(t *testing.T) {
	caseRoot, opened := caseOf(t, frame(booking)+frame(acknowledgement))
	policy := policyOf(index.RetainValues, "PID[1]-3[1]")
	first, err := index.Encode(build(t, opened, policy))
	if err != nil {
		t.Fatal(err)
	}
	// Rebuilding is reopening the canonical directory and building again. The
	// same evidence and the same declarations must produce the same document,
	// which is what makes a lost or discarded index recoverable rather than a
	// second original nobody can reproduce.
	reopened, err := bundle.Open(caseRoot)
	if err != nil {
		t.Fatalf("reopen the case: %v", err)
	}
	second, err := index.Encode(build(t, reopened, policy))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("rebuilding the index from the same evidence produced a different document")
	}
}

func TestDamagedTruncatedAndUnknownIndexesAreRefusedAndTheEvidenceIsUntouched(t *testing.T) {
	_, opened := caseOf(t, frame(booking))
	document := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))
	encoded, err := index.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, refusal := range []struct {
		name    string
		data    []byte
		wantErr error
		reason  string
	}{
		{"unknown version", []byte(strings.Replace(string(encoded), `"readmit-index/v1"`, `"readmit-index/v2"`, 1)), index.ErrUnsupportedVersion, ""},
		{"truncated", encoded[:len(encoded)/2], nil, "invalid index document"},
		{"unknown member", []byte(strings.Replace(string(encoded), `"schema":"readmit-index/v1",`, `"schema":"readmit-index/v1","rebuilt":true,`, 1)), nil, "invalid index document"},
		{"altered retained value", []byte(strings.Replace(string(encoded), "TVJOLTFeXl5SRUFETUlUXk1S", "TVJOLTJeXl5SRUFETUlUXk1S", 1)), index.ErrDamaged, ""},
		{"altered byte span", []byte(strings.Replace(string(encoded), `"offset":77`, `"offset":76`, 1)), index.ErrDamaged, ""},
		{"altered case identity", []byte(strings.Replace(string(encoded), opened.Identity, strings.Repeat("0", 64), 1)), index.ErrDamaged, ""},
		{"contradictory length", []byte(strings.Replace(string(encoded), `"length":18`, `"length":19`, 1)), nil, "recorded length"},
		{"span outside the occurrence", []byte(strings.Replace(string(encoded), `"offset":77,"length":18`, `"offset":777,"length":18`, 1)), nil, "byte span inside its occurrence"},
		{"occurrence the case does not name", []byte(strings.Replace(string(encoded), `"sequence":1,`, `"sequence":2,`, 1)), nil, "disagree with the sources"},
		{"source the case does not name", []byte(strings.Replace(string(encoded), `"id":"s0001"`, `"id":"s0002"`, 1)), nil, "the way the case does"},
		{"more occurrences than the source declares", []byte(strings.Replace(string(encoded), `"occurrences":1`, `"occurrences":2`, 1)), nil, "disagree with the sources"},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			read, err := index.Decode(refusal.data)
			if err == nil {
				t.Fatalf("a %s index was served: %+v", refusal.name, read.Policy)
			}
			if refusal.wantErr != nil && !errors.Is(err, refusal.wantErr) {
				t.Fatalf("got %v, want %v", err, refusal.wantErr)
			}
			if refusal.reason != "" && !strings.Contains(err.Error(), refusal.reason) {
				t.Fatalf("got %v, want a reason naming %q", err, refusal.reason)
			}
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
		})
	}
}

func TestAnIndexOfOtherEvidenceIsRefusedRatherThanAnswered(t *testing.T) {
	_, first := caseOf(t, frame(booking))
	_, second := caseOf(t, frame(acknowledgement))
	document := build(t, first, policyOf(index.RetainValues, "PID[1]-3[1]"))
	if err := document.Describes(first); err != nil {
		t.Fatalf("an index of this case was refused for it: %v", err)
	}
	if err := document.Describes(second); !errors.Is(err, index.ErrStale) {
		t.Fatalf("an index of other evidence answered for this case: %v", err)
	}
	if err := document.Describes(nil); err == nil {
		t.Fatal("an index agreed with no evidence at all")
	}
	// The seal detects damage, not forgery, so what the index restates about
	// the case is checked against the verified bundle rather than trusted. Each
	// of these reseals cleanly and must still be refused.
	for name, forge := range map[string]func(index.Document) index.Document{
		"another provenance": func(d index.Document) index.Document { d.Case.Provenance = "generated"; return d },
		"another source digest": func(d index.Document) index.Document {
			d.Case.Sources[0].SHA256 = strings.Repeat("a", 64)
			return d
		},
		"another source framing": func(d index.Document) index.Document {
			d.Case.Sources[0].Terminator = hl7.LF
			return d
		},
		"another occurrence kind": func(d index.Document) index.Document {
			d.Records[0].Kind = bundle.Acknowledgement
			return d
		},
		"another observed time": func(d index.Document) index.Document {
			observed := time.Date(2026, 5, 5, 5, 5, 5, 0, time.UTC)
			d.Records[0].ObservedAt = &observed
			return d
		},
	} {
		t.Run(name, func(t *testing.T) {
			forged, err := index.Decode(mustEncode(t, forge(clone(t, document))))
			if err != nil {
				t.Fatalf("the forged index did not reseal: %v", err)
			}
			if err := forged.Describes(first); !errors.Is(err, index.ErrStale) {
				t.Fatalf("an index restating the case wrongly described it: %v", err)
			}
		})
	}
}

// clone is one independent copy of a document, so a forging test cannot change
// the document the next one starts from.
func clone(t testing.TB, document index.Document) index.Document {
	t.Helper()
	copied, err := index.Decode(mustEncode(t, document))
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	copied.Case.Sources = slices.Clone(copied.Case.Sources)
	copied.Records = slices.Clone(copied.Records)
	return copied
}

func mustEncode(t testing.TB, document index.Document) []byte {
	t.Helper()
	data, err := index.Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

func TestRetentionEndIsExplicitAndEndsServiceWithoutTouchingEvidence(t *testing.T) {
	caseRoot, opened := caseOf(t, frame(booking))
	before := evidenceFingerprint(t, caseRoot)
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	policy := policyOf(index.RetainValues, "PID[1]-3[1]")
	policy.RetainUntil = &end
	document := build(t, opened, policy)

	if err := document.Usable(end.Add(-time.Second)); err != nil {
		t.Fatalf("an index inside its retention was refused: %v", err)
	}
	if err := document.Usable(end); !errors.Is(err, index.ErrExpired) {
		t.Fatalf("an index served past the retention it declared: %v", err)
	}
	if err := document.Usable(end.Add(time.Hour)); !errors.Is(err, index.ErrExpired) {
		t.Fatalf("an index served past the retention it declared: %v", err)
	}
	// The refusal belongs to the index, not to the command line: a facade that
	// calls this package directly is refused by the same check.
	if _, err := document.Search(end, index.Query{Match: index.State, State: hl7.Present}); !errors.Is(err, index.ErrExpired) {
		t.Fatalf("an expired index answered a query: %v", err)
	}
	if _, err := document.Search(end.Add(-time.Second), index.Query{Match: index.State, State: hl7.Present}); err != nil {
		t.Fatalf("an index inside its retention refused a query: %v", err)
	}
	// An index with no declared end is retained until it is deleted, which is a
	// statement the policy makes rather than a default.
	indefinite := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))
	if err := indefinite.Usable(end.Add(100 * time.Hour)); err != nil {
		t.Fatalf("an index with no declared end was refused: %v", err)
	}
	if after := evidenceFingerprint(t, caseRoot); !maps.Equal(before, after) {
		t.Fatal("a retention decision changed the evidence")
	}
}

func TestRetentionPolicyDeclaresEveryChoiceExplicitly(t *testing.T) {
	_, opened := caseOf(t, frame(booking))
	for name, policy := range map[string]index.Policy{
		"no fields":              policyOf(index.RetainValues),
		"no retention form":      policyOf("", "PID[1]-3[1]"),
		"unknown retention form": policyOf("everything", "PID[1]-3[1]"),
		"repeated field":         policyOf(index.RetainValues, "PID[1]-3[1]", "PID[1]-3[1]"),
		"uncanonical field":      policyOf(index.RetainValues, "PID-3"),
		"invalid field":          policyOf(index.RetainValues, "not a selector"),
		"too many fields":        policyOf(index.RetainValues, manyFields(index.MaxFields+1)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := index.Build(context.Background(), opened, policy, builtAt()); err == nil {
				t.Fatal("an index was built from an incomplete declaration")
			}
			if err := index.ValidatePolicy(policy); err == nil {
				t.Fatal("an incomplete declaration validated")
			}
		})
	}
	if err := index.ValidatePolicy(policyOf(index.RetainValues, manyFields(index.MaxFields)...)); err != nil {
		t.Fatalf("the declared field limit was refused at the limit: %v", err)
	}
}

func manyFields(n int) []string {
	fields := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		fields = append(fields, "PID[1]-"+strconv.Itoa(i)+"[1]")
	}
	return fields
}

func TestPolicyDecodingRequiresEveryDeclarationAndRefusesUnknownOnes(t *testing.T) {
	sound := `{"fields":["PID[1]-3[1]"],"retention":"values","retain_until":null}`
	if _, err := index.Decode([]byte(`{"schema":"readmit-index/v1","policy":` + sound + `}`)); err == nil {
		t.Fatal("an index without a case was accepted")
	}
	for name, policy := range map[string]string{
		"absent retention end": `{"fields":["PID[1]-3[1]"],"retention":"values"}`,
		"absent fields":        `{"retention":"values","retain_until":null}`,
		"absent form":          `{"fields":["PID[1]-3[1]"],"retain_until":null}`,
		"unknown declaration":  `{"fields":["PID[1]-3[1]"],"retention":"values","retain_until":null,"forever":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := index.Decode([]byte(`{"schema":"readmit-index/v1","policy":` + policy + `}`))
			if err == nil || !strings.Contains(err.Error(), "invalid index document") {
				t.Fatalf("got %v, want a refusal of the document", err)
			}
		})
	}
}

func TestEachRetentionFormAnswersOnlyWhatItRetained(t *testing.T) {
	_, opened := caseOf(t, frame(booking))
	field := "PID[1]-3[1]"
	values := build(t, opened, policyOf(index.RetainValues, field))
	digests := build(t, opened, policyOf(index.RetainDigests, field))
	states := build(t, opened, policyOf(index.RetainStates, field))

	if digests.Records[0].Values[0].Digest == "" || len(digests.Records[0].Values[0].Bytes) != 0 {
		t.Fatalf("a digest index retained the wrong thing: %+v", digests.Records[0].Values[0])
	}
	if states.Records[0].Values[0].Digest != "" || len(states.Records[0].Values[0].Bytes) != 0 {
		t.Fatalf("a state index retained message content: %+v", states.Records[0].Values[0])
	}
	term := []byte("MRN-1^^^READMIT^MR")
	for name, document := range map[string]index.Document{"values": values, "digests": digests} {
		result, err := document.Search(builtAt(), index.Query{Match: index.Equals, Term: term})
		if err != nil || len(result.Hits) != 1 {
			t.Fatalf("%s did not answer an exact query: %v %+v", name, err, result)
		}
		miss, err := document.Search(builtAt(), index.Query{Match: index.Equals, Term: []byte("MRN-2^^^READMIT^MR")})
		if err != nil || len(miss.Hits) != 0 {
			t.Fatalf("%s matched a value it does not hold: %v %+v", name, err, miss)
		}
	}
	if _, err := digests.Search(builtAt(), index.Query{Match: index.Contains, Term: []byte("MRN-1")}); err == nil {
		t.Fatal("a digest index answered a substring query")
	}
	for _, match := range []index.Match{index.Equals, index.Contains} {
		if _, err := states.Search(builtAt(), index.Query{Match: match, Term: term}); err == nil {
			t.Fatalf("an index retaining no values answered a %s query", match)
		}
	}
	for _, document := range []index.Document{values, digests, states} {
		result, err := document.Search(builtAt(), index.Query{Match: index.State, State: hl7.Present})
		if err != nil || len(result.Hits) != 1 {
			t.Fatalf("a state query was not answered by every form: %v %+v", err, result)
		}
	}
	if result, err := values.Search(builtAt(), index.Query{Match: index.Contains, Term: []byte("READMIT")}); err != nil || len(result.Hits) != 1 {
		t.Fatalf("substring query: %v %+v", err, result)
	}
}

func TestQueriesThisIndexCannotAnswerAreRefusedByName(t *testing.T) {
	_, opened := caseOf(t, frame(booking))
	document := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))
	for name, query := range map[string]index.Query{
		"unindexed field": {Field: "PV1[1]-3[1]", Match: index.State, State: hl7.Present},
		"unknown match":   {Match: "regex", Term: []byte("x")},
		"unknown state":   {Match: index.State, State: "maybe"},
		"empty term":      {Match: index.Equals},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := document.Search(builtAt(), query); err == nil {
				t.Fatal("a query this index cannot answer was answered anyway")
			}
		})
	}
	result, err := document.Search(builtAt(), index.Query{Field: "PID[1]-3[1]", Match: index.State, State: hl7.Present})
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("a query of one declared field: %v %+v", err, result)
	}
}

func TestShortenedValuesAreUndecidedRatherThanMissing(t *testing.T) {
	long := strings.Repeat("A", index.MaxValueBytes) + "TAIL"
	message := strings.Replace(booking, "MRN-1^^^READMIT^MR", long, 1)
	_, opened := caseOf(t, frame(message))
	document := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))

	value := document.Records[0].Values[0]
	if !value.Truncated || len(value.Bytes) != index.MaxValueBytes || value.Length != len(long) {
		t.Fatalf("a long value was not retained as a stated prefix: truncated=%t kept=%d length=%d", value.Truncated, len(value.Bytes), value.Length)
	}
	// The whole value is not there, so a query it cannot settle is reported as
	// undecided; a query the retained prefix does settle is still answered.
	for name, query := range map[string]index.Query{
		"exact":         {Match: index.Equals, Term: []byte(long)},
		"absent suffix": {Match: index.Contains, Term: []byte("TAIL")},
	} {
		result, err := document.Search(builtAt(), query)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(result.Hits) != 0 || result.Undecided != 1 {
			t.Fatalf("%s reported %d matches and %d undecided", name, len(result.Hits), result.Undecided)
		}
	}
	result, err := document.Search(builtAt(), index.Query{Match: index.Contains, Term: []byte("AAAA")})
	if err != nil || len(result.Hits) != 1 || result.Undecided != 0 {
		t.Fatalf("a match inside the retained prefix was not answered: %v %+v", err, result)
	}
	// The index records the length of the whole value, so a term of another
	// length is a decided miss rather than something the prefix cannot settle.
	for name, query := range map[string]index.Query{
		"shorter term":               {Match: index.Equals, Term: []byte("A")},
		"longer term":                {Match: index.Equals, Term: []byte(long + "MORE")},
		"term longer than the value": {Match: index.Contains, Term: []byte(strings.Repeat("A", len(long)+1))},
	} {
		decided, err := document.Search(builtAt(), query)
		if err != nil || len(decided.Hits) != 0 || decided.Undecided != 0 {
			t.Fatalf("%s was not decided: %v %+v", name, err, decided)
		}
	}
	// A digest index has the whole value behind it, so the same case answers
	// the exact query the shortened one could not settle.
	whole := build(t, opened, policyOf(index.RetainDigests, "PID[1]-3[1]"))
	exact, err := whole.Search(builtAt(), index.Query{Match: index.Equals, Term: []byte(long)})
	if err != nil || len(exact.Hits) != 1 {
		t.Fatalf("a digest index did not settle a long value: %v %+v", err, exact)
	}
}

func TestOccurrencesTheCaseCannotDecodeCarryNoIndexedFields(t *testing.T) {
	_, opened := caseOf(t, frame(booking)+frame("NOT-HL7"))
	document := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))
	if len(document.Records) != 2 {
		t.Fatalf("records: %d", len(document.Records))
	}
	undecodable := document.Records[1]
	if undecodable.ParseError == "" || len(undecodable.Values) != 0 || undecodable.Kind != bundle.Unparsed {
		t.Fatalf("an undecodable occurrence was indexed as decoded: %+v", undecodable)
	}
	// Reporting it as an omitted field would say the case does not carry the
	// value, which is a different claim from never having decoded it.
	result, err := document.Search(builtAt(), index.Query{Match: index.State, State: hl7.Omitted})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 0 || result.Undecodable != 1 {
		t.Fatalf("undecodable evidence answered a field query: %d hits, %d undecodable", len(result.Hits), result.Undecodable)
	}
}

func TestBuildIsCancellableAndProducesNothingWhenCancelled(t *testing.T) {
	_, opened := caseOf(t, frame(booking)+frame(acknowledgement))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	document, err := index.Build(ctx, opened, policyOf(index.RetainValues, "PID[1]-3[1]"), builtAt())
	if err == nil {
		t.Fatalf("a cancelled build returned an index: %d records", len(document.Records))
	}
	if !strings.Contains(err.Error(), "cancel") || len(document.Records) != 0 {
		t.Fatalf("a cancelled build did not report cancellation: %v", err)
	}
}

func TestIndexIsWrittenOutsideTheEvidenceAndNeverOverwritesOne(t *testing.T) {
	caseRoot, opened := caseOf(t, frame(booking))
	document := build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))
	before := evidenceFingerprint(t, caseRoot)

	if _, err := index.Write(filepath.Join(caseRoot, "search.json"), document); err == nil {
		t.Fatal("an index was written inside retained evidence")
	}
	if _, err := index.Write(filepath.Join(caseRoot, "payloads", "search.json"), document); err == nil {
		t.Fatal("an index was written inside retained evidence")
	}
	destination := filepath.Join(t.TempDir(), "search.json")
	// Write reports the physical path it actually created, which is not always
	// the spelling it was handed; reading that path back is what proves the
	// index went where the path policy said it could go.
	written, err := index.Write(destination, document)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := index.Open(written); err != nil {
		t.Fatalf("the reported path does not hold the index: %v", err)
	}
	if _, err := index.Write(destination, document); err == nil {
		t.Fatal("an existing index was overwritten in place")
	}
	read, err := index.Open(destination)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := read.Describes(opened); err != nil {
		t.Fatalf("the written index did not describe the case it was built from: %v", err)
	}
	if !slices.Equal(read.Policy.Fields, document.Policy.Fields) || read.Case.Identity != document.Case.Identity {
		t.Fatal("the written index read back as a different index")
	}
	if after := evidenceFingerprint(t, caseRoot); !maps.Equal(before, after) {
		t.Fatal("indexing changed the evidence")
	}
}

func TestADamagedIndexOnDiskNeverChangesOrBlocksTheEvidence(t *testing.T) {
	caseRoot, opened := caseOf(t, frame(booking))
	before := evidenceFingerprint(t, caseRoot)
	destination := filepath.Join(t.TempDir(), "search.json")
	if _, err := index.Write(destination, build(t, opened, policyOf(index.RetainValues, "PID[1]-3[1]"))); err != nil {
		t.Fatal(err)
	}
	damaged, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	// A retained value altered in place stays structurally sound, so only the
	// digest over the contents stands between it and a wrong answer.
	altered := strings.Replace(string(damaged), "TVJOLTFeXl5SRUFETUlUXk1S", "TVJOLTJeXl5SRUFETUlUXk1S", 1)
	if altered == string(damaged) {
		t.Fatal("the test did not alter the retained value it meant to")
	}
	if err := os.WriteFile(destination, []byte(altered), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Open(destination); !errors.Is(err, index.ErrDamaged) {
		t.Fatalf("a damaged index was served: %v", err)
	}
	// The evidence is the canonical artifact: it is readable, unchanged, and a
	// fresh index of it is built from the same bytes.
	reopened, err := bundle.Open(caseRoot)
	if err != nil {
		t.Fatalf("a damaged index blocked the evidence: %v", err)
	}
	if reopened.Identity != opened.Identity {
		t.Fatal("the evidence changed while an index was damaged")
	}
	if after := evidenceFingerprint(t, caseRoot); !maps.Equal(before, after) {
		t.Fatal("a damaged index changed the evidence")
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Write(destination, build(t, reopened, policyOf(index.RetainValues, "PID[1]-3[1]"))); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	read, err := index.Open(destination)
	if err != nil || read.Describes(reopened) != nil {
		t.Fatalf("the rebuilt index did not describe the case: %v", err)
	}
}

func TestOpenRefusesWhatIsNotAReadableIndexFile(t *testing.T) {
	directory := t.TempDir()
	if _, err := index.Open(directory); err == nil {
		t.Fatal("a directory was read as an index")
	}
	if _, err := index.Open(filepath.Join(directory, "absent.json")); err == nil {
		t.Fatal("a missing file was read as an index")
	}
	oversized := filepath.Join(directory, "oversized.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte{' '}, index.MaxIndexBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Open(oversized); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("an oversized index was read: %v", err)
	}
}

func FuzzIndexDocument(f *testing.F) {
	_, opened := caseOf(f, frame(booking)+frame("NOT-HL7"))
	for _, retention := range []index.Retention{index.RetainValues, index.RetainDigests, index.RetainStates} {
		document, err := index.Build(context.Background(), opened, policyOf(retention, "PID[1]-3[1]", "PID[1]-7[1]"), builtAt())
		if err != nil {
			f.Fatal(err)
		}
		encoded, err := index.Encode(document)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(encoded)
	}
	f.Add([]byte(`{"schema":"readmit-index/v2"}`))
	f.Add([]byte(`{"schema":"readmit-index/v1","policy":{"fields":[],"retention":"values","retain_until":null}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		document, err := index.Decode(data)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if document.Schema != index.Schema || len(document.Policy.Fields) == 0 {
			t.Fatalf("an index without a contract version or declared fields was accepted: %+v", document.Policy)
		}
		// Whatever the reader accepts must survive being written back out, and
		// must answer the same way: a document that reads differently the
		// second time would be serving answers nothing stands behind.
		encoded, err := index.Encode(document)
		if err != nil {
			t.Fatal("accepted an index that cannot be written back")
		}
		again, err := index.Decode(encoded)
		if err != nil {
			t.Fatalf("a written index did not read back: %v", err)
		}
		if again.Digest != document.Digest || len(again.Records) != len(document.Records) {
			t.Fatal("a written index read back as a different index")
		}
		if document.Digest == "" {
			t.Fatal("a decoded index carried no verified digest")
		}
		result, err := document.Search(builtAt(), index.Query{Match: index.State, State: hl7.Present})
		if err != nil {
			t.Fatalf("an accepted index could not answer a state query: %v", err)
		}
		repeated, err := again.Search(builtAt(), index.Query{Match: index.State, State: hl7.Present})
		if err != nil || len(repeated.Hits) != len(result.Hits) || repeated.Undecided != result.Undecided {
			t.Fatal("a written index answered differently than the one it was written from")
		}
	})
}
