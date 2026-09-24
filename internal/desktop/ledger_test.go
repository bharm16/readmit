package desktop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
	"github.com/bharm16/readmit/internal/desktop"
)

// The shell binds two Go objects: the main evidence facade and the separate
// customer-hub administration reader. The latter lives in the desktop module
// so the released CLI never imports the hub module; inspect its declarations
// rather than importing that module back into the released root module.
func boundDesktopMethods(t *testing.T) map[string]bool {
	t.Helper()
	facade := reflect.TypeOf(&desktop.App{})
	bound := make(map[string]bool, facade.NumMethod()+2)
	for i := range facade.NumMethod() {
		bound["desktop.App."+facade.Method(i).Name] = true
	}
	shell, err := os.ReadFile("../../desktop/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shell), "new(hubadmin.Admin)") {
		t.Fatal("the shell no longer binds hubadmin.Admin")
	}
	packages, err := parser.ParseDir(token.NewFileSet(), "../../desktop/hubadmin", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	adminPackage, ok := packages["hubadmin"]
	if !ok {
		t.Fatal("the hub administration binding package is missing")
	}
	for _, file := range adminPackage.Files {
		for _, declaration := range file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || !method.Name.IsExported() {
				continue
			}
			for _, field := range method.Recv.List {
				receiver := field.Type
				if pointer, ok := receiver.(*ast.StarExpr); ok {
					receiver = pointer.X
				}
				if name, ok := receiver.(*ast.Ident); ok && name.Name == "Admin" {
					bound["hubadmin.Admin."+method.Name.Name] = true
				}
			}
		}
	}
	return bound
}

// The facade is the application's capability surface: every bound method is
// something the window can do. A method added without a ledger row fails
// here, so the coverage ledger cannot fall behind what the application
// actually offers (#244's completion rule).
func TestEveryBoundMethodHasALedgerRow(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	bound := boundDesktopMethods(t)
	if len(bound) == 0 {
		t.Fatal("the facade exposes no bound methods")
	}
	for source := range bound {
		if !ledger.Covered(capability.KindDesktop, source) {
			t.Errorf("bound method %s has no capability ledger row; add one to docs/capability-ledger.json naming its owner, backend, screen, action and checked tests", source)
		}
	}
}

// The reverse direction keeps the ledger honest: a row naming a method the
// facade no longer binds is stale coverage and fails the same check.
func TestEveryDesktopLedgerRowNamesABoundMethod(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	bound := boundDesktopMethods(t)
	for _, row := range ledger.Rows {
		if row.Kind != capability.KindDesktop {
			continue
		}
		if !bound[row.Source] {
			t.Errorf("desktop row %q names %q, which the facade no longer binds", row.ID, row.Source)
		}
	}
}
