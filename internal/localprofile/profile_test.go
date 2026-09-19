package localprofile_test

import (
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
)

const fixtureRoot = "../../testdata/fixtures/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureRoot + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// edit decodes the positive fixture loosely, lets the test reshape it, and
// encodes it again, for refusals a single textual edit cannot express.
func edit(t *testing.T, reshape func(document map[string]any)) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(fixture(t, "local-profile.json"), &document); err != nil {
		t.Fatalf("decode fixture loosely: %v", err)
	}
	reshape(document)
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode edited fixture: %v", err)
	}
	return data
}

// mutate applies one textual edit to the positive fixture and fails the test if
// the edit did not land, so a refusal is never proven against unchanged bytes.
func mutate(t *testing.T, old, new string) []byte {
	t.Helper()
	source := string(fixture(t, "local-profile.json"))
	if !strings.Contains(source, old) {
		t.Fatalf("the fixture does not contain %q", old)
	}
	return []byte(strings.Replace(source, old, new, 1))
}

// segment reaches one segment of a loosely decoded document by identifier, so
// a reshaping test does not depend on the order the fixture happens to be in.
func segment(t *testing.T, document map[string]any, id string) map[string]any {
	t.Helper()
	for _, entry := range document["segments"].([]any) {
		found := entry.(map[string]any)
		if found["id"] == id {
			return found
		}
	}
	t.Fatalf("the fixture does not constrain %s", id)
	return nil
}

func field(t *testing.T, document map[string]any, id string, position float64) map[string]any {
	t.Helper()
	for _, entry := range segment(t, document, id)["fields"].([]any) {
		found := entry.(map[string]any)
		if found["position"] == position {
			return found
		}
	}
	t.Fatalf("the fixture does not constrain %s-%v", id, position)
	return nil
}

func decoded(t *testing.T) localprofile.Profile {
	t.Helper()
	profile, err := localprofile.Decode(fixture(t, "local-profile.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return profile
}

func TestDecodeReadsTheFixtureAsWritten(t *testing.T) {
	profile := decoded(t)
	if profile.Schema != localprofile.Schema {
		t.Fatalf("decoded schema %q", profile.Schema)
	}
	if profile.Identity != (localprofile.Identity{ID: "fixture-local-siu", Version: "1"}) {
		t.Fatalf("identity %+v", profile.Identity)
	}
	if profile.Base.Pack != (profilepack.Identity{ID: "fixture-siu", Version: "1"}) ||
		profile.Base.HL7Version != "2.5.1" || profile.Base.Family != "SIU" {
		t.Fatalf("base %+v", profile.Base)
	}
	if len(profile.Terminology) != 1 || len(profile.Authorities) != 1 || len(profile.Dates) != 1 {
		t.Fatalf("declarations %d/%d/%d", len(profile.Terminology), len(profile.Authorities), len(profile.Dates))
	}
	if len(profile.Segments) != 2 || profile.Segments[0].ID != "SCH" || profile.Segments[1].ID != "ZPD" {
		t.Fatalf("segments %+v", profile.Segments)
	}
	if profile.Segments[0].SiteDefined() || !profile.Segments[1].SiteDefined() {
		t.Fatal("a Z-segment and a standard segment are not told apart")
	}
	// Every rule kind the required delivery names is present in the fixture,
	// so the refusals below are proven against a profile that exercises all
	// of them rather than a minimal one.
	reason := profile.Segments[1].Fields[2]
	if reason.Usage != localprofile.UsageConditional || reason.Condition == nil ||
		reason.Condition.Operator != localprofile.ConditionValueIn || reason.Terminology != "local-visit-reason" {
		t.Fatalf("the conditional coded field is %+v", reason)
	}
	if maximum, bounded := (localprofile.Cardinality{Min: 0, Max: localprofile.Unbounded}).Bounded(); bounded {
		t.Fatalf("an unbounded cardinality reported the maximum %d", maximum)
	}
	if maximum, bounded := (localprofile.Cardinality{Min: 1, Max: "3"}).Bounded(); !bounded || maximum != 3 {
		t.Fatalf("a bounded cardinality reported %d %v", maximum, bounded)
	}
}

// TestDecodeRefusesWhatItCannotStandBehind is the contract read as refusals.
// Every case is one edit of the positive fixture, so each proves exactly the
// rule it names and nothing else.
func TestDecodeRefusesWhatItCannotStandBehind(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"a contract this release does not read", mutate(t, "readmit-local-profile/v1", "readmit-local-profile/v2")},
		{"an unknown top-level member", edit(t, func(document map[string]any) {
			document["command"] = "sh -c true"
		})},
		{"an unknown member of a nested object", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 1)["cardinality"].(map[string]any)["exactly"] = float64(1)
		})},
		{"an HL7 version outside the sets a pack may cover", mutate(t, `"hl7_version": "2.5.1"`, `"hl7_version": "2.9"`)},
		{"a message family outside the sets a pack may cover", mutate(t, `"family": "SIU"`, `"family": "XYZ"`)},
		{"a profile id that is not an identifier", mutate(t, `"id": "fixture-local-siu"`, `"id": "1-local"`)},
		{"a pinned pack with no version", mutate(t, `"id": "fixture-siu",
      "version": "1"`, `"id": "fixture-siu",
      "version": ""`)},
		{"no segment at all", edit(t, func(document map[string]any) {
			document["segments"] = []any{}
		})},
		{"the same segment constrained twice", edit(t, func(document map[string]any) {
			document["segments"] = append(document["segments"].([]any), segment(t, document, "ZPD"))
		})},
		{"a segment id the parser would never produce", mutate(t, `"id": "ZPD"`, `"id": "ZPDX"`)},
		{"the same position constrained twice", edit(t, func(document map[string]any) {
			found := segment(t, document, "ZPD")
			found["fields"] = append(found["fields"].([]any), field(t, document, "ZPD", 1))
		})},
		{"a segment that constrains no position", edit(t, func(document map[string]any) {
			segment(t, document, "ZPD")["fields"] = []any{}
		})},
		{"a usage outside the conformance vocabulary", mutate(t, `"usage": "RE"`, `"usage": "MAY"`)},
		{"a required field that may be absent", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 1)["cardinality"].(map[string]any)["min"] = float64(0)
		})},
		{"a field that is not required and still repeats at least once", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 4)["cardinality"].(map[string]any)["min"] = float64(1)
		})},
		{"a cardinality maximum below its minimum", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 4)["cardinality"].(map[string]any)["max"] = "0"
		})},
		{"a cardinality maximum that is not a count", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 4)["cardinality"].(map[string]any)["max"] = "many"
		})},
		{"a conditional field with no condition", edit(t, func(document map[string]any) {
			delete(field(t, document, "ZPD", 3), "condition")
		})},
		{"a condition on a field that is not conditional", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 4)["condition"] = map[string]any{
				"segment": "SCH", "position": float64(25), "operator": "present",
			}
		})},
		{"a field whose condition is itself", edit(t, func(document map[string]any) {
			condition := field(t, document, "ZPD", 3)["condition"].(map[string]any)
			condition["segment"], condition["position"], condition["operator"] = "ZPD", float64(3), "present"
			delete(condition, "values")
		})},
		{"a condition operator outside the typed set", mutate(t, `"operator": "value_in"`, `"operator": "matches"`)},
		{"a value_in condition that tests nothing", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 3)["condition"].(map[string]any)["values"] = []any{}
		})},
		{"a present condition that carries values", edit(t, func(document map[string]any) {
			field(t, document, "SCH", 11)["condition"].(map[string]any)["values"] = []any{"BOOKED"}
		})},
		{"a field type outside the closed set", mutate(t, `"type": "SI"`, `"type": "ZZ"`)},
		{"a reference to a terminology set the profile does not declare", mutate(t, `"terminology": "local-visit-reason"`, `"terminology": "other-reasons"`)},
		{"a code table bound to a field that carries no code", fixture(t, "local-profile-refused.json")},
		{"an assigning authority on a field that carries no identifier", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 2)["type"] = "ST"
		})},
		{"a date rule on a field that carries no date", edit(t, func(document map[string]any) {
			field(t, document, "SCH", 11)["type"] = "ST"
		})},
		{"a binding with no data type to carry it", edit(t, func(document map[string]any) {
			delete(field(t, document, "ZPD", 3), "type")
		})},
		{"a field that is not supported and is still constrained", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 5)["type"] = "ST"
		})},
		{"the same terminology set declared twice", edit(t, func(document map[string]any) {
			document["terminology"] = append(document["terminology"].([]any), document["terminology"].([]any)[0])
		})},
		{"the same code declared twice in one set", edit(t, func(document map[string]any) {
			set := document["terminology"].([]any)[0].(map[string]any)
			set["codes"] = append(set["codes"].([]any), set["codes"].([]any)[0])
		})},
		{"a terminology binding outside the closed set", mutate(t, `"binding": "required"`, `"binding": "mandatory"`)},
		{"an authority that names nothing", edit(t, func(document map[string]any) {
			authority := document["authorities"].([]any)[0].(map[string]any)
			delete(authority, "namespace")
			delete(authority, "universal_id")
			delete(authority, "universal_id_type")
		})},
		{"a universal id with no type it is in", edit(t, func(document map[string]any) {
			delete(document["authorities"].([]any)[0].(map[string]any), "universal_id_type")
		})},
		{"a universal id type outside HL7 table 0301", mutate(t, `"universal_id_type": "ISO"`, `"universal_id_type": "LOCAL"`)},
		{"a date precision outside the closed set", mutate(t, `"precision": "minute"`, `"precision": "week"`)},
		{"a time zone required of a value with no time of day", mutate(t, `"precision": "minute"`, `"precision": "day"`)},
		{"a control character in a name a person reads", edit(t, func(document map[string]any) {
			field(t, document, "ZPD", 3)["name"] = "Localvisit reason"
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if profile, err := localprofile.Decode(tc.data); err == nil {
				t.Fatalf("decoded %+v", profile)
			}
		})
	}
}

func TestDecodeRefusesADocumentPastItsSizeLimit(t *testing.T) {
	oversized := make([]byte, localprofile.MaxProfileBytes+1)
	for i := range oversized {
		oversized[i] = ' '
	}
	if _, err := localprofile.Decode(oversized); err == nil {
		t.Fatal("decoded a document past the 4 MiB limit")
	}
}

// A profile assembled in Go is held to exactly what a profile read from a file
// is held to, so the refusals above cannot be walked around by construction.
func TestValidateHoldsAnAssembledProfileToTheSameContract(t *testing.T) {
	profile := decoded(t)
	if err := profile.Validate(); err != nil {
		t.Fatalf("the fixture does not validate: %v", err)
	}
	profile.Segments[1].Fields[4].Type = "ST"
	if err := profile.Validate(); err == nil {
		t.Fatal("validated a field that is not supported and is still constrained")
	}
}

// The closed sets an editor offers are the closed sets the reader accepts, so
// an editor cannot offer a choice the reader would refuse.
func TestTheOfferedChoicesAreTheAcceptedChoices(t *testing.T) {
	for _, usage := range localprofile.Usages() {
		profile := decoded(t)
		profile.Segments[1] = localprofile.Segment{
			ID:     "ZPD",
			Fields: []localprofile.Field{{Position: 9, Usage: usage}},
		}
		if usage == localprofile.UsageConditional {
			profile.Segments[1].Fields[0].Condition = &localprofile.Condition{
				Segment: "SCH", Position: 25, Operator: localprofile.ConditionPresent,
			}
		}
		if err := profile.Validate(); err != nil {
			t.Errorf("the usage %q an editor offers is refused: %v", usage, err)
		}
	}
	for _, precision := range localprofile.Precisions() {
		profile := decoded(t)
		profile.Dates[0].Precision = precision
		profile.Dates[0].TimeZone = localprofile.TimeZoneOptional
		if err := profile.Validate(); err != nil {
			t.Errorf("the precision %q an editor offers is refused: %v", precision, err)
		}
	}
	if len(localprofile.DataTypes()) == 0 || len(localprofile.UniversalIDTypes()) == 0 {
		t.Fatal("an editor is offered no data type or authority type at all")
	}
	if len(localprofile.ConditionOperators()) != 3 || len(localprofile.Precisions()) != 7 ||
		len(localprofile.TimeZoneRules()) != 3 || len(localprofile.Bindings()) != 2 {
		t.Fatal("the offered predicate, precision, time zone and binding sets are not the documented ones")
	}
}
