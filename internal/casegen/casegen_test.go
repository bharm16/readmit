package casegen_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/hl7"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../testdata/casegen", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// owned is a fixture request with the owned profile and pack it pins.
func owned(t *testing.T, request, family string) ([]byte, []byte, []byte) {
	t.Helper()
	return read(t, "request-"+request+".json"), read(t, "owned/profile-"+family+".json"), read(t, "owned/pack.json")
}

// edited decodes a JSON document, lets the test change it and encodes it again.
func edited(t *testing.T, raw []byte, change func(map[string]any)) []byte {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	change(v)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// repinned pins the request to the given profile and pack bytes.
func repinned(t *testing.T, request, profile, pack []byte) []byte {
	t.Helper()
	return edited(t, request, func(v map[string]any) {
		v["profile"].(map[string]any)["sha256"] = sum(profile)
		v["pack"].(map[string]any)["sha256"] = sum(pack)
	})
}

func generate(t *testing.T, request, profile, pack []byte) *casegen.Generation {
	t.Helper()
	g, err := casegen.Generate(context.Background(), request, profile, pack, casegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func caseOf(t *testing.T, g *casegen.Generation, variant string) (casegen.Case, [][]byte) {
	t.Helper()
	for i, c := range g.Record.Cases {
		if c.Variant == variant {
			return c, g.Messages(i)
		}
	}
	t.Fatalf("no case for variant %s", variant)
	return casegen.Case{}, nil
}

// field reads one selector of one message as text.
func field(t *testing.T, message []byte, selector string) string {
	t.Helper()
	doc, err := hl7.Parse(message, hl7.Options{Format: hl7.Raw, Terminator: hl7.CR})
	if err != nil {
		t.Fatal(err)
	}
	s, err := hl7.ParseSelector(selector)
	if err != nil {
		t.Fatal(err)
	}
	r, err := doc.Read(0, s, hl7.IgnoreMSH18)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != hl7.Present {
		return "<" + string(r.State) + ">"
	}
	return string(r.Decoded)
}

func TestIdenticalInputsReproduceExactBytes(t *testing.T) {
	request, profile, pack := owned(t, "siu", "siu")
	first, second := generate(t, request, profile, pack), generate(t, request, profile, pack)
	for i := range first.Record.Cases {
		if !slices.EqualFunc(first.Messages(i), second.Messages(i), bytes.Equal) {
			t.Fatal("identical inputs generated different bytes")
		}
	}
	if first.Record.ContentIdentity != second.Record.ContentIdentity || len(first.Record.ContentIdentity) != 64 {
		t.Fatal("identical inputs have different content identities")
	}
	a, err := first.Write(context.Background(), t.TempDir(), casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Row + "-" + c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Write(context.Background(), t.TempDir(), casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Row + "-" + c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Record, b.Record) {
		t.Fatal("the record depends on where the generation was written")
	}
}

func TestChangedInputsChangeRecordedAncestryPredictably(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	base := generate(t, request, profile, pack)

	// Another seed changes the seed and the control IDs, nothing else.
	reseeded := generate(t, edited(t, request, func(v map[string]any) { v["seed"] = 8 }), profile, pack)
	if reseeded.Record.Ancestry.Seed != 8 || reseeded.Record.Ancestry.Scenario != base.Record.Ancestry.Scenario || reseeded.Record.Ancestry.Profile != base.Record.Ancestry.Profile ||
		!slices.Equal(reseeded.Record.Ancestry.Variants, base.Record.Ancestry.Variants) || reseeded.Record.ContentIdentity == base.Record.ContentIdentity {
		t.Fatal("a new seed moved ancestry other than the seed")
	}
	for i, c := range base.Record.Cases {
		for k, message := range base.Messages(i) {
			other := reseeded.Messages(i)[k]
			if field(t, message, "MSH-10") == field(t, other, "MSH-10") {
				t.Fatal("a new seed kept a control ID")
			}
			same := strings.Replace(string(other), field(t, other, "MSH-10"), field(t, message, "MSH-10"), 1)
			if same != string(message) {
				t.Fatalf("a new seed changed more than MSH-10 in %s", c.Variant)
			}
		}
	}

	// Another profile changes the profile pin and nothing a message says.
	localized := edited(t, profile, func(v map[string]any) {
		v["segments"] = append(v["segments"].([]any), map[string]any{"id": "TQ1", "fields": []any{map[string]any{"position": 7, "usage": "R"}}})
	})
	reprofiled := generate(t, repinned(t, request, localized, pack), localized, pack)
	if reprofiled.Record.Ancestry.Profile.SHA256 != sum(localized) || reprofiled.Record.Ancestry.Seed != base.Record.Ancestry.Seed {
		t.Fatal("a new profile was not recorded")
	}
	for i := range base.Record.Cases {
		if !slices.EqualFunc(base.Messages(i), reprofiled.Messages(i), bytes.Equal) {
			t.Fatal("a profile adding a local rule changed the generated bytes")
		}
	}

	// A derived generation records its ancestor and changes nothing it says.
	ancestor := base.Record.ContentIdentity
	derived := generate(t, edited(t, request, func(v map[string]any) { v["derived_from"] = ancestor }), profile, pack)
	if derived.Record.Ancestry.DerivedFrom != ancestor || derived.Record.ContentIdentity == ancestor {
		t.Fatal("the ancestor was not recorded")
	}
	for i := range base.Record.Cases {
		if !slices.EqualFunc(base.Messages(i), derived.Messages(i), bytes.Equal) {
			t.Fatal("a derived generation changed the ancestor's bytes")
		}
	}

	// Another variant changes that variant's ancestry and case only.
	revised := generate(t, edited(t, request, func(v map[string]any) {
		mutation := v["variants"].([]any)[2].(map[string]any)["mutations"].([]any)[0].(map[string]any)
		mutation["step"] = "reschedule"
	}), profile, pack)
	for i, part := range base.Record.Ancestry.Variants {
		changed := part != revised.Record.Ancestry.Variants[i]
		bytesChanged := !slices.EqualFunc(base.Messages(i), revised.Messages(i), bytes.Equal)
		if changed != (part.ID == "duplicate-booking") || bytesChanged != changed {
			t.Fatalf("variant %s: ancestry changed %v, bytes changed %v", part.ID, changed, bytesChanged)
		}
	}
}

func TestBookingRescheduleAndCancellationAreSeparatePhases(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	g := generate(t, request, profile, pack)
	c, messages := caseOf(t, g, "baseline")
	if len(c.Phases) != 3 || len(messages) != 3 {
		t.Fatalf("phases %+v", c.Phases)
	}
	for i, want := range []struct{ step, event, trigger, start, status, after string }{
		{"book", "S12", "S12", "20260303090000+0000", "Booked", "0s"},
		{"reschedule", "S13", "S13", "20260305090000+0000", "Booked", "1h0m0s"},
		{"cancel", "S15", "S15", "20260305090000+0000", "Cancelled", "2h0m0s"},
	} {
		phase, message := c.Phases[i], messages[i]
		if phase.ID != want.step || phase.Event != want.event || !slices.Equal(phase.Occurrences, []int{i + 1}) || c.Occurrences[i].After != want.after {
			t.Fatalf("phase %d: %+v", i, phase)
		}
		// The appointment keeps its filler identity and namespace in every
		// phase; the reschedule's slot is what the cancellation carries.
		if field(t, message, "SCH-2.1") != "APPT-0001" || field(t, message, "SCH-2.2") != "SYNTH-SCHED" || field(t, message, "PID-3.1") != "MRN-0001" ||
			field(t, message, "MSH-9.2") != want.trigger || field(t, message, "TQ1-7") != want.start || field(t, message, "SCH-25.1") != want.status {
			t.Fatalf("phase %s says %q", want.step, message)
		}
		keys := c.Occurrences[i].Keys
		if len(keys) != 2 || keys[1] != (casegen.BusinessKey{Kind: "appointment", Namespace: "SYNTH-SCHED", Value: "APPT-0001"}) {
			t.Fatalf("business keys %+v", keys)
		}
	}
	if field(t, messages[0], "MSH-10") == field(t, messages[1], "MSH-10") {
		t.Fatal("two phases share a control ID")
	}
	// Reordering keeps the cancellation's own content and moves its arrival.
	moved, movedMessages := caseOf(t, g, "cancel-before-reschedule")
	if moved.Phases[1].ID != "cancel" || moved.Phases[2].ID != "reschedule" || !bytes.Equal(movedMessages[1], messages[2]) || moved.Occurrences[1].After != "1h0m0s" {
		t.Fatalf("moved phases %+v", moved.Phases)
	}
}

func TestNegativeVariantsAreBuiltExactlyAsDeclared(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	g := generate(t, request, profile, pack)
	_, baseline := caseOf(t, g, "baseline")

	c, m := caseOf(t, g, "duplicate-booking")
	if len(m) != 4 || !bytes.Equal(m[0], m[1]) || !c.Occurrences[1].Duplicate || c.Occurrences[1].ControlID != c.Occurrences[0].ControlID || !slices.Equal(c.Phases[0].Occurrences, []int{1, 2}) {
		t.Fatal("a duplicate delivery is not an exact adjacent retransmission")
	}
	if c.Ledger[0].Mutation.Op != "duplicate" || !slices.Equal(c.Ledger[0].Occurrences, []int{1, 2}) {
		t.Fatalf("ledger %+v", c.Ledger)
	}

	c, m = caseOf(t, g, "missing-booking")
	if len(m) != 2 || c.Phases[0].ID != "reschedule" || !bytes.Equal(m[0], baseline[1]) || len(c.Ledger[0].Occurrences) != 0 {
		t.Fatal("an omitted prerequisite is still sent")
	}

	c, m = caseOf(t, g, "foreign-namespace")
	if field(t, m[1], "SCH-2.2") != "OTHER-SCHED" || field(t, m[0], "SCH-2.2") != "SYNTH-SCHED" || field(t, m[2], "SCH-2.2") != "SYNTH-SCHED" {
		t.Fatal("an altered namespace leaked into another step")
	}
	if c.Occurrences[1].Keys[1].Namespace != "OTHER-SCHED" {
		t.Fatal("the altered namespace is not the recorded business key")
	}

	c, m = caseOf(t, g, "invalid-start")
	if field(t, m[1], "TQ1-7") != "20261301000000" {
		t.Fatalf("an invalid value was repaired: %q", m[1])
	}
	if !c.Validation.Evaluated || c.Validation.Verdict != "fail" || c.Polarity != "negative" {
		t.Fatalf("the declared invalid value evaluates %+v", c.Validation)
	}
	failed := false
	for _, f := range c.Validation.Report.Findings {
		failed = failed || f.Outcome == "fail" && f.Occurrence == "o002" && strings.HasPrefix(f.Selector, "TQ1")
	}
	if !failed {
		t.Fatal("the evaluator did not fail the declared invalid date")
	}

	request, profile, pack = owned(t, "siu", "siu")
	c, m = caseOf(t, generate(t, request, profile, pack), "null-name")
	if !strings.Contains(string(m[1]), "PID|1||SYNTH-PATIENT-A^^^READMIT^MR||\"\"") || c.Validation.Verdict != "fail" {
		t.Fatalf("an explicit null name was not written: %q", m[1])
	}
}

func TestResultCorrectionLinkageCanBeBroken(t *testing.T) {
	request, profile, pack := owned(t, "oru", "oru")
	g := generate(t, request, profile, pack)
	_, baseline := caseOf(t, g, "baseline")
	c, m := caseOf(t, g, "unlinked-correction")
	correction := m[5]
	if c.Occurrences[5].Step != "step-6" || field(t, correction, "OBR-3") != "<empty>" || field(t, correction, "ORC-3") != "<empty>" || field(t, baseline[5], "OBR-3.1") != "FILLER-001" {
		t.Fatalf("the correction still names its filler order: %q", correction)
	}
	if field(t, correction, "OBX[2]-4") != "2" || field(t, correction, "OBX[2]-11") != "C" || field(t, correction, "OBR-25") != "C" {
		t.Fatal("the correction lost its repeated observations")
	}
	c, _ = caseOf(t, g, "correction-before-final")
	order := []string{}
	for _, p := range c.Phases {
		order = append(order, p.ID)
	}
	if !slices.Equal(order, []string{"step-1", "step-2", "step-3", "step-6", "step-4", "step-5"}) {
		t.Fatalf("phases %v", order)
	}
}

func TestAPositiveCaseTheProfileFailsIsRefusedNotRepaired(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	// A site rule the scenario model cannot meet: a Z-segment it never writes.
	strict := edited(t, profile, func(v map[string]any) {
		v["segments"] = []any{map[string]any{"id": "SCH", "fields": []any{map[string]any{"position": 3, "usage": "R"}}}}
	})
	_, err := casegen.Generate(context.Background(), repinned(t, request, strict, pack), strict, pack, casegen.Options{})
	var unsupported *casegen.UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Support[0].Status != casegen.Unsupported || !strings.Contains(unsupported.Support[0].Reason, "SCH[1]-3") {
		t.Fatalf("a failing positive case was generated: %v", err)
	}
}

func TestAnEventThePackDoesNotDeclareIsUnsupportedByName(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	without := edited(t, pack, func(v map[string]any) {
		messages := v["messages"].([]any)
		v["messages"] = slices.DeleteFunc(messages, func(m any) bool { return m.(map[string]any)["structure"] == "SIU_S13" })
	})
	_, err := casegen.Generate(context.Background(), repinned(t, request, profile, without), profile, without, casegen.Options{})
	var unsupported *casegen.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatal(err)
	}
	for _, s := range unsupported.Support {
		want := casegen.Supported
		if s.Event == "S13" {
			want = casegen.Unsupported
			if s.Reason != "the pack declares no SIU^S13 message in HL7 2.5.1" {
				t.Fatal(s.Reason)
			}
		}
		if s.Status != want {
			t.Fatalf("%+v", s)
		}
	}
}

// A site's own rules shape the message: a field the local profile does not
// support is left unwritten, and an essential one refuses its event by name.
func TestTheLocalProfileExcludesWhatItDoesNotSupport(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	site := func(position int) []byte {
		return edited(t, profile, func(v map[string]any) {
			v["segments"] = []any{map[string]any{"id": "SCH", "fields": []any{map[string]any{"position": position, "usage": "X"}}}}
		})
	}
	without := site(25)
	_, m := caseOf(t, generate(t, repinned(t, request, without, pack), without, pack), "baseline")
	if field(t, m[0], "SCH-25") != "<omitted>" || field(t, m[0], "SCH-20.1") != "CLERK-1" {
		t.Fatalf("the site's X rule was not honoured: %q", m[0])
	}
	essential := site(2)
	_, err := casegen.Generate(context.Background(), repinned(t, request, essential, pack), essential, pack, casegen.Options{})
	var unsupported *casegen.UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Support[0].Reason != "SCH-2 is not supported by the local profile" {
		t.Fatalf("an essential field the site excludes: %v", err)
	}
}

// Every member a request declares, an empty one included, reads back from the
// record exactly.
func TestEmptyDeclaredMembersSurviveTheRecord(t *testing.T) {
	request, profile, pack := owned(t, "adt", "adt")
	request = edited(t, request, func(v map[string]any) {
		edits := v["bindings"].(map[string]any)["edits"].([]any)
		edits[1].(map[string]any)["given"] = ""
	})
	g := generate(t, request, profile, pack)
	written, err := g.Write(context.Background(), t.TempDir(), casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	record, err := casegen.ReadRecord(written.Record)
	if err != nil || record.Request.Bindings.Edits[1].Given == nil || *record.Request.Bindings.Edits[1].Given != "" || record.Request.Bindings.Edits[1].Family != "SYNTHETIC" {
		t.Fatalf("%v %+v", err, record.Request.Bindings.Edits)
	}
	if record.Request.Bindings.Resources == nil || len(record.Request.Bindings.Resources) != 0 {
		t.Fatalf("an empty resource binding list: %+v", record.Request.Bindings)
	}
}

func TestNotesArePlacedAtTheStructuresFirstNTE(t *testing.T) {
	request, profile, pack := owned(t, "oru", "oru")
	request = edited(t, request, func(v map[string]any) { v["rows"].([]any)[0].(map[string]any)["notes"] = []any{"Patient note"} })
	_, m := caseOf(t, generate(t, request, profile, pack), "baseline")
	ids := []string{}
	for _, segment := range strings.Split(strings.TrimSuffix(string(m[3]), "\r"), "\r") {
		ids = append(ids, segment[:3])
	}
	if !slices.Equal(ids, []string{"MSH", "PID", "NTE", "ORC", "OBR", "OBX", "OBX"}) {
		t.Fatalf("segments %v", ids)
	}
}

func TestDeclaredDelimitersCharsetPrecisionAndOffsetReadBack(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	request = edited(t, request, func(v map[string]any) {
		wire := v["wire"].(map[string]any)
		wire["delimiters"], wire["precision"], wire["offset"] = "#*!@%", "minute", "-05:00"
		v["rows"] = []any{map[string]any{"id": "latin", "family": "O#Brien*Café", "given": "Zoë", "notes": []any{}, "charset": "8859/1"}}
		v["variants"] = []any{map[string]any{"id": "baseline", "polarity": "positive", "mutations": []any{
			map[string]any{"op": "charset", "step": "cancel", "charset": "UNICODE UTF-8"},
			map[string]any{"op": "offset", "step": "cancel", "offset": "+09:30"},
		}}}
	})
	g := generate(t, request, profile, pack)
	c, m := caseOf(t, g, "baseline")
	if !strings.HasPrefix(string(m[0]), "MSH#*!@%#READMIT#SYNTHETIC#RECEIVER#READMIT#202603020400-0500##SIU*S12*SIU_S12#") {
		t.Fatalf("header %q", m[0][:80])
	}
	// The row's escaped delimiters and Latin-1 bytes, exactly.
	if !bytes.Contains(m[0], []byte("PID#1##MRN-0001***SYNTH-MRN*MR##O@F@Brien@S@Caf\xe9*Zo\xeb\r")) {
		t.Fatalf("PID %q", m[0])
	}
	if !bytes.Contains(m[2], []byte("#202603022030+0930#")) || !bytes.Contains(m[2], []byte("O@F@Brien@S@Café*Zoë")) || field(t, m[2], "MSH-18") != "UNICODE UTF-8" {
		t.Fatalf("the cancellation's charset and offset: %q", m[2])
	}
	if c.Occurrences[0].Charset != "8859/1" || c.Occurrences[2].Charset != "UNICODE UTF-8" {
		t.Fatal("per-occurrence charset not recorded")
	}
	// The evaluator reads no Latin-1 text, so the case is undecided, not passed.
	if c.Validation.Verdict != "undecided" {
		t.Fatalf("verdict %s", c.Validation.Verdict)
	}
	ascii := edited(t, request, func(v map[string]any) { v["rows"].([]any)[0].(map[string]any)["charset"] = "ASCII" })
	if _, err := casegen.Generate(context.Background(), ascii, profile, pack, casegen.Options{}); err == nil || !strings.Contains(err.Error(), "ASCII") {
		t.Fatalf("ASCII accepted text it cannot represent: %v", err)
	}
}

func TestEditsCarryForwardOnlyFromStepsTheLifecycleTakes(t *testing.T) {
	request, profile, pack := owned(t, "adt", "adt")
	_, m := caseOf(t, generate(t, request, profile, pack), "baseline")
	if field(t, m[1], "PID-5.2") != "PATIENT" || field(t, m[3], "PID-5.2") != "RENAMED" || field(t, m[5], "PID-5.2") != "RENAMED" {
		t.Fatal("the update's rename is not carried")
	}
	if field(t, m[2], "PV1-3.1") != "WARD-B" || field(t, m[2], "PV1-6.1") != "WARD-A" || field(t, m[5], "PV1-3.1") != "WARD-B" || field(t, m[5], "PV1-6") != "<empty>" {
		t.Fatal("the transfer's locations are not carried")
	}
	// The merge: the surviving identity in PID, the prior one in MRG.
	if field(t, m[10], "PID-3.1") != "SYNTH-PATIENT-A" || field(t, m[10], "MRG-1.1") != "SYNTH-PATIENT-B" || field(t, m[10], "PID-3[2].1") != "EMPI-A" {
		t.Fatal("the merge identities")
	}

	request, profile, pack = owned(t, "siu", "siu")
	request = edited(t, request, func(v map[string]any) {
		bindings := v["bindings"].(map[string]any)
		bindings["edits"] = append(bindings["edits"].([]any), map[string]any{"step": "reschedule-after-cancellation", "op": "reschedule", "start_after": "96h", "duration_minutes": 15})
	})
	_, m = caseOf(t, generate(t, request, profile, pack), "baseline")
	if field(t, m[7], "TQ1-7") != "20260105120000+0000" || field(t, m[8], "TQ1-7") != "20260103120000+0000" {
		t.Fatal("a refused step's edit was carried forward")
	}
	// The reschedule moves every held resource's slot: RGS-2 and each resource
	// carry U and the filler status. The modification changes no resource and
	// sends none. A booking and a cancellation carry no action code.
	if field(t, m[3], "RGS-2") != "U" || field(t, m[3], "AIS-2") != "U" || field(t, m[3], "AIL-2") != "U" ||
		field(t, m[3], "AIS-10.1") != "Booked" || field(t, m[3], "AIL-12.1") != "Booked" {
		t.Fatalf("the reschedule's action codes: %q", m[3])
	}
	if field(t, m[4], "RGS-2") != "U" || strings.Contains(string(m[4]), "\rAIS") || strings.Contains(string(m[4]), "\rAIL") {
		t.Fatalf("the modification sent resources it does not change: %q", m[4])
	}
	if field(t, m[1], "AIS-2") != "<omitted>" && field(t, m[1], "AIS-2") != "<empty>" || field(t, m[5], "RGS-2") != "<omitted>" || field(t, m[5], "AIS-10.1") != "Cancelled" {
		t.Fatalf("action codes outside updating events: %q", m[5])
	}
}

func TestNotesTheStructureHasNoPlaceForAreRefused(t *testing.T) {
	request, profile, pack := owned(t, "adt", "adt")
	request = edited(t, request, func(v map[string]any) {
		v["rows"].([]any)[0].(map[string]any)["notes"] = []any{"A note"}
	})
	_, err := casegen.Generate(context.Background(), request, profile, pack, casegen.Options{})
	if err == nil || !strings.Contains(err.Error(), "no place for notes") {
		t.Fatalf("notes were dropped: %v", err)
	}
}

func TestWriteInstallsEachCaseAndTheRecordLast(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	var progress []casegen.Progress
	g, err := casegen.Generate(context.Background(), request, profile, pack, casegen.Options{Source: &casegen.Source{Item: "scenario-1", Revision: "3"}, Progress: func(p casegen.Progress) { progress = append(progress, p) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 1+len(g.Record.Cases) || progress[len(progress)-1].Done != len(g.Record.Cases) {
		t.Fatalf("progress %+v", progress)
	}
	// The saved revision a generation came from is part of what it is.
	if unsourced := generate(t, request, profile, pack); unsourced.Record.ContentIdentity == g.Record.ContentIdentity {
		t.Fatal("the saved scenario revision is not part of the content identity")
	}
	dir := t.TempDir()
	written, err := g.Write(context.Background(), dir, casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	record, err := casegen.ReadRecord(written.Record)
	if err != nil || record.Ancestry.Source.Revision != "3" || record.ContentIdentity != g.Record.ContentIdentity {
		t.Fatalf("%v %+v", err, record.Ancestry.Source)
	}
	for i, c := range record.Cases {
		opened, err := bundle.Open(filepath.Join(dir, c.Entry))
		if err != nil || opened.Identity != c.Identity || opened.Manifest.Provenance.Mode != bundle.Generated || opened.Manifest.Provenance.Generator.Seed != 7 {
			t.Fatalf("%s: %v", c.Entry, err)
		}
		for k, occurrence := range c.Occurrences {
			payload, err := os.ReadFile(filepath.Join(dir, c.Entry, occurrence.Payload))
			if err != nil || !bytes.Equal(payload, g.Messages(i)[k]) || sum(payload) != occurrence.SHA256 {
				t.Fatalf("%s occurrence %d payload differs", c.Entry, k+1)
			}
		}
	}
	if _, err := g.Write(context.Background(), dir, casegen.Placement{Record: "again.json", Entry: func(c casegen.Case) string { return c.Variant }}); err == nil {
		t.Fatal("an existing case was overwritten")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	other := t.TempDir()
	if _, err := g.Write(cancelled, other, casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Variant }}); err == nil {
		t.Fatal("a cancelled write completed")
	}
	if _, err := os.Stat(filepath.Join(other, "generation.json")); err == nil {
		t.Fatal("a cancelled write installed a record")
	}
	if _, err := casegen.Generate(cancelled, request, profile, pack, casegen.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled generation answered %v", err)
	}
}

// Two cases with the same content are one case bundle: a variant whose delay
// reorders nothing says what the baseline says.
func TestIdenticalCasesShareOneBundle(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	request = edited(t, request, func(v map[string]any) {
		v["variants"] = []any{
			map[string]any{"id": "baseline", "polarity": "positive", "mutations": []any{}},
			map[string]any{"id": "late-but-in-order", "polarity": "positive", "mutations": []any{map[string]any{"op": "delay", "step": "book", "after": "1s"}}},
		}
	})
	dir := t.TempDir()
	written, err := generate(t, request, profile, pack).Write(context.Background(), dir, casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	if written.Cases[1].Entry != "baseline" || written.Cases[1].Identity != written.Cases[0].Identity || written.Cases[1].Occurrences[0].After != "1s" {
		t.Fatalf("%+v", written.Cases[1])
	}
	if _, err := os.Stat(filepath.Join(dir, "late-but-in-order")); err == nil {
		t.Fatal("an identical case was written twice")
	}
}

func TestRequestRefusals(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	for name, change := range map[string]func(map[string]any){
		"unknown member":      func(v map[string]any) { v["extra"] = true },
		"missing seed":        func(v map[string]any) { delete(v, "seed") },
		"generator version":   func(v map[string]any) { v["generator_version"] = "readmit-case-generator-v2" },
		"short pin":           func(v map[string]any) { v["pack"].(map[string]any)["sha256"] = "abc" },
		"repeated delimiter":  func(v map[string]any) { v["wire"].(map[string]any)["delimiters"] = "||~\\&" },
		"signed offset hours": func(v map[string]any) { v["wire"].(map[string]any)["offset"] = "+-1:00" },
		"alphanumeric delim":  func(v map[string]any) { v["wire"].(map[string]any)["delimiters"] = "|^~\\A" },
		"unbound patient":     func(v map[string]any) { v["bindings"].(map[string]any)["patients"] = []any{} },
		"edit on another event": func(v map[string]any) {
			v["bindings"].(map[string]any)["edits"].([]any)[0].(map[string]any)["step"] = "book"
		},
		"positive omission": func(v map[string]any) {
			v["variants"].([]any)[1].(map[string]any)["polarity"] = "positive"
		},
		"operator member of another": func(v map[string]any) {
			v["variants"].([]any)[2].(map[string]any)["mutations"].([]any)[0].(map[string]any)["after"] = "1h"
		},
		"component selector": func(v map[string]any) {
			v["variants"].([]any)[5].(map[string]any)["mutations"].([]any)[0].(map[string]any)["selector"] = "TQ1-7.1"
		},
		"mutation of the delimiters": func(v map[string]any) {
			v["variants"].([]any)[5].(map[string]any)["mutations"].([]any)[0].(map[string]any)["selector"] = "MSH-2"
		},
		"unlink outside a correction": func(v map[string]any) {
			v["variants"] = append(v["variants"].([]any), map[string]any{"id": "unlink", "polarity": "negative", "mutations": []any{map[string]any{"op": "unlink-correction", "step": "cancel"}}})
		},
		"control characters": func(v map[string]any) { v["rows"].([]any)[0].(map[string]any)["family"] = "A\rB" },
		"refused lifecycle": func(v map[string]any) {
			v["scenario"].(map[string]any)["steps"].([]any)[0].(map[string]any)["expect"] = "refused"
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := casegen.Generate(context.Background(), edited(t, request, change), profile, pack, casegen.Options{}); err == nil {
				t.Fatal("refusal expected")
			}
		})
	}
	if _, err := casegen.Generate(context.Background(), request, profile, append(bytes.Clone(pack), ' '), casegen.Options{}); err == nil || !strings.Contains(err.Error(), "pins") {
		t.Fatalf("a pack that differs from its pin was used: %v", err)
	}
	orm := read(t, "owned/profile-orm.json")
	if _, err := casegen.Generate(context.Background(), repinned(t, edited(t, request, func(v map[string]any) {
		v["profile"].(map[string]any)["id"] = "owned-casegen-orm"
	}), orm, pack), orm, pack, casegen.Options{}); err == nil || !strings.Contains(err.Error(), "constrains ORM") {
		t.Fatalf("a profile of another family was used: %v", err)
	}
}

func TestSavedGeneratorPlansConvertWithoutDroppingAClause(t *testing.T) {
	template := read(t, "../fixtures/scenario-siu.json")
	plan := []byte(`{"schema":"readmit-scenario-generator/v1","generator_version":"readmit-scenario-generator-v1","seed":3,"template":` + string(template) + `,
 "rows":[{"id":"one","patient_name":"SYNTHETIC","notes":["First","Second"],"encoding":"utf-8"},{"id":"two","patient_name":"Café","notes":["Only","Two"],"encoding":"iso-8859-1"}],
 "variants":[{"id":"baseline","mutations":[]},
  {"id":"shifted","mutations":[{"op":"timezone","step":"book","offset":"-06:00"},{"op":"encoding","step":"book","encoding":"utf-8"},{"op":"delay","step":"book","after":"5m"}]},
  {"id":"broken","mutations":[{"op":"duplicate","step":"book"},{"op":"field","step":"book","field":"NTE-3","state":"null"},{"op":"field","step":"modify","field":"PID-5","state":"absent"}]}]}`)
	request, profile, pack := owned(t, "siu", "siu")
	var settings casegen.Settings
	if err := json.Unmarshal(request, &settings, json.RejectUnknownMembers(false)); err != nil {
		t.Fatal(err)
	}
	settings.Variants = []casegen.Variant{{ID: "added", Polarity: "negative", Mutations: []casegen.Mutation{{Op: "omit", Step: "book"}}}}
	converted, err := casegen.FromPlan(plan, settings)
	if err != nil {
		t.Fatal(err)
	}
	var r casegen.Request
	if err := json.Unmarshal(converted, &r); err != nil {
		t.Fatal(err)
	}
	if r.Seed != 3 || !strings.Contains(string(r.Scenario), `"siu-appointment-lifecycle"`) || len(r.Rows) != 2 || r.Rows[1].Charset != "8859/1" || r.Rows[1].Family != "Café" {
		t.Fatalf("%+v", r.Rows)
	}
	polarity := map[string]string{}
	for _, v := range r.Variants {
		polarity[v.ID] = v.Polarity
	}
	if polarity["baseline"] != "positive" || polarity["shifted"] != "positive" || polarity["broken"] != "negative" || polarity["added"] != "negative" || len(r.Variants[2].Mutations) != 4 {
		t.Fatalf("%+v", r.Variants)
	}
	g := generate(t, converted, profile, pack)
	if len(g.Record.Cases) != 8 {
		t.Fatalf("%d cases", len(g.Record.Cases))
	}
	_, m := caseOf(t, g, "broken")
	if field(t, m[1], "NTE[1]-3") != "<null>" || field(t, m[1], "NTE[2]-3") != "<null>" || !bytes.Equal(m[1], m[2]) {
		t.Fatalf("%q", m[1])
	}
	uneven := bytes.Replace(plan, []byte(`"notes":["Only","Two"]`), []byte(`"notes":["Only"]`), 1)
	_, err = casegen.FromPlan(uneven, settings)
	var unconvertible *casegen.UnconvertibleError
	if !errors.As(err, &unconvertible) || len(unconvertible.Clauses) != 1 || unconvertible.Clauses[0].Path != "variants[2].mutations[1]" {
		t.Fatalf("an unconvertible clause was dropped: %v", err)
	}
}
