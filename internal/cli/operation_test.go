package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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
		"readmit runner execute":      "",
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
