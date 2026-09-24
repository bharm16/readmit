package desktop_test

// The privacy status can say an activity is happening only while an operation
// that holds the slot carries that activity's name. So every operation of the
// window that can reach a network destination or change a target runs under
// a name, and the status reports that name's activity as active. The
// operations are enumerated from the facade itself — its bound methods, and
// what their own source does — and held to the reviewed inventory of
// destinationActivities, so an operation added later that reaches a
// destination without a name fails here rather than showing busy.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/sharing"
)

func TestEveryOperationThatCanReachADestinationRunsUnderADisclosedName(t *testing.T) {
	reaching, claims := reachingOf(t)
	// What the facade's own source shows reaching a destination is in the
	// reviewed inventory, so the inventory cannot fall behind the facade.
	facade := reflect.TypeFor[*desktop.App]()
	for i := range facade.NumMethod() {
		method := facade.Method(i).Name
		if why := reaching[method]; len(why) != 0 && destinationActivities[method] == "" {
			t.Errorf("%s can reach a destination (%s) but the reviewed inventory of destinationActivities does not name its activity", method, strings.Join(why, ", "))
		}
	}
	// Every operation in the inventory holds the slot only under a name, and
	// while it does the privacy status reports its own activity active. A hub
	// operation with no configuration selected refuses before it reaches
	// anything, so the window the names are read against has one.
	app := newApp(t, &chooser{})
	if result := app.SelectHubConfig(writeHubClientConfig(t, t.TempDir(), "https://127.0.0.1:1")); result.State != desktop.Completed {
		t.Fatalf("hub configuration: %+v", result)
	}
	for method, activity := range destinationActivities {
		if len(claims[method]) == 0 {
			t.Errorf("%s reaches a destination under %q but claims no operation slot the status could read", method, activity)
		}
		for _, name := range claims[method] {
			if name == "" {
				t.Errorf("%s reaches a destination under %q and holds the slot without a name", method, activity)
				continue
			}
			if reading := activeUnder(t, app, name); reading != activity {
				t.Errorf("%s runs as %q, and while it holds the slot the privacy status reads %q, want %q active", method, name, reading, activity)
			}
		}
	}
	// Without a hub configuration the same hub names make nothing active.
	unconfigured := newApp(t, &chooser{})
	for _, method := range []string{"ConnectHub", "StartHubAuth", "CompleteHubAuth"} {
		for _, name := range claims[method] {
			if reading := activeUnder(t, unconfigured, name); reading != "" {
				t.Errorf("with no hub configuration, %s's %q reads %q active", method, name, reading)
			}
		}
	}
	// The enumeration still reads each way of reaching a destination it relies
	// on: execution admission, the system resolver, and the hub's clients'
	// functions and methods.
	for _, method := range []string{"CheckTarget", "EvaluateSendPolicy", "CompleteHubAuth", "ListHubReviews", "EnrollRunner"} {
		if len(reaching[method]) == 0 {
			t.Errorf("the enumeration no longer finds that %s can reach a destination", method)
		}
	}
	// Local work stays local: none of these is mistaken for reaching anything.
	for _, method := range []string{"PreflightRun", "PreviewReduction", "PreviewCapture", "DisconnectHub", "SelectHubConfig", "SaveTarget", "DisclosureStatus"} {
		if why := reaching[method]; len(why) != 0 {
			t.Errorf("the enumeration reads local %s as reaching a destination (%s)", method, strings.Join(why, ", "))
		}
	}
}

// activeUnder holds the slot under name and returns what the privacy status
// then reports active.
func activeUnder(t *testing.T, app *desktop.App, name string) string {
	t.Helper()
	release, held := desktop.HoldSlotForTest(app, name)
	if !held {
		t.Fatalf("the slot was not free to hold as %q", name)
	}
	defer release()
	return activeNow(app)
}

// Every operation that can run a program an operator declared holds the slot
// only under a name, and the name alone never makes the declared-program row
// active: holding it reads exactly the row the operation reaches a
// destination under, if it has one. While a program runs under that name the
// row is active beside it, in the name's own sentence, which says whose
// program it is and that Readmit cannot vouch for where it connects.
func TestEveryOperationThatRunsADeclaredProgramRunsUnderANameTheStatusMaps(t *testing.T) {
	_, claims := reachingOf(t)
	app := newApp(t, &chooser{})
	if result := app.SelectHubConfig(writeHubClientConfig(t, t.TempDir(), "https://127.0.0.1:1")); result.State != desktop.Completed {
		t.Fatalf("hub configuration: %+v", result)
	}
	// A program running under a name with no sentence of its own is still
	// reported running, in a sentence that names no operation.
	reading, unattributed := runningUnder(t, app, "an-operation-with-no-sentence")
	if reading != "declared-program" || !strings.Contains(unattributed, "cannot see or vouch") {
		t.Fatalf("a program running under an unmapped name reads %q: %q", reading, unattributed)
	}
	for _, method := range operationsRunningDeclaredPrograms {
		if len(claims[method]) == 0 {
			t.Errorf("%s runs a declared program but claims no operation slot the status could read", method)
		}
		for _, name := range claims[method] {
			if name == "" {
				t.Errorf("%s runs a declared program and holds the slot without a name, so the status answers busy while it runs", method)
				continue
			}
			own := destinationActivities[method]
			if alone := activeUnder(t, app, name); alone != own {
				t.Errorf("%s holds the slot as %q, and with no program running the privacy status reads %q, want %q", method, name, alone, own)
			}
			want := "declared-program"
			if own != "" {
				want = own + ",declared-program"
			}
			reading, detail := runningUnder(t, app, name)
			if reading != want {
				t.Errorf("while %s's program runs as %q the privacy status reads %q, want %q", method, name, reading, want)
			}
			if detail == unattributed || !strings.Contains(detail, "cannot see or vouch") {
				t.Errorf("while %s's program runs as %q the declared-program row says %q, not whose program it is", method, name, detail)
			}
		}
	}
}

// runningUnder holds the slot under name while a declared program is counted
// running, and returns what the privacy status then reports active and what
// its declared-program row says.
func runningUnder(t *testing.T, app *desktop.App, name string) (string, string) {
	t.Helper()
	release, held := desktop.HoldSlotForTest(app, name)
	if !held {
		t.Fatalf("the slot was not free to hold as %q", name)
	}
	defer release()
	ended := desktop.DeclaredProgramRunningForTest(app)
	defer ended()
	return activeNow(app), declaredProgramDetail(app)
}

// declaredProgramDetail is what the privacy status's declared-program row
// says now.
func declaredProgramDetail(app *desktop.App) string {
	for _, state := range app.DisclosureStatus().States {
		if state.ID == "declared-program" {
			return state.Detail
		}
	}
	return ""
}

// Readmit starts a program in exactly two places, and both report it to the
// observer every operation's context carries: the one read of a locator
// (secret.Locator.Read), through which every credential, key and token is
// resolved, and a source's transfer program (evidencesource). No other
// package the facade can reach imports os/exec or starts a process another
// way, so no program runs that the declared-program row could miss.
func TestOnlyDeclaredProgramsAreStarted(t *testing.T) {
	starters := map[string]bool{}
	err := filepath.WalkDir("..", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if imported == "os/exec" {
				starters[filepath.ToSlash(path)] = true
			}
		}
		for _, starter := range []string{"os.StartProcess", "syscall.ForkExec", "syscall.Exec", "syscall.StartProcess"} {
			if strings.Contains(string(source), starter) {
				starters[filepath.ToSlash(path)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"../secret/secret.go": true, "../evidencesource/transfer.go": true}
	if !maps.Equal(starters, want) {
		t.Errorf("programs are started from %v; only the locator read and the transfer program report themselves (%v)", slices.Sorted(maps.Keys(starters)), slices.Sorted(maps.Keys(want)))
	}
	for path := range want {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), "DeclaredProgramStarting(ctx)") {
			t.Errorf("%s starts a declared program without reporting it", path)
		}
	}
}

// reachingOf reads, from the facade's own source, which bound methods can
// reach a network destination or change a target, and the name of every slot
// claim each makes. A method can reach one when it, or a facade method or
// function it calls:
//
//   - takes execution admission: work that reaches a destination, including a
//     fixture reset that changes one, reserves a runner instance (admissionsOf);
//   - hands the system resolver to a call, which looks a configured host name
//     up through the operating system;
//   - calls a function or method of the hub's client packages that takes a
//     context — the calls that wait on the hub, its identity provider or a
//     runner's hub. The rest of those packages read local files only.
//
// A claim is run (unnamed), runNamed, claim or begin, named by a literal or a
// constant of the package; one that is neither reads as "?".
func reachingOf(t *testing.T) (map[string][]string, map[string][]string) {
	t.Helper()
	contextual := map[string]map[string]bool{}
	contextualMethods := map[string]bool{}
	for _, client := range []string{"hubclient", "customerrunner"} {
		functions := map[string]bool{}
		for _, file := range parsePackage(t, filepath.Join("..", client)) {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || !function.Name.IsExported() || len(function.Type.Params.List) == 0 {
					continue
				}
				first, ok := function.Type.Params.List[0].Type.(*ast.SelectorExpr)
				if !ok || first.Sel.Name != "Context" || !isPackage(first.X, "context") {
					continue
				}
				if function.Recv != nil {
					contextualMethods[function.Name.Name] = true
				} else {
					functions[function.Name.Name] = true
				}
			}
		}
		contextual["github.com/bharm16/readmit/internal/"+client] = functions
	}

	methods, functions := map[string]*ast.FuncDecl{}, map[string]*ast.FuncDecl{}
	imports, constants := map[string]string{}, map[string]string{}
	for _, file := range parsePackage(t, ".") {
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			name := filepath.Base(path)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = path
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv != nil {
					methods[declaration.Name.Name] = declaration
				} else {
					functions[declaration.Name.Name] = declaration
				}
			case *ast.GenDecl:
				if declaration.Tok != token.CONST {
					continue
				}
				for _, spec := range declaration.Specs {
					value := spec.(*ast.ValueSpec)
					for i, name := range value.Names {
						if i < len(value.Values) {
							if literal, ok := value.Values[i].(*ast.BasicLit); ok && literal.Kind == token.STRING {
								constants[name.Name], _ = strconv.Unquote(literal.Value)
							}
						}
					}
				}
			}
		}
	}
	nameOf := func(expression ast.Expr) string {
		switch expression := expression.(type) {
		case *ast.BasicLit:
			if name, err := strconv.Unquote(expression.Value); err == nil {
				return name
			}
		case *ast.Ident:
			if name, ok := constants[expression.Name]; ok {
				return name
			}
		}
		return "?"
	}

	type found struct{ why, claims []string }
	derived := map[*ast.FuncDecl]*found{}
	var visit func(*ast.FuncDecl) *found
	visit = func(declaration *ast.FuncDecl) *found {
		if result, ok := derived[declaration]; ok {
			return result
		}
		result := &found{}
		derived[declaration] = result
		add := func(list *[]string, value string) {
			if !slices.Contains(*list, value) {
				*list = append(*list, value)
			}
		}
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, argument := range call.Args {
				if selector, ok := argument.(*ast.SelectorExpr); ok && selector.Sel.Name == "SystemResolver" && isPackage(selector.X, "sendpolicy") {
					add(&result.why, "the system resolver")
				}
			}
			function := call.Fun
			if generic, ok := function.(*ast.IndexListExpr); ok {
				function = generic.X
			}
			var callee *ast.FuncDecl
			switch function := function.(type) {
			case *ast.Ident:
				switch function.Name {
				case "run":
					add(&result.claims, "")
				case "runNamed":
					add(&result.claims, nameOf(call.Args[1]))
				default:
					callee = functions[function.Name]
				}
			case *ast.SelectorExpr:
				receiver, _ := function.X.(*ast.Ident)
				switch {
				case receiver != nil && receiver.Name == "a":
					if function.Sel.Name == "claim" || function.Sel.Name == "begin" {
						add(&result.claims, nameOf(call.Args[0]))
					}
					callee = methods[function.Sel.Name]
				case receiver != nil && imports[receiver.Name] != "":
					if contextual[imports[receiver.Name]][function.Sel.Name] {
						add(&result.why, receiver.Name+"."+function.Sel.Name)
					}
				case contextualMethods[function.Sel.Name]:
					add(&result.why, "the hub client's "+function.Sel.Name)
				}
			}
			if callee != nil {
				inner := visit(callee)
				for _, why := range inner.why {
					add(&result.why, why)
				}
				for _, claim := range inner.claims {
					add(&result.claims, claim)
				}
			}
			return true
		})
		return result
	}

	admissions := admissionsOf(t)
	reaching, claims := map[string][]string{}, map[string][]string{}
	facade := reflect.TypeFor[*desktop.App]()
	for i := range facade.NumMethod() {
		method := facade.Method(i).Name
		declaration := methods[method]
		if declaration == nil {
			t.Fatalf("bound method %s has no declaration in the facade's source", method)
		}
		result := visit(declaration)
		why := slices.Clone(result.why)
		if admissions[method]["execute"] {
			why = append([]string{"execution admission"}, why...)
		}
		reaching[method], claims[method] = why, result.claims
	}
	return reaching, claims
}

// parsePackage parses the non-test Go files of one package directory.
func parsePackage(t *testing.T, dir string) []*ast.File {
	t.Helper()
	sources, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fileset := token.NewFileSet()
	var files []*ast.File
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileset, source, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatalf("no Go source in %s", dir)
	}
	return files
}

// isPackage reports whether expression is the identifier of package name.
func isPackage(expression ast.Expr, name string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == name
}

// witness reads the privacy status each time a destination is reached, while
// the operation that reached it is still waiting on that destination: every
// destination below reads the status before it answers, so what it read is
// what a person refreshing the privacy region would have seen at that moment.
type witness struct {
	app  atomic.Pointer[desktop.App]
	mu   sync.Mutex
	seen []string
}

// observe records the activities the privacy status reports active now, or
// how it answered when it did not report them.
func (w *witness) observe() {
	app := w.app.Load()
	if app == nil {
		return
	}
	reading := activeNow(app)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen = append(w.seen, reading)
}

// during runs call, one operation, and holds every reading taken while it ran
// to activity: there has to be at least one, and each must name activity and
// nothing else.
func (w *witness) during(t *testing.T, operation, activity string, call func()) {
	t.Helper()
	w.mu.Lock()
	before := len(w.seen)
	w.mu.Unlock()
	call()
	w.mu.Lock()
	readings := slices.Clone(w.seen[before:])
	w.mu.Unlock()
	if len(readings) == 0 {
		t.Errorf("%s never reached its destination, so the privacy status was never read while it did", operation)
	}
	for _, reading := range readings {
		if reading != activity {
			t.Errorf("while %s reached its destination the privacy status read %q, want %q active", operation, reading, activity)
		}
	}
	if after := activeNow(w.app.Load()); after != "" {
		t.Errorf("after %s the privacy status still reports %q active", operation, after)
	}
}

// activeNow is the activities the privacy status reports active, comma
// separated, or how it answered when it reported none of them.
func activeNow(app *desktop.App) string {
	status := app.DisclosureStatus()
	if status.State != desktop.Completed {
		return "answered " + string(status.State)
	}
	var active []string
	for _, state := range status.States {
		if state.State == "active" {
			active = append(active, state.ID)
		}
	}
	return strings.Join(active, ",")
}

// witnessPeer is a destination on loopback that reads the privacy status as
// each connection reaches it. With acknowledge it then answers the one frame a
// send delivers, as a downstream system does; without, it closes a connection
// that carries nothing, which is all a connectivity check or a fixture reset's
// quiet check waits for.
func witnessPeer(t *testing.T, w *witness, acknowledge bool) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			w.observe()
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
	return listener.Addr().String()
}

// witnessLookups replaces name resolution with a resolver that reads the
// privacy status at each lookup and then refuses it, so nothing leaves this
// machine and a lookup is seen while the operation that asked it waits.
func witnessLookups(t *testing.T, w *witness) {
	t.Helper()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		w.observe()
		return nil, errors.New("lookup refused")
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
}

// A connectivity check, a fixture reset, a send-policy evaluation and a
// controlled reduction each reach the recorded environment or its name, and
// the environment row reads active while each one does, idle once it is done.
func TestDisclosureStatusReportsEnvironmentWorkActiveWhileItReachesTheEnvironment(t *testing.T) {
	w := &witness{}
	witnessLookups(t, w)
	app := workspaceApp(t)
	w.app.Store(app)
	dir := t.TempDir()
	recorded := app.ReadTarget(dir, "target.json")
	if recorded.State != desktop.Completed || recorded.Target == nil {
		t.Fatalf("new target: %+v", recorded)
	}
	environment := *recorded.Target
	environment.Name, environment.Classification, environment.ApprovedTransport = "lab-mllp", "nonproduction", true
	environment.Address = witnessPeer(t, w, false)
	if saved := app.SaveTarget(desktop.TargetSaveRequest{Workspace: dir, TargetFile: "target.json", Target: environment}); saved.State != desktop.Completed {
		t.Fatalf("target: %+v", saved)
	}
	writeDocument(t, dir, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	writeDocument(t, dir, "reset-plan.json", `{"schema":"readmit-reset-plan/v1","environment":"lab-mllp","actions":[`+
		`{"id":"quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"Confirm the endpoint is quiet"}]}`)

	w.during(t, "a connectivity check", "environment", func() {
		app.CheckTarget(desktop.TargetCheckRequest{Workspace: dir, TargetFile: "target.json", PolicyFile: "policy.json"})
	})
	w.during(t, "a fixture reset", "environment", func() {
		app.ResetTarget(desktop.TargetResetRequest{Workspace: dir, TargetFile: "target.json", PlanFile: "reset-plan.json",
			OutcomeFile: "reset-outcome.json", PolicyFile: "policy.json"})
	})
	w.during(t, "a send-policy evaluation", "environment", func() {
		app.EvaluateSendPolicy(desktop.SendPolicyEvalRequest{Workspace: dir, PolicyFile: "policy.json",
			Address: "scheduling.example.test:2575", Classification: "nonproduction", Explicit: true})
	})

	// A reduction's trials reset the same recorded environment and send to it.
	workspace := ackWorkspace(t, "127.0.0.1:1")
	if err := os.Remove(filepath.Join(workspace, "target.json")); err != nil {
		t.Fatal(err)
	}
	environment.Address = witnessPeer(t, w, true)
	if saved := app.SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "target.json", Target: environment}); saved.State != desktop.Completed {
		t.Fatalf("reduction target: %+v", saved)
	}
	writeAckSpec(t, workspace, "booking.json", "AE")
	writeDocument(t, workspace, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	writeDocument(t, workspace, "plan.json", `{"schema":"readmit-reset-plan/v1","environment":"lab-mllp","actions":[`+
		`{"id":"confirm","operator":"operator_confirms","authority":"none","instructions":"Empty the ledger"}]}`)
	opened := app.OpenCase(workspace, "case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the case a reduction reduces: %+v", opened)
	}
	w.during(t, "a controlled reduction", "environment", func() {
		app.StartReduction(desktop.ReductionRequest{Workspace: workspace, Case: "case", Identity: opened.Case.Identity,
			Spec: "booking.json", Assertions: []string{"ack"}, Trials: 4, Confirmations: 1, ResetPlan: "plan.json",
			Target: "target.json", Policy: "policy.json", Confirmed: []string{"confirm"}, Work: "reduction"})
	})
}

// A durable run and a suite run each send to their target, and the run row
// reads active while the target holds what they sent.
func TestDisclosureStatusReportsSendsActiveWhileTheyReachTheTarget(t *testing.T) {
	w := &witness{}
	app := workspaceApp(t)
	w.app.Store(app)
	workspace := ackWorkspace(t, witnessPeer(t, w, true))
	writeAckSpec(t, workspace, "booking.json", "AA")
	writeSuite(t, workspace, "nightly.json")

	run := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "booking.json", Output: "run"})
	if run.State != desktop.Completed || run.Preflight == nil {
		t.Fatalf("preflight: %+v", run)
	}
	w.during(t, "a durable run", "run", func() {
		app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "booking.json", Output: "run", Expected: run.Preflight.Identity})
	})
	suite := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "nightly.json", Environment: "east"})
	if suite.State != desktop.Completed || suite.Preflight == nil {
		t.Fatalf("suite preflight: %+v", suite)
	}
	w.during(t, "a suite run", "run", func() {
		app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: workspace, Suite: "nightly.json", Environment: "east",
			Output: "suite-run", Expected: suite.Preflight.Identity})
	})
}

// Checking a source's access and collecting from it each look its declared
// address up before anything else, and a capture listens on its port; the
// capture row reads active while each one does.
func TestDisclosureStatusReportsSourceWorkActiveWhileItReachesTheSource(t *testing.T) {
	w := &witness{}
	witnessLookups(t, w)
	app := workspaceApp(t)
	w.app.Store(app)
	root := t.TempDir()
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Transfer, Scope: "appointments",
		Address: "exports.example.test:22", Classification: "nonproduction", Command: "/bin/cat",
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "source.json", Source: source}); saved.State != desktop.Completed {
		t.Fatalf("source: %+v", saved)
	}
	writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["192.0.2.0/24"]}`)
	w.during(t, "a source access check", "capture", func() {
		app.DiagnoseSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", PolicyFile: "policy.json"})
	})
	w.during(t, "a source collection", "capture", func() {
		app.CollectSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", PolicyFile: "policy.json",
			OutputName: "staged", ReceiptName: "receipt.json"})
	})

	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if saved := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: root, PolicyFile: "receiver.json", Policy: policy}); saved.State != desktop.Completed {
		t.Fatalf("receiver policy: %+v", saved)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	w.during(t, "a capture", "capture", func() {
		done := make(chan desktop.CaptureSessionResult, 1)
		go func() {
			done <- app.StartCapture(desktop.CaptureRequest{Workspace: root, Kind: "collect", Address: address,
				PolicyFile: "receiver.json", OutputName: "case", JournalName: "journal", MaxMessages: 1, IdleTimeout: "5s"})
		}()
		// A sender reaches the capture's port, reads the status while the
		// capture is listening for it, and only then sends the one message
		// that completes the capture.
		var conn net.Conn
		for deadline := time.Now().Add(5 * time.Second); conn == nil && time.Now().Before(deadline); {
			if conn, err = net.DialTimeout("tcp", address, 50*time.Millisecond); err != nil {
				conn = nil
				time.Sleep(10 * time.Millisecond)
			}
		}
		if conn == nil {
			t.Error("the capture never listened on its port")
			app.Cancel("capture")
			<-done
			return
		}
		w.observe()
		_, _ = conn.Write(mllp.Frame([]byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")))
		_ = conn.Close()
		if session := <-done; session.State != desktop.Completed || session.Received != 1 {
			t.Errorf("the capture the status was read during: %+v", session)
		}
	})
}

// Enrolling a runner and executing its job each reach the hub the runner's
// configuration names, and the runner row reads active while they do.
func TestDisclosureStatusReportsRunnerWorkActiveWhileItReachesTheHub(t *testing.T) {
	w := &witness{}
	fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
	observe := w.observe
	fixture.observe.Store(&observe)
	w.app.Store(fixture.app)
	configPath := fixture.gateRunnerConfig(t)
	w.during(t, "a runner enrollment", "runner", func() { fixture.app.EnrollRunner(configPath) })
	jobPath := filepath.Join(t.TempDir(), "job.json")
	if saved := fixture.app.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: writableSpec(t, t.TempDir()), Output: jobPath}); saved.State != desktop.Completed {
		t.Fatalf("job: %+v", saved)
	}
	w.during(t, "a runner execution", "runner", func() {
		fixture.app.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: jobPath})
	})
	if fixture.admissions() == 0 {
		t.Fatal("no runner work reached the hub's runner admission")
	}
}

// Every hub operation reaches the configured hub or its identity provider, and
// the hub row reads active while each one does — including the sign-in, while
// it waits for the browser and while it exchanges the code — and not while the
// window merely holds a connection.
func TestDisclosureStatusReportsEveryHubOperationActiveWhileItReachesTheHub(t *testing.T) {
	w := &witness{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		w.observe()
		if strings.HasPrefix(r.URL.Path, "/health/") {
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(rw, "unavailable", http.StatusServiceUnavailable)
	})
	token := signedHubAccessToken(t, "doctor@hospital.org", []string{"evidence.read", "evidence.write"})
	fixture := newConnectedHubApp(t, mux, "doctor@hospital.org", []string{"evidence.read", "evidence.write"}, func(rw http.ResponseWriter, r *http.Request) {
		w.observe()
		rw.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(rw, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 1800})
	})
	app := fixture.app
	w.app.Store(app)
	if reading := activeNow(app); reading != "" {
		t.Fatalf("a connected hub nobody is using reads %q active", reading)
	}

	w.during(t, "a sign-in", "hub", func() {
		flow := app.StartHubAuth()
		if flow.State != desktop.Completed {
			t.Fatalf("sign-in: %+v", flow)
		}
		state := signInState(t, flow)
		go func() {
			// The browser returns only once the sign-in is waiting for it.
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
				if activeNow(app) == "hub" {
					break
				}
			}
			w.observe()
			returnToLoopback(flow, "code=auth-code&state="+state)
		}()
		if signed := app.CompleteHubAuth("", ""); signed.State != desktop.Completed || !signed.Authenticated {
			t.Errorf("sign-in: %+v", signed)
		}
	})

	workspace := t.TempDir()
	upload := filepath.Join(workspace, "upload.bin")
	writeDocument(t, workspace, "upload.bin", "synthetic upload")
	policy, err := json.Marshal(sharing.Policy{Schema: sharing.PolicySchema, Support: true, Destinations: []string{"local-file"}, MaxBytes: 65536}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, workspace, "sharing-policy.json", string(policy))
	spec := []byte(`{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`)
	review, err := expectation.Review("booking", spec, []profileversion.Version{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.Approve("booking", spec, []profileversion.Version{}, nil, review.Identity, "Local approver label", "synthetic rationale")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := release.Encode()
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, workspace, "booking-release.json", string(raw))
	digest := strings.Repeat("a", 64)
	project := "cardio-study"
	lifecycle := desktop.HubLifecycleCommandRequest{Project: project, ID: "revision-1", Kind: "revision", Resource: "evidence",
		Artifact: digest, Parents: []string{}, Reason: "synthetic revision"}

	for _, operation := range []struct {
		name string
		call func()
	}{
		{"HubStatus", func() { app.HubStatus() }},
		{"ListHubProjectArtifacts", func() { app.ListHubProjectArtifacts(project) }},
		{"DownloadHubArtifact", func() {
			app.DownloadHubArtifact(desktop.HubDownloadRequest{Project: project, Digest: digest, DestinationPath: filepath.Join(workspace, "artifact.bin")})
		}},
		{"UploadHubArtifact", func() { app.UploadHubArtifact(desktop.HubUploadRequest{Project: project, SourcePath: upload}) }},
		{"ListHubReviews", func() { app.ListHubReviews(project) }},
		{"SearchHubReviews", func() { app.SearchHubReviews(desktop.HubReviewQueryRequest{Project: project, Text: "synthetic"}) }},
		{"ListHubNotifications", func() { app.ListHubNotifications(project) }},
		{"SearchHubNotifications", func() { app.SearchHubNotifications(desktop.HubReviewQueryRequest{Project: project, Text: "synthetic"}) }},
		{"PostHubReview", func() {
			app.PostHubReview(desktop.HubReviewCommandRequest{Project: project, ID: "comment-1", Kind: "comment", Evidence: digest, Text: "synthetic comment"})
		}},
		{"PostHubReleaseReview", func() {
			app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{Project: project, Workspace: workspace, Entry: "booking-release.json",
				Kind: "review-request", ID: "release-1", Recipient: "reviewer@hospital.org", Text: "synthetic release"})
		}},
		{"PostHubSupportReview", func() {
			app.PostHubSupportReview(desktop.HubSupportReviewRequest{Project: project, Workspace: workspace, Entry: "sharing-policy.json",
				Kind: "support-policy", ID: "policy-1"})
		}},
		{"ListHubLifecycle", func() { app.ListHubLifecycle(project) }},
		{"PostHubLifecycle", func() { app.PostHubLifecycle(lifecycle) }},
		{"ReconcileHubOfflineDraft", func() { app.ReconcileHubOfflineDraft(lifecycle) }},
		{"DownloadHubExport", func() {
			app.DownloadHubExport(desktop.HubDownloadRequest{Project: project, Digest: digest, DestinationPath: filepath.Join(workspace, "export.bin")})
		}},
		{"DisconnectHub then ConnectHub", func() { app.DisconnectHub(); app.ConnectHub() }},
		{"DiagnoseHub", func() { app.DiagnoseHub() }},
	} {
		w.during(t, operation.name, "hub", operation.call)
	}

	// Starting a sign-in opens only the loopback listener the browser returns
	// to, which nothing outside can witness, and it is brief: it is read
	// continually while it starts, as many times as it takes to see it.
	readings := map[string]bool{}
	for attempt := 0; attempt < 500 && !readings["hub"]; attempt++ {
		maps.Copy(readings, sampling(app, func() {
			if flow := app.StartHubAuth(); flow.State != desktop.Completed {
				t.Errorf("sign-in start: %+v", flow)
			}
		}))
	}
	sampledActive(t, "the start of a sign-in", "hub", readings)
	app.DisconnectHub()
}

// sampling reads the privacy status every 100µs while call runs and returns
// every distinct reading, for an operation whose only receivers are ones it
// starts in this process, where no outside destination can read the status at
// the moment it is reached. The readings are returned once the reader stops.
func sampling(app *desktop.App, call func()) map[string]bool {
	readings := map[string]bool{}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			readings[activeNow(app)] = true
			select {
			case <-done:
				return
			case <-time.After(100 * time.Microsecond):
			}
		}
	}()
	call()
	close(done)
	<-stopped
	return readings
}

// sampledActive holds what sampling read during operation: the status reported
// activity active at least once, and otherwise only idle — never busy, never
// another activity.
func sampledActive(t *testing.T, operation, activity string, readings map[string]bool) {
	t.Helper()
	if !readings[activity] {
		t.Errorf("the privacy status never read %q active during %s: %v", activity, operation, readings)
	}
	for reading := range readings {
		if reading != "" && reading != activity {
			t.Errorf("during %s the privacy status read %q", operation, reading)
		}
	}
}

// A practice run, a disclosure review and a derived export send only to
// fixture receivers they start on loopback in this process — the latter two
// for their proofs — and the run row discloses them: read continually while
// each runs, the status reports the run active and never answers busy or names
// another activity.
func TestDisclosureStatusReportsPracticeAndProofRunsActiveWhileTheySend(t *testing.T) {
	held := func(t *testing.T, operation string, readings map[string]bool) {
		sampledActive(t, operation, "run", readings)
	}
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)
	held(t, "a practice run", sampling(app, func() {
		if result := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"}); result.State != desktop.Completed {
			t.Errorf("practice run: %+v", result)
		}
	}))

	privacy := workspaceApp(t)
	review := privacyFixture(t, "policy.json", "policy")
	var outcome *desktop.PrivacyReviewOutcome
	held(t, "a disclosure review", sampling(privacy, func() { outcome = derived(t, privacy, privacyRequest(review, "policy.json")) }))
	held(t, "a derived export", sampling(privacy, func() {
		exported := privacy.ExportDerivedPacket(desktop.PrivacyExportRequest{Workspace: review, Review: outcome.Review,
			LocalState: outcome.Private, Approval: outcome.Identity, Output: "packet"})
		if exported.State != desktop.Completed {
			t.Errorf("derived export: %+v", exported)
		}
	}))
}
