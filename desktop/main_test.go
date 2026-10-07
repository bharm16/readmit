package main

import "testing"

// The installed shell answers for its build identity without a window, and only
// when it was asked to. Every other argument, including the process serial
// number macOS passes to a bundled application, still opens the window.
func TestOnlyAnExplicitVersionRequestReplacesTheWindow(t *testing.T) {
	for _, arguments := range [][]string{{"--version"}, {"-version"}, {"--version", "extra"}} {
		if !reportsVersion(arguments) {
			t.Errorf("%v did not ask for the build identity", arguments)
		}
	}
	for _, arguments := range [][]string{nil, {}, {"-psn_0_1234"}, {"--versions"}, {"version"}, {"--Version"}} {
		if reportsVersion(arguments) {
			t.Errorf("%v was treated as a request for the build identity", arguments)
		}
	}
}

func TestEmbeddedReferenceInventoryContainsOnlyDeclaredResources(t *testing.T) {
	entries, err := referenceAssets.ReadDir("reference-assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() != "README.md" && entry.Name() != "library.zip" {
			t.Fatalf("unexpected embedded resource: %s", entry.Name())
		}
	}
}
