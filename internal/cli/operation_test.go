package cli

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/spf13/cobra"
)

// matchesTheUseLine reports whether a command's Args accepts and refuses
// exactly what the Use line's placeholder count implies at the three decisive
// counts: one below, the declared count, and one above.
func matchesTheUseLine(c *cobra.Command, count int) bool {
	if c.Args(c, make([]string, count)) != nil {
		return false
	}
	if count > 0 && c.Args(c, make([]string, count-1)) == nil {
		return false
	}
	return c.Args(c, make([]string, count+1)) != nil
}

func TestEveryRunnableCommandDeclaresItsArgsShape(t *testing.T) {
	root, _ := rootCommand("test")
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		if c.RunE != nil {
			if c.Args == nil {
				t.Errorf("%s is runnable but declares no argument shape", c.CommandPath())
			} else if count := placeholderCount(c.Use); matchesTheUseLine(c, count) {
				// The declared shape and the Use line agree, so a shape that
				// loosens around the declared count fails here rather than at
				// an operator's typo.
				if count > 0 && c.Args(c, make([]string, count-1)) == nil {
					t.Errorf("%s accepted %d arguments; its Use line declares %d", c.CommandPath(), count-1, count)
				}
				if c.Args(c, make([]string, count+1)) == nil {
					t.Errorf("%s accepted %d arguments; its Use line declares %d", c.CommandPath(), count+1, count)
				}
			}
		}
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func TestEveryCommandDeclaresAnOperationCapability(t *testing.T) {
	root, _ := rootCommand("test")
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		_, declared := c.Annotations[capabilityAnnotation]
		if c.RunE != nil && operationCapability(c) == "unsupported-operation" {
			t.Errorf("%s declares no operation capability", c.CommandPath())
		}
		if c.RunE == nil && declared {
			t.Errorf("%s is not runnable; its capability declaration is dead", c.CommandPath())
		}
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func TestDeclaredCapabilitiesMatchTheAdmissionVocabulary(t *testing.T) {
	root, _ := rootCommand("test")
	cases := map[string]string{
		"readmit capture":             "author",
		"readmit synth":               "author",
		"readmit baseline approve":    "author",
		"readmit expectation release": "author",
		"readmit collect":             "execute",
		"readmit listen":              "execute",
		"readmit run start":           "execute",
		"readmit suite ci":            "execute",
		"readmit target reset":        "execute",
		"readmit redact reexecute":    "execute",
		"readmit inspect":             "",
		"readmit index show":          "",
		"readmit runner execute":      "execute",
		"readmit runner serve":        "execute",
		"readmit runner enroll":       "",
		"readmit license verify":      "",
		"readmit report export":       "",
	}
	for path, capability := range cases {
		cmd, _, err := root.Find(strings.Fields(path[len("readmit "):]))
		if err != nil || cmd == nil || cmd.CommandPath() != path {
			t.Errorf("%s not found in the command tree", path)
			continue
		}
		if got := operationCapability(cmd); got != capability {
			t.Errorf("%s capability = %q, want %q", path, got, capability)
		}
	}
}

// Every command's admission is the profile its annotations declare; the
// construction pass tells no command apart by its path. A suite held to one
// instance for the whole invocation, a runner that admits each job as its own
// execution, and a CI summary withheld until the execution settles are each
// declared where the command is built.
func TestEveryCommandsProfileIsDeclaredOnTheCommand(t *testing.T) {
	root, _ := rootCommand("test")
	cases := map[string]operationguard.Profile{
		"readmit inspect":        {Name: "readmit inspect"},
		"readmit capture":        {Name: "readmit capture", Author: true},
		"readmit target check":   {Name: "readmit target check", Interruptible: true, Execution: operationguard.Execute},
		"readmit suite run":      {Name: "readmit suite run", Interruptible: true, Execution: operationguard.Execute},
		"readmit suite ci":       {Name: "readmit suite ci", Interruptible: true, Execution: operationguard.Execute},
		"readmit runner execute": {Name: "readmit runner execute", Interruptible: true, Execution: operationguard.ExecuteEachJob},
		"readmit runner serve":   {Name: "readmit runner serve", Interruptible: true, Execution: operationguard.ExecuteEachJob},
		"readmit runner status":  {Name: "readmit runner status", Interruptible: true},
	}
	for path, want := range cases {
		cmd, _, err := root.Find(strings.Fields(path[len("readmit "):]))
		if err != nil || cmd == nil || cmd.CommandPath() != path {
			t.Errorf("%s not found in the command tree", path)
			continue
		}
		if got, declared := commandProfile(cmd); !declared || got != want {
			t.Errorf("%s declares %+v (declared %v), want %+v", path, got, declared, want)
		}
		if summary := cmd.Annotations[ciSummaryAnnotation] == "true"; summary != (path == "readmit suite ci") {
			t.Errorf("%s declares a CI summary %v", path, summary)
		}
	}
}

func TestConditionalCapabilitiesFollowTheirFlags(t *testing.T) {
	root, _ := rootCommand("test")
	test, _, err := root.Find([]string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := operationCapability(test); got != "" {
		t.Fatal("test without --send admitted:", got)
	}
	if err = test.Flags().Set("send", "true"); err != nil {
		t.Fatal(err)
	}
	if got := operationCapability(test); got != "execute" {
		t.Fatal("test --send not admitted as execute:", got)
	}

	quota, _, err := root.Find([]string{"project", "quota"})
	if err != nil {
		t.Fatal(err)
	}
	if got := operationCapability(quota); got != "" {
		t.Fatal("project quota without changes admitted:", got)
	}
	if err = quota.Flags().Set("max-bytes", "1024"); err != nil {
		t.Fatal(err)
	}
	if got := operationCapability(quota); got != "author" {
		t.Fatal("project quota --max-bytes not admitted as author:", got)
	}
}
