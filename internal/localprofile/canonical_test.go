package localprofile_test

import (
	"cmp"
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
)

func canonical(t *testing.T, profile localprofile.Profile) (localprofile.Profile, []byte) {
	t.Helper()
	ordered, document, err := localprofile.Canonical(profile)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return ordered, document
}

// The canonical document is the document a reader reads: the fixture read and
// written again is the fixture, byte for byte, so nothing is normalized,
// dropped or invented on the way through.
func TestTheFixtureIsItsOwnCanonicalDocument(t *testing.T) {
	_, document := canonical(t, decoded(t))
	if string(document) != string(fixture(t, "local-profile.json")) {
		t.Fatalf("the profile was rewritten:\n%s", document)
	}
}

// The same rules are the same bytes, whatever order a document's members,
// segments, fields and declared sets arrived in: order is not meaning in this
// contract, so it is never visible in what Canonical writes.
func TestCanonicalWritesOneDocumentWhateverTheOrder(t *testing.T) {
	// A second terminology set, authority and date rule, each sorting before
	// the fixture's own, so every declared list has an order to get wrong.
	declared := decoded(t)
	declared.Terminology = append(declared.Terminology, localprofile.TerminologySet{
		ID: "admission-reason", Binding: localprofile.BindingSuggested, Codes: []localprofile.Code{{Code: "E"}},
	})
	declared.Authorities = append(declared.Authorities, localprofile.Authority{ID: "admission-authority", Namespace: "SITE"})
	declared.Dates = append(declared.Dates, localprofile.DateHandling{
		ID: "admission-date", Precision: localprofile.PrecisionDay, TimeZone: localprofile.TimeZoneForbidden,
	})

	reversed := decoded(t)
	reversed.Terminology = slices.Clone(declared.Terminology)
	reversed.Authorities = slices.Clone(declared.Authorities)
	reversed.Dates = slices.Clone(declared.Dates)
	slices.Reverse(reversed.Segments)
	for i := range reversed.Segments {
		slices.Reverse(reversed.Segments[i].Fields)
	}
	slices.Reverse(reversed.Terminology)
	slices.Reverse(reversed.Authorities)
	slices.Reverse(reversed.Dates)

	// The reversed profile written compactly with every object's members in
	// alphabetical order, at every level, and read back through the reader.
	written, err := json.Marshal(reversed)
	if err != nil {
		t.Fatal(err)
	}
	var loose map[string]any
	if err := json.Unmarshal(written, &loose); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.Marshal(loose, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	reread, err := localprofile.Decode(reordered)
	if err != nil {
		t.Fatalf("decode the reordered document: %v", err)
	}

	ordered, want := canonical(t, declared)
	for name, arrangement := range map[string]localprofile.Profile{
		"reversed":                 reversed,
		"read from other members":  reread,
		"already in the one order": ordered,
	} {
		if _, document := canonical(t, arrangement); string(document) != string(want) {
			t.Errorf("%s: wrote a different document:\n%s", name, document)
		}
	}

	if !slices.IsSortedFunc(ordered.Segments, func(a, b localprofile.Segment) int { return cmp.Compare(a.ID, b.ID) }) ||
		!slices.IsSortedFunc(ordered.Terminology, func(a, b localprofile.TerminologySet) int { return cmp.Compare(a.ID, b.ID) }) ||
		!slices.IsSortedFunc(ordered.Authorities, func(a, b localprofile.Authority) int { return cmp.Compare(a.ID, b.ID) }) ||
		!slices.IsSortedFunc(ordered.Dates, func(a, b localprofile.DateHandling) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Fatalf("the profile answered is not in the order its document is written in: %+v", ordered)
	}
	for _, segment := range ordered.Segments {
		if !slices.IsSortedFunc(segment.Fields, func(a, b localprofile.Field) int { return cmp.Compare(a.Position, b.Position) }) {
			t.Fatalf("the fields of %s are not in position order", segment.ID)
		}
	}

	// The document is one the reader accepts, and it is its own canonical form.
	again, err := localprofile.Decode(want)
	if err != nil {
		t.Fatalf("Canonical wrote a document its own reader refuses: %v", err)
	}
	if _, rewritten := canonical(t, again); string(rewritten) != string(want) {
		t.Fatal("writing a canonical document again produced different bytes")
	}
}

// Canonical changes nothing it was given: the profile it answers shares no
// slice with the one it was handed, so neither can reach into the other.
func TestCanonicalSharesNothingWithTheProfileItWasGiven(t *testing.T) {
	given := decoded(t)
	slices.Reverse(given.Segments)
	ordered, _ := canonical(t, given)
	if given.Segments[0].ID != "ZPD" {
		t.Fatal("Canonical reordered the profile it was given")
	}
	ordered.Segments[0].Fields[0].Name = "Rewritten from outside"
	ordered.Terminology[0].Codes[0].Code = "REWRITTEN"
	ordered.Segments[1].Fields[2].Condition.Values[0] = "REWRITTEN"
	ordered.Segments[0].Fields[0].Cardinality.Min = 7
	if given.Segments[1].Fields[0].Name == "Rewritten from outside" ||
		given.Terminology[0].Codes[0].Code == "REWRITTEN" ||
		given.Segments[0].Fields[2].Condition.Values[0] == "REWRITTEN" ||
		given.Segments[1].Fields[0].Cardinality.Min == 7 {
		t.Fatal("the profile answered shares a slice with the profile given")
	}
}

// Canonical writes only what a reader would read, so a profile assembled in Go
// that the contract refuses is never written, and neither is one that
// constrains nothing.
func TestCanonicalRefusesAProfileTheReaderWouldRefuse(t *testing.T) {
	refused := decoded(t)
	refused.Segments[1].Fields[4].Type = "ST"
	if _, document, err := localprofile.Canonical(refused); err == nil {
		t.Fatalf("wrote a profile the reader refuses:\n%s", document)
	}
	empty := decoded(t)
	empty.Segments = nil
	if _, document, err := localprofile.Canonical(empty); err == nil {
		t.Fatalf("wrote a profile that constrains no segment:\n%s", document)
	}
}
