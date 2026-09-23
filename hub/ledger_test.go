package hub_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/capability"
)

// The service's route inventory and the capability ledger must agree in both
// directions: every route the inventory declares is covered by a row (or by a
// recorded, reviewed disposition), and every hub row names a route the
// inventory still declares. A route added to the handlers without its
// inventory entry and ledger row fails here, which is what keeps #244's
// coverage ledger from falling behind the service.
func TestServiceRoutesAndTheLedgerAgree(t *testing.T) {
	data, err := os.ReadFile("../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range hub.Routes() {
		source := "hub " + route.Method + " " + route.Path
		if !ledger.Covered(capability.KindHub, source) {
			t.Errorf("route %s has no capability ledger row; add one to docs/capability-ledger.json naming its owner and backend, and its screen, action and checked tests or its disposition", source)
		}
	}
	for _, row := range ledger.Rows {
		// The hub binary's own commands are hub rows too; the check beside
		// that binary holds them to its command set.
		if row.Kind != capability.KindHub || strings.HasPrefix(row.Source, "readmit-hub ") {
			continue
		}
		known := slices.ContainsFunc(hub.Routes(), func(route hub.Route) bool {
			return "hub "+route.Method+" "+route.Path == row.Source
		})
		if !known {
			t.Errorf("hub row %q names %q, which the route inventory does not declare", row.ID, row.Source)
		}
	}
}
