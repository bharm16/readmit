package desktop

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/testlicense"
)

// A saved FHIR lifecycle and its unchanged expectations run through the
// facade's adapter exactly as through the shared service the command line
// uses: the same checks decide, and the answer is the retained result.
func TestConnectedLifecycleAdapterRunsTheSharedServiceWithSavedExpectations(t *testing.T) {
	app := New(folderAnswer(t.TempDir()), ShellDocuments{Folder: t.TempDir()})
	if selected := app.SelectOperationPolicy(testlicense.New(t)); selected.State != Completed {
		t.Fatal(selected)
	}
	created := app.CreateNamedProject(NewProjectRequest{Name: "Scheduling", Location: app.ChooseProjectLocation().Location})
	if created.State != Completed {
		t.Fatalf("%+v", created)
	}
	root := created.Context.Project
	h := connectedlab.New(t, root)
	h.Lab.SetDownstream("encounter")
	query := "identifier=urn%3Areadmit-lab%3Aappointment%7CDESKTOP-1"
	appointments := h.Observe("book", "appointments", "after", "Appointment", query, "reference-fhir-store",
		connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.IdentityColumn())
	encounters := h.Observe("book", "encounters", "after", "Encounter", query, "authoritative-application-api",
		connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.ReferenceColumn("appointment", "appointment#0", "reference"))
	datasets := []connectedtest.Dataset{appointments, encounters}
	body := `{"resourceType":"Appointment","identifier":[{"system":"urn:readmit-lab:appointment","value":"DESKTOP-1"}],"status":"booked","start":"2026-05-01T09:00:00Z","participant":[{"status":"accepted"}]}`
	h.Compile(connectedtest.FlowTest{ID: "desktop-booking", Steps: []connectedtest.Step{h.FHIRStep("create", "POST", "Appointment", body, connectedtest.FHIRHeaders{}, nil)}, Phases: []connectedtest.FlowPhase{{ID: "book", Steps: []string{"create"}, Datasets: datasets, Responses: []connectedtest.ResponseCheck{{ID: "created", Step: "create", Outcome: "succeeded"}},
		Checks: h.Checks("book", datasets, connectedlab.RowCount("one-appointment", "appointments", 1), connectedlab.RowCount("one-encounter", "encounters", 1), connectedlab.Related("linked", "encounters", "appointment", "appointments", "identity"))}}})
	request := connectedLifecycleRequest{Context: created.Context, Plan: "flow-plan", Config: "flow-config.json", Instance: "desktop-run"}

	// Outside an admitted execution nothing is prepared or sent.
	h.Prepare("desktop-run")
	if _, declined := app.runConnectedLifecycle(context.Background(), request); declined.reason == "" || h.Lab.Creates.Load() != 0 {
		t.Fatal("the adapter ran without execution admission")
	}
	// A plan outside the project is never read.
	outside := request
	outside.Plan = filepath.Join(t.TempDir(), "flow-plan")
	var view ConnectedLifecycleView
	var declined refusal
	runNamed[CleanRunResult, *CleanRunResult](app, profiles["StartDurableRun"], func(ctx context.Context) CleanRunResult {
		if _, refused := app.runConnectedLifecycle(ctx, outside); refused.reason == "" {
			t.Error("a plan outside the project was accepted")
		}
		view, declined = app.runConnectedLifecycle(ctx, request)
		return CleanRunResult{State: Completed}
	})
	if declined.reason != "" || view.State != "complete" || view.Verdict != assertion.VerdictPass || len(view.Qualification) != 2 {
		t.Fatalf("adapter outcome %+v %v", view, declined)
	}
	if _, err := os.Stat(filepath.Join(root, view.Output, "manifest.json")); err != nil {
		t.Fatal("the adapter's result is not a retained project entry", err)
	}
	raw, _ := json.Marshal(view)
	for _, private := range []string{"DESKTOP-1", root, h.Lab.Base()} {
		if strings.Contains(string(raw), private) {
			t.Fatal("the adapter's view carries private evidence", private)
		}
	}
	// The same saved plan and expectations through the service directly.
	direct, _ := h.Run("service-run")
	if direct.Verdict != view.Verdict || len(direct.Phases) != len(view.Phases) {
		t.Fatal("the adapter and the service disagree")
	}
	for i, phase := range direct.Phases {
		a, _ := json.Marshal(phase.Checks)
		b, _ := json.Marshal(view.Phases[i].Checks)
		if string(a) != string(b) {
			t.Fatal("the adapter evaluated different checks", string(a), string(b))
		}
	}
	reopened, declined := app.openConnectedLifecycle(t.Context(), created.Context, view.Output)
	a, _ := json.Marshal(reopened)
	if declined.reason != "" || string(a) != string(raw) {
		t.Fatal("the retained result does not reopen to the same view", declined)
	}
}
