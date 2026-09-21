package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
	"github.com/spf13/cobra"
)

// The command tree is walked from the root the executable itself builds, so a
// command added to the CLI without a ledger row fails here instead of
// shipping as customer capability nobody owns. This is the check that keeps
// #244's coverage ledger from quietly falling behind the product.
func TestEveryRunnableCommandHasALedgerRow(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := rootCommand("test")

	var walk func(cmd *cobra.Command, path string)
	var runnable []string
	walk = func(cmd *cobra.Command, path string) {
		for _, sub := range cmd.Commands() {
			// The help topic is cobra's own plumbing, not a capability.
			if sub.Name() == "help" || sub.Hidden {
				continue
			}
			joined := path + " " + sub.Name()
			if len(sub.Commands()) == 0 {
				runnable = append(runnable, joined)
			}
			walk(sub, joined)
		}
	}
	walk(root, "readmit")
	if len(runnable) < 10 {
		t.Fatalf("the walked tree is implausibly small; the check is broken: %d commands", len(runnable))
	}

	// Commands with subcommands print usage; the runnable capability is the
	// leaf, so only leaves are owed a row.
	for _, command := range runnable {
		if !ledger.Covered(capability.KindCLI, command) {
			t.Errorf("command %q has no capability ledger row; add one to docs/capability-ledger.json naming its owner, screen, action and test", command)
		}
	}
}

// A row may not outlive the capability it names, either: a renamed or removed
// command leaves a stale row behind, and this direction catches it.
func TestEveryCLILedgerRowNamesARealCommand(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := rootCommand("test")
	var paths []string
	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		for _, sub := range cmd.Commands() {
			if sub.Name() == "help" || sub.Hidden {
				continue
			}
			paths = append(paths, path+" "+sub.Name())
			walk(sub, path+" "+sub.Name())
		}
	}
	walk(root, "readmit")
	for _, row := range ledger.Rows {
		if row.Kind != capability.KindCLI {
			continue
		}
		if !slices.ContainsFunc(paths, func(p string) bool { return strings.TrimSpace(p) == row.Source }) {
			t.Errorf("cli row %q names %q, which the command tree does not hold", row.ID, row.Source)
		}
	}
}
