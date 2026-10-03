package desktop_test

import (
	"fmt"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/mllp"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/observeinterval"
)

func captureObservation(t *testing.T, f *connectedAuthoring, source desktop.ItemRef) desktop.ItemRef {
	t.Helper()
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "received-hl7", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "SCH-1", Key: true, Required: true}, {Name: "status", Type: "text", Selector: "SCH-2", Required: true}, {Name: "tag", Type: "text", Selector: "SCH-3[2]", Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 1 << 20, TimeoutMS: 5000}}
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, IntentID: "received-observation", Draft: desktop.ItemDraft{Name: "Received appointment", Observation: &desktop.ObservationDraft{Connected: &desktop.ConnectedObservation{Schema: desktop.ConnectedObservationSchema, Phase: "after", Namespace: "received", Baseline: "before-run", BusinessKeys: []desktop.BusinessKeyMapping{{Field: "key", Variable: "appointment-key"}}, Projection: &projection, Capture: &desktop.ConnectedCaptureObservation{Source: source, RunSelector: "MSH-3", OutputKeySelector: "MSH-10", InputKeySelector: "MSH-10", Include: []observeinterval.ScopeFilter{}}, Completion: observeinterval.Definition{Schema: observeinterval.Schema, Enabled: true, Mode: "stream", Freshness: "ingress", HorizonMS: 100, SampleMS: 100, MaxGapMS: 1000, MaxSamples: 400, MaxRecords: 10, MaxBytes: 1 << 20}}}}})
	if saved.Saved == nil {
		t.Fatalf("captured observation: %+v", saved)
	}
	return *saved.Saved
}
func captureTestDraft(f *connectedAuthoring, observation desktop.ItemRef) desktop.ConnectedTestDraft {
	draft := f.reschedule()
	draft.Boundary = desktop.EngineOutputBoundary
	draft.Server = ""
	draft.Steps = draft.Steps[:1]
	draft.Steps[0].V2 = &desktop.ConnectedV2{RuntimeMarkerSelector: "MSH-3"}
	draft.Phases = draft.Phases[:1]
	draft.Phases[0].Steps = []string{draft.Steps[0].ID}
	draft.Phases[0].Observations = []desktop.ConnectedPhaseObservation{{Dataset: "received", Observation: observation, When: "after"}}
	expected := dataset.Value{State: "present", Type: "text", Text: "booked"}
	one := 1
	draft.Phases[0].Checks = []desktop.ConnectedCheck{{Name: "One matching output", Check: assertion.DatasetAssertion{ID: "count", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "received", Where: []assertion.RowFilter{}}, Count: &one}}, {Name: "Authored status", Check: assertion.DatasetAssertion{ID: "status", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "received", Where: []assertion.RowFilter{}}, Column: "status", Expected: &expected}}}
	tag := dataset.Value{State: "present", Type: "text", Text: "second"}
	draft.Phases[0].Checks = append(draft.Phases[0].Checks, desktop.ConnectedCheck{Name: "Second repetition", Check: assertion.DatasetAssertion{ID: "second-repeat", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "received", Where: []assertion.RowFilter{}}, Column: "tag", Expected: &tag}})
	draft.Phases[0].Acks = nil
	return draft
}
func savedCaptureTest(t *testing.T, f *connectedAuthoring, observation desktop.ItemRef) desktop.ItemRef {
	t.Helper()
	return f.save(t, "Received booking status", "capture-test", captureTestDraft(f, observation), f.v2.ID)
}
func TestSavedReceivedHL7TestCompilesOnlyExplicitSupportedRuntimeScope(t *testing.T) {
	f := newConnectedAuthoring(t)
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	socket.Close()
	source := exchangeSource(t, f.app, f.context, port)
	observation := captureObservation(t, f, source)
	savedCaptureTest(t, f, observation)
	duplicate := captureTestDraft(f, observation)
	step := duplicate.Steps[0]
	step.ID = "duplicate"
	duplicate.Steps = append(duplicate.Steps, step)
	duplicate.Phases[0].Steps = append(duplicate.Phases[0].Steps, step.ID)
	refused := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, IntentID: "duplicate-capture-input", Draft: desktop.ItemDraft{Name: "Duplicate intended identity", ConnectedTest: &duplicate, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	if refused.State != desktop.Failed || !strings.Contains(fmt.Sprint(refused.Problems), "unique original identity") {
		t.Fatalf("duplicate intended inputs admitted: %+v", refused)
	}
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: observation})
	opened.Draft.Name = "Unreadable input key"
	opened.Draft.Observation.Connected.Capture.InputKeySelector = "SCH-99"
	changed := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, IntentID: "unreadable-capture-key", Draft: *opened.Draft})
	if changed.Saved == nil {
		t.Fatalf("declaration: %+v", changed)
	}
	unreadable := captureTestDraft(f, *changed.Saved)
	refused = f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, IntentID: "unreadable-capture-input", Draft: desktop.ItemDraft{Name: "Unreadable intended identity", ConnectedTest: &unreadable, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	if refused.State != desktop.Failed || !strings.Contains(fmt.Sprint(refused.Problems), "readable, nonempty") {
		t.Fatalf("unreadable intended inputs admitted: %+v", refused)
	}
	incompatible := captureTestDraft(f, observation)
	incompatible.Server = f.fhir.ID
	incompatible.Phases[0].Observations = append(incompatible.Phases[0].Observations, desktop.ConnectedPhaseObservation{Dataset: "application", Observation: f.appointments, When: "after"})
	refused = f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, IntentID: "mixed-capture-fhir", Draft: desktop.ItemDraft{Name: "Unsupported capture with FHIR", ConnectedTest: &incompatible, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	if refused.State != desktop.Failed || !strings.Contains(fmt.Sprint(refused.Problems), "v2-only") {
		t.Fatalf("unsupported composition admitted: %+v", refused)
	}
	overlapping := captureTestDraft(f, observation)
	same := overlapping.Steps[0]
	same.ID = "again"
	same.After = []string{overlapping.Steps[0].ID}
	overlapping.Steps = append(overlapping.Steps, same)
	second := overlapping.Phases[0]
	second.ID = "late-phase"
	second.Steps = []string{same.ID}
	second.After = []connectedtest.PhaseDependency{{Phase: overlapping.Phases[0].ID, Requires: "pass"}}
	overlapping.Phases = append(overlapping.Phases, second)
	refused = f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, IntentID: "overlapping-capture-phases", Draft: desktop.ItemDraft{Name: "Indistinguishable late prior output", ConnectedTest: &overlapping, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	if refused.State != desktop.Failed || !strings.Contains(fmt.Sprint(refused.Problems), "late prior-phase output would be indistinguishable") {
		t.Fatalf("overlapping phase keys claimed attribution: %+v", refused)
	}
	if f.lab.Creates.Load() != 0 {
		t.Fatal("authoring admission executed setup")
	}
}

func TestSavedReceivedHL7NormalRunArmsBeforeStimulusAndRetainsActualScopedChecks(t *testing.T) {
	f := newConnectedAuthoring(t)
	sink, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := sink.Addr().String()
	port := sink.Addr().(*net.TCPAddr).Port
	sink.Close()
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var mode atomic.Value
	mode.Store("defective")
	var dispatched atomic.Int64
	errors := make(chan error, 20)
	go func() {
		for {
			connection, err := target.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				connection.SetDeadline(time.Now().Add(30 * time.Second))
				reader, _ := mllp.NewReader(connection, 65536)
				frame, err := reader.ReadFrame()
				if err != nil {
					errors <- err
					return
				}
				dispatched.Add(1)
				input := string(frame[1 : len(frame)-2])
				fields := strings.Split(strings.Split(input, "\r")[0], "|")
				marker, control := fields[2], fields[9]
				output, err := net.DialTimeout("tcp", address, time.Second)
				if err != nil {
					errors <- fmt.Errorf("capture not ready before actual stimulus: %w", err)
					return
				}
				output.SetDeadline(time.Now().Add(30 * time.Second))
				defer output.Close()
				status := "booked"
				selected := mode.Load().(string)
				if selected == "defective" {
					status = "wrong"
				}
				if selected == "foreign" {
					marker = strings.Repeat("0", 32)
				}
				if selected != "quiet" {
					body := "MSH*@$%!*" + marker + "*ENGINE***20260301090000**SIU@S12*" + control + "*P*2.5.1\rSCH*APT-1*" + status + "*first$second\r"
					if selected == "phase-late" && control == "MOVE-1" {
						late := strings.Replace(body, "*MOVE-1*", "*BOOK-1*", 1)
						late = strings.Replace(late, "*booked*", "*wrong*", 1)
						output.Write(mllp.Frame([]byte(late)))
						acks, _ := mllp.NewReader(output, 65536)
						if _, err := acks.ReadFrame(); err != nil {
							errors <- err
							return
						}
					}
					output.Write(mllp.Frame([]byte(body)))
					acks, _ := mllp.NewReader(output, 65536)
					if _, err := acks.ReadFrame(); err != nil {
						errors <- err
						return
					}
					if selected == "ambiguous" {
						output.Write(mllp.Frame([]byte(body)))
						if _, err := acks.ReadFrame(); err != nil {
							errors <- err
							return
						}
					}
				}
				fmt.Fprintf(connection, "\x0bMSH|^~\\&|ENGINE|LAB|||20260301090000||ACK|ACK-1|P|2.5.1\rMSA|AA|%s\r\x1c\r", control)
			}()
		}
	}()
	environment := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: f.v2})
	environment.Draft.Environment.Address = target.Addr().String()
	changed := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.EnvironmentItem, Item: f.v2.ID, BaseRevision: f.v2.Revision, IntentID: "captured-target", Draft: *environment.Draft})
	if changed.Saved == nil {
		t.Fatalf("target: %+v", changed)
	}
	f.v2 = *changed.Saved
	source := exchangeSource(t, f.app, f.context, port)
	observation := captureObservation(t, f, source)
	test := savedCaptureTest(t, f, observation)
	f.context.ProjectID = "" // Normal UI carries its logical scope; the facade owns the immutable project identity.
	for index, selected := range []string{"defective", "fixed", "foreign", "quiet", "ambiguous"} {
		t.Run(selected, func(t *testing.T) {
			before := dispatched.Load()
			mode.Store(selected)
			issued := f.app.IssueExchangeRuntimeMarker(f.context)
			if issued.State != desktop.Completed {
				t.Fatalf("marker: %+v", issued)
			}
			request := desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}, Run: &desktop.RunActionOptions{RuntimeMarker: issued.Marker}}
			review := prepared(t, f.app, request)
			if dispatched.Load() != before {
				t.Fatal("review sent a stimulus")
			}
			result := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: fmt.Sprintf("received-run-%d", index)})
			select {
			case err := <-errors:
				t.Fatal(err)
			default:
			}
			if result.Run == nil || result.Lifecycle == nil {
				t.Fatalf("actual captured run: %+v", result)
			}
			if selected == "fixed" && result.Lifecycle.Verdict != assertion.VerdictPass {
				interval, err := observeinterval.Open(t.Context(), filepath.Join(f.root, result.Lifecycle.Output, "phases", "reschedule", "intervals", "received"))
				t.Fatalf("corrected output failed: %+v intervalreason %s records %d err %v", result.Lifecycle, interval.Reason, len(interval.Records), err)
			}
			if selected == "defective" && result.Lifecycle.Verdict != assertion.VerdictFail {
				proof, readErr := connectedrun.OpenFlowEvidence(t.Context(), filepath.Join(f.root, result.Lifecycle.Output))
				interval, intervalErr := observeinterval.Open(t.Context(), filepath.Join(f.root, result.Lifecycle.Output, "phases", "reschedule", "intervals", "received"))
				t.Fatalf("wrong output undecided: %+v proof %+v err %v interval %+v err %v", result.Lifecycle, proof.Result, readErr, interval, intervalErr)
			}
			detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *result.Run, Reveal: true})
			if detail.Run == nil || detail.Run.Lifecycle == nil {
				t.Fatalf("retained: %+v", detail)
			}
			observed := f.app.ReadConnectedObservation(desktop.ConnectedObservationRequest{Context: f.context, Run: *result.Run, Phase: "reschedule", Dataset: "received", Reveal: true})
			if selected == "foreign" || selected == "ambiguous" {
				reason := "wrong-scope"
				if selected == "ambiguous" {
					reason = "ambiguous-scope"
				}
				if !strings.Contains(observed.Reason, reason) {
					t.Fatalf("collection scope reason collapsed: %+v", observed)
				}
				if observed.Available || result.Lifecycle.Verdict == assertion.VerdictPass {
					t.Fatalf("foreign traffic credited: %+v", observed)
				}
			} else {
				if !observed.Available {
					t.Fatalf("healthy capture unavailable: %+v phase %+v", observed, detail.Run.Lifecycle)
				}
			}
			if selected == "quiet" && observed.Total != 0 {
				t.Fatalf("healthy no output: %+v", observed)
			}
			for _, support := range detail.Run.Lifecycle.Observations {
				if support.Dataset == "received" && support.CaptureIdentity != "" {
					retained := f.app.OpenConnectedCapture(desktop.ConnectedCaptureRequest{Context: f.context, Run: *result.Run, Phase: "reschedule", Dataset: "received", Identity: support.CaptureIdentity})
					if retained.State != desktop.Completed || retained.Case == nil || retained.Case.Identity != support.CaptureIdentity {
						t.Fatalf("supporting raw capture: %+v", retained)
					}
				}
			}
			if selected == "fixed" && (len(observed.Rows) != 1 || observed.Rows[0].Occurrence != "s0001-e000001") {
				t.Fatalf("actual supporting occurrence lost: %+v", observed)
			}
			if reused := f.app.PrepareAction(request); reused.State != desktop.Failed {
				t.Fatalf("runtime marker reuse admitted: %+v", reused)
			}
		})
	}
	t.Run("late-prior-phase", func(t *testing.T) {
		mode.Store("phase-late")
		ordered := captureTestDraft(f, observation)
		originalSteps := f.reschedule().Steps
		originalSteps[0].V2 = &desktop.ConnectedV2{RuntimeMarkerSelector: "MSH-3"}
		originalSteps[1].V2 = &desktop.ConnectedV2{RuntimeMarkerSelector: "MSH-3"}
		ordered.Steps = originalSteps
		book, move := ordered.Phases[0], ordered.Phases[0]
		book.ID, book.Name, book.Steps = "book", "Book", []string{originalSteps[0].ID}
		move.ID, move.Name, move.Steps = "move", "Move", []string{originalSteps[1].ID}
		move.After = []connectedtest.PhaseDependency{{Phase: "book", Requires: "pass"}}
		ordered.Phases = []desktop.ConnectedPhase{book, move}
		scoped := f.save(t, "Late prior phase scope", "phase-scope-capture", ordered, f.v2.ID)
		issued := f.app.IssueExchangeRuntimeMarker(f.context)
		review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{scoped}, Run: &desktop.RunActionOptions{RuntimeMarker: issued.Marker}})
		actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "phase-key-run"})
		if actual.Run == nil || actual.Lifecycle == nil || actual.Lifecycle.Verdict != assertion.VerdictPass {
			t.Fatalf("late prior-phase output was credited: %+v", actual)
		}
		observed := f.app.ReadConnectedObservation(desktop.ConnectedObservationRequest{Context: f.context, Run: *actual.Run, Phase: "move", Dataset: "received", Reveal: true})
		if !observed.Available || observed.Total != 1 || len(observed.Rows) != 1 || observed.Rows[0].Occurrence != "s0001-e000003" {
			t.Fatalf("wrong intended phase occurrence: %+v", observed)
		}
		detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *actual.Run, Reveal: true})
		for _, support := range detail.Run.Lifecycle.Observations {
			if support.Phase == "move" {
				retained := f.app.OpenConnectedCapture(desktop.ConnectedCaptureRequest{Context: f.context, Run: *actual.Run, Phase: "move", Dataset: "received", Identity: support.CaptureIdentity})
				if retained.State != desktop.Completed || retained.Case == nil {
					t.Fatalf("late supporting capture: %+v", retained)
				}
			}
		}

	})
}

func TestSavedReceivedHL7InsufficientSampleBudgetNeverEstablishesAbsence(t *testing.T) {
	f := newConnectedAuthoring(t)
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	socket.Close()
	source := exchangeSource(t, f.app, f.context, port)
	observation := captureObservation(t, f, source)
	draft := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: observation})
	draft.Draft.Observation.Connected.Completion.MaxSamples = 2
	draft.Draft.Observation.Connected.Completion.SampleMS = 1
	changed := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, Item: observation.ID, BaseRevision: observation.Revision, IntentID: "limited-stream", Draft: *draft.Draft})
	if changed.Saved == nil {
		t.Fatalf("limited observation: %+v", changed)
	}
	test := savedCaptureTest(t, f, *changed.Saved)
	issued := f.app.IssueExchangeRuntimeMarker(f.context)
	reviewed := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}, Run: &desktop.RunActionOptions{RuntimeMarker: issued.Marker}})
	result := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: reviewed.Token, IntentID: "limited-received-run"})
	if result.Run == nil || result.Lifecycle == nil || result.Lifecycle.Verdict == assertion.VerdictPass {
		t.Fatalf("incomplete capture became pass: %+v", result)
	}
	observed := f.app.ReadConnectedObservation(desktop.ConnectedObservationRequest{Context: f.context, Run: *result.Run, Phase: "reschedule", Dataset: "received"})
	if observed.Available || !strings.Contains(observed.Reason, "safety-limit") {
		t.Fatalf("insufficient capacity claimed zero output: %+v", observed)
	}
}
