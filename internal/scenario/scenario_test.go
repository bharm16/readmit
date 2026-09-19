package scenario

import (
	"os"
	"strings"
	"testing"
	"time"
)

// booking is the smallest complete scenario: one patient, one appointment, one
// step. Every negative case below is this document with one thing changed.
const booking = `{"schema":"readmit-scenario/v1","scenario":{"id":"s","version":"1"},` +
	`"profile":"readmit-siu-lifecycle-v1","base_time":"2026-01-01T12:00:00Z",` +
	`"subjects":[{"id":"patient-a","kind":"patient","namespace":"READMIT","identifier":"P1","initial_state":"active"},` +
	`{"id":"appointment-a","kind":"appointment","namespace":"READMIT","identifier":"A1","patient":"patient-a","initial_state":"none"}],` +
	`"steps":[{"id":"book","event":"S12","subject":"appointment-a","after":"0s","expect":"accepted"}]}`

func TestDecodeReadsADesignedWorkflowAsWritten(t *testing.T) {
	designed, err := Decode([]byte(booking))
	if err != nil {
		t.Fatal(err)
	}
	if designed.Schema != Schema || designed.Scenario.ID != "s" || designed.Profile != SIULifecycle {
		t.Fatalf("%+v", designed)
	}
	if !designed.BaseTime.Equal(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("base time %s", designed.BaseTime)
	}
	if len(designed.Subjects) != 2 || designed.Subjects[1].Patient != "patient-a" || designed.Subjects[1].InitialState != AppointmentNone {
		t.Fatalf("%+v", designed.Subjects)
	}
	if len(designed.Steps) != 1 || designed.Steps[0].Event != "S12" || designed.Steps[0].Expect != Accepted {
		t.Fatalf("%+v", designed.Steps)
	}
}

// TestDecodeRefusesEveryDocumentItCannotDesign is the negative half of the
// reader. A scenario carries no command and no member this contract never
// declared, an outcome and an initial state are never assumed, an event is
// never borrowed from the profile that was not named, and every reference
// resolves before any step is walked.
func TestDecodeRefusesEveryDocumentItCannotDesign(t *testing.T) {
	for name, change := range map[string][2]string{
		"no schema":                 {`"schema":"readmit-scenario/v1",`, ``},
		"another schema":            {`readmit-scenario/v1`, `readmit-scenario/v2`},
		"unknown member":            {`"steps":[`, `"seed":0,"steps":[`},
		"unknown subject member":    {`"identifier":"A1",`, `"identifier":"A1","script":"./book.sh",`},
		"unknown step member":       {`"after":"0s"`, `"after":"0s","command":"./book.sh"`},
		"no expectation":            {`,"expect":"accepted"`, ``},
		"unknown expectation":       {`"expect":"accepted"`, `"expect":"maybe"`},
		"no initial state":          {`,"initial_state":"none"`, ``},
		"unknown initial state":     {`"initial_state":"none"`, `"initial_state":"pending"`},
		"state of another kind":     {`"initial_state":"none"}]`, `"initial_state":"discharged"}]`},
		"kind the profile lacks":    {`"kind":"appointment"`, `"kind":"visit"`},
		"event of another profile":  {`"event":"S12"`, `"event":"A01"`},
		"unknown event":             {`"event":"S12"`, `"event":"S99"`},
		"event of another kind":     {`"subject":"appointment-a"`, `"subject":"patient-a"`},
		"unknown profile":           {`readmit-siu-lifecycle-v1`, `readmit-siu-v1`},
		"undeclared subject":        {`"subject":"appointment-a"`, `"subject":"appointment-b"`},
		"undeclared patient link":   {`"patient":"patient-a"`, `"patient":"patient-z"`},
		"patient link to a booking": {`"patient":"patient-a"`, `"patient":"appointment-a"`},
		"patient with a patient":    {`"identifier":"P1",`, `"identifier":"P1","patient":"patient-a",`},
		"booking without a patient": {`"patient":"patient-a",`, ``},
		"unreachable subject": {`{"id":"appointment-a","kind":"appointment"`,
			`{"id":"patient-b","kind":"patient","namespace":"READMIT","identifier":"P2","initial_state":"active"},` +
				`{"id":"appointment-a","kind":"appointment"`},
		"surviving identity on a booking": {`"after":"0s"`, `"into":"patient-a","after":"0s"`},
		"repeated subject id":             {`"id":"appointment-a"`, `"id":"patient-a"`},
		"unreadable subject id":           {`"id":"appointment-a"`, `"id":"Appointment/A"`},
		"unreadable step id":              {`"id":"book"`, `"id":"1-book"`},
		"unreadable scenario id":          {`"id":"s"`, `"id":""`},
		"unreadable scenario version":     {`"version":"1"`, `"version":"one point oh!"`},
		"identifier with a delimiter":     {`"identifier":"A1"`, `"identifier":"A1^READMIT"`},
		"identifier with a space":         {`"identifier":"A1"`, `"identifier":"A 1"`},
		"empty authority":                 {`"namespace":"READMIT","identifier":"A1"`, `"namespace":"","identifier":"A1"`},
		"no steps":                        {`{"id":"book","event":"S12","subject":"appointment-a","after":"0s","expect":"accepted"}`, ``},
		"zero base time":                  {`2026-01-01T12:00:00Z`, `0001-01-01T00:00:00Z`},
		"subsecond base time":             {`2026-01-01T12:00:00Z`, `2026-01-01T12:00:00.5Z`},
		"negative offset":                 {`"after":"0s"`, `"after":"-1s"`},
		"subsecond offset":                {`"after":"0s"`, `"after":"1.5s"`},
		"unbounded offset":                {`"after":"0s"`, `"after":"9000h"`},
		"offset that is not a duration":   {`"after":"0s"`, `"after":"tomorrow"`},
	} {
		document := strings.Replace(booking, change[0], change[1], 1)
		if document == booking {
			t.Fatalf("%s changed nothing; the test is not exercising the reader", name)
		}
		if _, err := Decode([]byte(document)); err == nil {
			t.Errorf("%s was read as a designed workflow", name)
		}
	}
	if _, err := Decode(make([]byte, MaxBytes+1)); err == nil {
		t.Error("an oversized scenario was read")
	}
}

// TestDecodeRefusesASequenceWithoutItsOwnOrder keeps the document's order the
// sequence's order. Two events at one instant, and a step earlier than the one
// before it, are both refused rather than sorted into some order nobody wrote.
func TestDecodeRefusesASequenceWithoutItsOwnOrder(t *testing.T) {
	for name, second := range map[string]string{
		"same instant": `"0s"`,
		"earlier":      `"0s"`,
		"later":        `"1m"`,
	} {
		first := `"after":"0s"`
		if name == "earlier" {
			first = `"after":"1m"`
		}
		document := strings.Replace(booking,
			`"steps":[{"id":"book","event":"S12","subject":"appointment-a","after":"0s","expect":"accepted"}]`,
			`"steps":[{"id":"book","event":"S12","subject":"appointment-a",`+first+`,"expect":"accepted"},`+
				`{"id":"reschedule","event":"S13","subject":"appointment-a","after":`+second+`,"expect":"accepted"}]`, 1)
		_, err := Decode([]byte(document))
		if (err == nil) != (name == "later") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPreviewResolvesEveryStepAgainstTheProfileLifecycle(t *testing.T) {
	designed, err := Decode(fixture(t, "scenario-siu.json"))
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := Preview(designed)
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Accepted != 4 || timeline.Refused != 5 || len(timeline.Steps) != 9 {
		t.Fatalf("%d accepted, %d refused, %d steps", timeline.Accepted, timeline.Refused, len(timeline.Steps))
	}
	booked := timeline.Steps[1]
	if booked.Ordinal != 2 || booked.From != AppointmentNone || booked.To != AppointmentBooked || booked.Reason != "" {
		t.Fatalf("%+v", booked)
	}
	if !booked.At.Equal(timeline.BaseTime.Add(time.Minute)) {
		t.Fatalf("booking at %s", booked.At)
	}
	// A refused step changes nothing, so the steps after it are read against
	// the state the sequence actually reached.
	refused := timeline.Steps[2]
	if refused.From != AppointmentBooked || refused.To != AppointmentBooked || refused.Reason == "" {
		t.Fatalf("%+v", refused)
	}
	if timeline.Steps[3].From != AppointmentBooked {
		t.Fatalf("a refused booking changed the appointment: %+v", timeline.Steps[3])
	}
}

// TestPreviewCarriesEveryIdentityThroughTheSequence is the merge half: an
// identity merged away survives nothing, and neither does the visit booked
// under it.
func TestPreviewCarriesEveryIdentityThroughTheSequence(t *testing.T) {
	designed, err := Decode(fixture(t, "scenario-adt.json"))
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := Preview(designed)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, step := range timeline.Steps {
		reasons[step.ID] = step.Reason
	}
	for id, want := range map[string]string{
		"merge-a-patient-into-itself":                "a patient identity is never merged into itself",
		"merge-the-duplicate-identity":               "",
		"merge-an-identity-already-merged-away":      `A40 (merge patient identifier list) is not taken from "merged"`,
		"merge-into-an-identity-already-merged-away": "the surviving identity was itself merged away",
		"update-the-visit-of-a-merged-identity":      "the patient identity this visit belongs to was merged away",
		"cancel-a-discharge-that-never-happened":     `A13 (cancel discharge or end visit) is not taken from "admitted"`,
	} {
		if reasons[id] != want {
			t.Errorf("%s: %q, want %q", id, reasons[id], want)
		}
	}
}

// TestPreviewRefusesAWorkflowThatDisagreesWithItsProfile is the whole claim of
// this delivery. A negative case that is not negative, and a positive case the
// lifecycle refuses, are each refused by name rather than previewed: an author
// never learns from a designer that accepts both readings of what they wrote.
func TestPreviewRefusesAWorkflowThatDisagreesWithItsProfile(t *testing.T) {
	for name, document := range map[string][]byte{
		"a negative case the profile accepts": []byte(strings.Replace(booking, `"expect":"accepted"`, `"expect":"refused"`, 1)),
		"a positive case the profile refuses": fixture(t, "scenario-refused.json"),
	} {
		designed, err := Decode(document)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := Preview(designed); err == nil {
			t.Errorf("%s was previewed", name)
		}
	}
}

// TestPreviewIsAPureFunctionOfTheDocument keeps a designed workflow
// reproducible: no clock, no environment and no machine identity reaches it,
// so the same document previews identically wherever it is read.
func TestPreviewIsAPureFunctionOfTheDocument(t *testing.T) {
	for _, shipped := range []string{"scenario-adt.json", "scenario-siu.json"} {
		designed, err := Decode(fixture(t, shipped))
		if err != nil {
			t.Fatal(err)
		}
		first, err := Preview(designed)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
		second, err := Preview(designed)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Steps) != len(second.Steps) {
			t.Fatalf("%s: %d then %d steps", shipped, len(first.Steps), len(second.Steps))
		}
		for i := range first.Steps {
			if first.Steps[i] != second.Steps[i] {
				t.Fatalf("%s step %d: %+v then %+v", shipped, i+1, first.Steps[i], second.Steps[i])
			}
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
