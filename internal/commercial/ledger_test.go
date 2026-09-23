package commercial_test

import (
	"os"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
)

// The commercial portal's surface is walked from source — every account
// event the portal carries and every exported vendor operation behind it,
// this package's administration among them — and must agree with the
// ledger's portal rows in both directions, the way the command tree, the
// facade and the service routes agree with theirs. A vendor operation added
// here without a ledger row, or a row left behind by a removed one, fails.
func TestPortalSurfaceAndTheLedgerAgree(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	surface, err := capability.PortalSurface(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(surface, "portal event payment.completed") || !slices.Contains(surface, "portal commercial.Account.Issue") {
		t.Fatalf("the walked portal surface is implausible; the walk is broken: %v", surface)
	}
	for _, source := range surface {
		if !ledger.Covered(capability.KindPortal, source) {
			t.Errorf("portal operation %q has no capability ledger row; add one to docs/capability-ledger.json naming its owner, backend and screen, action and tests, or its disposition", source)
		}
	}
	for _, row := range ledger.Rows {
		if row.Kind == capability.KindPortal && !slices.Contains(surface, row.Source) {
			t.Errorf("portal row %q names %q, which the portal surface does not declare", row.ID, row.Source)
		}
	}
}
