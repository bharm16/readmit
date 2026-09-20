package desktop_test

import (
	"strings"
	"testing"
)

// This checks shell wiring only; rendered interaction is separately exercised
// with the existing frontend build. It does not claim native webview acceptance.
func TestContextHelpIsOfflineAndLinkedToEveryRegionAndStatus(t *testing.T) {
	help := read(t, frontendDirectory+"/ContextHelp.tsx")
	app := read(t, frontendDirectory+"/App.tsx")
	status := read(t, frontendDirectory+"/shell.tsx")
	if !strings.Contains(app, "<ContextHelp region={region.id}") || !strings.Contains(status, "<StateHelp state={state}") {
		t.Fatal("help must follow the displayed region and typed operation state")
	}
	for _, region := range shell(t).Regions {
		if !strings.Contains(help, string(region.ID)+":") {
			t.Errorf("missing help for %s", region.ID)
		}
	}
	for _, code := range []string{"RM-EMPTY", "RM-BUSY", "RM-CANCELLED", "RM-FAILED", "RM-PERMISSION", "RM-COMPLETED"} {
		if !strings.Contains(help, code) {
			t.Errorf("missing support category %s", code)
		}
	}
	for _, unsafe := range []string{"fetch(", "https://", "localStorage", "reason:", "reason?"} {
		if strings.Contains(help, unsafe) {
			t.Errorf("help must not collect context or transmit diagnostics: %s", unsafe)
		}
	}
}
