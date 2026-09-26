package desktop_test

// Nothing the window does on its own reaches a destination. Every destination
// a person can configure — a test target, a customer hub, a runner's hub and
// the commercial portal — is a live loopback listener that counts what reaches
// it, and name resolution is replaced by a resolver that refuses and counts
// every lookup. The window is then started again over the retained state, the
// way a new process starts, and everything it does without a click — startup,
// restoring the session and drafts, reopening where the person was, the
// environment screen's reads, and each inspection of a configured document —
// runs against them. The only connection is the one the explicit execution
// made before the restart.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/testlicense"
)

// countingEndpoint is one configured destination: it counts every connection
// that reaches it and, when acknowledging, answers one framed message with an
// accepting acknowledgement the way the independent fixture target does.
type countingEndpoint struct {
	address  string
	accepted atomic.Int64
}

func newCountingEndpoint(t *testing.T, acknowledge bool) *countingEndpoint {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	endpoint := &countingEndpoint{address: listener.Addr().String()}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			endpoint.accepted.Add(1)
			go func() {
				defer conn.Close()
				if !acknowledge {
					return
				}
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				_, _ = fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
			}()
		}
	}()
	return endpoint
}

func TestStartupRestorationAndInspectionReachNoConfiguredDestination(t *testing.T) {
	var lookups atomic.Int64
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		lookups.Add(1)
		return nil, errors.New("lookup refused")
	}}
	defer func() { net.DefaultResolver = previous }()

	target := newCountingEndpoint(t, true)
	hub := newCountingEndpoint(t, false)
	runnerHub := newCountingEndpoint(t, false)
	portal := newCountingEndpoint(t, false)

	// The window creates its own state folder, as it does in the user
	// configuration directory on first launch.
	state := filepath.Join(t.TempDir(), "readmit")
	configuration := t.TempDir()
	destinations := filepath.Join(configuration, "destinations.json")
	writeDocument(t, configuration, "destinations.json",
		`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://`+portal.address+`/checkouts"}`)
	hubConfig := writeHubClientConfig(t, configuration, "https://"+hub.address)
	window := func(chooser desktop.FolderChooser) *desktop.App {
		return desktop.NewWithOperationSelection(chooser, desktop.ShellDocuments{Folder: state})
	}

	// The first window configures every destination and executes once.
	workspace := ackWorkspace(t, target.address)
	writeAckSpec(t, workspace, "spec.json", "AA")
	first := window(&queueChooser{batches: [][]string{{destinations}}})
	if result := first.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := first.ChooseCommercialDestinations(); result.State != desktop.Completed {
		t.Fatalf("destinations: %+v", result)
	}
	if result := first.SelectHubConfig(hubConfig); result.State != desktop.Completed {
		t.Fatalf("hub configuration: %+v", result)
	}
	runnerRequest := runnerConfigInput(t, filepath.Join(state, "runner-root"))
	runnerRequest.Hub = "https://" + runnerHub.address
	runnerRequest.Output = filepath.Join(configuration, "runner.json")
	if result := first.SaveRunnerConfig(runnerRequest); result.State != desktop.Completed {
		t.Fatalf("runner configuration: %+v", result)
	}
	job := filepath.Join(configuration, "job.json")
	if result := first.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: writableSpec(t, t.TempDir()), Output: job}); result.State != desktop.Completed {
		t.Fatalf("runner job: %+v", result)
	}
	preflight := first.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "spec.json", Output: "run"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight: %+v", preflight)
	}
	// The destination is disclosed before anything reaches it.
	if preflight.Preflight.Target.Address != target.address || target.accepted.Load() != 0 {
		t.Fatalf("the preflight did not disclose the destination without reaching it: %+v, %d connections", preflight.Preflight.Target, target.accepted.Load())
	}
	executed := first.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "spec.json", Output: "run", Expected: preflight.Preflight.Identity})
	if executed.State != desktop.Completed || target.accepted.Load() != 1 {
		t.Fatalf("the explicit execution: %+v, %d connections", executed, target.accepted.Load())
	}
	if result := first.RecordView(desktop.View{Workspace: resolved(t, workspace), Region: "evidence", Case: "case", Run: filepath.Join(resolved(t, workspace), "run")}); result.State != desktop.Completed {
		t.Fatalf("view: %+v", result)
	}
	recorded := first.ReadTarget(workspace, "lab-target.json")
	if recorded.State != desktop.Completed || recorded.Target == nil {
		t.Fatalf("new named environment: %+v", recorded)
	}
	environment := *recorded.Target
	environment.Name, environment.Classification, environment.Address = "lab-mllp", "nonproduction", target.address
	if result := first.SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "lab-target.json", Target: environment}); result.State != desktop.Completed {
		t.Fatalf("named environment: %+v", result)
	}
	writeDocument(t, workspace, "send-policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	if result := first.OpenWorkspace(workspace); result.State != desktop.Completed {
		t.Fatalf("open: %+v", result)
	}
	draft := editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)
	draft.Workspace = resolved(t, workspace)
	if result := first.SaveEditorDraft(draft); result.State != desktop.Completed {
		t.Fatalf("draft: %+v", result)
	}

	// Everything the window keeps outside evidence — the recent folders, the
	// session, the drafts and its selections — is readable by its owner only.
	ownerOnly(t, state)

	// The second window is a new process over the same retained state. What
	// the interface calls without a click comes first, then reopening where
	// the person was, then every inspection of a configured destination.
	second := window(&queueChooser{})
	shell := second.Shell()
	if shell.State != desktop.Completed {
		t.Fatalf("shell: %+v", shell)
	}
	for name, state := range map[string]desktop.State{
		"filters":          second.Filters().State,
		"operation status": second.OperationStatus().State,
	} {
		if state != desktop.Completed && state != desktop.Empty {
			t.Errorf("%s at startup: %s", name, state)
		}
	}
	if reopened := second.OpenDurableRun(filepath.Join(resolved(t, workspace), "run")); reopened.State != desktop.Completed || reopened.Run == nil {
		t.Fatalf("reopening the run: %+v", reopened)
	}
	if drafts := second.EditorDrafts(); drafts.State != desktop.Completed || len(drafts.Drafts) != 1 {
		t.Fatalf("drafts: %+v", drafts)
	}
	if status := second.CommercialStatus(); status.State != desktop.Completed || !strings.Contains(status.Portal, portal.address) {
		t.Fatalf("the portal destination is not disclosed after a restart: %+v", status)
	}
	// The hub configuration is restored as selected and offline: the
	// selection is read back, and nothing is connected or signed in.
	if status := second.HubStatus(); status.State != desktop.Completed || status.ConfigPath != hubConfig || status.Connected || status.Authenticated {
		t.Fatalf("a restarted window did not restore the hub selection offline: %+v", status)
	}
	disclosed := map[string]string{}
	disclosure := second.DisclosureStatus()
	for _, state := range disclosure.States {
		disclosed[state.ID] = state.State
	}
	for id, want := range map[string]string{"run": "idle", "runner": "idle", "capture": "idle", "environment": "idle", "observe": "idle", "hub": "offline", "declared-program": "idle", "portal": "configured"} {
		if disclosed[id] != want {
			t.Errorf("after a restart %s is %q, want %q: %+v", id, disclosed[id], want, disclosure)
		}
	}

	if result := second.OpenWorkspace(workspace); result.State != desktop.Completed {
		t.Fatalf("reopen: %+v", result)
	}
	if result := second.OpenCase(workspace, "case"); result.State != desktop.Completed {
		t.Fatalf("reopen case: %+v", result)
	}
	second.Guide(workspace)
	if result := second.ReadTarget(workspace, "lab-target.json"); result.State != desktop.Completed || result.Target == nil || result.Target.Address != target.address {
		t.Fatalf("the environment screen does not disclose the recorded destination: %+v", result)
	}
	second.ReadSecrets(workspace, "secrets.json")
	if result := second.ReadSendPolicy(workspace, "send-policy.json"); result.State != desktop.Completed {
		t.Fatalf("send policy: %+v", result)
	}
	second.ReadResetPlan(workspace, "reset-plan.json")
	if result := second.OpenDurableRun(filepath.Join(workspace, "run")); result.State != desktop.Completed {
		t.Fatalf("reopen run: %+v", result)
	}
	second.DurableRunProgress(workspace, "run")
	if again := second.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "spec.json", Output: "run-again"}); again.State != desktop.Completed || again.Preflight.Target.Address != target.address {
		t.Fatalf("preflight after a restart: %+v", again)
	}
	if result := second.ReadRunnerConfig(runnerRequest.Output); result.State != desktop.Completed || result.Config == nil || !strings.Contains(result.Config.Hub, runnerHub.address) {
		t.Fatalf("the runner screen does not disclose its hub: %+v", result)
	}
	second.InspectRunnerJob(runnerRequest.Output, job)
	second.ShowRunnerAdmissions()
	second.ScenarioCatalog()
	second.ObservationSupport()
	// Selecting a hub configuration again is a local read: it is configured
	// and offline, and connecting stays a separate deliberate action.
	if result := second.SelectHubConfig(hubConfig); result.State != desktop.Completed || result.Connected {
		t.Fatalf("selecting the hub again: %+v", result)
	}
	if status := second.HubStatus(); status.Connected || status.Authenticated {
		t.Fatalf("selecting a hub connected to it: %+v", status)
	}
	second.ExplainHubCustody()

	ownerOnly(t, state)
	for name, endpoint := range map[string]*countingEndpoint{"test target": target, "hub": hub, "runner hub": runnerHub, "portal": portal} {
		want := int64(0)
		if endpoint == target {
			want = 1
		}
		if got := endpoint.accepted.Load(); got != want {
			t.Errorf("the %s received %d connections; only the explicit execution may reach a destination", name, got)
		}
	}
	if n := lookups.Load(); n != 0 {
		t.Errorf("the window resolved %d names without being asked to reach anything", n)
	}
}

// ownerOnly requires the shell's state folder and every document in it to be
// private to the account that runs the window, and all five documents it
// writes on the way here to exist.
func ownerOnly(t *testing.T, state string) {
	t.Helper()
	info, err := os.Stat(state)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the shell state folder is not private: %v %v", info.Mode(), err)
	}
	for _, name := range []string{"session.json", "drafts.json", "operations.json", "commercial.json", "hub.json"} {
		info, err := os.Lstat(filepath.Join(state, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is not a private regular file: %v %v", name, info, err)
		}
	}
}

// writeHubClientConfig writes a complete customer hub client configuration
// naming hub, with real certificate material and a key program, so selecting
// it is the same local read a real selection is.
func writeHubClientConfig(t *testing.T, dir, hub string) string {
	t.Helper()
	authority := newHubTestAuthority(t, "customer-hub-ca")
	certificate, key := authority.issue(t, "analyst@hospital.example", false)
	ca := filepath.Join(dir, "hub-ca.pem")
	client := filepath.Join(dir, "hub-client.pem")
	keyFile := filepath.Join(dir, "hub-client-key.pem")
	for path, data := range map[string][]byte{ca: authority.pem, client: certificate, keyFile: key} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(dir, "hub-client.json")
	writeDocument(t, dir, "hub-client.json", fmt.Sprintf(`{"schema":"readmit-hub-client/v1","hub":%q,"ca":%q,"certificate":%q,`+
		`"key":{"command":"/bin/cat","arguments":[%q]},`+
		`"idp":{"issuer":"https://idp.hospital.example","client_id":"readmit-desktop","audience":"hub","authorize_endpoint":"https://idp.hospital.example/authorize","token_endpoint":"https://idp.hospital.example/token","scopes":["evidence.read"]},`+
		`"projects":["alpha"]}`, hub, ca, client, keyFile))
	return config
}

// destinationActivities is the reviewed inventory of bound operations that can
// reach something outside this window — a connection, a listener or a name
// lookup — each with the disclosed activity whose row states its destination,
// data and authorization. It is a list, not an enumeration: an operation that
// starts reaching a destination has to be added here, and one the facade's own
// source shows reaching a destination fails TestEveryOperationThatCanReachADestinationRunsUnderADisclosedName
// until it is. Each runs only on the explicit action that calls it, which the
// test above holds for everything else, and under a name the privacy status
// reports as its activity. A program an operator declared, which may itself
// connect, is inventoried in operationsRunningDeclaredPrograms.
var destinationActivities = map[string]string{
	"StartDurableRun": "run", "ResumeDurableRun": "run", "StartSuiteRun": "run", "RunPractice": "run",
	"DeriveExportReview": "run", "ExportDerivedPacket": "run", "GenerateSyntheticPacket": "run", "SendReplay": "run", "ReexecuteReviewedEvidence": "run",
	"EnrollRunner": "runner", "ExecuteRunnerJob": "runner",
	"DiagnoseSource": "capture", "CollectSource": "capture", "StartCapture": "capture",
	"CheckTarget": "environment", "ResetTarget": "environment", "EvaluateSendPolicy": "environment", "StartReduction": "environment", "PreviewReplay": "environment",
	"CollectObservation": "observe",
	"DiagnoseHub":        "hub", "ConnectHub": "hub", "StartHubAuth": "hub", "CompleteHubAuth": "hub", "HubStatus": "hub",
	"ListHubProjectArtifacts": "hub", "DownloadHubArtifact": "hub", "UploadHubArtifact": "hub",
	"ListHubReviews": "hub", "SearchHubReviews": "hub", "ListHubNotifications": "hub", "SearchHubNotifications": "hub",
	"PostHubReview": "hub", "PostHubReleaseReview": "hub", "PostHubSupportReview": "hub",
	"ListHubLifecycle": "hub", "PostHubLifecycle": "hub", "DownloadHubExport": "hub", "ReconcileHubOfflineDraft": "hub",
	"ConnectOperatorHub": "hub", "ReadOperatorHubArtifact": "hub", "StoreOperatorHubArtifact": "hub",
}

// operationsRunningDeclaredPrograms is the reviewed inventory of bound
// operations that can run a program an operator declared: the locator of a
// credential reference, the key command of a hub configuration, the key and
// token commands of a runner configuration, the key program of a protection
// control, the locator naming the private key a target's client certificate
// or a TLS capture listener presents, a source's transfer program or
// credential locator, and an observation source's credential locator.
// Readmit runs such a program by the absolute path the operator declared and
// cannot see where it connects, so each runs under a name the privacy
// status's declared-program row has a sentence for, and the row is active
// while the program runs. An operation that also reaches a destination of its
// own is in destinationActivities too.
//
// It is a list, not an enumeration: finding the operations whose calls reach a
// program would need the engine's types, not only the facade's source. Every
// program Readmit starts reports itself, whichever operation runs it
// (TestOnlyDeclaredProgramsAreStarted), so a named operation missing here
// still shows the row active, in a sentence that does not say whose program
// it is; one that holds the slot without a name is answered busy, never idle.
var operationsRunningDeclaredPrograms = []string{
	"TestSecretReference", "RotateSecretReference", "ScanSecrets",
	"RotateProtectionControl", "PackProtectedPackage", "OpenProtectedPackage",
	"DiagnoseHub", "ConnectHub", "CompleteHubAuth", "ConnectOperatorHub",
	"EnrollRunner", "ExecuteRunnerJob",
	"CheckTarget", "ResetTarget", "StartReduction",
	"DiagnoseSource", "CollectSource", "StartCapture",
	"CollectObservation",
}

// The privacy status covers the reviewed inventories: every operation in them
// belongs to an activity the window discloses, and every disclosed activity
// is reached by something, so neither a blanket claim nor a stale row can
// stand in for the lists.
func TestEveryDestinationReachingOperationIsADisclosedActivity(t *testing.T) {
	disclosed := map[string]bool{}
	for _, operation := range shell(t).Privacy.Operations {
		disclosed[operation.ID] = true
	}
	reached := map[string]bool{"portal": true} // the portal is reached by the person's browser, never by the window
	bound := reflect.TypeOf(&desktop.App{})
	for name, activity := range destinationActivities {
		if _, ok := bound.MethodByName(name); !ok {
			t.Errorf("%s reaches %q but is not a bound operation", name, activity)
		}
		if !disclosed[activity] {
			t.Errorf("%s reaches a destination under %q, which the privacy status does not disclose", name, activity)
		}
		reached[activity] = true
	}
	for _, name := range operationsRunningDeclaredPrograms {
		if _, ok := bound.MethodByName(name); !ok {
			t.Errorf("%s runs a declared program but is not a bound operation", name)
		}
		reached["declared-program"] = true
	}
	if !disclosed["declared-program"] {
		t.Error("operations run programs an operator declared, and the privacy status does not disclose them")
	}
	for activity := range disclosed {
		if !reached[activity] {
			t.Errorf("the privacy status discloses %q, which no operation reaches", activity)
		}
	}
}
