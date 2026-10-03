package desktop_test

// A capture records in the background (#552): the window keeps reading while
// it runs, and whatever writes, sends, collects or executes waits for it,
// told which capture it waits for. What a capture that did not publish kept
// opens read-only; dropped files are sorted as a picker's choices; abandoned
// pasted sources are removed; and a TLS listener presents one of the
// project's named credentials.

import (
	"errors"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/secret"
)

func TestReadsAreAnsweredWhileACaptureRecordsAndWritesWait(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	retained := writeCase(t, root, "retained-read", framed(sampleImportHL7))
	source := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, source, "background-1")
	deliver(t, progress.BoundAddress)

	// Reads: the catalog, a saved object's draft, the capture history and a
	// draft's validation.
	if listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.SourceItem}); listed.State != desktop.Completed || listed.Page == nil || len(listed.Page.Items) != 1 {
		t.Fatalf("the catalog while a capture records: %+v", listed)
	}
	if draft := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: source}); draft.State != desktop.Completed || draft.Draft == nil {
		t.Fatalf("a draft while a capture records: %+v", draft)
	}
	history := app.ListCaptureSessions(context)
	if history.State != desktop.Completed || len(history.Sessions) != 1 || history.Sessions[0].State != desktop.CaptureRunning || history.Sessions[0].Retained {
		t.Fatalf("the history while a capture records: %+v", history)
	}
	if validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Name: "Other",
		Source: &desktop.CaptureSourceDraft{Type: desktop.APISource}}}); validated.State == desktop.Busy {
		t.Fatalf("a validation while a capture records: %+v", validated)
	}
	// An import's probe and preview read the chosen inputs and write
	// nothing, so they are answered too.
	export := filepath.Join(t.TempDir(), "feed.hl7")
	if err := os.WriteFile(export, []byte(sampleImportHL7), 0o600); err != nil {
		t.Fatal(err)
	}
	if probed := app.ProbeImport(desktop.ImportProbeRequest{Context: context, Files: []string{export}}); probed.State != desktop.Completed || len(probed.Inputs) != 1 {
		t.Fatalf("a probe while a capture records: %+v", probed)
	}
	plan := validDesktopPlan()
	if previewed := app.PreviewImport(desktop.ImportRequest{Context: context, Mode: "plan", Plan: &plan, Files: []string{export}}); previewed.State != desktop.Completed {
		t.Fatalf("a preview while a capture records: %+v", previewed)
	}
	// Field aggregation names its own cancellation, but reads only this
	// retained scope; it neither reveals values nor takes the capture's slot.
	values := app.ReadFieldValues(desktop.FieldValuesRequest{Scope: desktop.FieldValueScope{Workspace: root, Case: "retained-read", Identity: retained.Identity}, Selector: "MSH-10"})
	if values.State != desktop.Completed || values.Total != 1 || !values.ScanComplete || values.Revealed || len(values.Rows) != 1 || !values.Rows[0].Hidden || values.Rows[0].Value != "" {
		t.Fatalf("hidden field counts beside a capture: %+v", values)
	}
	// The capture itself is still read and still recording.
	if now := app.CaptureProgress(); now.Progress == nil || now.Progress.Received != 1 {
		t.Fatalf("progress beside the reads: %+v", now)
	}

	// Writes wait, naming the capture: a save, a paste, an import, and a
	// second capture.
	const waits = `the capture "Morning capture" is recording`
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "save-during",
		Draft: desktop.ItemDraft{Name: "Another", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource,
			Listener: &desktop.ListenerSettings{BindAddress: "127.0.0.1", Transport: desktop.PlainTransport, AckCode: "AA"}}}})
	if saved.State != desktop.Busy || !strings.HasPrefix(saved.Reason, waits) {
		t.Fatalf("a save while a capture records: %+v", saved)
	}
	if pasted := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7}); pasted.State != desktop.Busy || !strings.HasPrefix(pasted.Reason, waits) {
		t.Fatalf("a paste while a capture records: %+v", pasted)
	}
	if imported := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "During", IntentID: "import-during"}); imported.State != desktop.Busy || !strings.HasPrefix(imported.Reason, waits) {
		t.Fatalf("an import while a capture records: %+v", imported)
	}
	second := app.StartCapture(desktop.CaptureRequest{Context: context, Source: &source, Name: "Second", IntentID: "background-2"})
	if second.State != desktop.Busy || !strings.HasPrefix(second.Reason, waits) {
		t.Fatalf("a second capture: %+v", second)
	}

	// Finishing still publishes the one case, and the slot is free again.
	if finish := app.FinishCapture(); finish.State != desktop.Completed {
		t.Fatalf("finish: %+v", finish)
	}
	result := awaitCapture(t, done)
	if result.State != desktop.Completed || result.Outcome != desktop.CaptureFinished || len(registeredCases(t, root)) != 1 {
		t.Fatalf("the capture after reads beside it: %+v", result)
	}
	if after := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7}); after.State != desktop.Completed {
		t.Fatalf("a paste after the capture: %+v", after)
	}
}

func TestCancellingTheCaptureStillStopsItWhileAReadRuns(t *testing.T) {
	app, context := namedProject(t)
	source := listenerSource(t, app, context)
	done, _ := startedCapture(t, app, context, source, "background-cancel")
	stop := make(chan struct{})
	reading := make(chan struct{})
	go func() {
		defer close(reading)
		for {
			select {
			case <-stop:
				return
			default:
				app.ListCaptureSessions(context)
			}
		}
	}()
	app.Cancel("capture")
	result := awaitCapture(t, done)
	close(stop)
	<-reading
	if result.State != desktop.Cancelled || result.Outcome != desktop.CaptureCancelled {
		t.Fatalf("a capture cancelled beside reads: %+v", result)
	}
	if status := app.DisclosureStatus(); status.State != desktop.Completed {
		t.Fatalf("the privacy status after: %+v", status)
	}
}

// Only local reads run beside a capture. Named reads keep their own cancel
// controls but take no author/execution admission and reach no destination,
// as the facade's reviewed inventory requires.
func TestAReadThatRunsBesideACaptureReachesNothing(t *testing.T) {
	declared := desktop.DeclaredProfilesForTest()
	reaching, _ := reachingOf(t)
	beside, named := []string{}, []string{}
	for _, file := range parsePackage(t, ".") {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || !function.Name.IsExported() || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					fun := call.Fun
					if generic, ok := fun.(*ast.IndexListExpr); ok {
						fun = generic.X
					}
					if name, ok := fun.(*ast.Ident); ok && name.Name == "runRead" {
						beside = append(beside, function.Name.Name)
					}
					if name, ok := fun.(*ast.Ident); ok && name.Name == "runNamedRead" {
						named = append(named, function.Name.Name)
					}
				}
				return true
			})
		}
	}
	for _, method := range []string{"ListCatalog", "OpenItemDraft", "ReadMessages", "ListCaptureSessions", "OpenRetainedCapture"} {
		if !slices.Contains(beside, method) {
			t.Errorf("%s does not run beside a capture", method)
		}
	}
	// A read that names itself for its cancel control takes no admission.
	slices.Sort(named)
	if !slices.Equal(named, []string{"PreviewImport", "ProbeImport", "ReadFieldValues"}) {
		t.Errorf("the named reads beside a capture are %v", named)
	}
	for _, method := range named {
		profile, known := declared[method]
		if !known || profile.Name == "" || profile.Author || profile.Execution != operationguard.NoExecution {
			t.Errorf("%s runs beside a capture and takes admission: %+v", method, profile)
		}
		if why := reaching[method]; len(why) > 0 {
			t.Errorf("%s runs beside a capture and can reach a destination: %v", method, why)
		}
	}
	for _, method := range beside {
		if _, named := declared[method]; named {
			t.Errorf("%s runs beside a capture and declares a named profile", method)
		}
		if why := reaching[method]; len(why) > 0 {
			t.Errorf("%s runs beside a capture and can reach a destination: %v", method, why)
		}
	}
}

func TestACancelledCapturesRetainedDataOpensReadOnly(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	source := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, source, "retained-1")
	deliver(t, progress.BoundAddress)
	app.Cancel("capture")
	cancelled := awaitCapture(t, done)

	history := app.ListCaptureSessions(context)
	if len(history.Sessions) != 1 || !history.Sessions[0].Retained {
		t.Fatalf("the cancelled session's row: %+v", history)
	}
	before := digestTree(t, root)
	opened := app.OpenRetainedCapture(desktop.CaptureSessionRequest{Context: context, Session: cancelled.Session})
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Messages != 1 || opened.Workspace == "" {
		t.Fatalf("the retained data: %+v", opened)
	}
	// Its messages and their acknowledgements read as any case's do, by the
	// folder, name and identity.
	messages := app.ReadMessages(desktop.MessagesRequest{Workspace: opened.Workspace, Case: opened.Case.Name, Identity: opened.Case.Identity, Sort: grid.EvidenceOrder})
	if messages.State != desktop.Completed || len(messages.Rows) != 2 || messages.Rows[0].MessageCode != "ADT" {
		t.Fatalf("the retained messages: %+v", messages)
	}
	if after := digestTree(t, root); after != before || len(registeredCases(t, root)) != 0 {
		t.Fatal("opening retained data changed the project")
	}

	// A finished capture is opened as the case it registered.
	done, progress = startedCapture(t, app, context, source, "retained-2")
	deliver(t, progress.BoundAddress)
	app.FinishCapture()
	finished := awaitCapture(t, done)
	if refused := app.OpenRetainedCapture(desktop.CaptureSessionRequest{Context: context, Session: finished.Session}); refused.State != desktop.Failed || refused.Case != nil {
		t.Fatalf("a finished capture opened as retained data: %+v", refused)
	}
	if refused := app.OpenRetainedCapture(desktop.CaptureSessionRequest{Context: context, Session: "../elsewhere"}); refused.State != desktop.Failed {
		t.Fatalf("a session that is not one: %+v", refused)
	}
}

func TestDroppedFilesAreSortedAsTheirPickerChoices(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	folder := t.TempDir()
	file, archive, inner := filepath.Join(folder, "one.hl7"), filepath.Join(folder, "exports.ZIP"), filepath.Join(folder, "nested")
	writeDocument(t, folder, "one.hl7", sampleImportHL7)
	writeDocument(t, folder, "exports.ZIP", "PK")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(folder, "link.hl7")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	sorted := app.ClassifyDroppedSources([]string{file, inner, archive, link, filepath.Join(folder, "gone.hl7"), "relative.hl7"})
	if sorted.State != desktop.Completed || !slices.Equal(sorted.Files, []string{file}) || !slices.Equal(sorted.Folders, []string{inner}) ||
		!slices.Equal(sorted.Archives, []string{archive}) || len(sorted.Refused) != 3 || sorted.Refused[0].Name != "link.hl7" {
		t.Fatalf("a drop: %+v", sorted)
	}
	if empty := app.ClassifyDroppedSources(nil); empty.State != desktop.Empty {
		t.Fatalf("an empty drop: %+v", empty)
	}
}

func TestPastedSourcesNoImportReadAreRemovedAfterAWeek(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	old := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7})
	recent := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7})
	if old.State != desktop.Completed || recent.State != desktop.Completed {
		t.Fatalf("staged: %+v %+v", old, recent)
	}
	staged := filepath.Join(root, ".readmit", "staged-sources")
	aged := time.Now().Add(-desktop.StagedSourceRetention - time.Hour)
	if err := os.Chtimes(filepath.Join(staged, old.StagedID), aged, aged); err != nil {
		t.Fatal(err)
	}
	if next := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7}); next.State != desktop.Completed {
		t.Fatalf("staged: %+v", next)
	}
	if _, err := os.Stat(filepath.Join(staged, old.StagedID)); !os.IsNotExist(err) {
		t.Fatalf("a week-old staged source was kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staged, recent.StagedID)); err != nil {
		t.Fatalf("a recent staged source was removed: %v", err)
	}
}

func TestATLSListenerPresentsANamedMLLPCredentialScopedToItsAddress(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "listener.pem", "certificate")
	for _, credential := range []desktop.CredentialSaveRequest{
		{Context: context, Name: "listener-key", Purpose: secret.MLLPEndpoint, Address: "127.0.0.1:2575"},
		{Context: context, Name: "other-address", Purpose: secret.MLLPEndpoint, Address: "127.0.0.1:2576"},
		{Context: context, Name: "source-key", Purpose: secret.SourceEndpoint, Address: "127.0.0.1:2575"},
	} {
		credential.Store, credential.Command, credential.Arguments = secret.OSKeychain, "/usr/bin/security", []string{"find-generic-password", "-s", credential.Name}
		if created := app.SaveCredential(credential); created.State != desktop.Completed {
			t.Fatalf("register %s: %+v", credential.Name, created)
		}
	}
	listener := func(reference string) *desktop.CaptureSourceDraft {
		return &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575,
			Transport: desktop.TLSTransport, AckCode: "AA", TLSCertificate: "listener.pem", TLSKeyReference: reference}}
	}
	for _, reference := range []string{"missing", "other-address", "source-key"} {
		result := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Name: "TLS", Source: listener(reference)}})
		if len(result.Problems) != 1 || result.Problems[0].Field != "source.listener.tls_key_reference" {
			t.Errorf("%s: %+v", reference, result)
		}
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "tls-listener", Draft: desktop.ItemDraft{Name: "TLS", Source: listener("listener-key")}})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("a listener presenting a named credential: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.Draft == nil || opened.Draft.Source.Listener.SecretsFile != desktop.ProjectSecrets {
		t.Fatalf("the saved listener names the project's credentials: %+v", opened)
	}
	// A credential a capture source presents is not removed from under it.
	removed := app.RemoveCredential(desktop.CredentialRequest{Context: context, Name: "listener-key"})
	if removed.State != desktop.Failed || len(removed.Referring) != 1 || removed.Referring[0].Ref.ID != saved.Saved.ID {
		t.Fatalf("removing a presented credential: %+v", removed)
	}
}

func TestAResponderSavedAsChoicesRoundTripsWithoutItsDocument(t *testing.T) {
	app, context := namedProject(t)
	listener := desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575, Transport: desktop.PlainTransport, AckCode: "AE"}
	draft := desktop.ItemDraft{Name: "Scheduling QA", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener,
		ResponderChoices: &desktop.ResponderChoices{Enhanced: true, Fault: "delay", FaultDelayMS: 200}}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "choices-1", Draft: draft})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	source := opened.Draft.Source
	want := desktop.ResponderChoices{Name: "listener", SourceLabel: "listener", AcceptedMessageTypes: source.Responder.AcceptedMessageTypes,
		Enhanced: true, Fault: "delay", FaultDelayMS: 200}
	if source.ResponderChoices == nil || !reflect.DeepEqual(*source.ResponderChoices, want) || source.ResponderChoices.AcceptedMessageTypes.Operator != "any-message-type" {
		t.Fatalf("the choices a saved responder opens as: %+v", source.ResponderChoices)
	}
	// The published responder: the listener's code, and the fault held to
	// its exact address.
	published := source.Responder
	if published.Schema != "readmit-receiver-policy/v3" || published.Acknowledgement.Code != "AE" || published.Faults == nil ||
		!slices.Equal(published.Faults.ApprovedTestEndpoints, []string{"127.0.0.1:2575"}) || published.Faults.Steps[0].DelayMS != 200 {
		t.Fatalf("the published responder: %+v", published)
	}

	// A responder the controls cannot wholly express — a second fault step —
	// keeps it when its choices are saved again unchanged.
	twoSteps := *published
	faults := *published.Faults
	faults.Steps = append(slices.Clone(faults.Steps), faults.Steps[0])
	faults.Steps[1].Message, faults.Steps[1].Action, faults.Steps[1].DelayMS = 2, "reject", 0
	twoSteps.Faults = &faults
	document := desktop.ItemDraft{Name: "Scheduling QA", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener, Responder: &twoSteps}}
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Item: saved.Saved.ID, BaseRevision: "1", IntentID: "choices-2", Draft: document})
	if second.Outcome != desktop.SavedOutcome {
		t.Fatalf("a two-step responder: %+v", second)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *second.Saved}).Draft.Source
	again := desktop.ItemDraft{Name: "Scheduling QA", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener, ResponderChoices: reopened.ResponderChoices}}
	third := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Item: saved.Saved.ID, BaseRevision: "2", IntentID: "choices-3", Draft: again})
	if third.Outcome != desktop.SavedOutcome {
		t.Fatalf("resaving the choices: %+v", third)
	}
	if kept := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *third.Saved}).Draft.Source.Responder; !reflect.DeepEqual(kept, reopened.Responder) {
		t.Fatalf("resaving unchanged choices lost what they cannot express: %+v, was %+v", kept, reopened.Responder)
	}

	// A fault is refused on anything but a loopback listener with a fixed
	// port, and a fault outside the vocabulary.
	for name, refused := range map[string]desktop.CaptureSourceDraft{
		"a remote bind": {Type: desktop.MLLPListenerSource, Listener: &desktop.ListenerSettings{BindAddress: "0.0.0.0", Port: 2575, AllowRemote: true, Transport: desktop.PlainTransport, AckCode: "AA"},
			ResponderChoices: &desktop.ResponderChoices{Fault: "reject"}},
		"a chosen port": {Type: desktop.MLLPListenerSource, Listener: &desktop.ListenerSettings{BindAddress: "127.0.0.1", Transport: desktop.PlainTransport, AckCode: "AA"},
			ResponderChoices: &desktop.ResponderChoices{Fault: "reject"}},
		"an unknown fault": {Type: desktop.MLLPListenerSource, Listener: &listener, ResponderChoices: &desktop.ResponderChoices{Fault: "explode"}},
	} {
		result := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Name: "Refused", Source: &refused}})
		if len(result.Problems) != 1 || result.Problems[0].Field != "source.responder.faults" {
			t.Errorf("%s: %+v", name, result)
		}
	}
}

// The source editor starts each type from the vocabulary; a start saves as
// soon as what has no default is chosen, and holds nothing else to fill.
func TestEachCaptureSourceStartSavesOnceItsChoicesAreMade(t *testing.T) {
	app, context := namedProject(t)
	starts := app.Shell().Shell.Vocabulary.CaptureSourceStarts
	types := []desktop.CaptureSourceType{}
	for _, start := range starts {
		types = append(types, start.Type)
	}
	if !slices.Equal(types, []desktop.CaptureSourceType{desktop.LocalFolderSource, desktop.TransferSource, desktop.MLLPListenerSource}) {
		t.Fatalf("the starts: %v", types)
	}
	folder := t.TempDir()
	for i, start := range starts {
		draft := start
		switch draft.Type {
		case desktop.LocalFolderSource:
			evidence := *draft.Evidence
			if evidence.Root != "" || evidence.Name != "" || evidence.Scope != "" {
				t.Fatalf("a folder start chose for the person: %+v", evidence)
			}
			evidence.Name, evidence.Scope, evidence.Root = "exports", "appointments", folder
			draft.Evidence = &evidence
		case desktop.TransferSource:
			evidence := *draft.Evidence
			if evidence.Command != "" || evidence.Address != "" || evidence.Name != "" {
				t.Fatalf("a transfer start chose for the person: %+v", evidence)
			}
			evidence.Name, evidence.Scope, evidence.Command, evidence.Address = "sftp", "appointments", "/usr/local/bin/fetch", "archive.example.test:22"
			draft.Evidence = &evidence
		}
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "start-" + string(draft.Type),
			Draft: desktop.ItemDraft{Name: "Start " + strconv.Itoa(i), Source: &draft}})
		if saved.Outcome != desktop.SavedOutcome {
			t.Errorf("the %s start: %+v", draft.Type, saved)
		}
	}
}

// A certificate or client authority the file dialog chose is recorded as the
// entry it names directly in the project folder; anything else is refused at
// its field when the source is saved.
func TestAListenersTLSFilesAreFilesDirectlyInTheProject(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	writeDocument(t, root, "listener.pem", "certificate")
	writeDocument(t, root, "clients.pem", "authority")
	if err := os.Mkdir(filepath.Join(root, "certs"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, filepath.Join(root, "certs"), "nested.pem", "certificate")
	outside := t.TempDir()
	writeDocument(t, outside, "outside.pem", "certificate")
	if err := os.Symlink(filepath.Join(outside, "outside.pem"), filepath.Join(root, "linked.pem")); err != nil {
		t.Fatal(err)
	}
	listener := func(certificate, authority string) *desktop.CaptureSourceDraft {
		return &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575,
			Transport: desktop.MutualTLSTransport, AckCode: "AA", TLSCertificate: certificate, ClientCA: authority}}
	}
	chosen := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Name: "TLS",
		Source: listener(filepath.Join(root, "listener.pem"), filepath.Join(root, "clients.pem"))}})
	for _, problem := range chosen.Problems {
		if problem.Field == "source.listener.tls_certificate" || problem.Field == "source.listener.client_ca" {
			t.Fatalf("chosen project files: %+v", chosen.Problems)
		}
	}
	for name, refused := range map[string]string{
		"a file in a folder of the project": filepath.Join(root, "certs", "nested.pem"),
		"a file outside the project":        filepath.Join(outside, "outside.pem"),
		"a link in the project":             filepath.Join(root, "linked.pem"),
		"a nested entry name":               "certs/nested.pem",
		"a missing file":                    filepath.Join(root, "missing.pem"),
	} {
		result := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Name: "TLS", Source: listener(refused, "clients.pem")}})
		if !slices.ContainsFunc(result.Problems, func(problem desktop.FieldProblem) bool { return problem.Field == "source.listener.tls_certificate" }) {
			t.Errorf("%s: %+v", name, result.Problems)
		}
	}
	// What is saved is the entry name.
	if created := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "listener-key", Purpose: secret.MLLPEndpoint, Address: "127.0.0.1:2575",
		Store: secret.OSKeychain, Command: "/usr/bin/security", Arguments: []string{"find-generic-password", "-s", "listener-key"}}); created.State != desktop.Completed {
		t.Fatalf("register: %+v", created)
	}
	source := listener(filepath.Join(root, "listener.pem"), filepath.Join(root, "clients.pem"))
	source.Listener.TLSKeyReference = "listener-key"
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "tls-files", Draft: desktop.ItemDraft{Name: "TLS", Source: source}})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved}).Draft.Source.Listener
	if opened.TLSCertificate != "listener.pem" || opened.ClientCA != "clients.pem" {
		t.Fatalf("saved TLS files: %+v", opened)
	}
}

// The responder a listener's choices compose is the first contract version
// that declares them: the original-mode rule alone is v1, the fixed-code
// enhanced rule v2, and a fault v3, which declares enhanced mode unsupported
// when none was chosen; a fault that does not wait declares no delay.
func TestAResponderChoicesVersionIsChosenByTheFacade(t *testing.T) {
	app, context := namedProject(t)
	listener := desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575, Transport: desktop.PlainTransport, AckCode: "AA"}
	composed := func(choices desktop.ResponderChoices) collection.Policy {
		t.Helper()
		result := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem,
			Draft: desktop.ItemDraft{Name: "Listener", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener, ResponderChoices: &choices}}})
		if len(result.Problems) != 0 || result.Projection == nil || result.Projection.Source.Responder == nil {
			t.Fatalf("%+v: %+v", choices, result)
		}
		return *result.Projection.Source.Responder
	}
	if plain := composed(desktop.ResponderChoices{}); plain.Schema != collection.PolicySchemaV1 || plain.Enhanced != nil || plain.Faults != nil {
		t.Fatalf("the original-mode rule alone: %+v", plain)
	}
	if enhanced := composed(desktop.ResponderChoices{Enhanced: true}); enhanced.Schema != collection.PolicySchema || enhanced.Enhanced == nil ||
		enhanced.Enhanced.Operator != collection.EnhancedFixedCodes || enhanced.Enhanced.ApplicationDelivery != collection.SameConnection {
		t.Fatalf("the enhanced rule: %+v", enhanced)
	}
	if faulting := composed(desktop.ResponderChoices{Fault: "delay", FaultDelayMS: 75}); faulting.Schema != collection.FaultPolicySchema ||
		faulting.Enhanced == nil || faulting.Enhanced.Operator != collection.EnhancedUnsupported || faulting.Faults.Steps[0].DelayMS != 75 {
		t.Fatalf("a delay fault: %+v", faulting)
	}
	if rejecting := composed(desktop.ResponderChoices{Fault: "reject", FaultDelayMS: 75}); rejecting.Faults.Steps[0].DelayMS != 0 {
		t.Fatalf("a fault that does not wait: %+v", rejecting.Faults)
	}
}

// A folder source read whole whose import then fails keeps what it staged
// as a capture whose finalization failed, and Retry finalization imports it
// again without reading the source; a source that cannot be read is an
// interrupted capture.
func TestAFolderCaptureWhoseImportFailsIsRetriedWithoutReadingItsSourceAgain(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	export := t.TempDir()
	writeDocument(t, export, "one.hl7", sampleImportHL7)
	plan := validDesktopPlan()
	evidence := evidencesource.Source{Name: "exports", Kind: evidencesource.Directory, Scope: "appointments", Root: export,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20}, Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "save-folder",
		Draft: desktop.ItemDraft{Name: "Exports", Source: &desktop.CaptureSourceDraft{Type: desktop.LocalFolderSource, Evidence: &evidence, Plan: &plan}}})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	// The import after the whole read fails, as a full disk would fail it.
	desktop.SetSaveFaultForTest(app, func(point string) error {
		if point == "capture-import" {
			return errors.New("no space left on device")
		}
		return nil
	})
	result := app.StartCapture(desktop.CaptureRequest{Context: context, Source: saved.Saved, Name: "Exported appointments", IntentID: "folder-1"})
	desktop.SetSaveFaultForTest(app, nil)
	if result.State != desktop.Failed || result.Outcome != desktop.CaptureFinalizeFailed || result.Session == "" || len(registeredCases(t, root)) != 0 {
		t.Fatalf("an import that failed after a whole read: %+v", result)
	}
	// Retry imports the staged collection: the source itself is gone, and
	// the one case is registered from what was read.
	if err := os.RemoveAll(export); err != nil {
		t.Fatal(err)
	}
	retried := app.RetryCaptureFinalization(desktop.CaptureSessionRequest{Context: context, Session: result.Session})
	if retried.State != desktop.Completed || retried.Case == nil {
		t.Fatalf("a retry of the staged collection: %+v", retried)
	}
	if cases := registeredCases(t, root); len(cases) != 1 || cases[0].Title != "Exported appointments" {
		t.Fatalf("registered: %+v", cases)
	}

	// A source that cannot be read is interrupted, never finalize-failed.
	missing := evidence
	missing.Root = filepath.Join(t.TempDir(), "absent")
	unread := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "save-missing",
		Draft: desktop.ItemDraft{Name: "Missing", Source: &desktop.CaptureSourceDraft{Type: desktop.LocalFolderSource, Evidence: &missing, Plan: &plan}}})
	if unread.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", unread)
	}
	interrupted := app.StartCapture(desktop.CaptureRequest{Context: context, Source: unread.Saved, Name: "Missing", IntentID: "folder-2"})
	if interrupted.Outcome != desktop.CaptureInterrupted {
		t.Fatalf("a source that cannot be read: %+v", interrupted)
	}
}
