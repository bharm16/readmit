package desktop_test

// The window admits every operation the way the command line admits the same
// operation, and the capability ledger says which admission that is. Work that
// reaches a destination reserves a runner instance; writing a new authored
// document admits the author; and no refusal waits for a control to have been
// disabled. Each refused path below is called straight through the facade, and
// every named operation's admission is the profile it declares.

import (
	"context"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/capability"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/testlicense"
)

// authorOnlyPolicy activates a current term that assigns this device an
// author seat and selects no runner authority: authoring is admitted and
// execution is not.
func authorOnlyPolicy(t *testing.T) string {
	t.Helper()
	signer := newSigning(t)
	entitlementPath, trustPath := writeReceived(t, "author-only", signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour))), signer.trustBytes(t))
	dir := t.TempDir()
	policy := operationguard.Policy{Schema: operationguard.PolicySchema, Entitlement: entitlementPath, Trust: trustPath,
		State: filepath.Join(dir, "clock.json"), Author: "alice", Device: "desk"}
	encoded, err := operationguard.EncodePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "operation-policy.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.Activate(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// windowWith is a window with the given operation policy selected, or none.
func windowWith(t *testing.T, policy string) *desktop.App {
	t.Helper()
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	if policy != "" {
		if result := app.SelectOperationPolicy(policy); result.State != desktop.Completed {
			t.Fatal(result)
		}
	}
	return app
}

// A connectivity check, a fixture reset and an observation collection each
// reach a destination, so each needs a runner instance exactly as `readmit
// target check`, `target reset` and `observe collect` do. Without an
// activation, or with one that selects no runner authority, each is refused
// before anything is reached, while the same author-only activation still
// admits authoring. With a runner authority the same check does reach the
// recorded address.
func TestWorkThatReachesADestinationIsAdmittedAsExecution(t *testing.T) {
	endpoint := newCountingEndpoint(t, false)
	workspace := t.TempDir()
	authorOnly := authorOnlyPolicy(t)
	full := testlicense.New(t)
	recorded := windowWith(t, full).ReadTarget(workspace, "lab-target.json")
	environment := *recorded.Target
	environment.Name, environment.Classification, environment.Address = "lab-mllp", "nonproduction", endpoint.address
	if saved := windowWith(t, authorOnly).SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "lab-target.json", Target: environment}); saved.State != desktop.Completed {
		t.Fatalf("an author-only activation did not admit authoring a target: %+v", saved)
	}
	operations := map[string]func(*desktop.App) any{
		"CheckTarget": func(app *desktop.App) any {
			return app.CheckTarget(desktop.TargetCheckRequest{Workspace: workspace, TargetFile: "lab-target.json"})
		},
		"ResetTarget": func(app *desktop.App) any {
			return app.ResetTarget(desktop.TargetResetRequest{Workspace: workspace, TargetFile: "lab-target.json", PlanFile: "reset-plan.json", OutcomeFile: "reset-outcome.json"})
		},
		"CollectObservation": func(app *desktop.App) any {
			return app.CollectObservation(desktop.ObservationCollectFacadeRequest{Workspace: workspace, SourceFile: "source.json", WindowFile: "window.json",
				OutputFile: "completion.json", SnapshotDir: "snapshot", Authorize: true})
		},
	}
	for name, call := range operations {
		t.Run(name, func(t *testing.T) {
			for policy, reason := range map[string]string{"": operationguard.ErrUnavailable.Error(), authorOnly: entitlement.ErrAuthorityNotNamed.Error()} {
				state, said := stateOf(call(windowWith(t, policy)))
				if state != desktop.PermissionDenied || said != reason {
					t.Errorf("admitted without a runner instance (policy %q): %s %q", filepath.Base(policy), state, said)
				}
			}
			if reached := endpoint.accepted.Load(); reached != 0 {
				t.Fatalf("a refused execution reached the destination %d times", reached)
			}
		})
	}
	if checked := windowWith(t, full).CheckTarget(desktop.TargetCheckRequest{Workspace: workspace, TargetFile: "lab-target.json"}); checked.State == desktop.PermissionDenied || endpoint.accepted.Load() != 1 {
		t.Fatalf("an admitted check did not reach the recorded address: %+v, %d connections", checked, endpoint.accepted.Load())
	}
}

// Writing an edited canonical test or assertion set as a new document is
// authoring, admitted as saving an authored test or set is: without an
// activation it is refused and nothing is written.
func TestExportingAnEditedTestOrAssertionSetIsAdmittedAsAuthoring(t *testing.T) {
	workspace := t.TempDir()
	assertions, err := os.ReadFile("../../testdata/fixtures/assertion-set.json")
	if err != nil {
		t.Fatal(err)
	}
	unactivated := windowWith(t, "")
	for output, state := range map[string]desktop.State{
		"spec.json":       unactivated.ExportTest(desktop.CanonicalTestRequest{Workspace: workspace, Document: canonicalSpec, Output: "spec.json"}).State,
		"assertions.json": unactivated.ExportAssertionSet(desktop.CanonicalAssertionRequest{Workspace: workspace, Document: string(assertions), Output: "assertions.json"}).State,
	} {
		if state != desktop.PermissionDenied {
			t.Errorf("an unactivated window wrote %s: %s", output, state)
		}
		if _, err := os.Lstat(filepath.Join(workspace, output)); !os.IsNotExist(err) {
			t.Errorf("a refused export wrote %s", output)
		}
	}
	activated := windowWith(t, testlicense.New(t))
	if result := activated.ExportAssertionSet(desktop.CanonicalAssertionRequest{Workspace: workspace, Document: string(assertions), Output: "assertions.json"}); result.State != desktop.Completed {
		t.Fatalf("an activated window could not export an assertion set: %+v", result)
	}
}

// Every execution a named operation declares is admitted, bounded by the
// operation guard's MaxDuration and settled, and a settlement failure
// overrides whatever the work answered: the one module the command line
// admits the same operations through does it, so no operation's copy can
// leave the bound or the settlement out. A runner's job is bounded where the
// runner admits it.
func TestEveryDeclaredExecutionIsBoundedAndSettled(t *testing.T) {
	app := windowWith(t, testlicense.New(t))
	executions := 0
	for method, profile := range desktop.DeclaredProfilesForTest() {
		if profile.Execution == operationguard.NoExecution {
			continue
		}
		executions++
		started := time.Now()
		var deadline time.Time
		var bounded bool
		state, reason := desktop.RunUnderProfileForTest(app, method, func(ctx context.Context) {
			if profile.Execution == operationguard.ExecuteEachJob {
				if err := operationguard.RunJob(ctx, func(ctx context.Context) error {
					deadline, bounded = ctx.Deadline()
					return nil
				}); err != nil {
					t.Errorf("%s's job was not admitted: %v", method, err)
				}
				return
			}
			deadline, bounded = ctx.Deadline()
		})
		if state != desktop.Completed {
			t.Errorf("%s under its declared profile: %s %q", method, state, reason)
		}
		if limit := started.Add(operationguard.MaxDuration); !bounded || deadline.After(limit.Add(time.Minute)) || deadline.Before(limit.Add(-time.Minute)) {
			t.Errorf("%s executes with deadline %v (bounded %v), want the operation guard's MaxDuration", method, deadline, bounded)
		}
	}
	if executions < 13 {
		t.Fatalf("implausibly few declared executions: %d", executions)
	}
	for _, method := range []string{"DiagnoseSource", "CollectSource", "CheckTarget", "ResetTarget", "CollectObservation"} {
		if profile := desktop.DeclaredProfilesForTest()[method]; profile.Execution != operationguard.Execute {
			t.Errorf("%s declares execution %v, want one execution held and bounded for the operation", method, profile.Execution)
		}
	}
	// The record the instance is released into is no longer readable when
	// the work ends, so the operation answers the settlement failure.
	policy := testlicense.New(t)
	unsettled := windowWith(t, policy)
	state, reason := desktop.RunUnderProfileForTest(unsettled, "CheckTarget", func(context.Context) {
		if err := os.WriteFile(filepath.Join(filepath.Dir(policy), "admissions.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if state != desktop.Failed || reason != "runner settlement failed; reconcile the retained admission before new work" {
		t.Fatalf("an execution whose settlement failed answered %s %q", state, reason)
	}
	// A profile the table does not declare runs nothing.
	ran := false
	if state, _ := desktop.RunUnderProfileForTest(app, "NotAnOperation", func(context.Context) { ran = true }); state != desktop.Failed || ran {
		t.Fatalf("an undeclared operation answered %s and ran %v", state, ran)
	}
}

// A suite run holds one runner instance for its whole execution and rechecks
// it before each job, as `readmit suite run` does
// (TestEveryJobAnExecutionQueuesIsRecheckedUnderItsInstance): an activation
// released while the first job waits for its acknowledgement lets that job
// finish and refuses the next, which sends nothing.
func TestASuiteRunRechecksItsAdmissionBeforeEachJob(t *testing.T) {
	peer := newDelayedAckingPeer(t, "AA", 500*time.Millisecond)
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "booking.json", "AA")
	writeSuiteRows(t, workspace, "nightly.json", "one", "two")
	policy := testlicense.New(t)
	app := windowWith(t, policy)
	answered := make(chan desktop.SuiteRunResult, 1)
	go func() {
		answered <- app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: workspace, Suite: "nightly.json", Environment: "east", Output: "suite-run"})
	}()
	for peer.deliveries() == 0 {
		select {
		case result := <-answered:
			t.Fatalf("the suite answered before its first job sent: %+v", result)
		case <-time.After(time.Millisecond):
		}
	}
	if err := operationguard.Release(policy); err != nil {
		t.Fatal(err)
	}
	result := <-answered
	if result.State != desktop.Completed || result.Report == nil {
		t.Fatalf("suite run: %+v", result)
	}
	jobs := map[string]string{}
	for _, job := range result.Report.Jobs {
		jobs[job.ID] = job.Admission + " " + job.Reason
	}
	if result.Report.Executed != 1 || result.Report.Refused != 1 || jobs["booking-one"] != "executed " ||
		jobs["booking-two"] != "refused "+entitlement.ErrReleased.Error() {
		t.Fatalf("a job started after the activation was released: %+v", jobs)
	}
	if sent := peer.deliveries(); sent != 1 {
		t.Fatalf("the refused job sent: %d deliveries", sent)
	}
}

// A send nobody approved is refused as the request it is, but only once the
// operation holds the slot and before admission is asked, as every send has
// been: while another operation runs it is busy, and afterwards it is refused
// with its own reason even where nothing could have admitted it.
func TestAnUnapprovedSendIsRefusedWhileItHoldsTheSlotBeforeAdmission(t *testing.T) {
	app := windowWith(t, "")
	sends := map[string]func() (desktop.State, string){
		"SendReplay": func() (desktop.State, string) {
			return stateOf(app.SendReplay(desktop.ReplaySendRequest{}))
		},
		"ReexecuteReviewedEvidence": func() (desktop.State, string) {
			return stateOf(app.ReexecuteReviewedEvidence(desktop.ReexecutionSendRequest{}))
		},
	}
	release, held := desktop.HoldSlotForTest(app, "another-operation")
	if !held {
		t.Fatal("the slot was not free")
	}
	for name, send := range sends {
		if state, _ := send(); state != desktop.Busy {
			t.Errorf("%s answered %s while another operation held the slot", name, state)
		}
	}
	release()
	for name, send := range sends {
		if state, reason := send(); state != desktop.Failed || strings.Contains(reason, operationguard.ErrUnavailable.Error()) {
			t.Errorf("%s without approval answered %s %q, want its own refusal before admission", name, state, reason)
		}
	}
}

// A capture its execution admission declines never started, so it reports
// the failed phase; one its author admission declines, first, reports none,
// as it always has.
func TestACaptureDeclinedByItsExecutionAdmissionReportsTheFailedPhase(t *testing.T) {
	request := desktop.CaptureRequest{Workspace: t.TempDir(), Kind: "collect", OutputName: "captured"}
	unactivated := windowWith(t, "").StartCapture(request)
	if unactivated.State != desktop.PermissionDenied || unactivated.Phase != "" {
		t.Errorf("a capture refused by its author admission: %s, phase %q", unactivated.State, unactivated.Phase)
	}
	authorOnly := windowWith(t, authorOnlyPolicy(t)).StartCapture(request)
	if authorOnly.State != desktop.PermissionDenied || authorOnly.Reason != entitlement.ErrAuthorityNotNamed.Error() || authorOnly.Phase != desktop.CaptureFailed {
		t.Errorf("a capture refused by its execution admission: %s %q, phase %q", authorOnly.State, authorOnly.Reason, authorOnly.Phase)
	}
}

// localAdmissionsOf reads, from the facade's own source, the admission local
// work takes. Local work runs unnamed and declares no profile: it admits the
// author when run is told it writes, or part way through once its request has
// been read (admitAuthor), and a write decided at run time is conditional.
// Every named operation's admission is its declared profile instead, so a
// method's callees that declare one are not read. An execution asked of the
// guard directly is read too, so one taken outside a declared profile fails.
// The preflight's admission preview asks and settles at once without
// admitting any work, so it is not an admission.
func localAdmissionsOf(t *testing.T, declared map[string]operationguard.Profile) map[string]map[string]bool {
	t.Helper()
	bodies := map[string]*ast.BlockStmt{}
	for _, file := range parsePackage(t, ".") {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv != nil && function.Body != nil {
				bodies[function.Name.Name] = function.Body
			}
		}
	}
	derived := map[string]map[string]bool{}
	var visit func(string, map[string]bool) map[string]bool
	visit = func(method string, seen map[string]bool) map[string]bool {
		if found, ok := derived[method]; ok {
			return found
		}
		found := map[string]bool{}
		if _, named := declared[method]; seen[method] || named || method == "admissionPreview" {
			return found
		}
		seen[method] = true
		ast.Inspect(bodies[method], func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function := call.Fun
			if generic, ok := function.(*ast.IndexListExpr); ok {
				function = generic.X
			}
			if slot, ok := function.(*ast.Ident); ok && slot.Name == "run" {
				switch literal, _ := call.Args[2].(*ast.Ident); {
				case literal != nil && literal.Name == "true":
					found["author"] = true
				case literal == nil || literal.Name != "false":
					found["conditional"] = true
				}
			}
			selector, ok := function.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "admitAuthor":
				found["author"] = true
			case "AdmitContext", "Admit":
				if capability, ok := call.Args[len(call.Args)-1].(*ast.BasicLit); ok {
					found[strings.Trim(capability.Value, `"`)] = true
				}
			}
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "a" && bodies[selector.Sel.Name] != nil {
				for admission := range visit(selector.Sel.Name, seen) {
					found[admission] = true
				}
			}
			return true
		})
		derived[method] = found
		return found
	}
	for method := range bodies {
		visit(method, map[string]bool{})
	}
	return derived
}

// Every desktop row of the capability ledger declares the local admission its
// bound method takes, in both directions: a named operation's row states
// exactly the admission its declared profile takes, a local method's row the
// author admission its work takes, and a row that promises an admission is
// held to one. A conditional local write may declare either. Execution is
// admitted only under a declared profile, and every declared profile belongs
// to a bound method the ledger covers.
func TestEveryDesktopLedgerRowDeclaresTheAdmissionItsMethodTakes(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	declared := desktop.DeclaredProfilesForTest()
	local := localAdmissionsOf(t, declared)
	covered := map[string]bool{}
	for _, row := range ledger.Rows {
		if row.Kind != capability.KindDesktop {
			continue
		}
		method := strings.TrimPrefix(row.Source, "desktop.App.")
		covered[method] = true
		takes := local[method]
		if profile, named := declared[method]; named {
			takes = map[string]bool{}
			for _, admission := range profile.Prerequisites() {
				takes[admission] = true
			}
		} else if takes["execute"] {
			t.Errorf("%s admits execution outside a declared profile", method)
		}
		for _, admission := range []string{"author", "execute"} {
			declaredByRow := slices.Contains(row.Prerequisites, admission)
			if admission == "author" && takes["conditional"] {
				continue
			}
			if declaredByRow != takes[admission] {
				t.Errorf("%s: the ledger declares %s admission %v, the facade takes it %v", row.ID, admission, declaredByRow, takes[admission])
			}
		}
	}
	facade := reflect.TypeFor[*desktop.App]()
	for method := range declared {
		if _, bound := facade.MethodByName(method); !bound {
			t.Errorf("%s declares a profile but is not a bound operation", method)
		}
		if !covered[method] {
			t.Errorf("%s declares a profile but no ledger row covers it", method)
		}
	}
}
