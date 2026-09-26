package profilepack_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
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
	if err := json.Unmarshal(fixture(t, "profile-pack.json"), &document); err != nil {
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
	source := string(fixture(t, "profile-pack.json"))
	if !strings.Contains(source, old) {
		t.Fatalf("the fixture does not contain %q", old)
	}
	return []byte(strings.Replace(source, old, new, 1))
}

func TestDecodeReadsTheFixtureAsWritten(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pack.Schema != profilepack.Schema {
		t.Fatalf("decoded schema %q", pack.Schema)
	}
	if got := pack.Identity; got != (profilepack.Identity{ID: "fixture-siu", Version: "1"}) {
		t.Fatalf("identity %+v", got)
	}
	if pack.Provenance.Source.Revision != "1" || pack.Provenance.License.SPDX != "LicenseRef-readmit-fixture" ||
		pack.Provenance.RightsReview.Status != profilepack.ReviewApproved {
		t.Fatalf("provenance %+v", pack.Provenance)
	}
	if len(pack.Coverage) != 3 || len(pack.Labels) != 1 {
		t.Fatalf("coverage %d labels %d", len(pack.Coverage), len(pack.Labels))
	}
}

// TestSupportIsOneAnswerPerLevelAndCombination is the ADR-0009 rule read as a
// query test: the four levels are separate answers, a combination the pack
// never declared is unknown rather than inherited from a neighbour, and only a
// declared "supported" passes.
func TestSupportIsOneAnswerPerLevelAndCombination(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tc := range []struct {
		version, family string
		level           profilepack.Level
		want            profilepack.Outcome
	}{
		{"2.5.1", "SIU", profilepack.LevelParse, profilepack.OutcomeSupported},
		{"2.5.1", "SIU", profilepack.LevelLabels, profilepack.OutcomeSupported},
		{"2.5.1", "SIU", profilepack.LevelStructural, profilepack.OutcomeUnsupported},
		{"2.5.1", "SIU", profilepack.LevelWorkflow, profilepack.OutcomeUnsupported},
		// The same version under another family answers for itself.
		{"2.5.1", "ADT", profilepack.LevelParse, profilepack.OutcomeSupported},
		{"2.5.1", "ADT", profilepack.LevelLabels, profilepack.OutcomeUntested},
		// The same family under another version answers for itself.
		{"2.4", "SIU", profilepack.LevelParse, profilepack.OutcomeUntested},
		{"2.4", "SIU", profilepack.LevelLabels, profilepack.OutcomeUnsupported},
		// Combinations the pack never declared, and names outside the closed
		// sets, are unknown; nothing about a neighbour is borrowed.
		{"2.5.1", "ORU", profilepack.LevelParse, profilepack.OutcomeUnknown},
		{"2.8.2", "SIU", profilepack.LevelParse, profilepack.OutcomeUnknown},
		{"2.5", "SIU", profilepack.LevelLabels, profilepack.OutcomeUnknown},
		{"2.5.1", "siu", profilepack.LevelParse, profilepack.OutcomeUnknown},
		{"2.5.1", "SIU", profilepack.Level("semantic"), profilepack.OutcomeUnknown},
		{"", "", profilepack.LevelParse, profilepack.OutcomeUnknown},
	} {
		got := pack.Support(tc.version, tc.family, tc.level)
		if got != tc.want {
			t.Errorf("Support(%q, %q, %q) = %q, want %q", tc.version, tc.family, tc.level, got, tc.want)
		}
		if got.Passing() != (tc.want == profilepack.OutcomeSupported) {
			t.Errorf("Support(%q, %q, %q).Passing() = %v for %q", tc.version, tc.family, tc.level, got.Passing(), got)
		}
	}
}

// TestOutcomesIsTheFourLevelsAnsweredAtOnce holds the one answer about a
// combination to the four answers Support gives one level at a time, across
// every combination of the closed sets and some outside them, and Covered to
// whether the pack declares the combination at all.
func TestOutcomesIsTheFourLevelsAnsweredAtOnce(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	declared := map[dictionary.Combination]bool{}
	for _, coverage := range pack.Coverage {
		declared[dictionary.Combination{Version: coverage.HL7Version, Family: coverage.Family}] = true
	}
	combinations := []dictionary.Combination{{Version: "2.9", Family: "SIU"}, {Version: "2.5.1", Family: "siu"}, {}}
	for _, version := range profilepack.HL7Versions() {
		for _, family := range profilepack.Families() {
			combinations = append(combinations, dictionary.Combination{Version: version, Family: family})
		}
	}
	for _, combination := range combinations {
		got := pack.Outcomes(combination.Version, combination.Family)
		want := profilepack.Outcomes{
			Parse:      pack.Support(combination.Version, combination.Family, profilepack.LevelParse),
			Labels:     pack.Support(combination.Version, combination.Family, profilepack.LevelLabels),
			Structural: pack.Support(combination.Version, combination.Family, profilepack.LevelStructural),
			Workflow:   pack.Support(combination.Version, combination.Family, profilepack.LevelWorkflow),
		}
		if got != want {
			t.Errorf("Outcomes(%q, %q) = %+v, want %+v", combination.Version, combination.Family, got, want)
		}
		if got.Covered() != declared[combination] {
			t.Errorf("Outcomes(%q, %q).Covered() = %v", combination.Version, combination.Family, got.Covered())
		}
	}
	if got := pack.Outcomes("2.5.1", "SIU"); got != (profilepack.Outcomes{Parse: profilepack.OutcomeSupported, Labels: profilepack.OutcomeSupported,
		Structural: profilepack.OutcomeUnsupported, Workflow: profilepack.OutcomeUnsupported}) {
		t.Fatalf("the fixture's own combination: %+v", got)
	}
	unknown := profilepack.Outcomes{Parse: profilepack.OutcomeUnknown, Labels: profilepack.OutcomeUnknown,
		Structural: profilepack.OutcomeUnknown, Workflow: profilepack.OutcomeUnknown}
	if got := (profilepack.Pack{}).Outcomes("2.5.1", "SIU"); got != unknown || got.Covered() {
		t.Fatalf("a pack nothing decoded answered %+v", got)
	}
	if (profilepack.Outcomes{}).Covered() {
		t.Fatal("outcomes no pack answered cover a combination")
	}
}

func TestLabelIsReturnedOnlyUnderASupportedLabelsCombination(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := pack.Label("2.5.1", "SIU", "SCH", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeSupported, Name: "Placer Appointment ID"}) {
		t.Fatalf("labelled position: %+v", got)
	}
	// A supported combination with no name at that position is still
	// supported: the pack knows the version and says nothing about MSH-4.
	if got := pack.Label("2.5.1", "SIU", "MSH", 4); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeSupported}) {
		t.Fatalf("unlabelled position: %+v", got)
	}
	if got := pack.Label("2.5.1", "SIU", "ZPD", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeSupported}) {
		t.Fatalf("unlabelled segment: %+v", got)
	}
	// The 2.5.1 labels content exists, and an ADT message may not use it:
	// labels for that combination are untested, so the name is withheld.
	if got := pack.Label("2.5.1", "ADT", "SCH", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUntested}) {
		t.Fatalf("untested combination leaked a label: %+v", got)
	}
	if got := pack.Label("2.4", "SIU", "SCH", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUnsupported}) {
		t.Fatalf("unsupported combination leaked a label: %+v", got)
	}
	if got := pack.Label("2.5.1", "ORU", "SCH", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUnknown}) {
		t.Fatalf("undeclared combination leaked a label: %+v", got)
	}
	if got := pack.Label("2.5.1", "SIU", "SCH", 0); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeSupported}) {
		t.Fatalf("position zero: %+v", got)
	}
}

// TestDeclaredReadsTheCombinationFromTheParsedMessage is the parser
// compatibility test: a pack answers about the version and family a message
// declares in MSH-12 and MSH-9 as the byte-preserving parser exposes them, the
// bytes are never rewritten, and a message whose declaration the pack does
// not cover gets an unknown answer rather than a guess.
func TestDeclaredReadsTheCombinationFromTheParsedMessage(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tc := range []struct {
		name    string
		version string
		family  string
		labels  profilepack.Outcome
	}{
		{"siu-lf.hl7", "2.5.1", "SIU", profilepack.OutcomeSupported},
		{"adt-cr.hl7", "2.5.1", "ADT", profilepack.OutcomeUntested},
		{"ack-crlf.hl7", "2.5.1", "ACK", profilepack.OutcomeUnknown},
	} {
		source := fixture(t, tc.name)
		doc, err := hl7.Parse(source, hl7.Options{})
		if err != nil {
			t.Fatalf("parse %s: %v", tc.name, err)
		}
		declared := dictionary.Declared(doc, 0)
		if declared != (dictionary.Combination{Version: tc.version, Family: tc.family}) {
			t.Fatalf("%s declared %+v", tc.name, declared)
		}
		if got := pack.Support(declared.Version, declared.Family, profilepack.LevelLabels); got != tc.labels {
			t.Fatalf("%s labels support %q, want %q", tc.name, got, tc.labels)
		}
		if string(doc.Serialize()) != string(source) {
			t.Fatalf("%s: consulting the pack changed the evidence bytes", tc.name)
		}
	}
	// A message declaring a version and type with components and repetitions
	// is read at its first component, exactly as the inspector selects MSH-12.1.
	doc, err := hl7.Parse([]byte("MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12^SIU_S12|X|P|2.5.1^USA~2.4\rSCH|1\r"), hl7.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := dictionary.Declared(doc, 0); got != (dictionary.Combination{Version: "2.5.1", Family: "SIU"}) {
		t.Fatalf("componentised declaration %+v", got)
	}
	// An omitted MSH-9 or MSH-12, or a message index outside the document,
	// declares nothing; nothing is inferred from the segments present.
	doc, err = hl7.Parse([]byte("MSH|^~\\&|A|B|C|D|20260101000000\rSCH|1\r"), hl7.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := dictionary.Declared(doc, 0); got != (dictionary.Combination{}) {
		t.Fatalf("omitted declaration %+v", got)
	}
	if got := dictionary.Declared(doc, 1); got != (dictionary.Combination{}) {
		t.Fatalf("absent message %+v", got)
	}
	if got := pack.Support("", "", profilepack.LevelParse); got != profilepack.OutcomeUnknown {
		t.Fatalf("an undeclared message must be unknown, got %q", got)
	}
}

// TestExistingLabelsAreDescribableWithoutChanging shows how a pack would
// describe the bundled readmit-field-labels/v1 dictionary: the pack is built in
// this test from the dictionary the engine already ships, the dictionary
// itself is untouched, and every label the dictionary answers today the pack
// answers identically for the one version it covers.
func TestExistingLabelsAreDescribableWithoutChanging(t *testing.T) {
	labels, err := dictionary.Load()
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	if labels.Contract != "readmit-field-labels/v1" || labels.HL7Version != "2.5.1" {
		t.Fatalf("the bundled dictionary changed: %q %q", labels.Contract, labels.HL7Version)
	}
	source, err := os.ReadFile("../dictionary/fields-v251.json")
	if err != nil {
		t.Fatalf("read dictionary source: %v", err)
	}
	digest := sha256.Sum256(source)
	segments := map[string]map[int]string{}
	labels.Positions(func(segment string, position int, name string) {
		if segments[segment] == nil {
			segments[segment] = map[int]string{}
		}
		segments[segment][position] = name
	})
	var coverage []map[string]string
	for _, family := range profilepack.Families() {
		coverage = append(coverage, map[string]string{
			"hl7_version": "2.5.1", "family": family,
			"parse": "supported", "labels": "supported",
			"structural": "unsupported", "workflow": "unsupported",
		})
	}
	document, err := json.Marshal(map[string]any{
		"schema": profilepack.Schema,
		"pack":   map[string]string{"id": "nhapi-labels-v251", "version": "1"},
		"provenance": map[string]any{
			"source": map[string]string{
				"name":     "nHapi NHapi.Model.V251 segment classes",
				"location": "https://github.com/nHapiNET/nHapi",
				"revision": "2495edd1e23a85ab9146cb03947c17d45120cf1f",
			},
			"extraction": map[string]string{
				"method":         "Numbered field labels only, from each segment class's field-list documentation; format changed to JSON.",
				"content_digest": "sha256:" + hex.EncodeToString(digest[:]),
			},
			"license":       map[string]string{"spdx": "MPL-2.0", "notice": "licenses/nhapi-MPL-2.0.txt"},
			"rights_review": map[string]string{"status": "approved", "reference": "docs/dictionary-provenance.md"},
		},
		"coverage": coverage,
		"labels":   []map[string]any{{"hl7_version": "2.5.1", "segments": segments}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	pack, err := profilepack.Decode(document)
	if err != nil {
		t.Fatalf("a pack describing the bundled dictionary must decode: %v", err)
	}
	labels.Positions(func(segment string, position int, name string) {
		for _, family := range profilepack.Families() {
			if got := pack.Label("2.5.1", family, segment, position); got.Name != name || got.Outcome != profilepack.OutcomeSupported {
				t.Fatalf("%s-%d under %s: %+v, want %q", segment, position, family, got, name)
			}
		}
		if got := pack.Label("2.4", "SIU", segment, position); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUnknown}) {
			t.Fatalf("%s-%d leaked into 2.4: %+v", segment, position, got)
		}
	})
	if got := pack.Support("2.5.1", "SIU", profilepack.LevelStructural); got.Passing() {
		t.Fatal("a label dictionary does not certify structure")
	}
	// The dictionary's own reader is unchanged by any of this, and every
	// caller is answered from the one parse.
	again, err := dictionary.Load()
	if err != nil || again != labels {
		t.Fatalf("dictionary after describing it: %v", err)
	}
}

// TestAPackNotProducedByDecodeAnswersUnknown: the reader's refusals cannot be
// bypassed by assembling a Pack in Go. Every exported field is set to what a
// decoded pack could never carry, and every answer is still unknown.
func TestAPackNotProducedByDecodeAnswersUnknown(t *testing.T) {
	assembled := profilepack.Pack{
		Schema:   profilepack.Schema,
		Identity: profilepack.Identity{ID: "assembled", Version: "1"},
		Coverage: []profilepack.Coverage{{HL7Version: "2.5.1", Family: "SIU",
			Parse: profilepack.Supported, Labels: profilepack.Supported, Structural: profilepack.Supported, Workflow: profilepack.Supported}},
		Labels: []profilepack.Labels{{HL7Version: "2.5.1", Segments: map[string]map[int]string{"SCH": {1: "Placer Appointment ID"}}}},
	}
	for _, level := range []profilepack.Level{profilepack.LevelParse, profilepack.LevelLabels, profilepack.LevelStructural, profilepack.LevelWorkflow} {
		if got := assembled.Support("2.5.1", "SIU", level); got != profilepack.OutcomeUnknown {
			t.Fatalf("an assembled pack answered %q for %q", got, level)
		}
	}
	if got := assembled.Label("2.5.1", "SIU", "SCH", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUnknown}) {
		t.Fatalf("an assembled pack answered %+v", got)
	}
	if err := assembled.Satisfies(assembled.Identity); err == nil {
		t.Fatal("an assembled pack satisfied a pin")
	}
}

func TestSatisfiesPinsExactIdentityAndVersion(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := pack.Satisfies(profilepack.Identity{ID: "fixture-siu", Version: "1"}); err != nil {
		t.Fatalf("exact pin refused: %v", err)
	}
	if err := pack.Satisfies(profilepack.Identity{ID: "fixture-adt", Version: "9"}); err == nil || strings.Contains(err.Error(), "fixture") {
		t.Fatalf("a mismatch repeats an identity or passes: %v", err)
	}
	for name, pin := range map[string]profilepack.Identity{
		"other id":         {ID: "fixture-adt", Version: "1"},
		"other version":    {ID: "fixture-siu", Version: "2"},
		"empty pin":        {},
		"case differs":     {ID: "Fixture-SIU", Version: "1"},
		"version prefixed": {ID: "fixture-siu", Version: "1.0"},
	} {
		if err := pack.Satisfies(pin); err == nil {
			t.Errorf("%s: pin %+v accepted by %+v", name, pin, pack.Identity)
		}
	}
}

func TestClosedSetsAreExactlyTheDecidedOnes(t *testing.T) {
	if got := strings.Join(profilepack.HL7Versions(), " "); got != "2.3.1 2.4 2.5 2.5.1 2.6 2.7.1 2.8.2" {
		t.Fatalf("versions %q", got)
	}
	if got := strings.Join(profilepack.Families(), " "); got != "ADT SIU ORM ORU" {
		t.Fatalf("families %q", got)
	}
	declared := profilepack.Levels()
	if len(declared) != 4 || declared[0] != profilepack.LevelParse || declared[1] != profilepack.LevelLabels ||
		declared[2] != profilepack.LevelStructural || declared[3] != profilepack.LevelWorkflow {
		t.Fatalf("levels %q", declared)
	}
	declared[0] = "semantic"
	if profilepack.Levels()[0] != profilepack.LevelParse {
		t.Fatal("a caller could edit the closed set")
	}
	versions := profilepack.HL7Versions()
	versions[0] = "9.9"
	if profilepack.HL7Versions()[0] != "2.3.1" {
		t.Fatal("a caller could edit the closed set")
	}
}

// TestDecodeRefusesWhatItCannotStandBehind is the reader's negative path: every
// refusal the contract documents, each proven against a single edit of the
// fixture that decodes as written.
func TestDecodeRefusesWhatItCannotStandBehind(t *testing.T) {
	structuralClaim := `"structural": "supported",
      "workflow": "unsupported"`
	cases := map[string][]byte{
		"negative fixture claims workflow": fixture(t, "profile-pack-refused.json"),
		"not json":                         []byte("{"),
		"empty":                            []byte(""),
		"later contract":                   mutate(t, "readmit-profile-pack/v1", "readmit-profile-pack/v2"),
		"another contract":                 mutate(t, "readmit-profile-pack/v1", "readmit-field-labels/v1"),
		"unknown top-level member":         mutate(t, `"coverage": [`, `"script": "extract.py", "coverage": [`),
		"unknown pack member": mutate(t, `"version": "1"
  },`, `"version": "1", "channel": "beta"
  },`),
		"unknown coverage member": mutate(t, `"workflow": "unsupported"
    },
    {
      "hl7_version": "2.5.1",
      "family": "ADT"`, `"workflow": "unsupported", "semantic": "supported"
    },
    {
      "hl7_version": "2.5.1",
      "family": "ADT"`),
		"unknown provenance member": mutate(t, `"license": {`, `"copied_from": "upstream", "license": {`),
		"unknown labels member": mutate(t, `"hl7_version": "2.5.1",
      "segments"`, `"hl7_version": "2.5.1", "datatypes": {},
      "segments"`),
		"command member": mutate(t, `"coverage": [`, `"command": "sh -c true", "coverage": [`),
		"missing pack": mutate(t, `"pack": {
    "id": "fixture-siu",
    "version": "1"
  },`, ``),
		"missing provenance": mutate(t, `"source": {
      "name": "readmit fixture authors",
      "location": "testdata/fixtures/profile-pack.json",
      "revision": "1"
    },`, ``),
		"missing coverage member": mutate(t, `"parse": "untested",
      "labels": "unsupported",`, `"labels": "unsupported",`),
		"missing labels member": mutate(t, `"hl7_version": "2.5.1",
      "segments"`, `"segments"`),
		"empty id":               mutate(t, `"id": "fixture-siu"`, `"id": ""`),
		"uppercase id":           mutate(t, `"id": "fixture-siu"`, `"id": "Fixture-SIU"`),
		"id starting with digit": mutate(t, `"id": "fixture-siu"`, `"id": "1fixture"`),
		"empty version": mutate(t, `"version": "1"
  },`, `"version": ""
  },`),
		"version not starting with digit": mutate(t, `"version": "1"
  },`, `"version": "v1"
  },`),
		"empty source name":          mutate(t, `"name": "readmit fixture authors"`, `"name": ""`),
		"empty revision":             mutate(t, `"revision": "1"`, `"revision": ""`),
		"empty extraction method":    mutate(t, `"method": "Hand-authored field names for the fixture SIU segments; nothing was extracted from an upstream library."`, `"method": ""`),
		"digest without algorithm":   mutate(t, `sha256:0000000000000000000000000000000000000000000000000000000000000000`, `0000000000000000000000000000000000000000000000000000000000000000`),
		"digest too short":           mutate(t, `sha256:0000000000000000000000000000000000000000000000000000000000000000`, `sha256:00`),
		"digest uppercase":           mutate(t, `sha256:0000000000000000000000000000000000000000000000000000000000000000`, `sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA`),
		"empty spdx":                 mutate(t, `"spdx": "LicenseRef-readmit-fixture"`, `"spdx": ""`),
		"empty notice":               mutate(t, `"notice": "testdata/README.md"`, `"notice": ""`),
		"absolute notice":            mutate(t, `"notice": "testdata/README.md"`, `"notice": "/etc/passwd"`),
		"traversing notice":          mutate(t, `"notice": "testdata/README.md"`, `"notice": "../LICENSE"`),
		"unknown review status":      mutate(t, `"status": "approved"`, `"status": "waived"`),
		"approved without reference": mutate(t, `"reference": "testdata/README.md"`, `"reference": ""`),
		"no coverage": edit(t, func(document map[string]any) {
			document["coverage"] = []any{}
			delete(document, "labels")
		}),
		"unknown hl7 version":     mutate(t, `"hl7_version": "2.4"`, `"hl7_version": "2.9"`),
		"hl7 version with spaces": mutate(t, `"hl7_version": "2.4"`, `"hl7_version": " 2.4"`),
		"unknown family":          mutate(t, `"family": "ADT"`, `"family": "ACK"`),
		"lowercase family":        mutate(t, `"family": "ADT"`, `"family": "adt"`),
		"duplicate combination":   mutate(t, `"family": "ADT"`, `"family": "SIU"`),
		"unknown level value":     mutate(t, `"labels": "untested"`, `"labels": "partial"`),
		"empty level value":       mutate(t, `"labels": "untested"`, `"labels": ""`),
		"labels supported without parse": mutate(t, `"parse": "untested",
      "labels": "unsupported"`, `"parse": "untested",
      "labels": "supported"`),
		"structural claimed": mutate(t, `"structural": "unsupported",
      "workflow": "unsupported"`, structuralClaim),
		"workflow claimed": mutate(t, `"workflow": "unsupported"
    },
    {`, `"workflow": "supported"
    },
    {`),
		"labels supported without content": mutate(t, `"parse": "untested",
      "labels": "unsupported"`, `"parse": "supported",
      "labels": "supported"`),
		"labels content for an uncovered version": mutate(t, `"labels": [
    {
      "hl7_version": "2.5.1"`, `"labels": [
    {
      "hl7_version": "2.6"`),
		"labels content under an unknown version": mutate(t, `"labels": [
    {
      "hl7_version": "2.5.1"`, `"labels": [
    {
      "hl7_version": "3.0"`),
		"duplicate labels version": mutate(t, `"labels": [
    {`, `"labels": [
    {"hl7_version": "2.5.1", "segments": {"MSH": {"1": "Field Separator"}}},
    {`),
		"labels with no segments": mutate(t, `"segments": {
        "MSH": {
          "9": "Message Type",
          "10": "Message Control ID",
          "12": "Version ID"
        },
        "SCH": {
          "1": "Placer Appointment ID",
          "2": "Filler Appointment ID"
        }
      }`, `"segments": {}`),
		"segment with no positions": mutate(t, `"SCH": {
          "1": "Placer Appointment ID",
          "2": "Filler Appointment ID"
        }`, `"SCH": {}`),
		"lowercase segment":           mutate(t, `"SCH": {`, `"sch": {`),
		"four-letter segment":         mutate(t, `"SCH": {`, `"SCHX": {`),
		"segment starting with digit": mutate(t, `"SCH": {`, `"1CH": {`),
		"position zero":               mutate(t, `"1": "Placer Appointment ID"`, `"0": "Placer Appointment ID"`),
		"negative position":           mutate(t, `"1": "Placer Appointment ID"`, `"-1": "Placer Appointment ID"`),
		"position past the bound":     mutate(t, `"1": "Placer Appointment ID"`, `"1000": "Placer Appointment ID"`),
		"non-numeric position":        mutate(t, `"1": "Placer Appointment ID"`, `"one": "Placer Appointment ID"`),
		"empty label":                 mutate(t, `"Placer Appointment ID"`, `""`),
		"label over the bound":        mutate(t, `"Placer Appointment ID"`, `"`+strings.Repeat("x", 129)+`"`),
		"control character in label":  mutate(t, `"Placer Appointment ID"`, `"Placer]0;owned"`),
		"newline in label":            mutate(t, `"Placer Appointment ID"`, `"Placer\nAppointment"`),
		"invalid utf-8 in label":      mutate(t, `"Placer Appointment ID"`, "\"Placer\xff\""),
		"duplicate member": mutate(t, `"family": "SIU",
      "parse": "supported"`, `"family": "SIU",
      "parse": "supported",
      "parse": "supported"`),
		"over the size bound": []byte(`{"schema":"readmit-profile-pack/v1","pad":"` + strings.Repeat("x", profilepack.MaxPackBytes) + `"}`),
	}
	for name, document := range cases {
		if _, err := profilepack.Decode(document); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestPendingRightsReviewDecodes: a pack under review is readable, so a
// reviewer can inspect exactly what will be bundled. Whether it may be bundled
// is the documented gate, not a decode failure.
func TestPendingRightsReviewDecodes(t *testing.T) {
	pack, err := profilepack.Decode(mutate(t, `"status": "approved"`, `"status": "pending"`))
	if err != nil {
		t.Fatalf("a pending pack must decode: %v", err)
	}
	if pack.Provenance.RightsReview.Status != profilepack.ReviewPending {
		t.Fatalf("status %q", pack.Provenance.RightsReview.Status)
	}
	if _, err := profilepack.Decode(mutate(t, `"status": "approved",
      "reference": "testdata/README.md"`, `"status": "pending",
      "reference": ""`)); err != nil {
		t.Fatalf("a pending pack with no reference yet must decode: %v", err)
	}
}

// TestBundleableIsTheRecordedRightsReview is the one-pack gate a library and
// the window's pack inspection both ask: an approved recorded review is
// bundleable, a pending one is readable and not bundleable, and a pack nothing
// decoded is neither.
func TestBundleableIsTheRecordedRightsReview(t *testing.T) {
	approved, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := approved.Bundleable(); err != nil {
		t.Fatalf("an approved review is not bundleable: %v", err)
	}
	pending, err := profilepack.Decode(fixture(t, "profile-pack-pending-review.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := pending.Bundleable(); err == nil || !strings.Contains(err.Error(), "records no approved rights review") {
		t.Fatalf("a pending review is bundleable: %v", err)
	}
	assembled := profilepack.Pack{Schema: approved.Schema, Identity: approved.Identity, Provenance: approved.Provenance, Coverage: approved.Coverage}
	if err := assembled.Bundleable(); err == nil {
		t.Fatal("a pack nothing decoded is bundleable")
	}
}

func TestParseOnlyPackCarriesNoLabels(t *testing.T) {
	document := edit(t, func(document map[string]any) {
		document["coverage"].([]any)[0].(map[string]any)["labels"] = "untested"
		delete(document, "labels")
	})
	pack, err := profilepack.Decode(document)
	if err != nil {
		t.Fatalf("a parse-only pack must decode: %v", err)
	}
	if len(pack.Labels) != 0 {
		t.Fatalf("labels %+v", pack.Labels)
	}
	if got := pack.Label("2.5.1", "SIU", "SCH", 1); got != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUntested}) {
		t.Fatalf("parse-only pack answered a label: %+v", got)
	}
	if got := pack.Support("2.5.1", "SIU", profilepack.LevelParse); !got.Passing() {
		t.Fatalf("parse support %q", got)
	}
}
