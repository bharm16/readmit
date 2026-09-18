package desktop_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// bindingsFile is the frontend's only view of the Go facade. Wails publishes
// each bound method at window.go.desktop.App.<Method>, so a method added to the
// facade without a declaration here is a binding the frontend cannot call.
const bindingsFile = "../../desktop/frontend/src/bindings.ts"

func TestFrontendBindingsCoverTheFacade(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	bindings := string(declarations)
	if !strings.Contains(bindings, "window.go?.desktop?.App") {
		t.Fatalf("%s does not read the namespace Wails publishes for this facade", bindingsFile)
	}
	facade := reflect.TypeOf(&desktop.App{})
	if facade.NumMethod() == 0 {
		t.Fatal("the facade exposes no bound methods")
	}
	for i := range facade.NumMethod() {
		method := facade.Method(i).Name
		if !strings.Contains(bindings, method+"(") {
			t.Errorf("facade method %s has no typed declaration in %s", method, bindingsFile)
		}
	}
}

// The frontend receives typed objects, never rendered prose, so the six states
// the facade can report must all be expressible on that side of the boundary.
func TestFrontendBindingsDeclareEveryOperationState(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []desktop.State{desktop.Empty, desktop.Busy, desktop.Cancelled, desktop.Failed, desktop.PermissionDenied, desktop.Completed} {
		if !strings.Contains(string(declarations), `"`+string(state)+`"`) {
			t.Errorf("state %q has no declaration in %s", state, bindingsFile)
		}
	}
}

// The frontend reads these members by name. A Go member the declarations do not
// carry is a silently broken interface that still type-checks on both sides, so
// every member of every bound result type must be declared.
func TestFrontendBindingsDeclareEveryResultMember(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	bindings := string(declarations)
	for _, bound := range []reflect.Type{
		reflect.TypeOf(desktop.WorkspaceResult{}),
		reflect.TypeOf(desktop.Workspace{}),
		reflect.TypeOf(desktop.Artifact{}),
		reflect.TypeOf(desktop.CaseResult{}),
		reflect.TypeOf(desktop.Case{}),
		reflect.TypeOf(desktop.RecentResult{}),
	} {
		for i := range bound.NumField() {
			member, _, _ := strings.Cut(bound.Field(i).Tag.Get("json"), ",")
			if member == "" {
				t.Fatalf("%s.%s carries no JSON member name", bound.Name(), bound.Field(i).Name)
			}
			if !strings.Contains(bindings, member+":") && !strings.Contains(bindings, member+"?:") {
				t.Errorf("%s member %q has no typed declaration in %s", bound.Name(), member, bindingsFile)
			}
		}
	}
}
