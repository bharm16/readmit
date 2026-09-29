package scenario_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/scenario"
)

const resourceWorkflow = `{"schema":"readmit-scenario/v2","scenario":{"id":"resources","version":"1"},"profile":"readmit-siu-lifecycle-v2","base_time":"2026-04-06T08:00:00Z",
"subjects":[{"id":"patient-a","kind":"patient","namespace":"N","identifier":"P1","initial_state":"active"},
 {"id":"appointment-a","kind":"appointment","namespace":"N","identifier":"A1","patient":"patient-a","initial_state":"none"},
 {"id":"consult","kind":"resource","namespace":"SVC","identifier":"CONSULT","appointment":"appointment-a","initial_state":"booked"},
 {"id":"room","kind":"resource","namespace":"LOC","identifier":"ROOM-1","appointment":"appointment-a","initial_state":"booked"},
 {"id":"doctor","kind":"resource","namespace":"STAFF","identifier":"DR-1","appointment":"appointment-a","initial_state":"none"}],
"steps":[{"id":"add-before-booking","event":"S18","subject":"doctor","after":"0s","expect":"refused"},
 {"id":"book","event":"S12","subject":"appointment-a","after":"1m","expect":"accepted"},
 {"id":"cancel-room","event":"S20","subject":"room","after":"2m","expect":"accepted"},
 {"id":"add-doctor","event":"S18","subject":"doctor","after":"3m","expect":"accepted"},
 {"id":"cancel-room-again","event":"S20","subject":"room","after":"4m","expect":"refused"},
 {"id":"add-doctor-again","event":"S18","subject":"doctor","after":"5m","expect":"refused"},
 {"id":"cancel","event":"S15","subject":"appointment-a","after":"6m","expect":"accepted"},
 {"id":"cancel-after-cancellation","event":"S20","subject":"doctor","after":"7m","expect":"refused"}]}`

func TestResourceParticipationIsAddedAndCancelledByItsOwnEvents(t *testing.T) {
	timeline, err := scenario.PreviewDocument([]byte(resourceWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ from, to, reason string }{
		{"none", "none", "the appointment this resource participates in is not booked"},
		{"none", "booked", ""},
		{"booked", "cancelled", ""},
		{"none", "booked", ""},
		{"cancelled", "cancelled", `S20 (cancellation of service/resource on appointment) is not taken from "cancelled"`},
		{"booked", "booked", `S18 (addition of service/resource on appointment) is not taken from "booked"`},
		{"booked", "cancelled", ""},
		{"booked", "booked", "the appointment this resource participates in is not booked"},
	}
	if len(timeline.Steps) != len(want) || timeline.Profile != scenario.SIUResourceLifecycle {
		t.Fatalf("%+v", timeline)
	}
	for i, step := range timeline.Steps {
		if string(step.From) != want[i].from || string(step.To) != want[i].to || step.Reason != want[i].reason {
			t.Errorf("step %d %s: %s -> %s (%s)", i+1, step.ID, step.From, step.To, step.Reason)
		}
	}
}

func TestResourceSubjectsBelongOnlyToTheVersionThatDeclaresThem(t *testing.T) {
	for name, document := range map[string]string{
		"v1 names the v2 lifecycle": strings.Replace(resourceWorkflow, `"readmit-scenario/v2"`, `"readmit-scenario/v1"`, 1),
		"v2 names a v1 lifecycle":   strings.Replace(resourceWorkflow, `"readmit-siu-lifecycle-v2"`, `"readmit-siu-lifecycle-v1"`, 1),
		"a resource names a patient": strings.Replace(resourceWorkflow, `"appointment":"appointment-a","initial_state":"booked"},
 {"id":"room"`, `"appointment":"appointment-a","patient":"patient-a","initial_state":"booked"},
 {"id":"room"`, 1),
		"a resource names no appointment":               strings.Replace(resourceWorkflow, `"identifier":"DR-1","appointment":"appointment-a",`, `"identifier":"DR-1",`, 1),
		"an appointment names an appointment":           strings.Replace(resourceWorkflow, `"identifier":"A1","patient":"patient-a",`, `"identifier":"A1","patient":"patient-a","appointment":"appointment-a",`, 1),
		"a resource names a patient as its appointment": strings.Replace(resourceWorkflow, `"identifier":"DR-1","appointment":"appointment-a"`, `"identifier":"DR-1","appointment":"patient-a"`, 1),
		"a resource declared in the order state":        strings.Replace(resourceWorkflow, `"identifier":"DR-1","appointment":"appointment-a","initial_state":"none"`, `"identifier":"DR-1","appointment":"appointment-a","initial_state":"ordered"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := scenario.PreviewDocument([]byte(document)); err == nil {
				t.Fatal("refusal expected")
			}
		})
	}
	// A v1 document still reads exactly as before, and a v1 subject naming an
	// appointment is refused rather than read.
	v1 := `{"schema":"readmit-scenario/v1","scenario":{"id":"s","version":"1"},"profile":"readmit-siu-lifecycle-v1","base_time":"2026-01-01T12:00:00Z",
"subjects":[{"id":"patient-a","kind":"patient","namespace":"N","identifier":"P1","initial_state":"active"},
 {"id":"appointment-a","kind":"appointment","namespace":"N","identifier":"A1","patient":"patient-a","appointment":"appointment-a","initial_state":"none"}],
"steps":[{"id":"book","event":"S12","subject":"appointment-a","after":"0s","expect":"accepted"}]}`
	if _, err := scenario.PreviewDocument([]byte(v1)); err == nil || !strings.Contains(err.Error(), "only a resource names an appointment") {
		t.Fatalf("a v1 subject named an appointment: %v", err)
	}
}
