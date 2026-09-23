package capability_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bharm16/readmit/internal/capability"
)

// The shipped ledger is itself read through the strict contract it declares,
// so a row that skips a member, an owner, or its screen/action/backend fails
// here before any surface check leans on it; then every reference it names is
// resolved against this repository, so a renamed test or a moved operation
// fails here rather than leaving a row that points at nothing.
func TestShippedLedgerIsACheckedContractDocument(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatalf("the shipped capability ledger does not read: %v", err)
	}
	if ledger.Schema != capability.SchemaV2 {
		t.Fatalf("the shipped ledger is %s; its tests must be checkable references under %s", ledger.Schema, capability.SchemaV2)
	}
	if ledger.Program != "bharm16/readmit#244" {
		t.Fatalf("the ledger does not name the program it covers: %s", ledger.Program)
	}
	for _, kind := range []string{capability.KindCLI, capability.KindDesktop, capability.KindHub, capability.KindPortal} {
		if !slices.ContainsFunc(ledger.Rows, func(row capability.Row) bool { return row.Kind == kind }) {
			t.Errorf("the ledger holds no %s rows", kind)
		}
	}
	if err := ledger.Check(os.DirFS("../..")); err != nil {
		t.Errorf("the shipped ledger names references this repository does not hold:\n%v", err)
	}
}

// A v1 document still reads under v1's own rules: prose tests, a free-text
// disposition, and no portal kind. It never passes the reference check.
func TestVersionOneStillReadsAndCannotBeChecked(t *testing.T) {
	v1 := `{"schema":"readmit-capability-ledger/v1","program":"bharm16/readmit#244","rows":[` +
		`{"id":"cli.test","source":"readmit test","kind":"cli","capability":"run a test","owner":"bharm16/readmit#257",` +
		`"screen":"s","action":"a","test":"t","implemented":false},` +
		`{"id":"hub.live","source":"hub GET /health/live","kind":"hub","capability":"probe","owner":"bharm16/readmit#261",` +
		`"disposition":"not customer work"}]}`
	ledger, err := capability.Decode([]byte(v1))
	if err != nil {
		t.Fatalf("a complete v1 ledger was refused: %v", err)
	}
	if ledger.Schema != capability.SchemaV1 || ledger.Rows[0].Test != "t" || !ledger.Rows[1].Disposed() {
		t.Fatalf("the v1 ledger read differently: %+v", ledger)
	}
	if err := ledger.Check(fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "prose") {
		t.Fatalf("a v1 ledger passed the reference check: %v", err)
	}
	for name, broken := range map[string]string{
		"unknown member":  `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[],"extra":1}`,
		"unknown version": `{"schema":"readmit-capability-ledger/v9","program":"p","rows":[]}`,
		"no declaration":  `{"program":"p","rows":[]}`,
		"no owner":        `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"cli","capability":"c","screen":"s","action":"a","test":"t","implemented":false}]}`,
		"no screen":       `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"cli","capability":"c","owner":"bharm16/readmit#1","action":"a","test":"t","implemented":false}]}`,
		"a v2 member":     `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"cli","capability":"c","owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":false,"backend":{"package":"internal/x","name":"Run"}}]}`,
		"a portal row":    `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"portal event e","kind":"portal","capability":"c","owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":false}]}`,
		"a tooling row":   `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"make check","kind":"tooling","capability":"c","owner":"bharm16/readmit#1","disposition":"developer tooling"}]}`,
		"two rows one source": `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[` +
			`{"id":"a","source":"readmit test","kind":"cli","capability":"c","owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":false},` +
			`{"id":"b","source":"readmit test","kind":"cli","capability":"c2","owner":"bharm16/readmit#2","screen":"s","action":"a","test":"t","implemented":false}]}`,
		"disposed and implemented": `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"hub","capability":"c","owner":"bharm16/readmit#1","disposition":"not customer work","screen":"also a screen","implemented":false}]}`,
	} {
		if _, err := capability.Decode([]byte(broken)); err == nil {
			t.Errorf("v1 %s was accepted", name)
		}
	}
	// A blank disposition has always read as none under v1, so a covered row
	// carrying one still reads; two rows sharing an id is refused, as v1's
	// own rule always said.
	blank := `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"cli","capability":"c",` +
		`"owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":true,"disposition":" "}]}`
	if ledger, err := capability.Decode([]byte(blank)); err != nil || ledger.Rows[0].Disposed() {
		t.Errorf("a v1 row with a blank disposition no longer reads as covered: %v", err)
	}
	twice := `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[` +
		`{"id":"x","source":"a","kind":"cli","capability":"c","owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":false},` +
		`{"id":"x","source":"b","kind":"cli","capability":"c","owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":false}]}`
	if _, err := capability.Decode([]byte(twice)); err == nil {
		t.Error("two v1 rows sharing an id were accepted")
	}
	if _, err := capability.Decode([]byte(`{"schema":"readmit-capability-ledger/v9","program":"p","rows":[]}`)); !errors.Is(err, capability.ErrUnsupportedVersion) {
		t.Errorf("an unknown version is not reported as unsupported: %v", err)
	}
}

// v2 rows, spelled as the three shapes the contract allows.
const (
	implementedRow = `{"id":"cli.test","source":"readmit test","kind":"cli","capability":"run a test","owner":"bharm16/readmit#257",` +
		`"screen":"the runs panel","action":"run a saved test",` +
		`"backend":{"package":"internal/testrunner","name":"Run"},` +
		`"inputs":["readmit-test/v1"],"outputs":["readmit-result/v1","hl7"],"prerequisites":["execute"],` +
		`"gui_test":{"file":"desktop/frontend/src/runs.test.tsx","name":"runs a saved test"},` +
		`"parity_test":{"package":"internal/desktop","name":"TestRunMatchesTheCommandLine"},` +
		`"implemented":true}`
	openRow = `{"id":"cli.replay","source":"readmit replay","kind":"cli","capability":"send","owner":"bharm16/readmit#300",` +
		`"screen":"a replay screen","action":"send selected messages",` +
		`"backend":{"package":"internal/replay","name":"Plan.Send"},"inputs":["readmit-case/v3"],"outputs":[],` +
		`"prerequisites":["execute-if-send"],"implemented":false}`
	disposedRow = `{"id":"portal.sign","source":"portal commercial.Account.Issue","kind":"portal","capability":"sign","owner":"bharm16/readmit#265",` +
		`"backend":{"package":"internal/commercial","name":"Account.Issue"},"inputs":[],"outputs":["readmit-entitlement/v2"],` +
		`"prerequisites":["vendor"],` +
		`"disposition":{"kind":"vendor-issuance","reason":"the vendor's issuer signs; no customer machine does"},"implemented":false}`
)

const toolingRow = `{"id":"tooling.make.check","source":"make check","kind":"tooling","capability":"vet and format-check the tree",` +
	`"owner":"bharm16/readmit#244","inputs":[],"outputs":[],"prerequisites":[],` +
	`"disposition":{"kind":"developer-tooling","reason":"a developer entry point"},"implemented":false}`

func v2(rows ...string) string {
	return `{"schema":"readmit-capability-ledger/v2","program":"bharm16/readmit#244","rows":[` + strings.Join(rows, ",") + `]}`
}

// A superseded row retires an earlier address only by naming, as its
// successor, the implemented row of its own surface that answers the
// capability now.
func TestSupersededNamesAnImplementedSuccessor(t *testing.T) {
	successor := strings.NewReplacer(`"id":"cli.test"`, `"id":"hub.v2"`, `"source":"readmit test"`, `"source":"hub GET /v2/history"`,
		`"kind":"cli"`, `"kind":"hub"`).Replace(implementedRow)
	superseded := func(successor string) string {
		return `{"id":"hub.v1","source":"hub GET /v1/history","kind":"hub","capability":"read history","owner":"bharm16/readmit#262",` +
			`"backend":{"package":"hub","name":"Store.reviewRequest"},"inputs":[],"outputs":[],"prerequisites":["hub:evidence.read"],` +
			`"disposition":{"kind":"superseded","reason":"kept for v1 clients","successor":"` + successor + `"},"implemented":false}`
	}
	if _, err := capability.Decode([]byte(v2(successor, superseded("hub GET /v2/history")))); err != nil {
		t.Fatalf("a superseded row naming its implemented successor was refused: %v", err)
	}
	for name, ledger := range map[string]string{
		"no such row":               v2(successor, superseded("hub GET /v3/history")),
		"a longer source contained": v2(successor, superseded("hub GET /v2/hist")),
		"successor of another kind": v2(implementedRow, superseded("readmit test")),
		"successor still open": v2(strings.Replace(openRow, `"kind":"cli"`, `"kind":"hub"`, 1),
			superseded("readmit replay")),
		"names only itself":   v2(superseded("hub GET /v1/history")),
		"no successor member": v2(successor, strings.Replace(superseded("x"), `,"successor":"x"`, ``, 1)),
		"successor not superseded": v2(strings.Replace(disposedRow,
			`"reason":"the vendor's issuer signs; no customer machine does"`,
			`"reason":"the vendor's issuer signs; no customer machine does","successor":"portal trial.Issue"`, 1)),
	} {
		if _, err := capability.Decode([]byte(ledger)); err == nil {
			t.Errorf("superseded with %s was accepted", name)
		}
	}
}

func TestVersionTwoReadsImplementedOpenAndDisposedRows(t *testing.T) {
	ledger, err := capability.Decode([]byte(v2(implementedRow, openRow, disposedRow, toolingRow)))
	if err != nil {
		t.Fatalf("a complete v2 ledger was refused: %v", err)
	}
	if tooling := ledger.Rows[3]; tooling.Kind != capability.KindTooling || tooling.Backend != (capability.Symbol{}) ||
		tooling.Disposition.Kind != capability.DispositionDeveloperTooling {
		t.Errorf("the tooling row read differently: %+v", tooling)
	}
	implemented, open, disposed := ledger.Rows[0], ledger.Rows[1], ledger.Rows[2]
	if implemented.Backend != (capability.Symbol{Package: "internal/testrunner", Name: "Run"}) ||
		implemented.GUITest != (capability.FrontendTest{File: "desktop/frontend/src/runs.test.tsx", Name: "runs a saved test"}) ||
		implemented.ParityTest != (capability.GoTest{Package: "internal/desktop", Name: "TestRunMatchesTheCommandLine"}) || !implemented.Implemented {
		t.Errorf("the implemented row read differently: %+v", implemented)
	}
	if !slices.Equal(implemented.Inputs, []string{"readmit-test/v1"}) || !slices.Equal(implemented.Outputs, []string{"readmit-result/v1", capability.RawHL7}) ||
		!slices.Equal(implemented.Prerequisites, []string{"execute"}) {
		t.Errorf("the implemented row's documents and prerequisites read differently: %+v", implemented)
	}
	if open.Implemented || open.GUITest != (capability.FrontendTest{}) || open.Backend.Name != "Plan.Send" || len(open.Outputs) != 0 {
		t.Errorf("the open row read differently: %+v", open)
	}
	if !disposed.Disposed() || disposed.Disposition.Kind != capability.DispositionVendorIssuance || disposed.Kind != capability.KindPortal {
		t.Errorf("the disposed row read differently: %+v", disposed)
	}
}

// Every way a v2 document can fail its contract is refused, not repaired.
func TestVersionTwoRefusals(t *testing.T) {
	replace := func(row, old, new string) string {
		if !strings.Contains(row, old) {
			t.Fatalf("fixture does not hold %q", old)
		}
		return strings.Replace(row, old, new, 1)
	}
	for name, broken := range map[string]string{
		"unknown row member":              v2(replace(openRow, `"implemented":false`, `"implemented":false,"test":"prose"`)),
		"unknown backend member":          v2(replace(openRow, `"name":"Plan.Send"`, `"name":"Plan.Send","line":3`)),
		"missing backend":                 v2(replace(openRow, `"backend":{"package":"internal/replay","name":"Plan.Send"},`, ``)),
		"null backend":                    v2(replace(openRow, `{"package":"internal/replay","name":"Plan.Send"}`, `null`)),
		"backend without a package":       v2(replace(openRow, `"package":"internal/replay",`, ``)),
		"backend outside the tree":        v2(replace(openRow, `internal/replay`, `../replay`)),
		"backend absolute":                v2(replace(openRow, `internal/replay`, `/internal/replay`)),
		"backend not an identifier":       v2(replace(openRow, `Plan.Send`, `Plan.Send()`)),
		"backend with a backslash":        v2(replace(openRow, `internal/replay`, `internal\\replay`)),
		"missing inputs":                  v2(replace(openRow, `"inputs":["readmit-case/v3"],`, ``)),
		"null outputs":                    v2(replace(openRow, `"outputs":[]`, `"outputs":null`)),
		"missing prerequisites":           v2(replace(openRow, `"prerequisites":["execute-if-send"],`, ``)),
		"input not a contract":            v2(replace(openRow, `"readmit-case/v3"`, `"case bundle"`)),
		"input without a version":         v2(replace(openRow, `"readmit-case/v3"`, `"readmit-case"`)),
		"input named twice":               v2(replace(openRow, `["readmit-case/v3"]`, `["readmit-case/v3","readmit-case/v3"]`)),
		"output not a contract":           v2(replace(openRow, `"outputs":[]`, `"outputs":["HL7"]`)),
		"prerequisite unknown":            v2(replace(openRow, `"execute-if-send"`, `"admin"`)),
		"prerequisite hub action unknown": v2(replace(openRow, `"execute-if-send"`, `"hub:delete"`)),
		"prerequisite named twice":        v2(replace(openRow, `["execute-if-send"]`, `["execute","execute"]`)),
		"null action":                     v2(replace(openRow, `"send selected messages"`, `null`)),
		"null parity test":                v2(replace(implementedRow, `{"package":"internal/desktop","name":"TestRunMatchesTheCommandLine"}`, `null`)),
		"null disposition":                v2(replace(openRow, `"implemented":false`, `"disposition":null,"implemented":false`)),
		"null successor":                  v2(replace(disposedRow, `"reason":"the vendor's issuer signs; no customer machine does"`, `"reason":"r","successor":null`)),
		"empty disposition object":        v2(replace(openRow, `"implemented":false`, `"disposition":{},"implemented":false`)),
		"blank disposition members":       v2(replace(openRow, `"implemented":false`, `"disposition":{"kind":"","reason":""},"implemented":false`)),
		"disposition without a kind":      v2(replace(disposedRow, `"kind":"vendor-issuance"`, `"kind":""`)),
		"empty gui test object":           v2(replace(implementedRow, `{"file":"desktop/frontend/src/runs.test.tsx","name":"runs a saved test"}`, `{}`)),
		"blank gui test members":          v2(replace(openRow, `"implemented":false`, `"gui_test":{"file":"","name":""},"implemented":false`)),
		"empty parity test object":        v2(replace(implementedRow, `{"package":"internal/desktop","name":"TestRunMatchesTheCommandLine"}`, `{}`)),
		"blank parity test members":       v2(replace(openRow, `"implemented":false`, `"parity_test":{"package":"","name":""},"implemented":false`)),
		"empty backend object":            v2(replace(openRow, `{"package":"internal/replay","name":"Plan.Send"}`, `{}`)),
		"blank backend members":           v2(replace(openRow, `{"package":"internal/replay","name":"Plan.Send"}`, `{"package":"","name":""}`)),
		"blank screen":                    v2(replace(openRow, `"a replay screen"`, `""`)),
		"blank screen on a disposed row":  v2(replace(disposedRow, `"implemented":false`, `"screen":"","implemented":false`)),
		"blank successor":                 v2(replace(disposedRow, `"reason":"the vendor's issuer signs; no customer machine does"`, `"reason":"r","successor":""`)),
		"tooling with a backend":          v2(replace(toolingRow, `"inputs":[]`, `"backend":{"package":"internal/x","name":"Run"},"inputs":[]`)),
		"tooling not disposed": v2(replace(toolingRow, `"disposition":{"kind":"developer-tooling","reason":"a developer entry point"},`,
			`"screen":"s","action":"a",`)),
		"tooling disposed otherwise": v2(replace(toolingRow, `developer-tooling`, `machine-interface`)),
		"missing implemented":        v2(replace(openRow, `,"implemented":false`, ``)),
		"null screen":                v2(replace(openRow, `"a replay screen"`, `null`)),
		"no screen":                  v2(replace(openRow, `"screen":"a replay screen",`, ``)),
		"no action":                  v2(replace(openRow, `"action":"send selected messages",`, ``)),
		"open row names a test":      v2(replace(openRow, `"implemented":false`, `"gui_test":{"file":"desktop/frontend/src/runs.test.tsx","name":"x"},"implemented":false`)),
		"open row names parity":      v2(replace(openRow, `"implemented":false`, `"parity_test":{"package":"internal/desktop","name":"TestX"},"implemented":false`)),
		"implemented without gui":    v2(replace(implementedRow, `"gui_test":{"file":"desktop/frontend/src/runs.test.tsx","name":"runs a saved test"},`, ``)),
		"implemented without parity": v2(replace(implementedRow,
			`"parity_test":{"package":"internal/desktop","name":"TestRunMatchesTheCommandLine"},`, ``)),
		"null gui test":           v2(replace(implementedRow, `{"file":"desktop/frontend/src/runs.test.tsx","name":"runs a saved test"}`, `null`)),
		"gui test without a name": v2(replace(implementedRow, `,"name":"runs a saved test"`, ``)),
		"gui test not a test":     v2(replace(implementedRow, `runs.test.tsx`, `RunPanel.tsx`)),
		"gui test outside src":    v2(replace(implementedRow, `desktop/frontend/src/runs.test.tsx`, `tools/runs.test.tsx`)),
		"parity not a test":       v2(replace(implementedRow, `TestRunMatchesTheCommandLine`, `RunMatchesTheCommandLine`)),
		"prose owner":             v2(replace(openRow, `bharm16/readmit#300`, `the runs team`)),
		"unknown kind":            v2(replace(openRow, `"kind":"cli"`, `"kind":"web"`)),
		"disposition kind open":   v2(replace(disposedRow, `vendor-issuance`, `later`)),
		"disposition no reason":   v2(replace(disposedRow, `,"reason":"the vendor's issuer signs; no customer machine does"`, ``)),
		"disposition empty reason": v2(replace(disposedRow,
			`"the vendor's issuer signs; no customer machine does"`, `" "`)),
		"disposed with a screen":      v2(replace(disposedRow, `"implemented":false`, `"screen":"s","implemented":false`)),
		"disposed and implemented":    v2(replace(disposedRow, `"implemented":false`, `"implemented":true`)),
		"disposed and tested":         v2(replace(disposedRow, `"implemented":false`, `"parity_test":{"package":"internal/commercial","name":"TestX"},"implemented":false`)),
		"disposed without a backend":  v2(replace(disposedRow, `"backend":{"package":"internal/commercial","name":"Account.Issue"},`, ``)),
		"two rows one source":         v2(openRow, replace(openRow, `"id":"cli.replay"`, `"id":"cli.replay2"`)),
		"two rows one id":             v2(openRow, replace(openRow, `"source":"readmit replay"`, `"source":"readmit replay2"`)),
		"no rows":                     `{"schema":"readmit-capability-ledger/v2","program":"p","rows":[]}`,
		"no program":                  `{"schema":"readmit-capability-ledger/v2","rows":[` + openRow + `]}`,
		"unknown document member":     `{"schema":"readmit-capability-ledger/v2","program":"p","rows":[` + openRow + `],"extra":1}`,
		"row is not an object":        v2(`[]`),
		"v1 disposition string in v2": v2(replace(disposedRow, `{"kind":"vendor-issuance","reason":"the vendor's issuer signs; no customer machine does"}`, `"not customer work"`)),
	} {
		if _, err := capability.Decode([]byte(broken)); err == nil {
			t.Errorf("v2 %s was accepted", name)
		}
	}
}

// The repository a checked row resolves against. Its shapes are the ones the
// real tree has: a domain function and a method, a platform-specific file, a
// frontend test file, and a Go test beside a helper that is not a test.
var repository = fstest.MapFS{
	"internal/testrunner/run.go":                    {Data: []byte("package testrunner\n\nconst spec, result = \"readmit-test/v1\", `readmit-result/v1`\n\nfunc Run() error { return nil }\n")},
	"internal/testrunner/doc.go":                    {Data: []byte("// Package testrunner writes \"readmit-comment/v1\" only in this comment.\npackage testrunner\n")},
	"internal/testrunner/spec_test.go":              {Data: []byte("package testrunner\n\nconst fixture = \"readmit-fixture/v1\"\n")},
	"internal/bundle/case.go":                       {Data: []byte("package bundle\n\nconst Schema = \"readmit-case/v3\"\n")},
	"internal/entitlement/v2.go":                    {Data: []byte("package entitlement\n\nconst SchemaV2 = \"readmit-entitlement/v2\"\n")},
	"desktop/frontend/node_modules/x/x.go":          {Data: []byte("package x\n\nconst y = \"readmit-vendored/v1\"\n")},
	"internal/testrunner/run_test.go":               {Data: []byte("package testrunner\n\nfunc Helper() {}\n")},
	"internal/replay/plan_unix.go":                  {Data: []byte("//go:build unix\n\npackage replay\n\ntype Plan struct{}\n\nfunc (p *Plan) Send() error { return nil }\n")},
	"internal/replay/plan_test.go":                  {Data: []byte("package replay\n\nimport \"testing\"\n\nfunc TestPlanSends(t *testing.T) {}\n")},
	"internal/commercial/issue.go":                  {Data: []byte("package commercial\n\ntype Account struct{}\n\nfunc (a Account) Issue() {}\n")},
	"internal/desktop/run_test.go":                  {Data: []byte("package desktop_test\n\nimport \"testing\"\n\nfunc TestRunMatchesTheCommandLine(t *testing.T) {}\n\nfunc helperTest(t *testing.T) {}\n")},
	"internal/desktop/app.go":                       {Data: []byte("package desktop\n\nfunc TestLooksLikeATestButIsNot() {}\n")},
	"desktop/frontend/src/runs.test.tsx":            {Data: []byte("import { test } from \"vitest\";\n\ntest(\"runs a saved test\", async () => {});\nit(\n  \"reports a refused run\",\n  () => {},\n);\nconst title = \"computed\";\ntest(title, () => {});\n")},
	"desktop/frontend/src/RunPanel.tsx":             {Data: []byte("export function RunPanel() { return null; }\n")},
	"desktop/frontend/src/journeys/run.journey.tsx": {Data: []byte("test(\"a saved test runs against the real facade\", async () => {});\n")},
	"desktop/frontend/src/empty.test.tsx":           {Data: []byte("// no tests yet\n")},
	"internal/broken/broken.go":                     {Data: []byte("package broken\n\nfunc (\n")},
	"internal/testsonly/only_test.go":               {Data: []byte("package testsonly\n\nfunc Nothing() {}\n")},
	"internal/testrunner/testdata/skip.txt":         {Data: []byte("not go\n")},
}

func TestCheckResolvesEveryReferenceKind(t *testing.T) {
	ledger, err := capability.Decode([]byte(v2(implementedRow, openRow, disposedRow)))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Check(repository); err != nil {
		t.Fatalf("a ledger whose references all resolve was refused:\n%v", err)
	}
	journey, err := capability.Decode([]byte(v2(strings.NewReplacer("src/runs.test.tsx", "src/journeys/run.journey.tsx",
		"runs a saved test", "a saved test runs against the real facade").Replace(implementedRow))))
	if err != nil {
		t.Fatal(err)
	}
	if err := journey.Check(repository); err != nil {
		t.Fatalf("a journey named as the interaction test did not resolve:\n%v", err)
	}
	multiLine, err := capability.Decode([]byte(v2(strings.Replace(implementedRow, "runs a saved test", "reports a refused run", 1))))
	if err != nil {
		t.Fatal(err)
	}
	if err := multiLine.Check(repository); err != nil {
		t.Fatalf("an it(...) title on its own line did not resolve:\n%v", err)
	}
}

// Each unresolved reference is reported by row and by what is missing, and
// every one of them is reported, not only the first.
func TestCheckRefusesReferencesTheTreeDoesNotHold(t *testing.T) {
	for name, tc := range map[string]struct{ row, old, new, want string }{
		"backend function missing": {openRow, `"name":"Plan.Send"`, `"name":"Plan.Receive"`, "Plan.Receive is not declared"},
		"backend method as a func": {openRow, `"name":"Plan.Send"`, `"name":"Send"`, "Send is not declared"},
		"backend package missing":  {openRow, `internal/replay`, `internal/nowhere`, "not a directory"},
		"backend only in a test":   {implementedRow, `"package":"internal/testrunner","name":"Run"`, `"package":"internal/testrunner","name":"Helper"`, "Helper is not declared"},
		"backend package unparsed": {openRow, `"package":"internal/replay","name":"Plan.Send"`, `"package":"internal/broken","name":"X"`, "cannot parse"},
		"backend package tests only": {openRow, `"package":"internal/replay","name":"Plan.Send"`,
			`"package":"internal/testsonly","name":"Nothing"`, "holds no Go files"},
		"input no source spells":  {openRow, `"readmit-case/v3"`, `"readmit-case/v9"`, "readmit-case/v9 is not a contract"},
		"output only in a test":   {implementedRow, `"readmit-result/v1","hl7"`, `"readmit-fixture/v1"`, "readmit-fixture/v1 is not a contract"},
		"output only in comment":  {implementedRow, `"readmit-result/v1","hl7"`, `"readmit-comment/v1"`, "readmit-comment/v1 is not a contract"},
		"output only vendored":    {implementedRow, `"readmit-result/v1","hl7"`, `"readmit-vendored/v1"`, "readmit-vendored/v1 is not a contract"},
		"gui title missing":       {implementedRow, `runs a saved test`, `runs an unsaved test`, `holds no test titled "runs an unsaved test"`},
		"gui title computed":      {implementedRow, `runs a saved test`, `computed`, `holds no test titled "computed"`},
		"gui title a prefix":      {implementedRow, `runs a saved test`, `runs a saved`, `holds no test titled "runs a saved"`},
		"gui file missing":        {implementedRow, `runs.test.tsx`, `gone.test.tsx`, "not a file"},
		"gui file declares none":  {implementedRow, `runs.test.tsx`, `empty.test.tsx`, "declares no test"},
		"parity test missing":     {implementedRow, `TestRunMatchesTheCommandLine`, `TestRunDiffersFromTheCommandLine`, "is not declared"},
		"parity names a non-test": {implementedRow, `"package":"internal/desktop","name":"TestRunMatchesTheCommandLine"`, `"package":"internal/desktop","name":"TestLooksLikeATestButIsNot"`, "is not declared"},
		"parity package missing":  {implementedRow, `"package":"internal/desktop","name":"TestRun`, `"package":"internal/elsewhere","name":"TestRun`, "not a directory"},
	} {
		if !strings.Contains(tc.row, tc.old) {
			t.Fatalf("%s: fixture does not hold %q", name, tc.old)
		}
		ledger, err := capability.Decode([]byte(v2(strings.Replace(tc.row, tc.old, tc.new, 1))))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		err = ledger.Check(repository)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Check = %v, want an error naming %q", name, err, tc.want)
		}
	}

	both, err := capability.Decode([]byte(v2(
		strings.Replace(implementedRow, `TestRunMatchesTheCommandLine`, `TestGone`, 1),
		strings.Replace(openRow, `Plan.Send`, `Plan.Gone`, 1))))
	if err != nil {
		t.Fatal(err)
	}
	err = both.Check(repository)
	if err == nil || !strings.Contains(err.Error(), "row cli.test") || !strings.Contains(err.Error(), "row cli.replay") {
		t.Errorf("Check did not report every unresolved row: %v", err)
	}
}

func TestPortalSurfaceWalksEventsAndVendorOperations(t *testing.T) {
	tree := fstest.MapFS{
		"internal/billing/billing.go": {Data: []byte("package billing\n\ntype EventType string\n\nconst (\n" +
			"\tEventPaid EventType = \"payment.completed\"\n\tEventCancelled EventType = \"cancellation.recorded\"\n)\n\n" +
			"const Schema = \"readmit-billing-event/v1\"\n\nfunc (a Account) Apply() {}\n\ntype Account struct{}\n\n" +
			"func SignEvent() {}\nfunc VerifyEvent() {}\n")},
		"internal/billing/billing_test.go": {Data: []byte("package billing\n\nconst EventTest EventType = \"test.only\"\n\nfunc SignFixture() {}\n")},
		"internal/entitlement/entitlement.go": {Data: []byte("package entitlement\n\ntype Trust struct{}\n\n" +
			"func Sign() {}\nfunc SignV2() {}\nfunc Verify() {}\nfunc (t Trust) SigningKey() {}\nfunc signature() {}\n")},
		"internal/commercial/commercial.go": {Data: []byte("package commercial\n\ntype Account struct{}\ntype contact struct{}\n\n" +
			"func New() Account { return Account{} }\nfunc (a Account) Issue() {}\nfunc (a *Account) UnmarshalJSON([]byte) error { return nil }\n" +
			"func (c contact) Exported() {}\nfunc helper() {}\n")},
		"internal/trial/trial.go": {Data: []byte("package trial\n\nfunc Issue() {}\n")},
	}
	surface, err := capability.PortalSurface(tree)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"portal billing.SignEvent",
		"portal commercial.Account.Issue",
		"portal commercial.New",
		"portal entitlement.Sign",
		"portal entitlement.SignV2",
		"portal event cancellation.recorded",
		"portal event payment.completed",
		"portal trial.Issue",
	}
	if !slices.Equal(surface, want) {
		t.Fatalf("PortalSurface = %v, want %v", surface, want)
	}

	withoutEvents := fstest.MapFS{
		"internal/billing/billing.go":         {Data: []byte("package billing\n\nfunc Apply() {}\n")},
		"internal/commercial/commercial.go":   tree["internal/commercial/commercial.go"],
		"internal/trial/trial.go":             tree["internal/trial/trial.go"],
		"internal/entitlement/entitlement.go": tree["internal/entitlement/entitlement.go"],
	}
	if _, err := capability.PortalSurface(withoutEvents); err == nil {
		t.Error("a billing package declaring no events produced a portal surface")
	}
	computed := fstest.MapFS{
		"internal/billing/billing.go":         {Data: []byte("package billing\n\ntype EventType string\n\nconst prefix = \"payment.\"\n\nconst EventPaid EventType = prefix + \"completed\"\n")},
		"internal/commercial/commercial.go":   tree["internal/commercial/commercial.go"],
		"internal/trial/trial.go":             tree["internal/trial/trial.go"],
		"internal/entitlement/entitlement.go": tree["internal/entitlement/entitlement.go"],
	}
	if _, err := capability.PortalSurface(computed); err == nil || !strings.Contains(err.Error(), "string literals") {
		t.Errorf("a computed account event was walked instead of refused: %v", err)
	}
	withoutTrial := fstest.MapFS{
		"internal/billing/billing.go":       tree["internal/billing/billing.go"],
		"internal/commercial/commercial.go": tree["internal/commercial/commercial.go"],
	}
	if _, err := capability.PortalSurface(withoutTrial); err == nil {
		t.Error("a missing vendor issuer package produced a portal surface")
	}
	withoutSigners := fstest.MapFS{
		"internal/billing/billing.go":       tree["internal/billing/billing.go"],
		"internal/commercial/commercial.go": tree["internal/commercial/commercial.go"],
		"internal/trial/trial.go":           tree["internal/trial/trial.go"],
	}
	if _, err := capability.PortalSurface(withoutSigners); err == nil {
		t.Error("a missing entitlement signer package produced a portal surface")
	}
}

// The repository's tooling is walked from its Makefile targets and its tools
// scripts and must agree with the ledger's tooling rows in both directions,
// so a developer entry point is recorded as such rather than silently absent.
func TestToolingSurfaceAndTheLedgerAgree(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	surface, err := capability.ToolingSurface(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(surface, "make check") || !slices.Contains(surface, "tools/smoke.py") {
		t.Fatalf("the walked tooling is implausible; the walk is broken: %v", surface)
	}
	for _, source := range surface {
		if !ledger.Covered(capability.KindTooling, source) {
			t.Errorf("tooling %q has no capability ledger row; add one to docs/capability-ledger.json disposed as developer tooling", source)
		}
	}
	for _, row := range ledger.Rows {
		if row.Kind == capability.KindTooling && !slices.Contains(surface, row.Source) {
			t.Errorf("tooling row %q names %q, which the repository does not declare", row.ID, row.Source)
		}
	}
}

func TestToolingSurfaceWalksMakeTargetsAndToolsScripts(t *testing.T) {
	tree := fstest.MapFS{
		"Makefile": {Data: []byte(".DEFAULT_GOAL := check\n.PHONY: check test\n\nPKGS ?=\nARGS ?=\n\ncheck:\n\tgo vet ./...\n" +
			"test: check\n\tgo test ./...\n\ntest-focused:\n\t@test -n \"$(PKGS)\"\nFOO := bar\n")},
		"tools/smoke.py":          {Data: []byte("")},
		"tools/test_smoke.py":     {Data: []byte("")},
		"tools/distribution.json": {Data: []byte("{}")},
		"tools/testdata/x.py":     {Data: []byte("")},
	}
	surface, err := capability.ToolingSurface(tree)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"make check", "make test", "make test-focused", "tools/smoke.py"}
	if !slices.Equal(surface, want) {
		t.Fatalf("ToolingSurface = %v, want %v", surface, want)
	}
	if _, err := capability.ToolingSurface(fstest.MapFS{"tools/smoke.py": {Data: []byte("")}}); err == nil {
		t.Error("a tree without a Makefile produced a tooling surface")
	}
}
