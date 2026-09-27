package desktop_test

import (
	"strings"
	"testing"
)

// This checks shell wiring only; rendered interaction is separately exercised
// with the existing frontend build. It does not claim native webview acceptance.
func TestContextHelpIsOfflineAndCoversEveryRegion(t *testing.T) {
	help := read(t, frontendDirectory+"/ContextHelp.tsx")
	app := read(t, frontendDirectory+"/App.tsx")
	status := read(t, frontendDirectory+"/shell.tsx")
	if !strings.Contains(app, "<HelpTopics") {
		t.Fatal("help must cover every region of the window")
	}
	// A status says its state and reason; it carries no generic help code.
	if strings.Contains(status, "StateHelp") || strings.Contains(help, "RM-") {
		t.Fatal("a status must not append a generic help disclosure")
	}
	for _, region := range shell(t).Regions {
		if !strings.Contains(help, string(region.ID)+":") {
			t.Errorf("missing help for %s", region.ID)
		}
	}
	for _, unsafe := range []string{"fetch(", "https://", "localStorage", "reason:", "reason?"} {
		if strings.Contains(help, unsafe) {
			t.Errorf("help must not collect context or transmit diagnostics: %s", unsafe)
		}
	}
}
