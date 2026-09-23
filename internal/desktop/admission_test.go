package desktop_test

// The window admits every operation the way the command line admits the same
// operation, and the capability ledger says which admission that is. Work that
// reaches a destination reserves a runner instance; writing a new authored
// document admits the author; and no refusal waits for a control to have been
// disabled. Each refused path below is called straight through the facade.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// admissionsOf reads, from the facade's own source, which local admissions
// each bound method takes: an operation slot claimed for a write admits the
// author, and an execute admission or the runner's operation guard reserves a
// runner instance, directly or through the methods it calls. A write decided
// at run time is conditional. The preflight's admission preview asks and
// settles at once without admitting any work, so it is not an admission.
func admissionsOf(t *testing.T) map[string]map[string]bool {
	t.Helper()
	fileset := token.NewFileSet()
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string]*ast.BlockStmt{}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileset, source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
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
		if seen[method] || method == "admissionPreview" {
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
			if slot, ok := function.(*ast.Ident); ok && (slot.Name == "run" || slot.Name == "runNamed") {
				writes := call.Args[2]
				if slot.Name == "runNamed" {
					writes = call.Args[3]
				}
				switch literal, _ := writes.(*ast.Ident); {
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
			case "admitExecution", "WithOperationGuard":
				found["execute"] = true
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
// bound method actually takes, in both directions: a method that admits the
// author or reserves a runner instance says so, and a row that promises an
// admission is held to one. A conditional write may declare either.
func TestEveryDesktopLedgerRowDeclaresTheAdmissionItsMethodTakes(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	derived := admissionsOf(t)
	for _, row := range ledger.Rows {
		if row.Kind != capability.KindDesktop {
			continue
		}
		method := strings.TrimPrefix(row.Source, "desktop.App.")
		found := derived[method]
		for _, admission := range []string{"author", "execute"} {
			declared := slices.Contains(row.Prerequisites, admission)
			takes := found[admission]
			if admission == "author" && found["conditional"] {
				continue
			}
			if declared != takes {
				t.Errorf("%s: the ledger declares %s admission %v, the facade takes it %v", row.ID, admission, declared, takes)
			}
		}
	}
}
