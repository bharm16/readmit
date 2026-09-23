package desktop_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
	"github.com/bharm16/readmit/internal/desktop"
)

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
	facade := reflect.TypeOf(&desktop.App{})
	if facade.NumMethod() == 0 {
		t.Fatal("the facade exposes no bound methods")
	}
	for i := range facade.NumMethod() {
		source := "desktop.App." + facade.Method(i).Name
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
	facade := reflect.TypeOf(&desktop.App{})
	bound := make(map[string]bool, facade.NumMethod())
	for i := range facade.NumMethod() {
		bound["desktop.App."+facade.Method(i).Name] = true
	}
	for _, row := range ledger.Rows {
		if row.Kind != capability.KindDesktop {
			continue
		}
		if !bound[row.Source] {
			t.Errorf("desktop row %q names %q, which the facade no longer binds", row.ID, row.Source)
		}
	}
}
