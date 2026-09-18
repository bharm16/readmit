package grid_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

// saved is an independently authored document, written the way an operator
// would read it rather than produced by the encoder it is compared against.
const saved = `{"schema":"readmit-filters/v1","filters":[` +
	`{"name":"rejected acknowledgements","kinds":["ack"],"sources":["s0002"],` +
	`"observed_from":null,"observed_until":null,"ack_codes":["AE","AR"],` +
	`"fields":[{"selector":"PID[1]-3[1]","match":"contains","term":"MRN-","state":""}]},` +
	`{"name":"unbooked appointments","kinds":["message"],"sources":[],` +
	`"observed_from":"2026-01-01T12:00:00Z","observed_until":"2026-01-02T12:00:00Z",` +
	`"ack_codes":[],"fields":[{"selector":"PID[1]-7[1]","match":"state","term":"","state":"null"}]}],` +
	`"selected":"rejected acknowledgements"}`

func decoded(t *testing.T, document string) grid.Document {
	t.Helper()
	read, err := grid.Decode([]byte(document))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return read
}

func TestASavedFilterDocumentCarriesEveryAxisAndTheSelection(t *testing.T) {
	document := decoded(t, saved)
	if document.Schema != grid.Schema || len(document.Filters) != 2 || document.Selected != "rejected acknowledgements" {
		t.Fatalf("the document did not read back as written: %+v", document)
	}
	first := document.Filters[0]
	if len(first.Kinds) != 1 || first.Kinds[0] != bundle.Acknowledgement ||
		len(first.Sources) != 1 || first.Sources[0] != "s0002" ||
		first.ObservedFrom != nil || first.ObservedUntil != nil ||
		len(first.AckCodes) != 2 || len(first.Fields) != 1 {
		t.Fatalf("the first filter lost an axis: %+v", first)
	}
	if first.Fields[0].Selector != patient || first.Fields[0].Match != index.Contains || first.Fields[0].Term != "MRN-" {
		t.Fatalf("the value predicate did not read back: %+v", first.Fields[0])
	}
	second := document.Filters[1]
	if second.ObservedFrom == nil || !second.ObservedFrom.Equal(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("the declared time bound did not read back: %+v", second)
	}
	if second.Fields[0].Match != index.State || second.Fields[0].State != hl7.Null {
		t.Fatalf("the state predicate did not read back: %+v", second.Fields[0])
	}
	if selected := document.Find(document.Selected); selected == nil || selected.Name != first.Name {
		t.Fatalf("the selection does not name a saved filter: %+v", selected)
	}
	if document.Find("nothing saved under this name") != nil {
		t.Fatal("a name nothing saved was found")
	}
}

// What the reader accepts must survive being written back out and read again,
// or the document a person saved is not the document they get.
func TestASavedFilterDocumentRoundTrips(t *testing.T) {
	document := decoded(t, saved)
	encoded, err := grid.Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	again := decoded(t, string(encoded))
	if len(again.Filters) != len(document.Filters) || again.Selected != document.Selected {
		t.Fatalf("a written document read back as a different one: %+v", again)
	}
	repeated, err := grid.Encode(again)
	if err != nil || string(repeated) != string(encoded) {
		t.Fatalf("writing the same document twice produced different bytes: %v", err)
	}
	if empty := grid.Empty(); grid.Validate(empty) != nil || len(empty.Filters) != 0 || empty.Selected != "" {
		t.Fatalf("a viewer who has saved nothing has no valid document: %+v", empty)
	}
}

// Unknown members and unknown versions are errors, and an omitted declaration
// is one too: a filter that quietly decoded a missing axis into a permissive
// zero value would hide records nobody asked it to hide.
func TestASavedFilterDocumentRefusesUnknownMembersAndOmittedDeclarations(t *testing.T) {
	for name, document := range map[string]string{
		"unknown member":              strings.Replace(saved, `"selected":`, `"sorted":true,"selected":`, 1),
		"unknown member in a filter":  strings.Replace(saved, `"name":"rejected acknowledgements",`, `"name":"rejected acknowledgements","colour":"red",`, 1),
		"unknown member in a field":   strings.Replace(saved, `"selector":"PID[1]-3[1]",`, `"selector":"PID[1]-3[1]","ignore_case":true,`, 1),
		"omitted axis":                strings.Replace(saved, `"sources":["s0002"],`, "", 1),
		"omitted time bound":          strings.Replace(saved, `"observed_from":null,`, "", 1),
		"omitted predicate state":     strings.Replace(saved, `,"state":""`, "", 1),
		"omitted selection":           strings.Replace(saved, `,"selected":"rejected acknowledgements"`, "", 1),
		"selection nothing saved":     strings.Replace(saved, `"selected":"rejected acknowledgements"`, `"selected":"something else"`, 1),
		"two filters of one name":     strings.Replace(saved, `"name":"unbooked appointments"`, `"name":"rejected acknowledgements"`, 1),
		"an unnamed filter":           strings.Replace(saved, `"name":"unbooked appointments"`, `"name":""`, 1),
		"a predicate asking for both": strings.Replace(saved, `"term":"MRN-","state":""`, `"term":"MRN-","state":"present"`, 1),
		"a predicate asking neither":  strings.Replace(saved, `"term":"MRN-","state":""`, `"term":"","state":""`, 1),
		"an unmatched predicate":      strings.Replace(saved, `"match":"contains"`, `"match":"resembles"`, 1),
		"a non-canonical selector":    strings.Replace(saved, `"selector":"PID[1]-3[1]"`, `"selector":"PID-3"`, 1),
		"an unknown occurrence type":  strings.Replace(saved, `"kinds":["ack"]`, `"kinds":["acknowledgement"]`, 1),
		"a repeated occurrence type":  strings.Replace(saved, `"kinds":["ack"]`, `"kinds":["ack","ack"]`, 1),
		"a time filter ending first":  strings.Replace(saved, `"observed_until":"2026-01-02T12:00:00Z"`, `"observed_until":"2025-01-02T12:00:00Z"`, 1),
		"a control character":         strings.Replace(saved, `"term":"MRN-"`, `"term":"MRN-\u0007"`, 1),
		"not an object":               `[]`,
		"not JSON at all":             `{`,
	} {
		if _, err := grid.Decode([]byte(document)); err == nil {
			t.Fatalf("the reader accepted %s", name)
		}
	}
}

// A version this release does not read is named as such rather than reported as
// an invalid document: there is no migration and no repair, and the two
// refusals have different remedies.
func TestAnUnreadableVersionIsNamedRatherThanCalledInvalid(t *testing.T) {
	later := strings.Replace(saved, grid.Schema, "readmit-filters/v2", 1)
	if _, err := grid.Decode([]byte(later)); !errors.Is(err, grid.ErrUnsupportedVersion) {
		t.Fatalf("a later contract version was reported as %v", err)
	}
	if _, err := grid.Decode([]byte(strings.Replace(saved, grid.Schema, "readmit-index/v1", 1))); !errors.Is(err, grid.ErrUnsupportedVersion) {
		t.Fatal("another readmit contract was read as saved filters")
	}
}

func TestASavedFilterDocumentIsBounded(t *testing.T) {
	document := grid.Empty()
	for i := range grid.MaxFilters + 1 {
		document.Filters = append(document.Filters, grid.Filter{Name: "filter " + string(rune('a'+i%26)) + strings.Repeat("x", i)})
	}
	if err := grid.Validate(document); err == nil {
		t.Fatal("a viewer saved more filters than this release stores")
	}
	if _, err := grid.Decode([]byte(strings.Repeat(" ", grid.MaxDocumentBytes+1))); err == nil {
		t.Fatal("an oversized document was read")
	}
	long := grid.Filter{Name: "long",
		Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Equals, Term: strings.Repeat("A", grid.MaxTermBytes+1)}}}
	if err := grid.ValidateFilter(long); err == nil {
		t.Fatal("a filter looked for more bytes than an index retains")
	}
	if err := grid.ValidateFilter(grid.Filter{Name: strings.Repeat("n", grid.MaxNameBytes+1)}); err == nil {
		t.Fatal("a filter was named past the bound")
	}
}

// A saved filter is read from a file a person can edit. Any bytes at all must
// produce a document this release can also write back and use, or be refused
// with a bounded diagnostic that names no term.
func FuzzFilterDocument(f *testing.F) {
	f.Add([]byte(saved))
	f.Add([]byte(`{"schema":"readmit-filters/v1","filters":[],"selected":""}`))
	f.Add([]byte(`{"schema":"readmit-filters/v2"}`))
	f.Add([]byte(`{"schema":"readmit-filters/v1","filters":[{"name":"n","kinds":[],"sources":[],` +
		`"observed_from":null,"observed_until":null,"ack_codes":[],"fields":[]}],"selected":"n"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		document, err := grid.Decode(data)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if document.Schema != grid.Schema {
			t.Fatalf("a document without this contract version was accepted: %q", document.Schema)
		}
		if document.Selected != "" && document.Find(document.Selected) == nil {
			t.Fatal("a selection naming no saved filter was accepted")
		}
		encoded, err := grid.Encode(document)
		if err != nil {
			t.Fatal("accepted a document that cannot be written back")
		}
		again, err := grid.Decode(encoded)
		if err != nil || len(again.Filters) != len(document.Filters) || again.Selected != document.Selected {
			t.Fatalf("a written document did not read back: %v", err)
		}
		// Every filter this reader accepts can be applied to an index without
		// reading past what that index retains: it either answers, or refuses
		// by name with a bounded diagnostic that repeats no term.
		empty := index.Document{Schema: index.Schema, Records: []index.Record{}}
		for i := range document.Filters {
			selected, err := grid.Select(empty, now(), &document.Filters[i], whole())
			if err != nil {
				if len(err.Error()) > 256 {
					t.Fatal("unbounded diagnostic")
				}
				continue
			}
			if selected.Total != 0 || selected.Matched != 0 || selected.Excluded != 0 || len(selected.Rows) != 0 {
				t.Fatalf("an index of nothing answered with something: %+v", selected)
			}
		}
	})
}
