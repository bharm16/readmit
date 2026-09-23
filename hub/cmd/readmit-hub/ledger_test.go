package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
)

// The hub binary's operations are the words run compares flag.Arg(0) with,
// read from its own source, so an operation added to the dispatch without a
// capability ledger row fails here, and so does a row naming an operation the
// binary no longer dispatches. Its HTTP routes are checked beside the route
// inventory; these are the operator's commands on the hub host.
func TestHubCommandsAndTheLedgerAgree(t *testing.T) {
	data, err := os.ReadFile("../../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var commands []string
	operation := func(expression ast.Expr) bool {
		call, ok := expression.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return false
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Arg" {
			return false
		}
		pkg, ok := selector.X.(*ast.Ident)
		index, isLiteral := call.Args[0].(*ast.BasicLit)
		return ok && pkg.Name == "flag" && isLiteral && index.Value == "0"
	}
	word := func(expression ast.Expr) {
		literal, ok := expression.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatalf("an operation is dispatched on a computed word; the ledger check cannot read it")
		}
		name, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		commands = append(commands, "readmit-hub "+name)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BinaryExpr:
			if typed.Op == token.EQL && operation(typed.X) {
				word(typed.Y)
			}
		case *ast.SwitchStmt:
			if typed.Tag != nil && operation(typed.Tag) {
				for _, clause := range typed.Body.List {
					for _, value := range clause.(*ast.CaseClause).List {
						word(value)
					}
				}
			}
		}
		return true
	})
	if len(commands) < 5 {
		t.Fatalf("the walked dispatch is implausibly small; the check is broken: %v", commands)
	}
	for _, command := range commands {
		if !ledger.Covered(capability.KindHub, command) {
			t.Errorf("hub operation %q has no capability ledger row; add one to docs/capability-ledger.json naming its owner and backend, and its screen, action and checked tests or its disposition", command)
		}
	}
	for _, row := range ledger.Rows {
		if row.Kind == capability.KindHub && strings.HasPrefix(row.Source, "readmit-hub ") && !slices.Contains(commands, row.Source) {
			t.Errorf("hub row %q names %q, which the binary does not dispatch", row.ID, row.Source)
		}
	}
}
