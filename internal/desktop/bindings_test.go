package desktop_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

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
// every member of every type the facade can cross the seam with must be
// declared. The set of types is computed from the bound methods' own
// signatures — parameters and results, and everything reachable from them —
// so a type added to the facade is checked without this test being told.
func TestFrontendBindingsDeclareEveryResultMember(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	bindings := string(declarations)
	for _, bound := range facadeTypes(t) {
		for i := range bound.NumField() {
			field := bound.Field(i)
			if !field.IsExported() {
				continue
			}
			member, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if member == "-" {
				continue
			}
			if member == "" {
				t.Fatalf("%s.%s carries no JSON member name", bound.Name(), field.Name)
			}
			if !strings.Contains(bindings, member+":") && !strings.Contains(bindings, member+"?:") {
				t.Errorf("%s member %q has no typed declaration in %s", bound.Name(), member, bindingsFile)
			}
		}
	}
}

// facadeTypes walks the facade's bound methods and reports every struct type
// their signatures can put on the seam. Members of the leaf kinds JSON
// marshals without structure — strings, numbers, times, byte slices —
// contribute nothing to check.
func facadeTypes(t *testing.T) []reflect.Type {
	t.Helper()
	facade := reflect.TypeOf(&desktop.App{})
	if facade.NumMethod() == 0 {
		t.Fatal("the facade exposes no bound methods")
	}
	var seed []reflect.Type
	for i := range facade.NumMethod() {
		method := facade.Method(i).Type
		for j := 1; j < method.NumIn(); j++ {
			seed = append(seed, method.In(j))
		}
		for j := range method.NumOut() {
			seed = append(seed, method.Out(j))
		}
	}
	var structs []reflect.Type
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(at reflect.Type) {
		for at.Kind() == reflect.Pointer {
			at = at.Elem()
		}
		if seen[at] {
			return
		}
		seen[at] = true
		switch at.Kind() {
		case reflect.Struct:
			if at == reflect.TypeOf(time.Time{}) {
				return
			}
			structs = append(structs, at)
			for i := range at.NumField() {
				walk(at.Field(i).Type)
			}
		case reflect.Slice, reflect.Array:
			walk(at.Elem())
		case reflect.Map:
			walk(at.Key())
			walk(at.Elem())
		}
	}
	for _, at := range seed {
		walk(at)
	}
	if len(structs) == 0 {
		t.Fatal("the facade's signatures reach no struct type")
	}
	return structs
}
